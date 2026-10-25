package voicecodec

import (
	"errors"
	"testing"
)

func TestResampleLinear(t *testing.T) {
	tests := []struct {
		name     string
		input    []int16
		fromRate int
		toRate   int
		want     []int16
		wantErr  error
	}{
		{name: "copy same rate", input: []int16{1, 2, 3}, fromRate: 8000, toRate: 8000, want: []int16{1, 2, 3}},
		{name: "upsample double", input: []int16{0, 1000, 0}, fromRate: 8000, toRate: 16000, want: []int16{0, 500, 1000, 500, 0, 0}},
		{name: "downsample half", input: []int16{0, 500, 1000, 500, 0, -500}, fromRate: 16000, toRate: 8000, want: []int16{0, 1000, 0}},
		{name: "empty", input: []int16{}, fromRate: 8000, toRate: 16000, want: []int16{}},
		{name: "invalid rate", input: []int16{1}, fromRate: 0, toRate: 8000, wantErr: ErrInvalidSampleRate},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResampleLinear(tt.input, tt.fromRate, tt.toRate)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ResampleLinear() error = %v, want %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("ResampleLinear() length = %d, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("ResampleLinear()[%d] = %d, want %d", i, got[i], tt.want[i])
				}
			}
		})
	}
}
