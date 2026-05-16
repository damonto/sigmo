package at

import (
	"bytes"
	"testing"
)

func TestCRSMSW(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    []byte
		wantErr bool
	}{
		{
			name: "decode success",
			raw:  `+CRSM: 144,0,"AABB"`,
			want: []byte{0xAA, 0xBB},
		},
		{
			name: "empty data",
			raw:  `+CRSM: 144,0,""`,
		},
		{
			name:    "missing prefix",
			raw:     `OK`,
			wantErr: true,
		},
		{
			name:    "short response",
			raw:     `+CRSM: 144`,
			wantErr: true,
		},
		{
			name:    "unexpected status",
			raw:     `+CRSM: 106,130,""`,
			wantErr: true,
		},
		{
			name:    "invalid hex",
			raw:     `+CRSM: 144,0,"XX"`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := (&CRSM{}).sw(tt.raw)
			if (err != nil) != tt.wantErr {
				t.Fatalf("sw() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !bytes.Equal(got, tt.want) {
				t.Fatalf("sw() = %X, want %X", got, tt.want)
			}
		})
	}
}
