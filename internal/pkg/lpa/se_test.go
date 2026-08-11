package lpa

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/damonto/euicc-go/driver"
	"github.com/damonto/sigmo/internal/pkg/modem"
	wwanmodem "github.com/damonto/wwan-go/modem"
)

func TestDiscoverSEsTriesESTKProductAIDFallback(t *testing.T) {
	tests := []struct {
		name       string
		atr        []byte
		simSlot    uint32
		targetSlot uint8
		channel    *fakeSmartCardChannel
		wantIDs    []string
		wantOpened bool
		wantClosed int
		wantErr    error
	}{
		{
			name:       "known ESTKme ATR",
			atr:        estkmeATRs[0],
			channel:    &fakeSmartCardChannel{transmitResponse: []byte("ESTKme Max\x90\x00")},
			wantIDs:    []string{SEID0, SEID1},
			wantOpened: true,
			wantClosed: 1,
		},
		{
			name:       "ordinary ATR uses default SE without product AID",
			atr:        []byte{0x3B, 0x00},
			channel:    &fakeSmartCardChannel{transmitResponse: []byte("ESTKme Max\x90\x00")},
			wantIDs:    []string{SEIDDefault},
			wantOpened: false,
			wantClosed: 0,
		},
		{
			name:       "missing ATR probes product AID",
			channel:    &fakeSmartCardChannel{transmitResponse: []byte("ESTKme Plus+\x90\x00")},
			wantIDs:    []string{SEID0, SEID1},
			wantOpened: true,
			wantClosed: 1,
		},
		{
			name:       "product AID unavailable uses default SE",
			atr:        estkmeATRs[0],
			channel:    &fakeSmartCardChannel{openLogicalChannelErr: errAIDNotSupported},
			wantIDs:    []string{SEIDDefault},
			wantOpened: true,
			wantClosed: 1,
		},
		{
			name:       "inactive slot probes product AID",
			atr:        []byte{0x3B, 0x00},
			simSlot:    1,
			targetSlot: 2,
			channel:    &fakeSmartCardChannel{transmitResponse: []byte("ESTKme Max\x90\x00")},
			wantIDs:    []string{SEID0, SEID1},
			wantOpened: true,
			wantClosed: 1,
		},
		{
			name:       "transient product probe remains retryable",
			atr:        estkmeATRs[0],
			channel:    &fakeSmartCardChannel{openLogicalChannelErr: context.DeadlineExceeded},
			wantOpened: true,
			wantClosed: 1,
			wantErr:    context.DeadlineExceeded,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			targetSlot := tt.targetSlot
			if targetSlot == 0 {
				targetSlot = 1
			}
			current := &modem.Modem{
				EquipmentIdentifier: "test:" + tt.name,
				PrimarySIMSlot:      tt.simSlot,
				SIM:                 &modem.SIM{Slot: tt.simSlot, ATR: tt.atr},
			}
			if len(tt.atr) == 0 || tt.targetSlot != 0 {
				current.PrimaryPort = "/dev/cdc-wdm0"
				current.Ports = []modem.ModemPort{{PortType: wwanmodem.PortQMI, Device: current.PrimaryPort}}
			}
			got, err := discoverSEsAtSlot(t.Context(), current, targetSlot, func(context.Context, *modem.Modem) (driver.SmartCardChannel, error) {
				return tt.channel, nil
			})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("DiscoverSEs() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				if tt.channel.disconnects != tt.wantClosed {
					t.Fatalf("channel disconnects = %d, want %d", tt.channel.disconnects, tt.wantClosed)
				}
				return
			}
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
			if tt.channel.disconnects != tt.wantClosed {
				t.Fatalf("channel disconnects = %d, want %d", tt.channel.disconnects, tt.wantClosed)
			}
		})
	}
}

func TestDiscoverSEsReservesSIMSlotDuringProbe(t *testing.T) {
	m := &modem.Modem{
		EquipmentIdentifier: "test:reserved-discovery",
		PrimarySIMSlot:      1,
		SIM:                 &modem.SIM{Slot: 1, ATR: estkmeATRs[0]},
	}
	channel := &fakeSmartCardChannel{openLogicalChannelErr: errAIDNotSupported}
	_, err := discoverSEs(t.Context(), m, func(context.Context, *modem.Modem) (driver.SmartCardChannel, error) {
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
		defer cancel()
		if _, reserveErr := m.ReserveSIMSlot(ctx); !errors.Is(reserveErr, context.DeadlineExceeded) {
			t.Fatalf("ReserveSIMSlot() during discovery error = %v, want %v", reserveErr, context.DeadlineExceeded)
		}
		return channel, nil
	})
	if err != nil {
		t.Fatalf("discoverSEs() error = %v", err)
	}

	release, err := m.ReserveSIMSlot(t.Context())
	if err != nil {
		t.Fatalf("ReserveSIMSlot() after discovery error = %v", err)
	}
	release()
}
