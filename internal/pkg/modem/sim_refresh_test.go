package modem

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/damonto/uicc-go/qcom"
	"github.com/damonto/uicc-go/qcom/uim"
	"github.com/damonto/uicc-go/usim/simfile"
	"github.com/godbus/dbus/v5"
)

func TestEnsureSIMVisible(t *testing.T) {
	oldDelay := simSettleDelay
	simSettleDelay = time.Nanosecond
	t.Cleanup(func() {
		simSettleDelay = oldDelay
	})

	oldNotReadyInterval := simNotReadyRetryInterval
	simNotReadyRetryInterval = time.Nanosecond
	t.Cleanup(func() {
		simNotReadyRetryInterval = oldNotReadyInterval
	})

	oldNotReadyCount := simNotReadyRetryCount
	simNotReadyRetryCount = 1
	t.Cleanup(func() {
		simNotReadyRetryCount = oldNotReadyCount
	})

	oldPostRepowerInterval := simPostRepowerPollInterval
	simPostRepowerPollInterval = time.Nanosecond
	t.Cleanup(func() {
		simPostRepowerPollInterval = oldPostRepowerInterval
	})

	oldPostRepowerCount := simPostRepowerPollCount
	simPostRepowerPollCount = 1
	t.Cleanup(func() {
		simPostRepowerPollCount = oldPostRepowerCount
	})

	oldInterval := simVisiblePollInterval
	simVisiblePollInterval = time.Millisecond
	t.Cleanup(func() {
		simVisiblePollInterval = oldInterval
	})

	target := SIMTarget{Slot: 1, ICCID: "8986000000000000000"}
	tests := []struct {
		name      string
		modem     *Modem
		reader    *fakeQMIUIMReader
		timeout   time.Duration
		wantErr   error
		wantCalls []string
	}{
		{
			name: "returns visible modem without QMI access",
			modem: &Modem{
				EquipmentIdentifier: "imei-1",
				PrimarySimSlot:      1,
				Sim:                 &SIM{Identifier: target.ICCID},
			},
		},
		{
			name: "refreshes ModemManager only when QMI is target and ready",
			modem: &Modem{
				dbusObject:          &fakeBusObject{errors: map[string][]error{ModemInterface + ".Simple.GetStatus": {nil}}},
				EquipmentIdentifier: "imei-1",
				PrimaryPort:         "/dev/cdc-wdm0",
				Ports:               []ModemPort{{PortType: ModemPortTypeQmi, Device: "/dev/cdc-wdm0"}},
				PrimarySimSlot:      1,
				Sim:                 &SIM{Identifier: "old"},
			},
			reader: &fakeQMIUIMReader{
				slotStatus: qmiTestSlotStatus(1, target.ICCID),
				cardStatus: qmiTestCardStatus(
					uim.ApplicationStateReady,
					uim.PersonalizationStateReady,
					[]byte{0xA0, 0x00},
				),
			},
			timeout:   5 * time.Millisecond,
			wantErr:   context.DeadlineExceeded,
			wantCalls: []string{"slot-status", "card-status", "slot-status", "card-status"},
		},
		{
			name: "repowers SIM only after QMI stays not ready",
			modem: &Modem{
				EquipmentIdentifier: "imei-1",
				PrimaryPort:         "/dev/cdc-wdm0",
				Ports:               []ModemPort{{PortType: ModemPortTypeQmi, Device: "/dev/cdc-wdm0"}},
				PrimarySimSlot:      1,
				Sim:                 &SIM{Identifier: "old"},
			},
			reader: &fakeQMIUIMReader{
				slotStatus: qmiTestSlotStatus(1, target.ICCID),
				cardStatus: qmiTestCardStatus(
					uim.ApplicationStateDetected,
					uim.PersonalizationStateInProgress,
					[]byte{0xA0, 0x00},
				),
				slotStatuses: []uim.SlotStatus{
					qmiTestSlotStatus(1, target.ICCID),
					qmiTestSlotStatus(1, target.ICCID),
					qmiTestInactiveSlotStatus(1),
				},
			},
			timeout: 5 * time.Millisecond,
			wantErr: context.DeadlineExceeded,
			wantCalls: []string{
				"slot-status", "card-status",
				"card-status", "change-provisioning:1",
				"slot-status", "card-status",
				"power-off:1", "slot-status", "power-on:1",
			},
		},
		{
			name: "does not repower SIM when QMI status read fails",
			modem: &Modem{
				EquipmentIdentifier: "imei-1",
				PrimaryPort:         "/dev/cdc-wdm0",
				Ports:               []ModemPort{{PortType: ModemPortTypeQmi, Device: "/dev/cdc-wdm0"}},
				PrimarySimSlot:      1,
				Sim:                 &SIM{Identifier: "old"},
			},
			reader: &fakeQMIUIMReader{
				slotStatusErr: errors.New("slot status rejected"),
			},
			timeout:   5 * time.Millisecond,
			wantErr:   context.DeadlineExceeded,
			wantCalls: []string{"slot-status", "slot-status", "slot-status"},
		},
		{
			name: "does not recover SIM when QMI target does not match",
			modem: &Modem{
				EquipmentIdentifier: "imei-1",
				PrimaryPort:         "/dev/cdc-wdm0",
				Ports:               []ModemPort{{PortType: ModemPortTypeQmi, Device: "/dev/cdc-wdm0"}},
				PrimarySimSlot:      1,
				Sim:                 &SIM{Identifier: "old"},
			},
			reader: &fakeQMIUIMReader{
				slotStatus: qmiTestSlotStatusActiveWithSlotICCID(2, 1, "8986000000000000001"),
				cardStatus: qmiTestCardStatus(
					uim.ApplicationStateDetected,
					uim.PersonalizationStateInProgress,
					[]byte{0xA0, 0x00},
				),
			},
			timeout:   5 * time.Millisecond,
			wantErr:   context.DeadlineExceeded,
			wantCalls: []string{"slot-status", "card-status", "slot-status", "card-status"},
		},
		{
			name: "does not recover SIM when QMI ICCID is invalid",
			modem: &Modem{
				EquipmentIdentifier: "imei-1",
				PrimaryPort:         "/dev/cdc-wdm0",
				Ports:               []ModemPort{{PortType: ModemPortTypeQmi, Device: "/dev/cdc-wdm0"}},
				PrimarySimSlot:      1,
				Sim:                 &SIM{Identifier: "old"},
			},
			reader: &fakeQMIUIMReader{
				slotStatus: qmiTestSlotStatusRawICCID(1, []byte{0x9A}),
				cardStatus: qmiTestCardStatus(
					uim.ApplicationStateDetected,
					uim.PersonalizationStateInProgress,
					[]byte{0xA0, 0x00},
				),
			},
			timeout:   5 * time.Millisecond,
			wantErr:   context.DeadlineExceeded,
			wantCalls: []string{"slot-status", "slot-status", "slot-status"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := &Registry{
				modems: map[dbus.ObjectPath]*Modem{"/modem/1": tt.modem},
			}
			if tt.reader != nil {
				withQMIUIMReader(t, tt.modem.PrimaryPort, 1, tt.reader, nil)
			}
			ctx := context.Background()
			if tt.timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, tt.timeout)
				defer cancel()
			}

			got, err := registry.EnsureSIMVisible(ctx, tt.modem, target)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("EnsureSIMVisible() error = %v, want %v", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("EnsureSIMVisible() error = %v", err)
			}
			if tt.wantErr == nil && got != tt.modem {
				t.Fatalf("EnsureSIMVisible() modem = %p, want %p", got, tt.modem)
			}
			if tt.reader != nil && !slices.Equal(callPrefixWithoutClose(tt.reader.calls, len(tt.wantCalls)), tt.wantCalls) {
				t.Fatalf("reader calls prefix = %v, want %v", tt.reader.calls, tt.wantCalls)
			}
		})
	}
}

