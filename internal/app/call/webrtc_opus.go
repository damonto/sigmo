package call

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/damonto/ims-go/ims/voice/codec/pcmu"
	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media/samplebuilder"

	pims "github.com/damonto/sigmo/internal/app/ims"
	"github.com/damonto/sigmo/internal/pkg/voicecodec"
)

const opusMediaFMTP = "minptime=10;stereo=0;sprop-stereo=0;maxplaybackrate=16000;sprop-maxcapturerate=16000;useinbandfec=0;usedtx=0"

func opusReceiverConfig(fmtp string) voicecodec.OpusEncoderConfig {
	var config voicecodec.OpusEncoderConfig
	for param := range strings.SplitSeq(fmtp, ";") {
		key, value, ok := strings.Cut(strings.TrimSpace(param), "=")
		if !ok {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "maxaveragebitrate":
			if n >= 6000 && n <= 510000 {
				config.MaxBitrate = n
			}
		case "maxplaybackrate":
			if n >= 8000 && n <= 48000 {
				config.MaxPlaybackRate = n
			}
		}
	}
	return config
}

func (b *webRTCBridge) prepareAudioTrack() error {
	if b.track != nil {
		return errors.New("call audio is already negotiated")
	}
	for _, transceiver := range b.pc.GetTransceivers() {
		if transceiver.Kind() != webrtc.RTPCodecTypeAudio || transceiver.Receiver() == nil {
			continue
		}
		available := transceiver.Receiver().GetParameters().Codecs
		// Pion preserves the offer's order for these negotiated codecs. Honor
		// the client's preference among the formats the bridge supports.
		for _, codec := range available {
			opus := strings.EqualFold(codec.MimeType, webrtc.MimeTypeOpus)
			if !opus && !strings.EqualFold(codec.MimeType, webrtc.MimeTypePCMU) {
				continue
			}
			if opus {
				var err error
				b.opus, err = voicecodec.NewOpusEncoder(opusReceiverConfig(codec.SDPFmtpLine))
				if err != nil {
					return err
				}
				// Opus parameters describe each receiver independently. Do not
				// echo the offer's FEC, stereo or bandwidth preferences as ours.
				codec.SDPFmtpLine = opusMediaFMTP
			}
			if err := transceiver.SetCodecPreferences([]webrtc.RTPCodecParameters{codec}); err != nil {
				return fmt.Errorf("select call audio codec: %w", err)
			}
			track, err := webrtc.NewTrackLocalStaticRTP(codec.RTPCodecCapability, "audio", "sigmo-call")
			if err != nil {
				return fmt.Errorf("create call audio track: %w", err)
			}
			sender, err := b.pc.AddTrack(track)
			if err != nil {
				return fmt.Errorf("add call audio track: %w", err)
			}
			b.track = track
			go drainRTCP(sender)
			return nil
		}
	}
	return ErrUnsupportedCodec
}

// opusDownlinkWriter maps the IMS clock onto Opus's 48 kHz RTP clock. A gap
// discards partial PCM rather than joining speech from different time intervals.
type opusDownlinkWriter struct {
	track          *webrtc.TrackLocalStaticRTP
	encoder        *voicecodec.OpusEncoder
	buffer         []int16
	started        bool
	sourceBase     uint32
	sourceEnd      uint32
	timestampBase  uint32
	timestamp      uint32
	sequenceNumber uint16
	ssrc           uint32
}

func newOpusDownlinkWriter(track *webrtc.TrackLocalStaticRTP, encoder *voicecodec.OpusEncoder) *opusDownlinkWriter {
	return &opusDownlinkWriter{track: track, encoder: encoder,
		timestampBase: random32(), sequenceNumber: random16(), ssrc: random32()}
}

func (w *opusDownlinkWriter) write(pcm []int16, sampleRate int, sourceTimestamp uint32, clockRate int) error {
	if clockRate <= 0 || sampleRate <= 0 {
		return errors.New("invalid IMS audio clock rate")
	}
	if w.started && int32(sourceTimestamp-w.sourceEnd) < 0 {
		return nil // Late or duplicate RTP must not advance the stateful encoder.
	}
	if !w.started {
		w.sourceBase = sourceTimestamp
	}
	if !w.started || sourceTimestamp != w.sourceEnd {
		w.buffer = w.buffer[:0]
		w.timestamp = w.timestampBase + uint32(uint64(sourceTimestamp-w.sourceBase)*voicecodec.OpusClockRate/uint64(clockRate))
	}
	w.started = true
	w.sourceEnd = sourceTimestamp + uint32(len(pcm)*clockRate/sampleRate)
	converted, err := voicecodec.ResampleLinear(pcm, sampleRate, voicecodec.OpusSampleRate)
	if err != nil {
		return err
	}
	w.buffer = append(w.buffer, converted...)
	for len(w.buffer) >= voicecodec.OpusSamplesPerFrame {
		payload, err := w.encoder.Encode(w.buffer[:voicecodec.OpusSamplesPerFrame])
		if err != nil {
			return err
		}
		packet := rtp.Packet{Version: 2, PayloadType: opusPayloadType,
			SequenceNumber: w.sequenceNumber, Timestamp: w.timestamp, SSRC: w.ssrc, Payload: payload}
		if err := w.track.WriteRTP(&packet); err != nil {
			return err
		}
		w.buffer = w.buffer[voicecodec.OpusSamplesPerFrame:]
		w.sequenceNumber++
		w.timestamp += voicecodec.OpusClockRate / 50
	}
	return nil
}

