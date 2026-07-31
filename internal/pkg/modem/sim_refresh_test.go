package modem

import (
	"context"
	"testing"
	"time"
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
			replacement.runtimeMu.Lock()
			replacement.Sim = &SIM{modem: replacement, Slot: 1, Active: true, Identifier: "8901000000000000001"}
			replacement.runtimeMu.Unlock()
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
		current.runtimeMu.Lock()
		current.Sim = &SIM{modem: current, Slot: 1, Active: true, Identifier: "8901000000000000001"}
		current.runtimeMu.Unlock()
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
		Ports:               []ModemPort{{PortType: ModemPortTypeQmi, Device: "/dev/cdc-wdm0"}},
	}
	if visible {
		modem.Sim = &SIM{modem: modem, Slot: 1, Active: true, Identifier: "8901000000000000001"}
	}
	return modem
}