func TestQMISIMTargetDoesNotMatchSlotOnlyTargetWithoutSlotStatus(t *testing.T) {
	modem := &Modem{
		EquipmentIdentifier: "imei-1",
		PrimaryPort:         "/dev/cdc-wdm0",
		Ports:               []ModemPort{{PortType: ModemPortTypeQmi, Device: "/dev/cdc-wdm0"}},
		PrimarySimSlot:      1,
	}
	reader := &fakeQMIUIMReader{
		slotStatusErr: qcom.QMIErrorNotSupported,
		cardStatus: qmiTestCardStatus(
			uim.ApplicationStateReady,
			uim.PersonalizationStateReady,
			[]byte{0xA0, 0x00},
		),
	}
	withQMIUIMReader(t, modem.PrimaryPort, 1, reader, nil)

	state, err := qmiSIMStateForTarget(context.Background(), modem, SIMTarget{Slot: 1})
	if err != nil {
		t.Fatalf("qmiSIMStateForTarget() error = %v", err)
	}
	if !state.supported {
		t.Fatal("qmiSIMStateForTarget() supported = false, want true")
	}
	if !state.ready {
		t.Fatal("qmiSIMStateForTarget() ready = false, want true")
	}
	if state.matches {
		t.Fatal("qmiSIMStateForTarget() matches = true, want false")
	}
}

