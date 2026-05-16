package at

import (
	"bytes"
	"testing"
)

func TestCSIMSW(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    []byte
		wantErr bool
	}{
		{
			name: "decode success",
			raw:  `+CSIM: 4,"9000"`,
			want: []byte{0x90, 0x00},
		},
		{
			name:    "missing comma",
			raw:     `+CSIM: 6`,
			wantErr: true,
		},
		{
			name:    "missing prefix",
			raw:     `OK`,
			wantErr: true,
		},
		{
			name:    "invalid length",
			raw:     `+CSIM: x,"9000"`,
			wantErr: true,
		},
		{
			name:    "mismatched length",
			raw:     `+CSIM: 6,"9000"`,
			wantErr: true,
		},
		{
			name:    "empty data",
			raw:     `+CSIM: 0,""`,
			wantErr: true,
		},
		{
			name:    "invalid hex",
			raw:     `+CSIM: 2,"XX"`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := (&CSIM{}).sw(tt.raw)
			if (err != nil) != tt.wantErr {
				t.Fatalf("sw() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !bytes.Equal(got, tt.want) {
				t.Fatalf("sw() = %X, want %X", got, tt.want)
			}
		})
	}
}
