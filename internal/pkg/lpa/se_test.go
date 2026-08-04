package lpa

import (
	"context"
	"errors"
	"testing"

	"github.com/damonto/euicc-go/driver"
	"github.com/damonto/sigmo/internal/pkg/modem"
)

func TestDiscoverSEsTriesESTKProductAIDFallback(t *testing.T) {
	tests := []struct {
		name       string
		atr        []byte
		channel    *fakeSmartCardChannel
		wantIDs    []string
		wantOpened bool
	}{
		{
			name:       "known ESTKme ATR",
			atr:        estkmeATRs[0],
			channel:    &fakeSmartCardChannel{transmitResponse: []byte("ESTKme Max\x90\x00")},
			wantIDs:    []string{SEID0, SEID1},
			wantOpened: true,
		},
		{
			name:       "ordinary ATR uses default SE without product AID",
			atr:        []byte{0x3B, 0x00},
			channel:    &fakeSmartCardChannel{transmitResponse: []byte("ESTKme Max\x90\x00")},
			wantIDs:    []string{SEIDDefault},
			wantOpened: false,
		},
		{
			name:       "missing ATR uses default SE without product AID",
			channel:    &fakeSmartCardChannel{transmitResponse: []byte("ESTKme Plus+\x90\x00")},
			wantIDs:    []string{SEIDDefault},
			wantOpened: false,
		},
		{
			name:       "product AID unavailable uses default SE",
			atr:        estkmeATRs[0],
			channel:    &fakeSmartCardChannel{openLogicalChannelErr: errors.New("not found")},
			wantIDs:    []string{SEIDDefault},
			wantOpened: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := discoverSEs(t.Context(), &modem.Modem{
				EquipmentIdentifier: "test:" + tt.name,
				Sim:                 &modem.SIM{ATR: tt.atr},
			}, func(context.Context, *modem.Modem) (driver.SmartCardChannel, error) {
				return tt.channel, nil
			})
			if err != nil {
				t.Fatalf("DiscoverSEs() error = %v", err)
			}
			if len(got) != len(tt.wantIDs) {
				t.Fatalf("len(SEs) = %d, want %d", len(got), len(tt.wantIDs))
			}
			for i, se := range got {
				if se.ID != tt.wantIDs[i] {
					t.Fatalf("SE[%d].ID = %q, want %q", i, se.ID, tt.wantIDs[i])
				}
			}
			if got := tt.channel.disconnects > 0; got != tt.wantOpened {
				t.Fatalf("channel opened = %v, want %v", got, tt.wantOpened)
			}
		})
	}
}