func TestEnsureSIMVisibleConfirmsNotReadyBeforeRepower(t *testing.T) {
	oldDelay := simSettleDelay
	simSettleDelay = time.Nanosecond
	t.Cleanup(func() {
		simSettleDelay = oldDelay
	})

	oldNotReadyInterval := simNotReadyRetryInterval
	simNotReadyRetryInterval = time.Nanosecond
	t.Cleanup(func() {
		simNotReadyRetryInterval = oldNotReadyInterval
	})

	oldNotReadyCount := simNotReadyRetryCount
	simNotReadyRetryCount = 3
	t.Cleanup(func() {
		simNotReadyRetryCount = oldNotReadyCount
	})

	oldPostRepowerInterval := simPostRepowerPollInterval
	simPostRepowerPollInterval = time.Nanosecond
	t.Cleanup(func() {
		simPostRepowerPollInterval = oldPostRepowerInterval
	})

	oldPostRepowerCount := simPostRepowerPollCount
	simPostRepowerPollCount = 1
	t.Cleanup(func() {
		simPostRepowerPollCount = oldPostRepowerCount
	})

	target := SIMTarget{Slot: 1, ICCID: "8986000000000000000"}
	modem := &Modem{
		EquipmentIdentifier: "imei-1",
		PrimaryPort:         "/dev/cdc-wdm0",
		Ports:               []ModemPort{{PortType: ModemPortTypeQmi, Device: "/dev/cdc-wdm0"}},
		PrimarySimSlot:      1,
		Sim:                 &SIM{Identifier: "old"},
	}
	reader := &fakeQMIUIMReader{
		slotStatus: qmiTestSlotStatus(1, target.ICCID),
		cardStatus: qmiTestCardStatus(
			uim.ApplicationStateDetected,
			uim.PersonalizationStateInProgress,
			[]byte{0xA0, 0x00},
		),
		slotStatuses: []uim.SlotStatus{
			qmiTestSlotStatus(1, target.ICCID),
			qmiTestSlotStatus(1, target.ICCID),
			qmiTestSlotStatus(1, target.ICCID),
			qmiTestSlotStatus(1, target.ICCID),
			qmiTestInactiveSlotStatus(1),
		},
	}
	registry := &Registry{
		modems: map[dbus.ObjectPath]*Modem{"/modem/1": modem},
	}
	withQMIUIMReader(t, modem.PrimaryPort, 1, reader, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := registry.EnsureSIMVisible(ctx, modem, target)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("EnsureSIMVisible() error = %v, want %v", err, context.DeadlineExceeded)
	}

	wantCalls := []string{
		"slot-status", "card-status",
		"card-status", "change-provisioning:1",
		"slot-status", "card-status",
		"slot-status", "card-status",
		"slot-status", "card-status",
		"power-off:1", "slot-status", "power-on:1",
	}
	if !slices.Equal(callPrefixWithoutClose(reader.calls, len(wantCalls)), wantCalls) {
		t.Fatalf("reader calls prefix = %v, want %v", reader.calls, wantCalls)
	}
}

