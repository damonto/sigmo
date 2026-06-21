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

	oldPowerCycleDelay := qmiSIMPowerCycleDelay
	qmiSIMPowerCycleDelay = time.Nanosecond
	t.Cleanup(func() {
		qmiSIMPowerCycleDelay = oldPowerCycleDelay
	})

	oldInterval := simVisiblePollInterval
	simVisiblePollInterval = time.Millisecond
	t.Cleanup(func() {
		simVisiblePollInterval = oldInterval
	})

	oldGracePeriod := simReenumerationGracePeriod
	simReenumerationGracePeriod = time.Nanosecond
	t.Cleanup(func() {
		simReenumerationGracePeriod = oldGracePeriod
	})

	target := SIMTarget{Slot: 1, ICCID: "8986000000000000000"}
	tests := []struct {
		name      string
		modem     *Modem
		target    SIMTarget
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
			name: "power cycles when QMI is target and ready but modem did not reenumerate",
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
			wantCalls: []string{"power-off:1", "power-on:1", "slot-status", "card-status"},
		},
		{
			name: "power cycles then activates provisioning when USIM is not ready",
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
			},
			timeout: 5 * time.Millisecond,
			wantErr: context.DeadlineExceeded,
			wantCalls: []string{
				"power-off:1", "power-on:1",
				"slot-status", "card-status",
				"card-status", "change-provisioning:1",
			},
		},
		{
			name:   "recovers not initialized USIM when QMI ICCID is unavailable",
			target: SIMTarget{ICCID: target.ICCID},
			modem: &Modem{
				EquipmentIdentifier: "imei-1",
				PrimaryPort:         "/dev/cdc-wdm0",
				Ports:               []ModemPort{{PortType: ModemPortTypeQmi, Device: "/dev/cdc-wdm0"}},
				PrimarySimSlot:      1,
				Sim:                 &SIM{Identifier: "old"},
			},
			reader: &fakeQMIUIMReader{
				cardStatus: qmiTestCardStatus(
					uim.ApplicationStateDetected,
					uim.PersonalizationStateUnknown,
					[]byte{0xA0, 0x00},
				),
				slotStatus: qmiTestSlotStatusRawICCID(1, nil),
			},
			timeout: 5 * time.Millisecond,
			wantErr: context.DeadlineExceeded,
			wantCalls: []string{
				"power-off:1", "power-on:1",
				"slot-status", "card-status",
				"card-status", "change-provisioning:1",
			},
		},
		{
			name:   "power cycles ready USIM when SlotStatus cannot confirm ICCID",
			target: SIMTarget{ICCID: target.ICCID},
			modem: &Modem{
				EquipmentIdentifier: "imei-1",
				PrimaryPort:         "/dev/cdc-wdm0",
				Ports:               []ModemPort{{PortType: ModemPortTypeQmi, Device: "/dev/cdc-wdm0"}},
				PrimarySimSlot:      1,
				Sim:                 &SIM{Identifier: "old"},
			},
			reader: &fakeQMIUIMReader{
				cardStatus: qmiTestCardStatus(
					uim.ApplicationStateReady,
					uim.PersonalizationStateReady,
					[]byte{0xA0, 0x00},
				),
				slotStatusErr: qcom.QMIErrorNotSupported,
			},
			timeout: 5 * time.Millisecond,
			wantErr: context.DeadlineExceeded,
			wantCalls: []string{
				"power-off:1", "power-on:1",
				"slot-status", "card-status",
			},
		},
		{
			name: "power cycles before QMI status read failure",
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
			wantCalls: []string{"power-off:1", "power-on:1", "slot-status"},
		},
		{
			name: "power cycles when QMI ICCID mismatches target",
			modem: &Modem{
				EquipmentIdentifier: "imei-1",
				PrimaryPort:         "/dev/cdc-wdm0",
				Ports:               []ModemPort{{PortType: ModemPortTypeQmi, Device: "/dev/cdc-wdm0"}},
				PrimarySimSlot:      1,
				Sim:                 &SIM{Identifier: "old"},
			},
			reader: &fakeQMIUIMReader{
				cardStatus: qmiTestCardStatus(
					uim.ApplicationStateDetected,
					uim.PersonalizationStateInProgress,
					[]byte{0xA0, 0x00},
				),
				slotStatus: qmiTestSlotStatusActiveWithSlotICCID(2, 1, "8986000000000000001"),
			},
			timeout:   5 * time.Millisecond,
			wantErr:   context.DeadlineExceeded,
			wantCalls: []string{"power-off:1", "power-on:1", "slot-status", "card-status"},
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
			wantCalls: []string{"power-off:1", "power-on:1", "slot-status"},
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
			simTarget := target
			if tt.target.valid() {
				simTarget = tt.target
			}
			ctx := context.Background()
			if tt.timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, tt.timeout)
				defer cancel()
			}

			got, err := registry.EnsureSIMVisible(ctx, tt.modem, simTarget)
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

