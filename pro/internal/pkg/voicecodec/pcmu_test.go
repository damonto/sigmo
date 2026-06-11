//go:build wifi_calling

package voicecodec

import "testing"

func TestPCMUDecodeEncode(t *testing.T) {
	tests := []struct {
		name    string
		samples []int16
	}{
		{name: "silence", samples: []int16{0, 0, 0}},
		{name: "speech range", samples: []int16{-12000, -1000, 0, 1000, 12000}},
		{name: "clipped range", samples: []int16{-32768, 32767}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded := EncodePCMU(tt.samples)
			if len(encoded) != len(tt.samples) {
				t.Fatalf("EncodePCMU() length = %d, want %d", len(encoded), len(tt.samples))
			}
			decoded := DecodePCMU(encoded)
			if len(decoded) != len(tt.samples) {
				t.Fatalf("DecodePCMU() length = %d, want %d", len(decoded), len(tt.samples))
			}
			for i, sample := range tt.samples {
				got := decoded[i]
				if absInt(int(got)-int(sample)) > 1024 {
					t.Fatalf("round trip sample[%d] = %d, want near %d", i, got, sample)
				}
			}
		})
	}
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