func TestEnsureSIMVisibleResetsNotReadyCountAfterQMIError(t *testing.T) {
	oldDelay := simSettleDelay
	simSettleDelay = time.Nanosecond
	t.Cleanup(func() {
		simSettleDelay = oldDelay
	})

	oldNotReadyInterval := simNotReadyRetryInterval
	simNotReadyRetryInterval = time.Nanosecond
	t.Cleanup(func() {
		simNotReadyRetryInterval = oldNotReadyInterval
	})

	oldNotReadyCount := simNotReadyRetryCount
	simNotReadyRetryCount = 2
	t.Cleanup(func() {
		simNotReadyRetryCount = oldNotReadyCount
	})

	oldPostRepowerInterval := simPostRepowerPollInterval
	simPostRepowerPollInterval = time.Nanosecond
	t.Cleanup(func() {
		simPostRepowerPollInterval = oldPostRepowerInterval
	})

	oldPostRepowerCount := simPostRepowerPollCount
	simPostRepowerPollCount = 1
	t.Cleanup(func() {
		simPostRepowerPollCount = oldPostRepowerCount
	})

	target := SIMTarget{Slot: 1, ICCID: "8986000000000000000"}
	modem := &Modem{
		EquipmentIdentifier: "imei-1",
		PrimaryPort:         "/dev/cdc-wdm0",
		Ports:               []ModemPort{{PortType: ModemPortTypeQmi, Device: "/dev/cdc-wdm0"}},
		PrimarySimSlot:      1,
		Sim:                 &SIM{Identifier: "old"},
	}
	reader := &fakeQMIUIMReader{
		slotStatus: qmiTestSlotStatus(1, target.ICCID),
		cardStatus: qmiTestCardStatus(
			uim.ApplicationStateDetected,
			uim.PersonalizationStateInProgress,
			[]byte{0xA0, 0x00},
		),
		slotStatuses: []uim.SlotStatus{
			qmiTestSlotStatus(1, target.ICCID),
			qmiTestSlotStatus(1, target.ICCID),
			qmiTestSlotStatus(1, target.ICCID),
			qmiTestSlotStatus(1, target.ICCID),
			qmiTestSlotStatus(1, target.ICCID),
			qmiTestInactiveSlotStatus(1),
		},
		slotStatusErrs: []error{
			nil,
			nil,
			errors.New("slot status rejected"),
			nil,
			nil,
			nil,
		},
	}
	registry := &Registry{
		modems: map[dbus.ObjectPath]*Modem{"/modem/1": modem},
	}
	withQMIUIMReader(t, modem.PrimaryPort, 1, reader, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := registry.EnsureSIMVisible(ctx, modem, target)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("EnsureSIMVisible() error = %v, want %v", err, context.DeadlineExceeded)
	}

	wantCalls := []string{
		"slot-status", "card-status",
		"card-status", "change-provisioning:1",
		"slot-status", "card-status",
		"slot-status",
		"slot-status", "card-status",
		"slot-status", "card-status",
		"power-off:1", "slot-status", "power-on:1",
	}
	if !slices.Equal(callPrefixWithoutClose(reader.calls, len(wantCalls)), wantCalls) {
		t.Fatalf("reader calls prefix = %v, want %v", reader.calls, wantCalls)
	}
}

