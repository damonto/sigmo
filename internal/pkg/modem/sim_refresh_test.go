package modem

import (
	"context"
	"errors"
	"testing"
	"time"

	devicewwan "github.com/damonto/sigmo/internal/pkg/modem/wwan"
	wwanmodem "github.com/damonto/wwan-go/modem"
)

type simRefreshDevice struct {
	*fakeDeviceControl
	onActivate func()
	onPower    func()
}

func (d *simRefreshDevice) ActivateProvisioningIfSIMMissing(context.Context) error {
	d.calls = append(d.calls, "activate-provisioning")
	if d.onActivate != nil {
		d.onActivate()
	}
	return d.activateErr
}

func (d *simRefreshDevice) PowerCycleSIM(context.Context) error {
	d.calls = append(d.calls, "power-cycle")
	if d.onPower != nil {
		d.onPower()
	}
	return d.powerErr
}

func TestEnsureSIMVisibleProvisionsEachGenerationOnce(t *testing.T) {
	restoreSIMRefreshTiming(t, 0, time.Millisecond, time.Hour)
	const path = "/sys/devices/modem-1"
	current := simRefreshTestModem(path, 1, false)
	replacement := simRefreshTestModem(path, 2, false)
	registry := &Registry{modems: map[string]*Modem{path: current}, started: true}
	device := &simRefreshDevice{fakeDeviceControl: &fakeDeviceControl{}}
	activateCalls := 0
	device.onActivate = func() {
		activateCalls++
		registry.mu.Lock()
		defer registry.mu.Unlock()
		switch activateCalls {
		case 1:
			registry.modems[path] = replacement
		case 2:
			device.state = devicewwan.SIMState{
				Supported: true,
				Matches:   true,
				Ready:     true,
				ICCID:     "8901000000000000001",
				Slot:      1,
			}
		}
	}
	registry.openDevice = fakeDeviceOpener(t, device, nil)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := registry.ensureSIMVisible(ctx, current, SIMTarget{Slot: 1, ICCID: "8901000000000000001"}, false, false)
	if err != nil {
		t.Fatalf("ensureSIMVisible() error = %v", err)
	}
	if result.Modem != replacement || !result.ReloadObserved {
		t.Fatalf("result = %+v, want replacement generation", result)
	}
	if activateCalls != 2 {
		t.Fatalf("provisioning calls = %d, want one for each generation", activateCalls)
	}
}

func TestEnsureSIMVisiblePowerCyclesAfterGracePeriod(t *testing.T) {
	restoreSIMRefreshTiming(t, 0, time.Millisecond, 0)
	const path = "/sys/devices/modem-1"
	current := simRefreshTestModem(path, 1, false)
	registry := &Registry{modems: map[string]*Modem{path: current}, started: true}
	device := &simRefreshDevice{fakeDeviceControl: &fakeDeviceControl{}}
	activateCalls := 0
	powerCalls := 0
	device.onActivate = func() { activateCalls++ }
	device.onPower = func() {
		powerCalls++
		device.state = devicewwan.SIMState{
			Supported: true,
			Matches:   true,
			Ready:     true,
			ICCID:     "8901000000000000001",
			Slot:      1,
		}
	}
	registry.openDevice = fakeDeviceOpener(t, device, nil)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := registry.ensureSIMVisible(ctx, current, SIMTarget{Slot: 1, ICCID: "8901000000000000001"}, true, false)
	if err != nil {
		t.Fatalf("ensureSIMVisible() error = %v", err)
	}
	if result.Modem != current {
		t.Fatalf("result modem = %p, want current modem %p", result.Modem, current)
	}
	if activateCalls != 1 || powerCalls != 1 {
		t.Fatalf("provision/power calls = %d/%d, want 1/1", activateCalls, powerCalls)
	}
}

