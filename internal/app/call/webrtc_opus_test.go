package call

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	imsvoice "github.com/damonto/ims-go/ims/voice"
	"github.com/damonto/ims-go/ims/voice/codec/pcmu"
	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media/samplebuilder"

	"github.com/damonto/sigmo/internal/pkg/voicecodec"
)

func TestOpusReaderReordersWraparoundAndConcealsLoss(t *testing.T) {
	encoder, err := voicecodec.NewOpusEncoder(voicecodec.OpusEncoderConfig{})
	if err != nil {
		t.Fatal(err)
	}
	decoder, err := voicecodec.NewOpusDecoder()
	if err != nil {
		t.Fatal(err)
	}
	reader := &webRTCAudioReader{decoder: decoder,
		buffer: samplebuilder.New(3, &codecs.OpusPacket{}, voicecodec.OpusClockRate)}
	packets := make([]*rtp.Packet, 8)
	for i := range packets {
		payload, err := encoder.Encode(make([]int16, voicecodec.OpusSamplesPerFrame))
		if err != nil {
			t.Fatal(err)
		}
		packets[i] = &rtp.Packet{Header: rtp.Header{SequenceNumber: uint16(65534 + i),
			Timestamp: uint32(i * 960)}, Payload: payload}
	}
	// Reorder at sequence wraparound and lose the packet at timestamp 2880.
	for _, i := range []int{0, 2, 1, 4, 5, 6, 7} {
		reader.buffer.Push(packets[i])
	}
	for i, want := range []int{320, 320, 320, 640} {
		pcm, rate, err := reader.read()
		if err != nil || rate != 16000 || len(pcm) != want {
			t.Fatalf("read %d: samples=%d rate=%d error=%v", i, len(pcm), rate, err)
		}
	}
}

func TestOpusDownlinkPreservesClockAndDropsPartialAudioAtGaps(t *testing.T) {
	encoder, err := voicecodec.NewOpusEncoder(voicecodec.OpusEncoderConfig{})
	if err != nil {
		t.Fatal(err)
	}
	track, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{
		MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2}, "audio", "fixture")
	if err != nil {
		t.Fatal(err)
	}
	writer := newOpusDownlinkWriter(track, encoder)
	writer.timestampBase = 1000
	// Two 10 ms IMS packets form one 20 ms Opus packet.
	if err := writer.write(make([]int16, 160), 16000, 0xffffff00, 16000); err != nil {
		t.Fatal(err)
	}
	if err := writer.write(make([]int16, 160), 16000, 0xffffffa0, 16000); err != nil {
		t.Fatal(err)
	}
	if writer.timestamp != 1960 || len(writer.buffer) != 0 {
		t.Fatalf("first packet timestamp=%d buffered=%d", writer.timestamp, len(writer.buffer))
	}
	if err := writer.write(make([]int16, 160), 16000, 0x40, 16000); err != nil {
		t.Fatal(err)
	}
	// Skip 10 ms across the source clock wrap; never concatenate the old half frame.
	if err := writer.write(make([]int16, 320), 16000, 0x180, 16000); err != nil {
		t.Fatal(err)
	}
	if writer.timestamp != 3880 || len(writer.buffer) != 0 {
		t.Fatalf("gap timestamp=%d buffered=%d", writer.timestamp, len(writer.buffer))
	}
	before := writer.sequenceNumber
	if err := writer.write(make([]int16, 320), 16000, 0x180, 16000); err != nil {
		t.Fatal(err)
	}
	if writer.sequenceNumber != before {
		t.Fatal("duplicate RTP advanced the encoder")
	}
}

type loopbackCallMedia struct {
	incoming chan []byte
	outgoing chan []byte
	format   imsvoice.NegotiatedMedia
}

func (m *loopbackCallMedia) Media() imsvoice.NegotiatedMedia {
	if m.format.Negotiated {
		return m.format
	}
	return negotiatedMedia(imsvoice.CodecPCMU, 8000)
}