func TestEnsureSIMVisibleWaitsAfterRepowerWithIndependentContext(t *testing.T) {
	oldDelay := simSettleDelay
	simSettleDelay = time.Nanosecond
	t.Cleanup(func() {
		simSettleDelay = oldDelay
	})

	oldNotReadyInterval := simNotReadyRetryInterval
	simNotReadyRetryInterval = time.Nanosecond
	t.Cleanup(func() {
		simNotReadyRetryInterval = oldNotReadyInterval
	})

	oldNotReadyCount := simNotReadyRetryCount
	simNotReadyRetryCount = 1
	t.Cleanup(func() {
		simNotReadyRetryCount = oldNotReadyCount
	})

	oldPostRepowerInterval := simPostRepowerPollInterval
	simPostRepowerPollInterval = time.Nanosecond
	t.Cleanup(func() {
		simPostRepowerPollInterval = oldPostRepowerInterval
	})

	oldPostRepowerCount := simPostRepowerPollCount
	simPostRepowerPollCount = 1
	t.Cleanup(func() {
		simPostRepowerPollCount = oldPostRepowerCount
	})

	target := SIMTarget{Slot: 1, ICCID: "8986000000000000000"}
	modem := &Modem{
		EquipmentIdentifier: "imei-1",
		PrimaryPort:         "/dev/cdc-wdm0",
		Ports:               []ModemPort{{PortType: ModemPortTypeQmi, Device: "/dev/cdc-wdm0"}},
		PrimarySimSlot:      1,
		Sim:                 &SIM{Identifier: "old"},
	}
	reader := &fakeQMIUIMReader{
		slotStatus: qmiTestSlotStatus(1, target.ICCID),
		cardStatus: qmiTestCardStatus(
			uim.ApplicationStateDetected,
			uim.PersonalizationStateInProgress,
			[]byte{0xA0, 0x00},
		),
		slotStatuses: []uim.SlotStatus{
			qmiTestSlotStatus(1, target.ICCID),
			qmiTestSlotStatus(1, target.ICCID),
			qmiTestInactiveSlotStatus(1),
		},
	}
	registry := &Registry{
		modems: map[dbus.ObjectPath]*Modem{"/modem/1": modem},
	}
	withQMIUIMReader(t, modem.PrimaryPort, 1, reader, nil)

	ctx, cancel := context.WithCancel(context.Background())
	reader.afterPowerOff = func() {
		cancel()
		registry.mu.Lock()
		defer registry.mu.Unlock()
		modem.Sim = &SIM{Identifier: target.ICCID}
	}

	got, err := registry.EnsureSIMVisible(ctx, modem, target)
	if err != nil {
		t.Fatalf("EnsureSIMVisible() error = %v", err)
	}
	if got != modem {
		t.Fatalf("EnsureSIMVisible() modem = %p, want %p", got, modem)
	}
}

