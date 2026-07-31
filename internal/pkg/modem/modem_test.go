package modem

import (
	"context"
	"errors"
	"testing"

	wwanmodem "github.com/damonto/wwan-go/modem"
)

func TestReserveSIMSlot(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T, m *Modem, release func())
	}{
		{
			name: "honors canceled context",
			run: func(t *testing.T, m *Modem, _ func()) {
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				if _, err := m.ReserveSIMSlot(ctx); !errors.Is(err, context.Canceled) {
					t.Fatalf("ReserveSIMSlot() error = %v, want %v", err, context.Canceled)
				}
			},
		},
		{
			name: "release is idempotent and reservation is reusable",
			run: func(t *testing.T, m *Modem, release func()) {
				release()
				release()
				nextRelease, err := m.ReserveSIMSlot(t.Context())
				if err != nil {
					t.Fatalf("ReserveSIMSlot() after release error = %v", err)
				}
				nextRelease()
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := new(Modem)
			release, err := m.ReserveSIMSlot(t.Context())
			if err != nil {
				t.Fatalf("ReserveSIMSlot() error = %v", err)
			}
			defer release()
			tt.run(t, m, release)
		})
	}
}

func TestLegacyBandEncoding(t *testing.T) {
	tests := []struct {
		name     string
		semantic wwanmodem.Band
		legacy   ModemBand
	}{
		{name: "LTE B41", semantic: wwanmodem.Band{Technology: wwanmodem.TechnologyLTE, Number: 41}, legacy: 71},
		{name: "NR n78", semantic: wwanmodem.Band{Technology: wwanmodem.TechnologyNR5GSA, Number: 78}, legacy: 378},
		{name: "UMTS B1", semantic: wwanmodem.Band{Technology: wwanmodem.TechnologyUMTS, Number: 1}, legacy: 5},
		{name: "GSM 900", semantic: wwanmodem.Band{Technology: wwanmodem.TechnologyGSM, Number: 900}, legacy: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			legacy, ok := legacyBand(tt.semantic)
			if !ok || legacy != tt.legacy {
				t.Fatalf("legacyBand(%+v) = %d, %t; want %d, true", tt.semantic, legacy, ok, tt.legacy)
			}
			semantic, ok := semanticBand(tt.legacy)
			if !ok || semantic.Number != tt.semantic.Number || semantic.Technology&tt.semantic.Technology == 0 {
				t.Fatalf("semanticBand(%d) = %+v, %t; want technology %d band %d", tt.legacy, semantic, ok, tt.semantic.Technology, tt.semantic.Number)
			}
		})
	}
}

func TestLegacyBandsIncludeAny(t *testing.T) {
	got := legacyBands([]wwanmodem.Band{{Technology: wwanmodem.TechnologyLTE, Number: 41}}, true)
	want := []ModemBand{ModemBandAny, 71}
	if len(got) != len(want) || got[0] != 71 || got[1] != ModemBandAny {
		t.Fatalf("legacyBands() = %v, want [71 %d]", got, ModemBandAny)
	}
}

func TestProfileIDUsesCachedSIMWithoutTransport(t *testing.T) {
	modem := &Modem{Sim: &SIM{Identifier: " 8901000000000000001 "}}
	got, err := modem.ProfileID(t.Context())
	if err != nil {
		t.Fatalf("ProfileID() error = %v", err)
	}
	if got != "8901000000000000001" {
		t.Fatalf("ProfileID() = %q", got)
	}
}

func TestConsumeModemStreamRejectsNilStream(t *testing.T) {
	if err := consumeModemStream[int](context.Background(), nil, func(int) {}); err == nil {
		t.Fatal("consumeModemStream() error = nil, want nil stream error")
	}
}
