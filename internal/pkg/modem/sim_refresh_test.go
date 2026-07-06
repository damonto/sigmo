package modem

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	mdevice "github.com/damonto/sigmo/internal/pkg/modem/device"
	"github.com/godbus/dbus/v5"
)

func TestEnsureSIMVisible(t *testing.T) {
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

	oldGracePeriod := simReenumerationGracePeriod
	simReenumerationGracePeriod = 0
	t.Cleanup(func() {
		simReenumerationGracePeriod = oldGracePeriod
	})

	target := SIMTarget{Slot: 1, ICCID: "8986000000000000000"}
	tests := []struct {
		name      string
		modem     *Modem
		target    SIMTarget
		device    *fakeDeviceControl
		timeout   time.Duration
		wantErr   error
		wantCalls []string
	}{
		{
			name: "returns visible modem without device access",
			modem: &Modem{
				EquipmentIdentifier: "imei-1",
				PrimarySimSlot:      1,
				Sim:                 &SIM{Identifier: target.ICCID},
			},
		},
		{
			name: "power cycles when device is target and ready but modem did not reenumerate",
			modem: &Modem{
				dbusObject:          &fakeBusObject{errors: map[string][]error{ModemInterface + ".Simple.GetStatus": {nil}}},
				EquipmentIdentifier: "imei-1",
				PrimaryPort:         "/dev/cdc-wdm0",
				Ports:               []ModemPort{{PortType: ModemPortTypeQmi, Device: "/dev/cdc-wdm0"}},
				PrimarySimSlot:      1,
				Sim:                 &SIM{Identifier: "old"},
			},
			device: &fakeDeviceControl{
				state: mdevice.SIMState{Supported: true, Matches: true, Recoverable: true, Ready: true, Slot: 1, ICCID: target.ICCID},
			},
			timeout:   5 * time.Millisecond,
			wantErr:   context.DeadlineExceeded,
			wantCalls: []string{"power-cycle", "sim-state"},
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
			device: &fakeDeviceControl{
				state: mdevice.SIMState{Supported: true, Matches: true, Recoverable: true, Slot: 1, ICCID: target.ICCID},
			},
			timeout:   5 * time.Millisecond,
			wantErr:   context.DeadlineExceeded,
			wantCalls: []string{"power-cycle", "sim-state", "activate-provisioning"},
		},
		{
			name:   "recovers not initialized USIM when device ICCID is unavailable",
			target: SIMTarget{ICCID: target.ICCID},
			modem: &Modem{
				EquipmentIdentifier: "imei-1",
				PrimaryPort:         "/dev/cdc-wdm0",
				Ports:               []ModemPort{{PortType: ModemPortTypeQmi, Device: "/dev/cdc-wdm0"}},
				PrimarySimSlot:      1,
				Sim:                 &SIM{Identifier: "old"},
			},
			device: &fakeDeviceControl{
				state: mdevice.SIMState{Supported: true, Recoverable: true, Slot: 1},
			},
			timeout:   5 * time.Millisecond,
			wantErr:   context.DeadlineExceeded,
			wantCalls: []string{"power-cycle", "sim-state", "activate-provisioning"},
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
			device: &fakeDeviceControl{
				state: mdevice.SIMState{Supported: true, Recoverable: true, Ready: true, Slot: 1},
			},
			timeout:   5 * time.Millisecond,
			wantErr:   context.DeadlineExceeded,
			wantCalls: []string{"power-cycle", "sim-state"},
		},
		{
			name: "power cycles before device status read failure",
			modem: &Modem{
				EquipmentIdentifier: "imei-1",
				PrimaryPort:         "/dev/cdc-wdm0",
				Ports:               []ModemPort{{PortType: ModemPortTypeQmi, Device: "/dev/cdc-wdm0"}},
				PrimarySimSlot:      1,
				Sim:                 &SIM{Identifier: "old"},
			},
			device: &fakeDeviceControl{
				stateErr: errors.New("slot status rejected"),
			},
			timeout:   5 * time.Millisecond,
			wantErr:   context.DeadlineExceeded,
			wantCalls: []string{"power-cycle", "sim-state"},
		},
		{
			name: "power cycles when device ICCID mismatches target",
			modem: &Modem{
				EquipmentIdentifier: "imei-1",
				PrimaryPort:         "/dev/cdc-wdm0",
				Ports:               []ModemPort{{PortType: ModemPortTypeQmi, Device: "/dev/cdc-wdm0"}},
				PrimarySimSlot:      1,
				Sim:                 &SIM{Identifier: "old"},
			},
			device: &fakeDeviceControl{
				state: mdevice.SIMState{Supported: true, Recoverable: true, ICCIDMismatch: true, Slot: 1, ICCID: "8986000000000000001"},
			},
			timeout:   5 * time.Millisecond,
			wantErr:   context.DeadlineExceeded,
			wantCalls: []string{"power-cycle", "sim-state"},
		},
		{
			name: "does not recover SIM when device ICCID is invalid",
			modem: &Modem{
				EquipmentIdentifier: "imei-1",
				PrimaryPort:         "/dev/cdc-wdm0",
				Ports:               []ModemPort{{PortType: ModemPortTypeQmi, Device: "/dev/cdc-wdm0"}},
				PrimarySimSlot:      1,
				Sim:                 &SIM{Identifier: "old"},
			},
			device: &fakeDeviceControl{
				stateErr: errors.New("decode ICCID"),
			},
			timeout:   5 * time.Millisecond,
			wantErr:   context.DeadlineExceeded,
			wantCalls: []string{"power-cycle", "sim-state"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := &Registry{
				modems: map[dbus.ObjectPath]*Modem{"/modem/1": tt.modem},
			}
			if tt.device != nil {
				registry.openDevice = fakeDeviceOpener(t, tt.device, nil)
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
			if tt.device != nil && !slices.Equal(tt.device.calls[:min(len(tt.device.calls), len(tt.wantCalls))], tt.wantCalls) {
				t.Fatalf("device calls prefix = %v, want %v", tt.device.calls, tt.wantCalls)
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

func TestDeviceSIMTargetDoesNotMatchSlotOnlyTargetWithoutSlotStatus(t *testing.T) {
	modem := &Modem{
		EquipmentIdentifier: "imei-1",
		PrimaryPort:         "/dev/cdc-wdm0",
		Ports:               []ModemPort{{PortType: ModemPortTypeQmi, Device: "/dev/cdc-wdm0"}},
		PrimarySimSlot:      1,
	}
	device := &fakeDeviceControl{
		state: mdevice.SIMState{Supported: true, Ready: true, Slot: 1},
	}
	openDevice := fakeDeviceOpener(t, device, nil)

	state, err := readDeviceSIMState(context.Background(), modem, SIMTarget{Slot: 1}, openDevice)
	if err != nil {
		t.Fatalf("readDeviceSIMState() error = %v", err)
	}
	if !state.Supported {
		t.Fatal("readDeviceSIMState() supported = false, want true")
	}
	if !state.Ready {
		t.Fatal("readDeviceSIMState() ready = false, want true")
	}
	if state.Matches {
		t.Fatal("readDeviceSIMState() matches = true, want false")
	}
}

func TestDeviceSIMStateMarksICCIDMismatchRecoverable(t *testing.T) {
	tests := []struct {
		name   string
		target SIMTarget
	}{
		{
			name:   "target iccid differs from device slot iccid",
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
			device := &fakeDeviceControl{
				state: mdevice.SIMState{Supported: true, Recoverable: true, ICCIDMismatch: true, Slot: 1, ICCID: "8986000000000000001"},
			}
			openDevice := fakeDeviceOpener(t, device, nil)

			state, err := readDeviceSIMState(context.Background(), modem, tt.target, openDevice)
			if err != nil {
				t.Fatalf("readDeviceSIMState() error = %v", err)
			}
			if state.Matches {
				t.Fatal("readDeviceSIMState() matches = true, want false")
			}
			if !state.Recoverable {
				t.Fatal("readDeviceSIMState() recoverable = false, want true")
			}
			if !state.ICCIDMismatch {
				t.Fatal("readDeviceSIMState() iccidMismatch = false, want true")
			}
			if state.Ready {
				t.Fatal("readDeviceSIMState() ready = true, want false")
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
	simReenumerationGracePeriod = 0
	t.Cleanup(func() {
		simReenumerationGracePeriod = oldGracePeriod
	})

	oldInterval := simVisiblePollInterval
	simVisiblePollInterval = time.Nanosecond
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
	device := &fakeDeviceControl{
		state: mdevice.SIMState{Supported: true, Matches: true, Recoverable: true, Slot: 1, ICCID: target.ICCID},
	}
	registry := &Registry{
		modems:     map[dbus.ObjectPath]*Modem{"/modem/1": modem},
		openDevice: fakeDeviceOpener(t, device, nil),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := registry.EnsureSIMVisible(ctx, modem, target)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("EnsureSIMVisible() error = %v, want %v", err, context.DeadlineExceeded)
	}

	wantCalls := []string{"power-cycle", "sim-state", "activate-provisioning"}
	if !slices.Equal(device.calls[:min(len(device.calls), len(wantCalls))], wantCalls) {
		t.Fatalf("device calls prefix = %v, want %v", device.calls, wantCalls)
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

	oldInterval := simVisiblePollInterval
	simVisiblePollInterval = time.Nanosecond
	t.Cleanup(func() {
		simVisiblePollInterval = oldInterval
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
	device := &fakeDeviceControl{
		state: mdevice.SIMState{Supported: true, Matches: true, Recoverable: true, Ready: true, Slot: 1, ICCID: target.ICCID},
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
		openDevice: fakeDeviceOpener(t, device, nil),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	_, err := registry.EnsureSIMVisible(ctx, current, target)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("EnsureSIMVisible() error = %v, want %v", err, context.DeadlineExceeded)
	}
	wantCalls := []string{"power-cycle", "sim-state"}
	if !slices.Equal(device.calls[:min(len(device.calls), len(wantCalls))], wantCalls) {
		t.Fatalf("device calls prefix = %v, want %v", device.calls, wantCalls)
	}
}

func TestPowerCycleSIMRequiresDevice(t *testing.T) {
	registry := &Registry{}
	modem := &Modem{
		EquipmentIdentifier: "imei-1",
		PrimaryPort:         "/dev/ttyUSB0",
		Ports:               []ModemPort{{PortType: ModemPortTypeAt, Device: "/dev/ttyUSB0"}},
		PrimarySimSlot:      1,
		Sim:                 &SIM{Identifier: "8986000000000000000"},
	}

	_, err := registry.PowerCycleSIM(context.Background(), modem, SIMTarget{})
	if !errors.Is(err, mdevice.ErrUnsupported) {
		t.Fatalf("PowerCycleSIM() error = %v, want %v", err, mdevice.ErrUnsupported)
	}
}

func TestCurrentSIMTarget(t *testing.T) {
	tests := []struct {
		name   string
		modem  *Modem
		target SIMTarget
		want   SIMTarget
	}{
		{
			name:   "keeps explicit target",
			modem:  testSIMTargetModem(ModemPortTypeQmi),
			target: SIMTarget{Slot: 2, ICCID: "8986000000000000001"},
			want:   SIMTarget{Slot: 2, ICCID: "8986000000000000001"},
		},
		{
			name: "uses modem primary SIM",
			modem: &Modem{
				PrimaryPort:    "/dev/cdc-wdm0",
				Ports:          []ModemPort{{PortType: ModemPortTypeQmi, Device: "/dev/cdc-wdm0"}},
				PrimarySimSlot: 2,
				Sim:            &SIM{Identifier: "8986000000000000002"},
			},
			want: SIMTarget{Slot: 2, ICCID: "8986000000000000002"},
		},
		{
			name:  "defaults QMI device slot",
			modem: testSIMTargetModem(ModemPortTypeQmi),
			want:  SIMTarget{Slot: 1},
		},
		{
			name:  "defaults MBIM device slot",
			modem: testSIMTargetModem(ModemPortTypeMbim),
			want:  SIMTarget{Slot: 1},
		},
		{
			name:  "does not default AT slot",
			modem: testSIMTargetModem(ModemPortTypeAt),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := currentSIMTarget(tt.modem, tt.target); got != tt.want {
				t.Fatalf("currentSIMTarget() = %#v, want %#v", got, tt.want)
			}
		})
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
	device := &fakeDeviceControl{}
	registry := &Registry{
		modems:     map[dbus.ObjectPath]*Modem{current.objectPath: current},
		openDevice: fakeDeviceOpener(t, device, nil),
	}

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
	if len(device.calls) != 0 {
		t.Fatalf("device calls = %v, want none", device.calls)
	}
}

func TestEnsureSIMVisibleSkipsInhibitWhenDisableEnableWorks(t *testing.T) {
	oldSettleDelay := simSettleDelay
	simSettleDelay = time.Nanosecond
	t.Cleanup(func() {
		simSettleDelay = oldSettleDelay
	})

	oldGracePeriod := simReenumerationGracePeriod
	simReenumerationGracePeriod = 0
	t.Cleanup(func() {
		simReenumerationGracePeriod = oldGracePeriod
	})

	oldInterval := simVisiblePollInterval
	simVisiblePollInterval = time.Nanosecond
	t.Cleanup(func() {
		simVisiblePollInterval = oldInterval
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

	device := &fakeDeviceControl{
		state: mdevice.SIMState{Supported: true, Matches: true, Recoverable: true, Ready: true, Slot: 1, ICCID: target.ICCID},
	}
	registry := &Registry{
		modems:     map[dbus.ObjectPath]*Modem{object.path: modem},
		openDevice: fakeDeviceOpener(t, device, nil),
	}

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
	simReenumerationGracePeriod = 0
	t.Cleanup(func() {
		simReenumerationGracePeriod = oldGracePeriod
	})

	oldInterval := simVisiblePollInterval
	simVisiblePollInterval = time.Nanosecond
	t.Cleanup(func() {
		simVisiblePollInterval = oldInterval
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

	device := &fakeDeviceControl{
		state: mdevice.SIMState{Supported: true, Matches: true, Recoverable: true, Ready: true, Slot: 1, ICCID: target.ICCID},
	}
	registry := &Registry{
		modems:     map[dbus.ObjectPath]*Modem{object.path: modem},
		openDevice: fakeDeviceOpener(t, device, nil),
	}

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

	target := SIMTarget{Slot: 1, ICCID: "8986000000000000000"}
	modem := &Modem{
		EquipmentIdentifier: "imei-1",
		PrimaryPort:         "/dev/cdc-wdm0",
		Ports:               []ModemPort{{PortType: ModemPortTypeQmi, Device: "/dev/cdc-wdm0"}},
		PrimarySimSlot:      1,
		Sim:                 &SIM{Identifier: "old"},
	}
	device := &fakeDeviceControl{
		state: mdevice.SIMState{Supported: true, Matches: true, Recoverable: true, Ready: true, Slot: 1, ICCID: target.ICCID},
	}
	registry := &Registry{
		modems:     map[dbus.ObjectPath]*Modem{"/modem/1": modem},
		openDevice: fakeDeviceOpener(t, device, nil),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	_, err := registry.PowerCycleSIM(ctx, modem, target)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("PowerCycleSIM() error = %v, want %v", err, context.DeadlineExceeded)
	}
	wantCalls := []string{"power-cycle", "sim-state"}
	if !slices.Equal(device.calls[:min(len(device.calls), len(wantCalls))], wantCalls) {
		t.Fatalf("device calls prefix = %v, want %v", device.calls, wantCalls)
	}
}

func TestEnsureSIMVisibleWaitsBeforeDeviceProbe(t *testing.T) {
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
	device := &fakeDeviceControl{}
	registry := &Registry{
		modems:     map[dbus.ObjectPath]*Modem{"/modem/1": modem},
		openDevice: fakeDeviceOpener(t, device, nil),
	}

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
	if len(device.calls) != 0 {
		t.Fatalf("device calls = %v, want none", device.calls)
	}
}

func TestEnsureSIMVisibleSkipsDeviceWhenModemIsMissing(t *testing.T) {
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
	device := &fakeDeviceControl{}
	registry := &Registry{
		modems:     map[dbus.ObjectPath]*Modem{},
		openDevice: fakeDeviceOpener(t, device, nil),
	}

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
	if len(device.calls) != 0 {
		t.Fatalf("device calls = %v, want none", device.calls)
	}
}

func fmtBoolEvent(action string, value bool) string {
	if value {
		return action + ":true"
	}
	return action + ":false"
}

func testSIMTargetModem(portType ModemPortType) *Modem {
	return &Modem{
		PrimaryPort: "/dev/cdc-wdm0",
		Ports:       []ModemPort{{PortType: portType, Device: "/dev/cdc-wdm0"}},
	}
}
