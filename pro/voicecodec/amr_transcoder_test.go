//go:build ims

package voicecodec

import (
	"context"
	"errors"
	"testing"
)

func TestAMRTranscoderEncodeDecode(t *testing.T) {
	tests := []struct {
		name         string
		codec        AMRCodec
		samples      int
		wantType     int
		wantBytes    int
		wantOutCount int
	}{
		{
			name:         "amr nb",
			codec:        CodecAMR,
			samples:      160,
			wantType:     7,
			wantBytes:    31,
			wantOutCount: 160,
		},
		{
			name:         "amr wb",
			codec:        CodecAMRWB,
			samples:      320,
			wantType:     8,
			wantBytes:    60,
			wantOutCount: 320,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			transcoder, err := NewAMRTranscoder(ctx, tt.codec)
			if err != nil {
				t.Fatalf("NewAMRTranscoder() error = %v", err)
			}
			t.Cleanup(func() {
				if err := transcoder.Close(ctx); err != nil {
					t.Errorf("Close() error = %v", err)
				}
			})

			frames, err := transcoder.Encode(ctx, amrTestPCM(tt.samples))
			if err != nil {
				t.Fatalf("Encode() error = %v", err)
			}
			if len(frames) != 1 {
				t.Fatalf("Encode() frames = %d, want 1", len(frames))
			}
			frame := frames[0]
			if frame.FrameType != tt.wantType || !frame.Quality || len(frame.Data) != tt.wantBytes {
				t.Fatalf("Encode() frame = type %d quality %v bytes %d, want type %d quality true bytes %d",
					frame.FrameType, frame.Quality, len(frame.Data), tt.wantType, tt.wantBytes)
			}

			pcm, err := transcoder.Decode(ctx, frame)
			if err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			if len(pcm) != tt.wantOutCount {
				t.Fatalf("Decode() samples = %d, want %d", len(pcm), tt.wantOutCount)
			}
		})
	}
}

func TestAMRTranscoderEncodeMultipleFrames(t *testing.T) {
	tests := []struct {
		name       string
		codec      AMRCodec
		samples    int
		wantFrames int
	}{
		{name: "amr nb", codec: CodecAMR, samples: 320, wantFrames: 2},
		{name: "amr wb", codec: CodecAMRWB, samples: 640, wantFrames: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			transcoder, err := NewAMRTranscoder(ctx, tt.codec)
			if err != nil {
				t.Fatalf("NewAMRTranscoder() error = %v", err)
			}

			t.Cleanup(func() {
				if err := transcoder.Close(ctx); err != nil {
					t.Errorf("Close() error = %v", err)
				}
			})

			frames, err := transcoder.Encode(ctx, amrTestPCM(tt.samples))
			if err != nil {
				t.Fatalf("Encode() error = %v", err)
			}
			if len(frames) != tt.wantFrames {
				t.Fatalf("Encode() frames = %d, want %d", len(frames), tt.wantFrames)
			}
		})
	}
}

func TestAMRTranscoderErrors(t *testing.T) {
	tests := []struct {
		name       string
		run        func(t *testing.T) error
		wantErr    error
		wantAnyErr bool
	}{
		{
			name: "unsupported codec",
			run: func(t *testing.T) error {
				t.Helper()
				_, err := NewAMRTranscoder(context.Background(), "EVS")
				return err
			},
			wantErr: ErrAMRCodecUnsupported,
		},
		{
			name: "partial frame",
			run: func(t *testing.T) error {
				t.Helper()
				ctx := context.Background()
				transcoder, err := NewAMRTranscoder(ctx, CodecAMR)
				if err != nil {
					t.Fatalf("NewAMRTranscoder() error = %v", err)
				}
				t.Cleanup(func() {
					if err := transcoder.Close(ctx); err != nil {
						t.Errorf("Close() error = %v", err)
					}
				})
				_, err = transcoder.Encode(ctx, make([]int16, AMRSamplesPerFrame(CodecAMR)-1))
				return err
			},
			wantAnyErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.run(t)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if tt.wantAnyErr && err == nil {
				t.Fatal("error = nil, want error")
			}
		})
	}
}

func amrTestPCM(samples int) []int16 {
	pcm := make([]int16, samples)
	for i := range pcm {
		pcm[i] = int16((i%113 - 56) * 96)
	}
	return pcm
}
