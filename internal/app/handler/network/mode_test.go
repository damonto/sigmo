package network

import (
	"testing"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

func TestModeResponse(t *testing.T) {
	tests := []struct {
		name    string
		mode    mmodem.ModemModePair
		current mmodem.ModemModePair
		want    ModeResponse
	}{
		{
			name: "current",
			mode: mmodem.ModemModePair{
				Allowed:   mmodem.ModemMode4G,
				Preferred: mmodem.ModemModeNone,
			},
			current: mmodem.ModemModePair{
				Allowed:   mmodem.ModemMode4G,
				Preferred: mmodem.ModemModeNone,
			},
			want: ModeResponse{
				Allowed:        uint32(mmodem.ModemMode4G),
				Preferred:      uint32(mmodem.ModemModeNone),
				AllowedLabel:   "4G",
				PreferredLabel: "None",
				Current:        true,
			},
		},
		{
			name: "supported",
			mode: mmodem.ModemModePair{
				Allowed:   mmodem.ModemMode4G | mmodem.ModemMode5G,
				Preferred: mmodem.ModemMode5G,
			},
			current: mmodem.ModemModePair{
				Allowed:   mmodem.ModemMode4G,
				Preferred: mmodem.ModemModeNone,
			},
			want: ModeResponse{
				Allowed:        uint32(mmodem.ModemMode4G | mmodem.ModemMode5G),
				Preferred:      uint32(mmodem.ModemMode5G),
				AllowedLabel:   "4G + 5G",
				PreferredLabel: "5G",
				Current:        false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := modeResponse(tt.mode, tt.current); got != tt.want {
				t.Fatalf("modeResponse() = %#v, want %#v", got, tt.want)
			}
		})
	}
}