type webRTCAudioReader struct {
	track    *webrtc.TrackRemote
	decoder  *voicecodec.OpusDecoder
	buffer   *samplebuilder.SampleBuilder
	started  bool
	nextTime uint32
	skipped  uint32 // Unconcealed time in the 48 kHz RTP clock.
}

func newWebRTCAudioReader(track *webrtc.TrackRemote) (*webRTCAudioReader, error) {
	r := &webRTCAudioReader{track: track}
	if strings.EqualFold(track.Codec().MimeType, webrtc.MimeTypeOpus) {
		decoder, err := voicecodec.NewOpusDecoder()
		if err != nil {
			return nil, err
		}
		r.decoder = decoder
		// At most three packets of reordering, plus the next timestamp needed by
		// SampleBuilder. There is no additional unbounded queue or reader goroutine.
		r.buffer = samplebuilder.New(3, &codecs.OpusPacket{}, voicecodec.OpusClockRate)
	}
	return r, nil
}

func (r *webRTCAudioReader) read() ([]int16, int, error) {
	r.skipped = 0
	for {
		if r.buffer != nil {
			if sample := r.buffer.Pop(); sample != nil {
				var pcm []int16
				gap := int32(sample.PacketTimestamp - r.nextTime)
				if r.started && gap < 0 {
					continue
				}
				if r.started && gap > 0 && gap <= voicecodec.OpusClockRate*120/1000 && gap%(voicecodec.OpusClockRate/50) == 0 {
					for gap >= voicecodec.OpusClockRate/50 {
						concealed, err := r.decoder.Conceal()
						if err != nil {
							return nil, 0, err
						}
						pcm = append(pcm, concealed...)
						gap -= voicecodec.OpusClockRate / 50
					}
				} else if r.started && gap > 0 {
					// Preserve long DTX gaps and short fractional-frame losses in
					// the IMS writer's clock instead of compressing the timeline.
					r.skipped = uint32(gap)
				}
				decoded, err := r.decoder.Decode(sample.Data)
				if err != nil {
					return nil, 0, err
				}
				r.nextTime = sample.PacketTimestamp + uint32(len(decoded)*voicecodec.OpusClockRate/voicecodec.OpusSampleRate)
				r.started = true
				return append(pcm, decoded...), voicecodec.OpusSampleRate, nil
			}
		}
		packet, _, err := r.track.ReadRTP()
		if err != nil {
			return nil, 0, err
		}
		if packet.PayloadType != uint8(r.track.PayloadType()) || len(packet.Payload) == 0 {
			continue
		}
		if r.buffer == nil {
			return pcmu.AppendDecode(nil, packet.Payload), pcmu.SampleRate, nil
		}
		r.buffer.Push(packet)
	}
}

func (b *webRTCBridge) runOpusPCMUDownlink(ctx context.Context) {
	writer := newOpusDownlinkWriter(b.track, b.opus)
	for {
		data, err := b.media.ReadPacket(ctx)
		if err != nil {
			b.stop()
			return
		}
		_, remote, ok := b.formats(bridgeCodec{pcmu: true})
		if !ok {
			b.stop()
			return
		}
		var packet rtp.Packet
		if err := packet.Unmarshal(data); err != nil || int(packet.PayloadType) != remote.PayloadType {
			continue
		}
		if err := writer.write(pcmu.AppendDecode(nil, packet.Payload), pcmu.SampleRate, packet.Timestamp, remote.ClockRate); err != nil {
			b.stop()
			return
		}
	}
}

func (b *webRTCBridge) runOpusPCMUUplink(ctx context.Context, track *webrtc.TrackRemote) {
	input, err := newWebRTCAudioReader(track)
	if err != nil {
		b.stop()
		return
	}
	sequenceNumber, timestamp, ssrc := random16(), random32(), random32()
	var buffer []int16
	for {
		pcm, rate, err := input.read()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				b.stop()
			}
			return
		}
		if input.skipped != 0 {
			timestamp += uint32(len(buffer)) + uint32(uint64(input.skipped)*pcmu.SampleRate/voicecodec.OpusClockRate)
			buffer = buffer[:0]
		}
		converted, err := voicecodec.ResampleLinear(pcm, rate, pcmu.SampleRate)
		if err != nil {
			b.stop()
			return
		}
		buffer = append(buffer, converted...)
		for len(buffer) >= pcmu.SampleRate/50 {
			local, _, ok := b.formats(bridgeCodec{pcmu: true})
			if !ok {
				b.stop()
				return
			}
			packet := rtp.Packet{Version: 2, PayloadType: uint8(local.PayloadType),
				SequenceNumber: sequenceNumber, Timestamp: timestamp, SSRC: ssrc, Payload: pcmu.AppendEncode(nil, buffer[:pcmu.SampleRate/50])}
			buffer = buffer[pcmu.SampleRate/50:]
			data, err := packet.Marshal()
			if err != nil {
				b.stop()
				return
			}
			if err := b.media.WritePacket(ctx, data); errors.Is(err, pims.ErrCallOnHold) {
				continue
			} else if err != nil {
				b.stop()
				return
			}
			sequenceNumber++
			timestamp += pcmu.SampleRate / 50
		}
	}
}
