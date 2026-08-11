package modem

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/damonto/wwan-go/cdcwdm"
	wwanmodem "github.com/damonto/wwan-go/modem"
	"github.com/damonto/wwan-go/qcom"
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

func TestDoneClosesWithModemGeneration(t *testing.T) {
	m := new(Modem)
	done := m.Done()
	select {
	case <-done:
		t.Fatal("Done() closed before Close()")
	default:
	}

	if err := m.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Done() remained open after Close()")
	}
	if got := m.Done(); got != done {
		t.Fatal("Done() returned a different lifecycle channel")
	}
}

func TestNetworkStateVersionChangesMonotonically(t *testing.T) {
	m := new(Modem)
	if got := m.NetworkStateVersion(); got != 0 {
		t.Fatalf("NetworkStateVersion() = %d, want zero value", got)
	}
	m.markNetworkStateChanged()
	m.markNetworkStateChanged()
	if got := m.NetworkStateVersion(); got != 2 {
		t.Fatalf("NetworkStateVersion() = %d, want 2", got)
	}
}

func TestWithReservedSIMSlotKeepsReservationUntilWorkflowCompletes(t *testing.T) {
	m := new(Modem)
	started := make(chan struct{})
	finish := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- m.withReservedSIMSlot(t.Context(), func() error {
			close(started)
			<-finish
			return nil
		})
	}()
	<-started

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if _, err := m.ReserveSIMSlot(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ReserveSIMSlot() during workflow error = %v, want %v", err, context.DeadlineExceeded)
	}
	close(finish)
	if err := <-done; err != nil {
		t.Fatalf("withReservedSIMSlot() error = %v", err)
	}

	release, err := m.ReserveSIMSlot(t.Context())
	if err != nil {
		t.Fatalf("ReserveSIMSlot() after workflow error = %v", err)
	}
	release()
}

func TestProfileIDUsesCachedSIMWithoutTransport(t *testing.T) {
	modem := &Modem{SIM: &SIM{Identifier: " 8901000000000000001 "}}
	got, err := modem.ProfileID(t.Context())
	if err != nil {
		t.Fatalf("ProfileID() error = %v", err)
	}
	if got != "8901000000000000001" {
		t.Fatalf("ProfileID() = %q", got)
	}
}

