package voicecodec

import (
	"encoding/json"
	"math"
	"os"
	"strconv"
	"testing"

	"github.com/pion/opus"
)

func TestOpusDecodesLibopusReferencePackets(t *testing.T) {
	data, err := os.ReadFile("testdata/opus-reference.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name   string
		Frames []struct {
			Packet  []byte
			Samples int
			RMS     float64
		}
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			decoder, err := NewOpusDecoder()
			if err != nil {
				t.Fatal(err)
			}
			for i, frame := range tc.Frames {
				decoded, err := decoder.Decode(frame.Packet)
				if err != nil || len(decoded) != frame.Samples {
					t.Fatalf("frame %d: samples=%d error=%v", i, len(decoded), err)
				}
				var energy float64
				for _, sample := range decoded {
					energy += float64(sample) * float64(sample)
				}
				rms := math.Sqrt(energy / float64(len(decoded)))
				if math.Abs(rms-frame.RMS) > max(150, frame.RMS*0.1) {
					t.Fatalf("frame %d: RMS=%f, reference=%f", i, rms, frame.RMS)
				}
			}
		})
	}
}

func TestOpusPreservesWidebandAudioAndRecoversAfterLoss(t *testing.T) {
	encoder, err := NewOpusEncoder(OpusEncoderConfig{})
	if err != nil {
		t.Fatal(err)
	}
	decoder, err := NewOpusDecoder()
	if err != nil {
		t.Fatal(err)
	}
	var energy float64
	for frame := range 30 {
		pcm := make([]int16, OpusSamplesPerFrame)
		for i := range pcm {
			// A 5 kHz signal is lost by the previous 8 kHz PCMU bridge.
			pcm[i] = int16(8000 * math.Sin(2*math.Pi*5000*float64(frame*len(pcm)+i)/OpusSampleRate))
		}
		packet, err := encoder.Encode(pcm)
		if err != nil {
			t.Fatal(err)
		}
		if frame == 10 {
			concealed, err := decoder.Conceal()
			if err != nil || len(concealed) != OpusSamplesPerFrame {
				t.Fatalf("conceal: samples=%d, error=%v", len(concealed), err)
			}
			continue
		}
		decoded, err := decoder.Decode(packet)
		if err != nil || len(decoded) != OpusSamplesPerFrame {
			t.Fatalf("decode: samples=%d, error=%v", len(decoded), err)
		}
		if frame > 15 {
			for _, sample := range decoded {
				energy += float64(sample) * float64(sample)
			}
		}
	}
	if rms := math.Sqrt(energy / (14 * OpusSamplesPerFrame)); rms < 1000 {
		t.Fatalf("wideband audio RMS=%f, want audible 5 kHz audio after loss", rms)
	}
}

func TestOpusDecoderAcceptsVariableSILKPacketDurations(t *testing.T) {
	for _, frames := range []int{1, 2, 3} {
		t.Run(strconv.Itoa(frames*20)+"ms", func(t *testing.T) {
			encoder, err := opus.NewEncoder(opus.WithBitrate(24000))
			if err != nil {
				t.Fatal(err)
			}
			pcm := make([]int16, frames*OpusSamplesPerFrame)
			for i := range pcm {
				pcm[i] = int16(6000 * math.Sin(2*math.Pi*440*float64(i)/OpusSampleRate))
			}
			packet := make([]byte, 1275)
			n, err := encoder.EncodeSILK(pcm, opus.BandwidthWideband, packet)
			if err != nil {
				t.Fatal(err)
			}
			decoder, err := NewOpusDecoder()
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := decoder.Decode(packet[:n])
			if err != nil || len(decoded) != len(pcm) {
				t.Fatalf("decode %d ms: samples=%d, error=%v", frames*20, len(decoded), err)
			}
		})
	}
}

func TestOpusRejectsInvalidFrames(t *testing.T) {
	encoder, err := NewOpusEncoder(OpusEncoderConfig{})
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range []int{0, 160, 319, 321, 640} {
		if _, err := encoder.Encode(make([]int16, size)); err == nil {
			t.Fatalf("accepted %d input samples", size)
		}
	}
	decoder, err := NewOpusDecoder()
	if err != nil {
		t.Fatal(err)
	}
	for _, packet := range [][]byte{nil, make([]byte, 8193), {0xff}} {
		if _, err := decoder.Decode(packet); err == nil {
			t.Fatalf("accepted malformed packet of %d bytes", len(packet))
		}
	}
}

func BenchmarkOpusVoiceFrame(b *testing.B) {
	encoder, err := NewOpusEncoder(OpusEncoderConfig{})
	if err != nil {
		b.Fatal(err)
	}
	decoder, err := NewOpusDecoder()
	if err != nil {
		b.Fatal(err)
	}
	pcm := make([]int16, OpusSamplesPerFrame)
	for i := range pcm {
		pcm[i] = int16(8000 * math.Sin(2*math.Pi*440*float64(i)/OpusSampleRate))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		packet, err := encoder.Encode(pcm)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := decoder.Decode(packet); err != nil {
			b.Fatal(err)
		}
	}
}
