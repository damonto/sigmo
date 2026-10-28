package voicecodec

import (
	"errors"
	"fmt"

	"github.com/pion/opus"
)

const (
	// OpusSampleRate is the mono PCM rate used by the IMS audio bridge.
	OpusSampleRate = 16000
	// OpusSamplesPerFrame is one 20 ms PCM frame.
	OpusSamplesPerFrame = OpusSampleRate / 50
	// OpusClockRate is the RTP clock rate, independent of the PCM rate.
	OpusClockRate = 48000
)

// OpusEncoder owns one outgoing stream. It accepts 20 ms of wideband mono PCM.
// Each direction has its own codec, so encoding never blocks the incoming stream.
type OpusEncoder struct {
	encoder *opus.Encoder
	pcm     [960]float32
	packet  [1275]byte
}

// OpusEncoderConfig contains the remote receiver's limits. Zero values use
// 32 kbit/s and 16 kHz wideband. The PCM input remains 16 kHz mono.
type OpusEncoderConfig struct {
	MaxBitrate      int
	MaxPlaybackRate int
}

// NewOpusEncoder creates a pure Go encoder capped at wideband voice audio.
func NewOpusEncoder(config OpusEncoderConfig) (*OpusEncoder, error) {
	bitrate := 32000
	if config.MaxBitrate >= 6000 {
		bitrate = min(bitrate, config.MaxBitrate)
	}
	bandwidth := opus.BandwidthWideband
	if config.MaxPlaybackRate > 0 && config.MaxPlaybackRate < OpusSampleRate {
		// CELT has no 12 kHz mode, so use narrowband for receivers capped below 16 kHz.
		bandwidth = opus.BandwidthNarrowband
	}
	encoder, err := opus.NewEncoder(opus.WithChannels(1), opus.WithBitrate(bitrate),
		opus.WithBandwidth(bandwidth), opus.WithVBR(true), opus.WithConstrainedVBR(true))
	if err != nil {
		return nil, fmt.Errorf("create Opus encoder: %w", err)
	}
	return &OpusEncoder{encoder: encoder}, nil
}

// Encode returns an owned Opus packet. The pinned encoder's CELT entry point
// requires 48 kHz PCM; interpolation preserves the existing 16 kHz voice band.
func (e *OpusEncoder) Encode(samples []int16) ([]byte, error) {
	if len(samples) != OpusSamplesPerFrame {
		return nil, errors.New("opus encoder requires one whole 20 ms PCM frame")
	}
	for i, sample := range samples {
		next := samples[min(i+1, len(samples)-1)]
		for phase := range 3 {
			e.pcm[i*3+phase] = (float32(sample)*float32(3-phase) + float32(next)*float32(phase)) / (3 * 32768)
		}
	}
	n, err := e.encoder.EncodeFloat32(e.pcm[:], e.packet[:])
	if err != nil {
		return nil, fmt.Errorf("encode Opus frame: %w", err)
	}
	return append([]byte(nil), e.packet[:n]...), nil
}

// OpusDecoder owns one incoming stream, including packet loss concealment state.
type OpusDecoder struct {
	decoder opus.Decoder
	pcm     [OpusSampleRate * 120 / 1000]int16
}

// NewOpusDecoder creates a decoder accepting SILK, hybrid and CELT packets.
func NewOpusDecoder() (*OpusDecoder, error) {
	decoder, err := opus.NewDecoderWithOutput(OpusSampleRate, 1)
	if err != nil {
		return nil, fmt.Errorf("create Opus decoder: %w", err)
	}
	return &OpusDecoder{decoder: decoder}, nil
}

// Decode returns owned PCM for one Opus packet, up to 120 ms.
func (d *OpusDecoder) Decode(packet []byte) ([]int16, error) {
	if len(packet) == 0 || len(packet) > 8192 {
		return nil, errors.New("invalid opus packet size")
	}
	n, err := d.decoder.DecodeToInt16(packet, d.pcm[:])
	if err != nil {
		return nil, fmt.Errorf("decode Opus packet: %w", err)
	}
	return append([]int16(nil), d.pcm[:n]...), nil
}

// Conceal returns 20 ms of replacement PCM for one missing voice frame.
func (d *OpusDecoder) Conceal() ([]int16, error) {
	pcm := d.pcm[:OpusSamplesPerFrame]
	if err := d.decoder.DecodePLC(pcm); err != nil {
		return nil, fmt.Errorf("conceal Opus frame: %w", err)
	}
	return append([]int16(nil), pcm...), nil
}