func TestEnsureSIMVisibleWaitsBeforeQMIProbe(t *testing.T) {
	oldDelay := simSettleDelay
	simSettleDelay = 10 * time.Millisecond
	t.Cleanup(func() {
		simSettleDelay = oldDelay
	})

	oldInterval := simVisiblePollInterval
	simVisiblePollInterval = time.Millisecond
	t.Cleanup(func() {
		simVisiblePollInterval = oldInterval
	})

	target := SIMTarget{Slot: 1, ICCID: "8986000000000000000"}
	modem := &Modem{
		EquipmentIdentifier: "imei-1",
		PrimaryPort:         "/dev/cdc-wdm0",
		Ports:               []ModemPort{{PortType: ModemPortTypeQmi, Device: "/dev/cdc-wdm0"}},
		PrimarySimSlot:      1,
		Sim:                 &SIM{Identifier: "old"},
	}
	registry := &Registry{
		modems: map[dbus.ObjectPath]*Modem{"/modem/1": modem},
	}
	reader := &fakeQMIUIMReader{}
	withQMIUIMReader(t, modem.PrimaryPort, 1, reader, nil)

	timer := time.AfterFunc(time.Millisecond, func() {
		registry.mu.Lock()
		defer registry.mu.Unlock()
		modem.Sim = &SIM{Identifier: target.ICCID}
	})
	t.Cleanup(func() {
		timer.Stop()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	got, err := registry.EnsureSIMVisible(ctx, modem, target)
	if err != nil {
		t.Fatalf("EnsureSIMVisible() error = %v", err)
	}
	if got != modem {
		t.Fatalf("EnsureSIMVisible() modem = %p, want %p", got, modem)
	}
	if len(reader.calls) != 0 {
		t.Fatalf("reader calls = %v, want none", reader.calls)
	}
}

func TestEnsureSIMVisibleSkipsQMIWhenModemIsMissing(t *testing.T) {
	oldDelay := simSettleDelay
	simSettleDelay = time.Nanosecond
	t.Cleanup(func() {
		simSettleDelay = oldDelay
	})

	oldInterval := simVisiblePollInterval
	simVisiblePollInterval = time.Millisecond
	t.Cleanup(func() {
		simVisiblePollInterval = oldInterval
	})

	target := SIMTarget{Slot: 1, ICCID: "8986000000000000000"}
	modem := &Modem{
		EquipmentIdentifier: "imei-1",
		PrimaryPort:         "/dev/cdc-wdm0",
		Ports:               []ModemPort{{PortType: ModemPortTypeQmi, Device: "/dev/cdc-wdm0"}},
		PrimarySimSlot:      1,
		Sim:                 &SIM{Identifier: target.ICCID},
	}
	registry := &Registry{
		modems: map[dbus.ObjectPath]*Modem{},
	}
	reader := &fakeQMIUIMReader{}
	withQMIUIMReader(t, modem.PrimaryPort, 1, reader, nil)

	timer := time.AfterFunc(time.Millisecond, func() {
		registry.mu.Lock()
		defer registry.mu.Unlock()
		registry.modems["/modem/1"] = modem
	})
	t.Cleanup(func() {
		timer.Stop()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	got, err := registry.EnsureSIMVisible(ctx, modem, target)
	if err != nil {
		t.Fatalf("EnsureSIMVisible() error = %v", err)
	}
	if got != modem {
		t.Fatalf("EnsureSIMVisible() modem = %p, want %p", got, modem)
	}
	if len(reader.calls) != 0 {
		t.Fatalf("reader calls = %v, want none", reader.calls)
	}
}

func callPrefixWithoutClose(calls []string, size int) []string {
	filtered := make([]string, 0, len(calls))
	for _, call := range calls {
		if call == "close" {
			continue
		}
		filtered = append(filtered, call)
		if len(filtered) == size {
			return filtered
		}
	}
	return filtered
}

func qmiTestSlotStatus(active uint8, iccid string) uim.SlotStatus {
	return qmiTestSlotStatusRawICCID(active, qmiTestICCID(iccid))
}

func qmiTestSlotStatusActiveWithSlotICCID(active, slot uint8, iccid string) uim.SlotStatus {
	slots := make([]uim.Slot, max(active, slot))
	slots[slot-1] = uim.Slot{
		PhysicalCardStatus: uim.PhysicalCardStatePresent,
		PhysicalSlotStatus: uim.SlotStateActive,
		LogicalSlot:        1,
		ICCID:              qmiTestICCID(iccid),
	}
	return uim.SlotStatus{ActiveSlot: active, Slots: slots}
}

func qmiTestSlotStatusRawICCID(active uint8, iccid []byte) uim.SlotStatus {
	slots := make([]uim.Slot, active)
	slots[active-1] = uim.Slot{
		PhysicalCardStatus: uim.PhysicalCardStatePresent,
		PhysicalSlotStatus: uim.SlotStateActive,
		LogicalSlot:        1,
		ICCID:              slices.Clone(iccid),
	}
	return uim.SlotStatus{ActiveSlot: active, Slots: slots}
}

func qmiTestICCID(iccid string) []byte {
	raw, err := simfile.ICCID(iccid).MarshalBinary()
	if err != nil {
		panic(err)
	}
	return raw
}

func TestDecodeQMIICCID(t *testing.T) {
	tests := []struct {
		name    string
		raw     []byte
		want    string
		wantErr bool
	}{
		{
			name: "swapped bcd",
			raw:  []byte{0x98, 0x68, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xF0},
			want: "8986000000000000000",
		},
		{
			name:    "invalid bcd",
			raw:     []byte{0x9A},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decodeQMIICCID(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatal("decodeQMIICCID() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("decodeQMIICCID() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("decodeQMIICCID() = %q, want %q", got, tt.want)
			}
		})
	}
}