func (m *loopbackCallMedia) ReadPacket(ctx context.Context) ([]byte, error) {
	select {
	case packet := <-m.incoming:
		return packet, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (m *loopbackCallMedia) WritePacket(ctx context.Context, packet []byte) error {
	select {
	case m.outgoing <- packet:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestWebRTCAudioNegotiationAndBidirectionalMedia(t *testing.T) {
	pcmuCodec := webrtc.RTPCodecParameters{RTPCodecCapability: webrtc.RTPCodecCapability{
		MimeType: webrtc.MimeTypePCMU, ClockRate: 8000, Channels: 1}, PayloadType: 0}
	opusCodec := webrtc.RTPCodecParameters{RTPCodecCapability: webrtc.RTPCodecCapability{
		MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2, SDPFmtpLine: "minptime=10;useinbandfec=1;stereo=1;maxaveragebitrate=24000"}, PayloadType: 111}
	dynamicPCMU := pcmuCodec
	dynamicPCMU.PayloadType = 96
	dynamicOpus := opusCodec
	dynamicOpus.PayloadType = 109
	opusFirst := []webrtc.RTPCodecParameters{opusCodec, pcmuCodec}
	pcmuFirst := []webrtc.RTPCodecParameters{pcmuCodec, opusCodec}
	engine, err := voicecodec.NewEngine(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(context.Background())
	for _, tc := range []struct {
		name        string
		codecs      []webrtc.RTPCodecParameters
		wantCodec   webrtc.RTPCodecParameters
		receiveOnly bool
		carrier     imsvoice.AudioCodec
	}{
		{name: "client prefers opus", codecs: opusFirst, wantCodec: opusCodec},
		{name: "client prefers PCMU", codecs: pcmuFirst, wantCodec: pcmuCodec},
		{name: "opus only", codecs: []webrtc.RTPCodecParameters{opusCodec}, wantCodec: opusCodec},
		{name: "PCMU only", codecs: []webrtc.RTPCodecParameters{pcmuCodec}, wantCodec: pcmuCodec},
		{name: "dynamic PCMU", codecs: []webrtc.RTPCodecParameters{dynamicPCMU, opusCodec}, wantCodec: dynamicPCMU},
		{name: "dynamic opus", codecs: []webrtc.RTPCodecParameters{dynamicOpus, pcmuCodec}, wantCodec: dynamicOpus},
		{name: "AMR to opus", codecs: opusFirst, wantCodec: opusCodec, carrier: imsvoice.CodecAMR},
		{name: "AMR-WB to opus", codecs: opusFirst, wantCodec: opusCodec, carrier: imsvoice.CodecAMRWB},
		{name: "EVS to opus", codecs: opusFirst, wantCodec: opusCodec, carrier: imsvoice.CodecEVS},
		{name: "AMR-WB to PCMU", codecs: pcmuFirst, wantCodec: pcmuCodec, carrier: imsvoice.CodecAMRWB},
		{name: "EVS to PCMU", codecs: pcmuFirst, wantCodec: pcmuCodec, carrier: imsvoice.CodecEVS},
		{name: "receive only prefers opus", codecs: opusFirst, wantCodec: opusCodec, receiveOnly: true},
		{name: "receive only prefers PCMU", codecs: pcmuFirst, wantCodec: pcmuCodec, receiveOnly: true},
		{name: "receive only opus", codecs: []webrtc.RTPCodecParameters{opusCodec}, wantCodec: opusCodec, receiveOnly: true},
		{name: "receive only PCMU", codecs: []webrtc.RTPCodecParameters{pcmuCodec}, wantCodec: pcmuCodec, receiveOnly: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			selected := tc.wantCodec
			opus := strings.EqualFold(selected.MimeType, webrtc.MimeTypeOpus)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			media := &loopbackCallMedia{incoming: make(chan []byte, 20), outgoing: make(chan []byte, 20)}
			rate := 8000
			if tc.carrier == "" {
				tc.carrier = imsvoice.CodecPCMU
			}
			if tc.carrier == imsvoice.CodecAMRWB || tc.carrier == imsvoice.CodecEVS {
				rate = 16000
			}
			media.format = negotiatedMedia(tc.carrier, rate)
			carrierCodec, err := mediaBridgeCodec(media.format)
			if err != nil {
				t.Fatal(err)
			}
			bridge, err := newWebRTCBridge(ctx, media, carrierCodec, engine, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer bridge.close(ctx)
			peer, err := webrtc.NewPeerConnection(webrtc.Configuration{})
			if err != nil {
				t.Fatal(err)
			}
			defer peer.Close()
			downlink := make(chan *rtp.Packet, 1)
			peer.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
				packet, _, err := track.ReadRTP()
				if err == nil {
					select {
					case downlink <- packet:
					case <-ctx.Done():
					}
				}
			})
			var local *webrtc.TrackLocalStaticRTP
			if tc.receiveOnly {
				_, err = peer.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio,
					webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly})
			} else {
				local, err = webrtc.NewTrackLocalStaticRTP(selected.RTPCodecCapability, "microphone", "fixture")
				if err == nil {
					_, err = peer.AddTrack(local)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, transceiver := range peer.GetTransceivers() {
				if err := transceiver.SetCodecPreferences(tc.codecs); err != nil {
					t.Fatal(err)
				}
			}
			gathered := webrtc.GatheringCompletePromise(peer)
			offer, err := peer.CreateOffer(nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := peer.SetLocalDescription(offer); err != nil {
				t.Fatal(err)
			}
			select {
			case <-gathered:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			gathered = webrtc.GatheringCompletePromise(bridge.pc)
			answer, err := bridge.answer(ctx, WebRTCSessionDescription{Type: "offer", SDP: peer.LocalDescription().SDP})
			if err != nil {
				t.Fatal(err)
			}
			if opus && (!strings.Contains(answer.SDP, "useinbandfec=0") || strings.Contains(answer.SDP, "useinbandfec=1")) {
				t.Fatalf("answer must describe our FEC capability: %s", answer.SDP)
			}
			if strings.Contains(strings.ToLower(answer.SDP), "opus/48000/2") != opus {
				t.Fatalf("unexpected codec in answer: %s", answer.SDP)
			}
			select {
			case <-gathered:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if err := peer.SetRemoteDescription(*bridge.pc.LocalDescription()); err != nil {
				t.Fatal(err)
			}
			var amr *voicecodec.AMRTranscoder
			var evs *voicecodec.EVSTranscoder
			if carrierCodec.amr != "" {
				mode, err := amrEncoderMode(carrierCodec.amr, "")
				if err != nil {
					t.Fatal(err)
				}
				amr, err = voicecodec.NewAMRTranscoder(ctx, carrierCodec.amr, mode)
				if err != nil {
					t.Fatal(err)
				}
				defer amr.Close(ctx)
			}
			if carrierCodec.evs {
				cfg, err := evsEncoderConfig(media.format.AudioFormat)
				if err != nil {
					t.Fatal(err)
				}
				evs, err = engine.NewEVSTranscoder(ctx, cfg)
				if err != nil {
					t.Fatal(err)
				}
				defer evs.Close(ctx)
			}
			for frame := range 10 {
				pcm := make([]int16, rate/50)
				for i := range pcm {
					pcm[i] = int16(8000 * math.Sin(2*math.Pi*440*float64(frame*len(pcm)+i)/float64(rate)))
				}
				payload := pcmu.AppendEncode(nil, pcm)
				if amr != nil {
					frames, err := amr.Encode(ctx, pcm)
					if err != nil {
						t.Fatal(err)
					}
					payload, err = (voicecodec.AMRPayload{Codec: carrierCodec.amr, Frames: frames}).MarshalBinary()
					if err != nil {
						t.Fatal(err)
					}
				}
				if evs != nil {
					encoded, err := evs.Encode(ctx, pcm)
					if err != nil {
						t.Fatal(err)
					}
					payload, err = encodeEVSPayload(encoded, false)
					if err != nil {
						t.Fatal(err)
					}
				}
				packet := rtp.Packet{Header: rtp.Header{Version: 2, SequenceNumber: uint16(frame), Timestamp: uint32(frame * len(pcm)), SSRC: 1}, Payload: payload}
				data, err := packet.Marshal()
				if err != nil {
					t.Fatal(err)
				}
				media.incoming <- data
			}
			select {
			case packet := <-downlink:
				if packet.PayloadType != uint8(selected.PayloadType) {
					t.Fatalf("downlink payload type=%d, want %d", packet.PayloadType, selected.PayloadType)
				}
				if opus {
					decoder, err := voicecodec.NewOpusDecoder()
					if err != nil {
						t.Fatal(err)
					}
					decoded, err := decoder.Decode(packet.Payload)
					if err != nil || len(decoded) != 320 {
						t.Fatalf("Opus downlink samples=%d error=%v", len(decoded), err)
					}
				} else if len(packet.Payload) != 160 {
					t.Fatalf("PCMU downlink samples=%d", len(packet.Payload))
				}
			case <-ctx.Done():
				t.Fatal("downlink: ", ctx.Err())
			}
			if tc.receiveOnly {
				return
			}
			var payload []byte
			if opus {
				encoder, err := voicecodec.NewOpusEncoder(voicecodec.OpusEncoderConfig{})
				if err != nil {
					t.Fatal(err)
				}
				payload, err = encoder.Encode(make([]int16, 320))
				if err != nil {
					t.Fatal(err)
				}
			} else {
				payload = pcmu.AppendEncode(nil, make([]int16, 160))
			}
			step := uint32(selected.ClockRate / 50)
			for frame := range 10 {
				if err := local.WriteRTP(&rtp.Packet{Header: rtp.Header{Version: 2, PayloadType: uint8(selected.PayloadType),
					SequenceNumber: uint16(frame), Timestamp: uint32(frame) * step, SSRC: 2}, Payload: payload}); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case data := <-media.outgoing:
				var packet rtp.Packet
				if err := packet.Unmarshal(data); err != nil {
					t.Fatal(err)
				}
				switch {
				case amr != nil:
					payload := voicecodec.AMRPayload{Codec: carrierCodec.amr}
					if err := payload.UnmarshalBinary(packet.Payload); err != nil {
						t.Fatal(err)
					}
					pcm, err := amr.Decode(ctx, payload.Frames[0])
					if err != nil || len(pcm) != rate/50 {
						t.Fatalf("AMR uplink samples=%d error=%v", len(pcm), err)
					}
				case evs != nil:
					frames, err := decodeEVSPayload(packet.Payload, false)
					if err != nil {
						t.Fatal(err)
					}
					pcm, err := evs.Decode(ctx, frames[0])
					if err != nil || len(pcm) != rate/50 {
						t.Fatalf("EVS uplink samples=%d error=%v", len(pcm), err)
					}
				default:
					if len(packet.Payload) != 160 {
						t.Fatalf("PCMU uplink samples=%d", len(packet.Payload))
					}
				}
			case <-ctx.Done():
				t.Fatal("uplink: ", ctx.Err())
			}
		})
	}
}
