package modem

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/damonto/wwan-go/cdcwdm"
	wwanmodem "github.com/damonto/wwan-go/modem"
	qmitransport "github.com/damonto/wwan-go/qcom/qmi"
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

func TestTerminalRuntimeError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "cdc-wdm disconnect", err: fmt.Errorf("watch status: %w", cdcwdm.ErrDisconnected), want: true},
		{name: "QMI terminal read", err: &qmitransport.TransportError{Err: errors.New("malformed QMUX frame")}, want: true},
		{name: "request canceled", err: context.Canceled, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isTerminalRuntimeError(tt.err); got != tt.want {
				t.Fatalf("isTerminalRuntimeError(%v) = %t, want %t", tt.err, got, tt.want)
			}
		})
	}
}

func TestApplyStatusCachesOverview(t *testing.T) {
	m := new(Modem)
	m.applyStatus(wwanmodem.Status{
		Power:         wwanmodem.PowerStateLow,
		SIM:           wwanmodem.SIMStateReady,
		Registration:  wwanmodem.RegistrationRoaming,
		Technology:    wwanmodem.TechnologyLTE | wwanmodem.TechnologyNR5GNSA,
		OperatorID:    " 46001 ",
		OperatorName:  " China Unicom ",
		SignalQuality: 78,
	})

	snapshot := m.Snapshot()
	if snapshot.State != ModemStateDisabled {
		t.Fatalf("state = %v, want %v", snapshot.State, ModemStateDisabled)
	}
	if !snapshot.AirplaneMode {
		t.Fatal("airplane mode = false, want true")
	}
	if snapshot.Registration != Modem3GPPRegistrationStateRoaming {
		t.Fatalf("registration = %v, want %v", snapshot.Registration, Modem3GPPRegistrationStateRoaming)
	}
	if !slices.Contains(snapshot.Access, ModemAccessTechnologyLte) || !slices.Contains(snapshot.Access, ModemAccessTechnology5GNR) {
		t.Fatalf("access technologies = %v, want LTE and 5G NR", snapshot.Access)
	}
	if snapshot.OperatorCode != "46001" || snapshot.OperatorName != "China Unicom" {
		t.Fatalf("operator = %q %q", snapshot.OperatorCode, snapshot.OperatorName)
	}
	if snapshot.SignalQuality != 78 {
		t.Fatalf("signal quality = %d, want 78", snapshot.SignalQuality)
	}
}

func TestAirplaneModeEnabled(t *testing.T) {
	tests := []struct {
		name  string
		state wwanmodem.PowerState
		want  bool
	}{
		{name: "unknown", state: wwanmodem.PowerStateUnknown},
		{name: "off", state: wwanmodem.PowerStateOff, want: true},
		{name: "low", state: wwanmodem.PowerStateLow, want: true},
		{name: "on", state: wwanmodem.PowerStateOn},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := airplaneModeEnabled(tt.state); got != tt.want {
				t.Errorf("airplaneModeEnabled(%d) = %t, want %t", tt.state, got, tt.want)
			}
		})
	}
}

func TestApplySIMSlotsCachesIdentity(t *testing.T) {
	m := new(Modem)
	m.applySIMInfo(wwanmodem.SIMInfo{
		Slot:         1,
		ICCID:        "8901000000000000001",
		OperatorID:   "46001",
		OperatorName: "China Unicom",
		ATR:          []byte{0x3b, 0x00},
	})
	m.applySIMSlots([]wwanmodem.SIMSlot{
		{Index: 2, ICCID: "8901000000000000002"},
		{Index: 1, Active: true, ICCID: "8901000000000000001"},
	})

	snapshot := m.Snapshot()
	if len(snapshot.Slots) != 2 {
		t.Fatalf("slot count = %d, want 2", len(snapshot.Slots))
	}
	if snapshot.Slots[0].Slot != 1 || !snapshot.Slots[0].Active {
		t.Fatalf("first slot = %+v, want active slot 1", snapshot.Slots[0])
	}
	if snapshot.Slots[0].OperatorIdentifier != "46001" {
		t.Fatalf("active slot operator = %q, want 46001", snapshot.Slots[0].OperatorIdentifier)
	}
	if snapshot.Slots[1].Slot != 2 || snapshot.Slots[1].Identifier != "8901000000000000002" {
		t.Fatalf("second slot = %+v", snapshot.Slots[1])
	}

	snapshot.Slots[0].ATR[0] = 0
	if got := m.Snapshot().Slots[0].ATR[0]; got != 0x3b {
		t.Fatalf("cached ATR was mutated through snapshot: %x", got)
	}
}

func TestApplySIMSlotsDoesNotMoveIdentityBetweenSlots(t *testing.T) {
	m := new(Modem)
	m.applySIMInfo(wwanmodem.SIMInfo{
		Slot:         1,
		ICCID:        "8901000000000000001",
		OperatorID:   "46001",
		OperatorName: "China Unicom",
		OwnNumbers:   []string{"+8613800000000"},
	})
	m.applySIMSlots([]wwanmodem.SIMSlot{
		{Index: 1},
		{Index: 2, Active: true},
	})

	snapshot := m.Snapshot()
	if snapshot.PrimarySIMSlot != 2 {
		t.Fatalf("primary slot = %d, want 2", snapshot.PrimarySIMSlot)
	}
	if snapshot.Slots[0].Identifier != "8901000000000000001" || snapshot.Slots[0].Active {
		t.Fatalf("slot one = %+v, want the inactive cached SIM", snapshot.Slots[0])
	}
	if snapshot.Slots[1].Identifier != "" || snapshot.Slots[1].OperatorIdentifier != "" {
		t.Fatalf("slot two inherited the old SIM identity: %+v", snapshot.Slots[1])
	}

	m.applySIMInfo(wwanmodem.SIMInfo{Slot: 2, ICCID: "8901000000000000002"})
	snapshot = m.Snapshot()
	if snapshot.Number != "" {
		t.Fatalf("number = %q, want stale number cleared", snapshot.Number)
	}
	if snapshot.Slots[0].Active || !snapshot.Slots[1].Active {
		t.Fatalf("slot activity = %+v", snapshot.Slots)
	}
	if snapshot.Slots[1].Identifier != "8901000000000000002" {
		t.Fatalf("slot two identifier = %q", snapshot.Slots[1].Identifier)
	}
}