func TestConsumeModemStreamRejectsNilStream(t *testing.T) {
	if err := consumeModemStream(t.Context(), nil, func(int) {}); err == nil {
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
		{name: "transient request timeout", err: fmt.Errorf("watch status: %w", context.DeadlineExceeded), want: false},
		{name: "QMI client IDs exhausted", err: fmt.Errorf("watch status: %w", qcom.QMIErrorClientIDsExhausted), want: true},
		{name: "ordinary QMI service error", err: qcom.QMIErrorNoNetworkFound, want: false},
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
	if snapshot.Status.Power != wwanmodem.PowerStateLow {
		t.Fatalf("power = %v, want %v", snapshot.Status.Power, wwanmodem.PowerStateLow)
	}
	if !snapshot.AirplaneMode() {
		t.Fatal("airplane mode = false, want true")
	}
	if snapshot.Status.Registration != wwanmodem.RegistrationRoaming {
		t.Fatalf("registration = %v, want %v", snapshot.Status.Registration, wwanmodem.RegistrationRoaming)
	}
	if snapshot.Status.Technology != wwanmodem.TechnologyLTE|wwanmodem.TechnologyNR5GNSA {
		t.Fatalf("access technology = %v, want LTE and 5G NR", snapshot.Status.Technology)
	}
	if snapshot.Status.OperatorID != "46001" || snapshot.Status.OperatorName != "China Unicom" {
		t.Fatalf("operator = %q %q", snapshot.Status.OperatorID, snapshot.Status.OperatorName)
	}
	if snapshot.Status.SignalQuality != 78 {
		t.Fatalf("signal quality = %d, want 78", snapshot.Status.SignalQuality)
	}
}

func TestApplyPowerStateCachesAirplaneMode(t *testing.T) {
	registered := wwanmodem.Status{
		Power:         wwanmodem.PowerStateOn,
		Registration:  wwanmodem.RegistrationRoaming,
		PacketService: wwanmodem.PacketServiceAttached,
		Technology:    wwanmodem.TechnologyLTE,
		OperatorID:    "46001",
		OperatorName:  "UNICOM",
		SignalQuality: 80,
	}
	tests := []struct {
		name  string
		state wwanmodem.PowerState
		want  wwanmodem.Status
	}{
		{name: "online preserves network status", state: wwanmodem.PowerStateOn, want: registered},
		{
			name:  "low power clears network status",
			state: wwanmodem.PowerStateLow,
			want: wwanmodem.Status{
				Power:         wwanmodem.PowerStateLow,
				Registration:  wwanmodem.RegistrationIdle,
				PacketService: wwanmodem.PacketServiceDetached,
			},
		},
		{
			name:  "offline clears network status",
			state: wwanmodem.PowerStateOff,
			want: wwanmodem.Status{
				Power:         wwanmodem.PowerStateOff,
				Registration:  wwanmodem.RegistrationIdle,
				PacketService: wwanmodem.PacketServiceDetached,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Modem{Status: registered}
			m.applyPowerState(tt.state)
			snapshot := m.Snapshot()
			if snapshot.Status != tt.want {
				t.Errorf("applyPowerState() status = %+v, want %+v", snapshot.Status, tt.want)
			}
			if !snapshot.StatusKnown {
				t.Error("applyPowerState() status known = false, want true")
			}
		})
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

func TestApplySIMInfoUsesKnownActivePhysicalSlot(t *testing.T) {
	m := new(Modem)
	m.applySIMSlots([]wwanmodem.SIMSlot{
		{Index: 1, State: wwanmodem.SIMStateReady, ICCID: "8901000000000000001"},
		{Index: 2, Active: true, State: wwanmodem.SIMStateReady, ICCID: "8901000000000000002"},
	})

	// The long-lived QMI client reports its configured slot 1 even though the
	// physical slot status identifies slot 2 as active.
	m.applySIMInfo(wwanmodem.SIMInfo{
		Slot:         1,
		ICCID:        "8901000000000000002",
		OperatorID:   "310240",
		OperatorName: "T-Mobile",
	})

	snapshot := m.Snapshot()
	if snapshot.PrimarySIMSlot != 2 {
		t.Fatalf("primary slot = %d, want 2", snapshot.PrimarySIMSlot)
	}
	if snapshot.Slots[0].Identifier != "8901000000000000001" || snapshot.Slots[0].Active {
		t.Fatalf("slot one was overwritten by active SIM info: %+v", snapshot.Slots[0])
	}
	if snapshot.Slots[1].Identifier != "8901000000000000002" || !snapshot.Slots[1].Active {
		t.Fatalf("slot two = %+v, want active physical slot", snapshot.Slots[1])
	}
	if snapshot.Slots[1].OperatorIdentifier != "310240" {
		t.Fatalf("slot two operator = %q, want 310240", snapshot.Slots[1].OperatorIdentifier)
	}
}

func TestApplySIMInfoMergesSparseMetadataForSameCard(t *testing.T) {
	tests := []struct {
		name         string
		update       wwanmodem.SIMInfo
		wantATR      []byte
		wantIMSI     string
		wantNumber   string
		wantOperator string
	}{
		{
			name:         "same ICCID preserves enriched fields",
			update:       wwanmodem.SIMInfo{Slot: 1, ICCID: "8901000000000000001"},
			wantATR:      []byte{0x3b, 0x00},
			wantIMSI:     "001010123456789",
			wantNumber:   "+12025550123",
			wantOperator: "Carrier",
		},
		{
			name:   "different ICCID clears previous metadata",
			update: wwanmodem.SIMInfo{Slot: 1, ICCID: "8901000000000000002"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := new(Modem)
			m.applySIMInfo(wwanmodem.SIMInfo{
				Slot:         1,
				ICCID:        "8901000000000000001",
				IMSI:         "001010123456789",
				OperatorName: "Carrier",
				ATR:          []byte{0x3b, 0x00},
				OwnNumbers:   []string{"+12025550123"},
			})

			m.applySIMInfo(tt.update)
			snapshot := m.Snapshot()
			if !slices.Equal(snapshot.SIM.ATR, tt.wantATR) {
				t.Fatalf("ATR = %x, want %x", snapshot.SIM.ATR, tt.wantATR)
			}
			if snapshot.SIM.IMSI != tt.wantIMSI {
				t.Fatalf("IMSI = %q, want %q", snapshot.SIM.IMSI, tt.wantIMSI)
			}
			if snapshot.Number != tt.wantNumber {
				t.Fatalf("number = %q, want %q", snapshot.Number, tt.wantNumber)
			}
			if snapshot.SIM.OperatorName != tt.wantOperator {
				t.Fatalf("operator = %q, want %q", snapshot.SIM.OperatorName, tt.wantOperator)
			}
		})
	}
}

func TestApplySIMInfoPreservesSlotHardwareDuringProfileTransition(t *testing.T) {
	atr := []byte{0x3B, 0x80, 0x81, 0x2F, 0x82, 0xAC}
	m := new(Modem)
	m.applySIMInfo(wwanmodem.SIMInfo{
		Slot:         1,
		ICCID:        "8901000000000000001",
		IMSI:         "001010123456789",
		EID:          "89049032000000000000000000000001",
		OperatorName: "Carrier",
		ATR:          atr,
		OwnNumbers:   []string{"+12025550123"},
	})

	// QMI briefly exposes the same slot without a profile identity while the
	// eUICC refresh is in progress, and its transient ATR is not authoritative.
	m.applySIMInfo(wwanmodem.SIMInfo{Slot: 1, ATR: []byte{0x3B, 0x00}})

	snapshot := m.Snapshot()
	if snapshot.SIM == nil {
		t.Fatal("active SIM = nil, want transitional slot state")
	}
	if !slices.Equal(snapshot.SIM.ATR, atr) || snapshot.SIM.EID != "89049032000000000000000000000001" {
		t.Fatalf("slot hardware = ATR %X EID %q, want preserved eUICC identity", snapshot.SIM.ATR, snapshot.SIM.EID)
	}
	if snapshot.SIM.Identifier != "" || snapshot.SIM.IMSI != "" || snapshot.SIM.OperatorName != "" || snapshot.Number != "" {
		t.Fatalf("profile metadata survived transition: SIM=%+v number=%q", snapshot.SIM, snapshot.Number)
	}
	if kind := snapshot.SIMKind(); kind != SIMKindEUICC {
		t.Fatalf("SIM kind = %q, want %q during eUICC profile transition", kind, SIMKindEUICC)
	}

	m.applySIMInfo(wwanmodem.SIMInfo{
		Slot:  1,
		State: wwanmodem.SIMStateReady,
		ICCID: "8901000000000000002",
	})
	snapshot = m.Snapshot()
	if !slices.Equal(snapshot.SIM.ATR, atr) || snapshot.SIM.EID != "89049032000000000000000000000001" {
		t.Fatalf("new profile hardware = ATR %X EID %q, want preserved eUICC identity", snapshot.SIM.ATR, snapshot.SIM.EID)
	}
	if snapshot.SIM.Identifier != "8901000000000000002" || snapshot.Status.SIM != wwanmodem.SIMStateReady {
		t.Fatalf("new profile state = SIM %+v status %v", snapshot.SIM, snapshot.Status.SIM)
	}
}

func TestApplySIMSlotsFiltersUnavailableSlots(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*Modem)
		slots []wwanmodem.SIMSlot
		want  []uint32
	}{
		{
			name: "inactive firmware placeholder",
			slots: []wwanmodem.SIMSlot{
				{Index: 1, Active: true, State: wwanmodem.SIMStateReady, ICCID: "8901000000000000001"},
				{Index: 2, State: wwanmodem.SIMStateAbsent},
			},
			want: []uint32{1},
		},
		{
			name: "active SIM without identifier",
			slots: []wwanmodem.SIMSlot{
				{Index: 1, Active: true},
				{Index: 2, State: wwanmodem.SIMStateAbsent},
			},
			want: []uint32{1},
		},
		{
			name: "two identified SIMs",
			slots: []wwanmodem.SIMSlot{
				{Index: 1, Active: true, State: wwanmodem.SIMStateReady, ICCID: "8901000000000000001"},
				{Index: 2, State: wwanmodem.SIMStateReady, ICCID: "8901000000000000002"},
			},
			want: []uint32{1, 2},
		},
		{
			name: "cached inactive SIM",
			setup: func(m *Modem) {
				m.applySIMInfo(wwanmodem.SIMInfo{Slot: 2, ICCID: "8901000000000000002"})
			},
			slots: []wwanmodem.SIMSlot{
				{Index: 1, Active: true, State: wwanmodem.SIMStateReady, ICCID: "8901000000000000001"},
				{Index: 2},
			},
			want: []uint32{1, 2},
		},
		{
			name: "removed cached SIM",
			setup: func(m *Modem) {
				m.applySIMInfo(wwanmodem.SIMInfo{Slot: 2, ICCID: "8901000000000000002"})
			},
			slots: []wwanmodem.SIMSlot{
				{Index: 1, Active: true, State: wwanmodem.SIMStateReady, ICCID: "8901000000000000001"},
				{Index: 2, State: wwanmodem.SIMStateAbsent},
			},
			want: []uint32{1},
		},
		{
			name: "absent SIM with stale identifier",
			slots: []wwanmodem.SIMSlot{
				{Index: 1, Active: true, State: wwanmodem.SIMStateReady, ICCID: "8901000000000000001"},
				{Index: 2, State: wwanmodem.SIMStateAbsent, ICCID: "8901000000000000002"},
			},
			want: []uint32{1},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := new(Modem)
			if tt.setup != nil {
				tt.setup(m)
			}

			m.applySIMSlots(tt.slots)

			if got := m.Snapshot().SIMSlots; !slices.Equal(got, tt.want) {
				t.Errorf("SIM slots = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestApplySIMSlotsIgnoresInactiveSlotWithoutICCID(t *testing.T) {
	m := new(Modem)
	m.applySIMSlots([]wwanmodem.SIMSlot{
		{Index: 1, Active: true, State: wwanmodem.SIMStateReady, ICCID: "8901000000000000001"},
		{Index: 2, State: wwanmodem.SIMStateReady},
	})

	if got := m.Snapshot().SIMSlots; !slices.Equal(got, []uint32{1}) {
		t.Fatalf("selectable SIM slots = %v, want [1]", got)
	}
}

func TestApplySIMSlotsDoesNotMoveIdentityBetweenSlots(t *testing.T) {
	m := new(Modem)
	m.applySIMInfo(wwanmodem.SIMInfo{
		Slot:         1,
		ICCID:        "8901000000000000001",
		OperatorID:   "46001",
		OperatorName: "China Unicom",
		ATR:          []byte{0x3B, 0x80, 0x81, 0x2F, 0x82, 0xAC},
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
	if snapshot.Slots[1].Identifier != "" || snapshot.Slots[1].OperatorIdentifier != "" || len(snapshot.Slots[1].ATR) != 0 {
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

func TestApplySIMSlotsReplacesChangedActiveIdentity(t *testing.T) {
	m := new(Modem)
	m.applySIMInfo(wwanmodem.SIMInfo{
		Slot:         1,
		ICCID:        "8901000000000000001",
		OperatorID:   "46001",
		OperatorName: "China Unicom",
		OwnNumbers:   []string{"+8613800000000"},
	})

	m.applySIMSlots([]wwanmodem.SIMSlot{
		{Index: 1, Active: true, ICCID: "8901000000000000002"},
	})

	snapshot := m.Snapshot()
	if snapshot.SIM == nil || snapshot.SIM.Identifier != "8901000000000000002" {
		t.Fatalf("active SIM = %+v, want new ICCID", snapshot.SIM)
	}
	if snapshot.SIM.OperatorIdentifier != "" || snapshot.Number != "" {
		t.Fatalf("active SIM retained stale identity: SIM=%+v number=%q", snapshot.SIM, snapshot.Number)
	}
}

func TestApplyActiveSIMIdentityReplacesStaleMetadata(t *testing.T) {
	m := new(Modem)
	m.applySIMInfo(wwanmodem.SIMInfo{
		Slot:         1,
		ICCID:        "8901000000000000001",
		OperatorID:   "46001",
		OperatorName: "China Unicom",
		OwnNumbers:   []string{"+8613800000000"},
	})

	m.applyActiveSIMIdentity(1, "8901000000000000002")

	snapshot := m.Snapshot()
	if snapshot.SIM == nil || snapshot.SIM.Identifier != "8901000000000000002" {
		t.Fatalf("active SIM = %+v, want new ICCID", snapshot.SIM)
	}
	if snapshot.SIM.OperatorIdentifier != "" || snapshot.SIM.OperatorName != "" || snapshot.Number != "" {
		t.Fatalf("active SIM retained stale metadata: SIM=%+v number=%q", snapshot.SIM, snapshot.Number)
	}
}