func TestReadCurrentESIMRequiresMatchingReadyState(t *testing.T) {
	const path = "/sys/devices/modem-1"
	const iccid = "8901000000000000001"
	modem := simRefreshTestModem(path, 1, false)
	registry := &Registry{modems: map[string]*Modem{path: modem}, started: true}
	device := &fakeDeviceControl{state: devicewwan.SIMState{Supported: true, Matches: true, ICCID: iccid, Slot: 1}}
	registry.openDevice = fakeDeviceOpener(t, device, nil)

	read, err := registry.readCurrentModem(context.Background(), modem, SIMTarget{ICCID: iccid})
	if err != nil {
		t.Fatalf("readCurrentModem() error = %v", err)
	}
	if read.SIMVisible {
		t.Fatal("SIMVisible = true, want false while USIM is not ready")
	}
	if snapshot := modem.Snapshot(); snapshot.SIM == nil || snapshot.SIM.Identifier != iccid {
		t.Fatalf("cached SIM = %+v, want ICCID %q", snapshot.SIM, iccid)
	}
	if len(device.calls) != 1 || device.calls[0] != "sim-state" {
		t.Fatalf("device calls = %v, want [sim-state]", device.calls)
	}

	device.state.Ready = true
	read, err = registry.readCurrentModem(context.Background(), modem, SIMTarget{ICCID: iccid})
	if err != nil {
		t.Fatalf("second readCurrentModem() error = %v", err)
	}
	if !read.SIMVisible {
		t.Fatal("SIMVisible = false, want true for matching ready USIM")
	}
}

func TestReadCurrentESIMUsesMBIMDevice(t *testing.T) {
	const path = "/sys/devices/modem-1"
	const iccid = "8901000000000000001"
	modem := simRefreshTestModem(path, 1, false)
	modem.Ports = []ModemPort{{PortType: wwanmodem.PortMBIM, Device: "/dev/cdc-wdm0"}}
	registry := &Registry{modems: map[string]*Modem{path: modem}, started: true}
	device := &fakeDeviceControl{state: devicewwan.SIMState{Supported: true, Matches: true, Ready: true, ICCID: iccid, Slot: 1}}
	var gotCfg devicewwan.Config
	registry.openDevice = func(cfg devicewwan.Config) (deviceControl, error) {
		gotCfg = cfg
		return device, nil
	}

	read, err := registry.readCurrentModem(context.Background(), modem, SIMTarget{ICCID: iccid})
	if err != nil {
		t.Fatalf("readCurrentModem() error = %v", err)
	}
	if !read.SIMVisible {
		t.Fatal("SIMVisible = false, want true")
	}
	if gotCfg.PortType != devicewwan.PortTypeMBIM {
		t.Fatalf("opened port type = %d, want MBIM", gotCfg.PortType)
	}
	if gotCfg.Device != "/dev/cdc-wdm0" || gotCfg.Slot != 1 {
		t.Fatalf("opened device config = %+v", gotCfg)
	}
}

func TestReadCurrentESIMReturnsSIMStateError(t *testing.T) {
	const path = "/sys/devices/modem-1"
	modem := simRefreshTestModem(path, 1, false)
	registry := &Registry{modems: map[string]*Modem{path: modem}, started: true}
	wantErr := errors.New("UIM unavailable")
	registry.openDevice = fakeDeviceOpener(t, &fakeDeviceControl{stateErr: wantErr}, nil)

	_, err := registry.readCurrentModem(context.Background(), modem, SIMTarget{ICCID: "8901000000000000001"})
	if !errors.Is(err, wantErr) {
		t.Fatalf("readCurrentModem() error = %v, want %v", err, wantErr)
	}
}

func restoreSIMRefreshTiming(t *testing.T, settle, poll, grace time.Duration) {
	t.Helper()
	previousSettle := simSettleDelay
	previousPoll := simVisiblePollInterval
	previousGrace := simReenumerationGracePeriod
	simSettleDelay = settle
	simVisiblePollInterval = poll
	simReenumerationGracePeriod = grace
	t.Cleanup(func() {
		simSettleDelay = previousSettle
		simVisiblePollInterval = previousPoll
		simReenumerationGracePeriod = previousGrace
	})
}

func simRefreshTestModem(path string, generation uint64, visible bool) *Modem {
	modem := &Modem{
		deviceKey:           path,
		generation:          generation,
		EquipmentIdentifier: "imei-1",
		PrimaryPort:         "/dev/cdc-wdm0",
		PrimarySimSlot:      1,
		SimSlots:            []uint32{1},
		Ports:               []ModemPort{{PortType: wwanmodem.PortQMI, Device: "/dev/cdc-wdm0"}},
	}
	if visible {
		modem.Sim = &SIM{modem: modem, Slot: 1, Active: true, Identifier: "8901000000000000001"}
	}
	return modem
}
