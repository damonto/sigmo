//go:build ims

package voicecodec

import (
	"context"
	"testing"
)

func TestEVSTranscoderEncodeDecode(t *testing.T) {
	ctx := context.Background()
	engine, err := NewEngine(ctx)
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}
	t.Cleanup(func() {
		if err := engine.Close(ctx); err != nil {
			t.Errorf("Engine.Close() error = %v", err)
		}
	})

	tests := []struct {
		name      string
		samples   []int16
		wantType  int
		wantBytes int
	}{
		{name: "native 13.2", samples: evsTestPCM(EVSSamplesPerFrame), wantType: 4, wantBytes: 33},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transcoder, err := engine.NewEVSTranscoder(ctx)
			if err != nil {
				t.Fatalf("NewEVSTranscoder() error = %v", err)
			}
			t.Cleanup(func() {
				if err := transcoder.Close(ctx); err != nil {
					t.Errorf("Close() error = %v", err)
				}
			})

			frame, err := transcoder.Encode(ctx, tt.samples)
			if err != nil {
				t.Fatalf("Encode() error = %v", err)
			}
			if frame.FrameType != tt.wantType || !frame.Quality || frame.AMRWBIO || len(frame.Data) != tt.wantBytes {
				t.Fatalf("Encode() frame = %+v, want native type %d quality true bytes %d", frame, tt.wantType, tt.wantBytes)
			}
			pcm, err := transcoder.Decode(ctx, frame)
			if err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			if len(pcm) != EVSSamplesPerFrame {
				t.Fatalf("Decode() samples = %d, want %d", len(pcm), EVSSamplesPerFrame)
			}
		})
	}
}

func TestEVSTranscoderDecodesSpecialFrames(t *testing.T) {
	ctx := context.Background()
	engine, err := NewEngine(ctx)
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}
	t.Cleanup(func() {
		if err := engine.Close(ctx); err != nil {
			t.Errorf("Engine.Close() error = %v", err)
		}
	})

	tests := []struct {
		name  string
		frame EVSFrame
	}{
		{name: "native sid", frame: EVSFrame{FrameType: 12, Quality: true, Data: make([]byte, 6)}},
		{name: "amr wb io sid", frame: EVSFrame{FrameType: 9, Quality: true, AMRWBIO: true, Data: make([]byte, 5)}},
		{name: "native no data", frame: EVSFrame{FrameType: 15, Quality: true}},
		{name: "native speech lost", frame: EVSFrame{FrameType: 14}},
		{name: "amr wb io speech lost", frame: EVSFrame{FrameType: 14, AMRWBIO: true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transcoder, err := engine.NewEVSTranscoder(ctx)
			if err != nil {
				t.Fatalf("NewEVSTranscoder() error = %v", err)
			}
			t.Cleanup(func() {
				if err := transcoder.Close(ctx); err != nil {
					t.Errorf("Close() error = %v", err)
				}
			})

			pcm, err := transcoder.Decode(ctx, tt.frame)
			if err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			if len(pcm) != EVSSamplesPerFrame {
				t.Fatalf("Decode() samples = %d, want %d", len(pcm), EVSSamplesPerFrame)
			}
		})
	}
}

func evsTestPCM(samples int) []int16 {
	pcm := make([]int16, samples)
	for i := range pcm {
		pcm[i] = int16((i%127 - 63) * 80)
	}
	return pcm
}
