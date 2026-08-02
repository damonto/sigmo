package network

import (
	"testing"

	wwanmodem "github.com/damonto/wwan-go/modem"
)

func TestModeResponse(t *testing.T) {
	tests := []struct {
		name    string
		mode    wwanmodem.Mode
		current wwanmodem.Mode
		want    ModeResponse
	}{
		{
			name: "current",
			mode: wwanmodem.Mode{
				Allowed: wwanmodem.TechnologyLTE,
			},
			current: wwanmodem.Mode{
				Allowed: wwanmodem.TechnologyLTE,
			},
			want: ModeResponse{
				Allowed:        uint64(wwanmodem.TechnologyLTE),
				AllowedLabel:   "LTE",
				PreferredLabel: "None",
				Current:        true,
			},
		},
		{
			name: "supported",
			mode: wwanmodem.Mode{
				Allowed:   wwanmodem.TechnologyLTE | wwanmodem.TechnologyNR5GSA,
				Preferred: wwanmodem.TechnologyNR5GSA,
			},
			current: wwanmodem.Mode{
				Allowed: wwanmodem.TechnologyLTE,
			},
			want: ModeResponse{
				Allowed:        uint64(wwanmodem.TechnologyLTE | wwanmodem.TechnologyNR5GSA),
				Preferred:      uint64(wwanmodem.TechnologyNR5GSA),
				AllowedLabel:   "LTE + 5G SA",
				PreferredLabel: "5G SA",
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
