//go:build wifi_calling

package voicecodec

import (
	"context"
	"testing"
)

func TestAMRCodecFactoryOpen(t *testing.T) {
	tests := []struct {
		name    string
		open    func(context.Context) (*AMRCodecFactory, error)
		wantErr bool
	}{
		{
			name: "invalid wasm",
			open: func(ctx context.Context) (*AMRCodecFactory, error) {
				return NewAMRCodecFactory(ctx, []byte("not wasm"))
			},
			wantErr: true,
		},
		{
			name: "embedded default",
			open: NewDefaultAMRCodecFactory,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			factory, err := tt.open(context.Background())
			if tt.wantErr {
				if err == nil {
					_ = factory.Close(context.Background())
					t.Fatal("open factory error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("open factory error = %v", err)
			}
			if err := factory.Close(context.Background()); err != nil {
				t.Fatalf("Close() error = %v", err)
			}
		})
	}
}

func BenchmarkWASMAMRCodec(b *testing.B) {
	ctx := context.Background()
	factory, err := NewDefaultAMRCodecFactory(ctx)
	if err != nil {
		b.Fatalf("NewDefaultAMRCodecFactory() error = %v", err)
	}
	defer factory.Close(ctx)
	codec, err := factory.NewCodec(ctx, CodecAMR)
	if err != nil {
		b.Fatalf("NewCodec() error = %v", err)
	}
	defer codec.Close(ctx)
	samples := make([]int16, AMRSamplesPerFrame(CodecAMR))

	b.ResetTimer()
	for range b.N {
		frames, err := codec.Encode(ctx, samples)
		if err != nil {
			b.Fatalf("Encode() error = %v", err)
		}
		if len(frames) == 0 {
			b.Fatal("Encode() frames = 0")
		}
		if _, err := codec.Decode(ctx, frames[0]); err != nil {
			b.Fatalf("Decode() error = %v", err)
		}
	}
}
