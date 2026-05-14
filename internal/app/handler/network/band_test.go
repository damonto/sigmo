package network

import (
	"errors"
	"testing"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

func TestValidateBandValues(t *testing.T) {
	tests := []struct {
		name      string
		supported []mmodem.ModemBand
		bands     []mmodem.ModemBand
		wantErr   error
	}{
		{
			name:      "accept supported bands",
			supported: []mmodem.ModemBand{mmodem.ModemBandAny, 71, 378},
			bands:     []mmodem.ModemBand{71, 378},
		},
		{
			name:      "reject empty",
			supported: []mmodem.ModemBand{mmodem.ModemBandAny, 71},
			wantErr:   errBandsRequired,
		},
		{
			name:      "reject unsupported",
			supported: []mmodem.ModemBand{mmodem.ModemBandAny, 71},
			bands:     []mmodem.ModemBand{72},
			wantErr:   errUnsupportedBand,
		},
		{
			name:      "reject any with other bands",
			supported: []mmodem.ModemBand{mmodem.ModemBandAny, 71},
			bands:     []mmodem.ModemBand{mmodem.ModemBandAny, 71},
			wantErr:   errAnyBandExclusive,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateBandValues(tt.supported, tt.bands)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("validateBandValues() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