func TestRefreshModemManagerSIMState(t *testing.T) {
	permissionErr := errors.New("permission denied")
	tests := []struct {
		name    string
		ctx     func() context.Context
		errors  map[string][]error
		wantErr error
	}{
		{
			name: "ignores ModemManager aborted refresh",
			ctx: func() context.Context {
				return context.Background()
			},
			errors: map[string][]error{
				ModemInterface + ".Simple.GetStatus": {nil},
				ModemInterface + ".Enable": {
					dbus.Error{Name: "org.freedesktop.ModemManager1.Error.Core.Aborted", Body: []any{"Operation aborted"}},
				},
			},
		},
		{
			name: "returns unexpected refresh error",
			ctx: func() context.Context {
				return context.Background()
			},
			errors: map[string][]error{
				ModemInterface + ".Simple.GetStatus": {nil},
				ModemInterface + ".Enable":           {permissionErr},
			},
			wantErr: permissionErr,
		},
		{
			name: "returns canceled context",
			ctx: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
			wantErr: context.Canceled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			object := &fakeBusObject{
				path:   "/org/freedesktop/ModemManager1/Modem/1",
				errors: tt.errors,
			}
			modem := &Modem{
				dbusObject:          object,
				objectPath:          object.path,
				EquipmentIdentifier: "imei-1",
			}

			err := refreshModemManagerSIMState(tt.ctx(), modem)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("refreshModemManagerSIMState() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("refreshModemManagerSIMState() error = %v", err)
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

func TestQMISIMStateMarksICCIDMismatchRecoverable(t *testing.T) {
	tests := []struct {
		name   string
		target SIMTarget
	}{
		{
			name:   "target iccid differs from QMI slot iccid",
			target: SIMTarget{Slot: 1, ICCID: "8986000000000000000"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			modem := &Modem{
				EquipmentIdentifier: "imei-1",
				PrimaryPort:         "/dev/cdc-wdm0",
				Ports:               []ModemPort{{PortType: ModemPortTypeQmi, Device: "/dev/cdc-wdm0"}},
				PrimarySimSlot:      1,
			}
			reader := &fakeQMIUIMReader{
				slotStatus: qmiTestSlotStatusActiveWithSlotICCID(2, 1, "8986000000000000001"),
				cardStatus: qmiTestCardStatus(
					uim.ApplicationStateDetected,
					uim.PersonalizationStateInProgress,
					[]byte{0xA0, 0x00},
				),
			}
			withQMIUIMReader(t, modem.PrimaryPort, 1, reader, nil)

			state, err := qmiSIMStateForTarget(context.Background(), modem, tt.target)
			if err != nil {
				t.Fatalf("qmiSIMStateForTarget() error = %v", err)
			}
			if state.matches {
				t.Fatal("qmiSIMStateForTarget() matches = true, want false")
			}
			if !state.recoverable {
				t.Fatal("qmiSIMStateForTarget() recoverable = false, want true")
			}
			if !state.iccidMismatch {
				t.Fatal("qmiSIMStateForTarget() iccidMismatch = false, want true")
			}
			if state.ready {
				t.Fatal("qmiSIMStateForTarget() ready = true, want false")
			}
		})
	}
}

func TestEnsureSIMVisiblePowerCyclesWhenModemDoesNotReenumerate(t *testing.T) {
	oldDelay := simSettleDelay
	simSettleDelay = time.Nanosecond
	t.Cleanup(func() {
		simSettleDelay = oldDelay
	})

	oldGracePeriod := simReenumerationGracePeriod
	simReenumerationGracePeriod = time.Nanosecond
	t.Cleanup(func() {
		simReenumerationGracePeriod = oldGracePeriod
	})

	oldPowerCycleDelay := qmiSIMPowerCycleDelay
	qmiSIMPowerCycleDelay = time.Nanosecond
	t.Cleanup(func() {
		qmiSIMPowerCycleDelay = oldPowerCycleDelay
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
		"power-off:1", "power-on:1",
		"slot-status", "card-status",
		"card-status", "change-provisioning:1",
	}
	if !slices.Equal(callPrefixWithoutClose(reader.calls, len(wantCalls)), wantCalls) {
		t.Fatalf("reader calls prefix = %v, want %v", reader.calls, wantCalls)
	}
}

func TestEnsureSIMVisibleDoesNotTreatFreshSnapshotAsReenumeration(t *testing.T) {
	oldDelay := simSettleDelay
	simSettleDelay = time.Nanosecond
	t.Cleanup(func() {
		simSettleDelay = oldDelay
	})

	oldGracePeriod := simReenumerationGracePeriod
	simReenumerationGracePeriod = time.Nanosecond
	t.Cleanup(func() {
		simReenumerationGracePeriod = oldGracePeriod
	})

	oldPowerCycleDelay := qmiSIMPowerCycleDelay
	qmiSIMPowerCycleDelay = time.Nanosecond
	t.Cleanup(func() {
		qmiSIMPowerCycleDelay = oldPowerCycleDelay
	})

	target := SIMTarget{Slot: 1, ICCID: "8986000000000000000"}
	current := &Modem{
		objectPath:          "/org/freedesktop/ModemManager1/Modem/1",
		EquipmentIdentifier: "imei-1",
		PrimaryPort:         "/dev/cdc-wdm0",
		Ports:               []ModemPort{{PortType: ModemPortTypeQmi, Device: "/dev/cdc-wdm0"}},
		PrimarySimSlot:      1,
		Sim:                 &SIM{Identifier: "old"},
	}
	registry := &Registry{
		modems: map[dbus.ObjectPath]*Modem{
			current.objectPath: {
				objectPath:          current.objectPath,
				EquipmentIdentifier: current.EquipmentIdentifier,
				PrimaryPort:         current.PrimaryPort,
				Ports:               current.Ports,
				PrimarySimSlot:      current.PrimarySimSlot,
				Sim:                 &SIM{Identifier: "old"},
			},
		},
	}
	reader := &fakeQMIUIMReader{
		slotStatus: qmiTestSlotStatus(1, target.ICCID),
		cardStatus: qmiTestCardStatus(
			uim.ApplicationStateReady,
			uim.PersonalizationStateReady,
			[]byte{0xA0, 0x00},
		),
	}
	withQMIUIMReader(t, current.PrimaryPort, 1, reader, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	_, err := registry.EnsureSIMVisible(ctx, current, target)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("EnsureSIMVisible() error = %v, want %v", err, context.DeadlineExceeded)
	}
	wantCalls := []string{"power-off:1", "power-on:1", "slot-status", "card-status"}
	if !slices.Equal(callPrefixWithoutClose(reader.calls, len(wantCalls)), wantCalls) {
		t.Fatalf("reader calls prefix = %v, want %v", reader.calls, wantCalls)
	}
}

func TestPowerCycleSIMRequiresQMI(t *testing.T) {
	registry := &Registry{}
	modem := &Modem{
		EquipmentIdentifier: "imei-1",
		PrimaryPort:         "/dev/ttyUSB0",
		Ports:               []ModemPort{{PortType: ModemPortTypeAt, Device: "/dev/ttyUSB0"}},
		PrimarySimSlot:      1,
		Sim:                 &SIM{Identifier: "8986000000000000000"},
	}

	_, err := registry.PowerCycleSIM(context.Background(), modem, SIMTarget{})
	if !errors.Is(err, errQMIRequiredForSIMPowerCycle) {
		t.Fatalf("PowerCycleSIM() error = %v, want %v", err, errQMIRequiredForSIMPowerCycle)
	}
}

func TestEnsureSIMVisibleWaitsForLateReenumerationBeforePowerCycle(t *testing.T) {
	oldSettleDelay := simSettleDelay
	simSettleDelay = time.Nanosecond
	t.Cleanup(func() {
		simSettleDelay = oldSettleDelay
	})

	oldInterval := simVisiblePollInterval
	simVisiblePollInterval = time.Millisecond
	t.Cleanup(func() {
		simVisiblePollInterval = oldInterval
	})

	oldGracePeriod := simReenumerationGracePeriod
	simReenumerationGracePeriod = 20 * time.Millisecond
	t.Cleanup(func() {
		simReenumerationGracePeriod = oldGracePeriod
	})

	target := SIMTarget{Slot: 1, ICCID: "8986000000000000000"}
	current := &Modem{
		objectPath:          "/org/freedesktop/ModemManager1/Modem/1",
		EquipmentIdentifier: "imei-1",
		PrimaryPort:         "/dev/cdc-wdm0",
		Ports:               []ModemPort{{PortType: ModemPortTypeQmi, Device: "/dev/cdc-wdm0"}},
		PrimarySimSlot:      1,
		Sim:                 &SIM{Identifier: "old"},
	}
	next := &Modem{
		objectPath:          "/org/freedesktop/ModemManager1/Modem/2",
		EquipmentIdentifier: current.EquipmentIdentifier,
		PrimaryPort:         current.PrimaryPort,
		Ports:               current.Ports,
		PrimarySimSlot:      target.Slot,
		Sim:                 &SIM{Identifier: target.ICCID},
	}
	registry := &Registry{
		modems: map[dbus.ObjectPath]*Modem{current.objectPath: current},
	}
	reader := &fakeQMIUIMReader{}
	withQMIUIMReader(t, current.PrimaryPort, 1, reader, nil)

	removeTimer := time.AfterFunc(time.Millisecond, func() {
		registry.mu.Lock()
		defer registry.mu.Unlock()
		delete(registry.modems, current.objectPath)
	})
	t.Cleanup(func() {
		removeTimer.Stop()
	})
	addTimer := time.AfterFunc(3*time.Millisecond, func() {
		registry.mu.Lock()
		defer registry.mu.Unlock()
		registry.modems[next.objectPath] = next
	})
	t.Cleanup(func() {
		addTimer.Stop()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	got, err := registry.EnsureSIMVisible(ctx, current, target)
	if err != nil {
		t.Fatalf("EnsureSIMVisible() error = %v", err)
	}
	if got != next {
		t.Fatalf("EnsureSIMVisible() modem = %p, want %p", got, next)
	}
	if len(reader.calls) != 0 {
		t.Fatalf("reader calls = %v, want none", reader.calls)
	}
}

func TestEnsureSIMVisibleSkipsInhibitWhenDisableEnableWorks(t *testing.T) {
	oldSettleDelay := simSettleDelay
	simSettleDelay = time.Nanosecond
	t.Cleanup(func() {
		simSettleDelay = oldSettleDelay
	})

	oldGracePeriod := simReenumerationGracePeriod
	simReenumerationGracePeriod = time.Nanosecond
	t.Cleanup(func() {
		simReenumerationGracePeriod = oldGracePeriod
	})

	oldPowerCycleDelay := qmiSIMPowerCycleDelay
	qmiSIMPowerCycleDelay = time.Nanosecond
	t.Cleanup(func() {
		qmiSIMPowerCycleDelay = oldPowerCycleDelay
	})

	target := SIMTarget{Slot: 1, ICCID: "8986000000000000000"}
	modem := &Modem{
		Device:              "modem-uid-1",
		EquipmentIdentifier: "imei-1",
		PrimaryPort:         "/dev/cdc-wdm0",
		Ports:               []ModemPort{{PortType: ModemPortTypeQmi, Device: "/dev/cdc-wdm0"}},
		PrimarySimSlot:      1,
		Sim:                 &SIM{Identifier: "old"},
	}
	object := &fakeBusObject{
		path: "/org/freedesktop/ModemManager1/Modem/1",
		afterCall: func(method string, args []any) {
			if method != ModemInterface+".Enable" || len(args) != 1 || args[0] != true {
				return
			}
			modem.Sim = &SIM{Identifier: target.ICCID}
		},
	}
	modem.dbusObject = object
	modem.objectPath = object.path

	var inhibitCalls []bool
	modem.inhibitDevice = func(_ context.Context, uid string, inhibit bool) error {
		if uid != modem.Device {
			t.Fatalf("inhibit uid = %q, want %q", uid, modem.Device)
		}
		inhibitCalls = append(inhibitCalls, inhibit)
		return nil
	}

	reader := &fakeQMIUIMReader{
		slotStatus: qmiTestSlotStatus(1, target.ICCID),
		cardStatus: qmiTestCardStatus(
			uim.ApplicationStateReady,
			uim.PersonalizationStateReady,
			[]byte{0xA0, 0x00},
		),
	}
	registry := &Registry{
		modems: map[dbus.ObjectPath]*Modem{object.path: modem},
	}
	withQMIUIMReader(t, modem.PrimaryPort, 1, reader, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	got, err := registry.EnsureSIMVisible(ctx, modem, target)
	if err != nil {
		t.Fatalf("EnsureSIMVisible() error = %v", err)
	}
	if got != modem {
		t.Fatalf("EnsureSIMVisible() modem = %p, want %p", got, modem)
	}
	if !slices.Equal(inhibitCalls, nil) {
		t.Fatalf("inhibit calls = %v, want none", inhibitCalls)
	}
	wantCalls := []string{ModemInterface + ".Simple.GetStatus", ModemInterface + ".Enable", ModemInterface + ".Enable"}
	if !slices.Equal(object.calls, wantCalls) {
		t.Fatalf("dbus calls = %v, want %v", object.calls, wantCalls)
	}
}

func TestEnsureSIMVisibleInhibitsAfterDisableEnableDoesNotWork(t *testing.T) {
	oldSettleDelay := simSettleDelay
	simSettleDelay = time.Nanosecond
	t.Cleanup(func() {
		simSettleDelay = oldSettleDelay
	})

	oldGracePeriod := simReenumerationGracePeriod
	simReenumerationGracePeriod = time.Nanosecond
	t.Cleanup(func() {
		simReenumerationGracePeriod = oldGracePeriod
	})

	oldPowerCycleDelay := qmiSIMPowerCycleDelay
	qmiSIMPowerCycleDelay = time.Nanosecond
	t.Cleanup(func() {
		qmiSIMPowerCycleDelay = oldPowerCycleDelay
	})

	oldReloadSettleDelay := modemReloadSettleDelay
	modemReloadSettleDelay = time.Nanosecond
	t.Cleanup(func() {
		modemReloadSettleDelay = oldReloadSettleDelay
	})

	target := SIMTarget{Slot: 1, ICCID: "8986000000000000000"}
	modem := &Modem{
		Device:              "modem-uid-1",
		EquipmentIdentifier: "imei-1",
		PrimaryPort:         "/dev/cdc-wdm0",
		Ports:               []ModemPort{{PortType: ModemPortTypeQmi, Device: "/dev/cdc-wdm0"}},
		PrimarySimSlot:      1,
		Sim:                 &SIM{Identifier: "old"},
	}
	var events []string
	object := &fakeBusObject{
		path: "/org/freedesktop/ModemManager1/Modem/1",
		afterCall: func(method string, args []any) {
			if method != ModemInterface+".Enable" || len(args) != 1 {
				return
			}
			events = append(events, fmtBoolEvent("enable", args[0].(bool)))
		},
	}
	modem.dbusObject = object
	modem.objectPath = object.path
	modem.inhibitDevice = func(_ context.Context, uid string, inhibit bool) error {
		if uid != modem.Device {
			t.Fatalf("inhibit uid = %q, want %q", uid, modem.Device)
		}
		events = append(events, fmtBoolEvent("inhibit", inhibit))
		if !inhibit {
			modem.Sim = &SIM{Identifier: target.ICCID}
		}
		return nil
	}

	reader := &fakeQMIUIMReader{
		slotStatus: qmiTestSlotStatus(1, target.ICCID),
		cardStatus: qmiTestCardStatus(
			uim.ApplicationStateReady,
			uim.PersonalizationStateReady,
			[]byte{0xA0, 0x00},
		),
	}
	registry := &Registry{
		modems: map[dbus.ObjectPath]*Modem{object.path: modem},
	}
	withQMIUIMReader(t, modem.PrimaryPort, 1, reader, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	got, err := registry.EnsureSIMVisible(ctx, modem, target)
	if err != nil {
		t.Fatalf("EnsureSIMVisible() error = %v", err)
	}
	if got != modem {
		t.Fatalf("EnsureSIMVisible() modem = %p, want %p", got, modem)
	}
	wantEvents := []string{"enable:false", "enable:true", "inhibit:true", "inhibit:false"}
	if !slices.Equal(events, wantEvents) {
		t.Fatalf("events = %v, want %v", events, wantEvents)
	}
}

func TestPowerCycleSIMRefreshesWithoutSecondPowerCycle(t *testing.T) {
	oldSettleDelay := simSettleDelay
	simSettleDelay = time.Nanosecond
	t.Cleanup(func() {
		simSettleDelay = oldSettleDelay
	})

	oldPowerCycleDelay := qmiSIMPowerCycleDelay
	qmiSIMPowerCycleDelay = time.Nanosecond
	t.Cleanup(func() {
		qmiSIMPowerCycleDelay = oldPowerCycleDelay
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
			uim.ApplicationStateReady,
			uim.PersonalizationStateReady,
			[]byte{0xA0, 0x00},
		),
	}
	registry := &Registry{
		modems: map[dbus.ObjectPath]*Modem{"/modem/1": modem},
	}
	withQMIUIMReader(t, modem.PrimaryPort, 1, reader, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	_, err := registry.PowerCycleSIM(ctx, modem, target)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("PowerCycleSIM() error = %v, want %v", err, context.DeadlineExceeded)
	}
	wantCalls := []string{"power-off:1", "power-on:1", "slot-status", "card-status"}
	if !slices.Equal(callPrefixWithoutClose(reader.calls, len(wantCalls)), wantCalls) {
		t.Fatalf("reader calls prefix = %v, want %v", reader.calls, wantCalls)
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

func fmtBoolEvent(action string, value bool) string {
	if value {
		return action + ":true"
	}
	return action + ":false"
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
