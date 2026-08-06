package modem

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	devicewwan "github.com/damonto/sigmo/internal/pkg/modem/wwan"
	wwanmodem "github.com/damonto/wwan-go/modem"
)

type simRefreshDevice struct {
	*fakeDeviceControl
	onActivate func()
	onSIMState func(context.Context, devicewwan.Target) (devicewwan.SIMState, error)
}

func (d *simRefreshDevice) ActivateProvisioningIfSIMMissing(context.Context) error {
	d.calls = append(d.calls, "activate-provisioning")
	err := d.activateErr
	if d.onActivate != nil {
		d.onActivate()
	}
	return err
}

func (d *simRefreshDevice) SIMState(ctx context.Context, target devicewwan.Target) (devicewwan.SIMState, error) {
	if d.onSIMState == nil {
		return d.fakeDeviceControl.SIMState(ctx, target)
	}
	d.calls = append(d.calls, "sim-state")
	return d.onSIMState(ctx, target)
}

func TestEnsureSIMVisibleSkipsPowerCycleWhenSIMIsVisible(t *testing.T) {
	restoreSIMRefreshTiming(t, 0, time.Millisecond)
	const path = "/sys/devices/modem-1"
	current := simRefreshTestModem(path, 1, false)
	registry := &Registry{modems: map[string]*Modem{path: current}, started: true}
	device := &simRefreshDevice{fakeDeviceControl: &fakeDeviceControl{state: devicewwan.SIMState{
		Supported: true,
		Matches:   true,
		Ready:     true,
		ICCID:     "8901000000000000001",
		Slot:      1,
	}}}
	registry.openDevice = fakeDeviceOpener(t, device, nil)

	result, err := registry.ensureSIMVisible(t.Context(), current, SIMTarget{Slot: 1, ICCID: "8901000000000000001"})
	if err != nil {
		t.Fatalf("ensureSIMVisible() error = %v", err)
	}
	if result.Modem != current {
		t.Fatalf("result modem = %p, want current modem %p", result.Modem, current)
	}
	if !slices.Equal(device.calls, []string{"sim-state"}) {
		t.Fatalf("device calls = %v, want only SIM state probe", device.calls)
	}
}

func TestEnsureSIMVisiblePublishesSIMProfileChange(t *testing.T) {
	restoreSIMRefreshTiming(t, 0, time.Millisecond)
	const (
		path     = "/sys/devices/modem-1"
		previous = "8901000000000000001"
		next     = "8901000000000000002"
	)
	current := simRefreshTestModem(path, 7, true)
	registry := &Registry{modems: map[string]*Modem{path: current}, started: true}
	device := &simRefreshDevice{fakeDeviceControl: &fakeDeviceControl{state: devicewwan.SIMState{
		Supported: true,
		Matches:   true,
		Ready:     true,
		ICCID:     next,
		Slot:      1,
	}}}
	registry.openDevice = fakeDeviceOpener(t, device, nil)
	var events []ModemEvent
	registry.subs = []subscription{{id: 1, fn: func(event ModemEvent) error {
		events = append(events, event)
		return nil
	}}}

	result, err := registry.EnsureSIMVisible(t.Context(), current, SIMTarget{Slot: 1, ICCID: next})
	if err != nil {
		t.Fatalf("EnsureSIMVisible() error = %v", err)
	}
	if result != current {
		t.Fatalf("result modem = %p, want %p", result, current)
	}
	if len(events) != 1 {
		t.Fatalf("published events = %d, want 1", len(events))
	}
	event := events[0]
	if event.Type != ModemEventSIMChanged || event.Modem != current || event.Path != path || event.Generation != 7 {
		t.Fatalf("event = %+v, want SIM change for current modem", event)
	}
	if event.PreviousSIMIdentifier != previous || event.SIMIdentifier != next {
		t.Fatalf("event SIM transition = %q -> %q, want %q -> %q", event.PreviousSIMIdentifier, event.SIMIdentifier, previous, next)
	}
}

func TestEnsureSIMVisibleWaitsForEUICCClassification(t *testing.T) {
	restoreSIMRefreshTiming(t, 0, time.Millisecond)
	const (
		path  = "/sys/devices/modem-1"
		iccid = "8901000000000000002"
	)
	modem := simRefreshTestModem(path, 1, true)
	registry := &Registry{modems: map[string]*Modem{path: modem}, started: true}
	state := devicewwan.SIMState{
		Supported: true,
		Matches:   true,
		Ready:     true,
		ICCID:     iccid,
		Slot:      1,
	}
	probes := 0
	device := &simRefreshDevice{fakeDeviceControl: &fakeDeviceControl{}}
	device.onSIMState = func(context.Context, devicewwan.Target) (devicewwan.SIMState, error) {
		probes++
		if probes == 2 {
			modem.applySIMInfo(wwanmodem.SIMInfo{
				Slot:  1,
				State: wwanmodem.SIMStateReady,
				ICCID: iccid,
				ATR:   []byte{0x3B, 0x80, 0x81, 0x2F, 0x82, 0xAC},
			})
		}
		return state, nil
	}
	registry.openDevice = fakeDeviceOpener(t, device, nil)

	result, err := registry.ensureSIMVisible(t.Context(), modem, SIMTarget{
		ICCID:        iccid,
		RequireEUICC: true,
	})
	if err != nil {
		t.Fatalf("ensureSIMVisible() error = %v", err)
	}
	if result.Modem != modem || probes != 2 {
		t.Fatalf("result modem/probes = %p/%d, want %p/2", result.Modem, probes, modem)
	}
	if kind := modem.Snapshot().SIMKind(); kind != SIMKindEUICC {
		t.Fatalf("SIM kind = %q, want %q", kind, SIMKindEUICC)
	}
}

func TestEnsureSIMVisibleDoesNotPowerCycleAfterProbeTimeout(t *testing.T) {
	restoreSIMRefreshTiming(t, 0, time.Millisecond)
	restoreSIMProbeTimeout(t, 10*time.Millisecond)
	const path = "/sys/devices/modem-1"
	current := simRefreshTestModem(path, 1, false)
	registry := &Registry{modems: map[string]*Modem{path: current}, started: true}
	device := &simRefreshDevice{fakeDeviceControl: &fakeDeviceControl{}}
	device.onSIMState = func(ctx context.Context, _ devicewwan.Target) (devicewwan.SIMState, error) {
		<-ctx.Done()
		return devicewwan.SIMState{Supported: true}, ctx.Err()
	}
	registry.openDevice = fakeDeviceOpener(t, device, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := registry.ensureSIMVisible(ctx, current, SIMTarget{Slot: 1, ICCID: "8901000000000000001"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ensureSIMVisible() error = %v, want %v", err, context.DeadlineExceeded)
	}
	if slices.Contains(device.calls, "power-cycle") {
		t.Fatalf("device calls = %v, want no power cycle after probe timeout", device.calls)
	}
}

func TestEnsureSIMVisibleProvisionsEachGenerationOnce(t *testing.T) {
	restoreSIMRefreshTiming(t, 0, time.Millisecond)
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
	result, err := registry.ensureSIMVisible(ctx, current, SIMTarget{Slot: 1, ICCID: "8901000000000000001"})
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

func TestEnsureSIMVisibleDiscardsProbeFromRetiredGeneration(t *testing.T) {
	restoreSIMRefreshTiming(t, 0, time.Millisecond)
	const (
		path  = "/sys/devices/modem-1"
		iccid = "8901000000000000002"
	)
	current := simRefreshTestModem(path, 1, false)
	replacement := simRefreshTestModem(path, 2, false)
	registry := &Registry{modems: map[string]*Modem{path: current}, started: true}
	state := devicewwan.SIMState{
		Supported: true,
		Matches:   true,
		Ready:     true,
		ICCID:     iccid,
		Slot:      1,
	}
	probes := 0
	device := &simRefreshDevice{fakeDeviceControl: &fakeDeviceControl{}}
	device.onSIMState = func(context.Context, devicewwan.Target) (devicewwan.SIMState, error) {
		probes++
		probed := current
		if probes > 1 {
			probed = replacement
		}
		probed.applySIMInfo(wwanmodem.SIMInfo{
			Slot:  1,
			State: wwanmodem.SIMStateReady,
			ICCID: iccid,
			ATR:   []byte{0x3B, 0x80, 0x81, 0x2F, 0x82, 0xAC},
		})
		if probes == 1 {
			registry.mu.Lock()
			registry.modems[path] = replacement
			registry.mu.Unlock()
		}
		return state, nil
	}
	registry.openDevice = fakeDeviceOpener(t, device, nil)

	result, err := registry.ensureSIMVisible(t.Context(), current, SIMTarget{ICCID: iccid, RequireEUICC: true})
	if err != nil {
		t.Fatalf("ensureSIMVisible() error = %v", err)
	}
	if result.Modem != replacement || !result.ReloadObserved {
		t.Fatalf("result = %+v, want ready replacement generation", result)
	}
	if probes != 2 {
		t.Fatalf("SIM probes = %d, want retired and replacement generations", probes)
	}
}

func TestEnsureSIMVisibleRetriesTransientProvisioningFailure(t *testing.T) {
	restoreSIMRefreshTiming(t, 0, time.Millisecond)
	const path = "/sys/devices/modem-1"
	current := simRefreshTestModem(path, 1, false)
	registry := &Registry{modems: map[string]*Modem{path: current}, started: true}
	device := &simRefreshDevice{fakeDeviceControl: &fakeDeviceControl{activateErr: errors.New("USIM application missing")}}
	activateCalls := 0
	device.onActivate = func() {
		activateCalls++
		switch activateCalls {
		case 1:
			device.activateErr = nil
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
	if _, err := registry.ensureSIMVisible(ctx, current, SIMTarget{Slot: 1, ICCID: "8901000000000000001"}); err != nil {
		t.Fatalf("ensureSIMVisible() error = %v", err)
	}
	if activateCalls != 2 {
		t.Fatalf("provisioning calls = %d, want retry after transient failure", activateCalls)
	}
}

func TestEnsureSIMVisibleDoesNotPowerCyclePersistentICCIDMismatch(t *testing.T) {
	restoreSIMRefreshTiming(t, 0, time.Millisecond)
	const path = "/sys/devices/modem-1"
	current := simRefreshTestModem(path, 1, false)
	registry := &Registry{modems: map[string]*Modem{path: current}, started: true}
	device := &simRefreshDevice{fakeDeviceControl: &fakeDeviceControl{state: devicewwan.SIMState{
		Supported:     true,
		Recoverable:   true,
		Ready:         true,
		ICCIDMismatch: true,
		ICCID:         "8901000000000000002",
		Slot:          1,
	}}}
	registry.openDevice = fakeDeviceOpener(t, device, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := registry.ensureSIMVisible(ctx, current, SIMTarget{Slot: 1, ICCID: "8901000000000000001"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ensureSIMVisible() error = %v, want %v", err, context.DeadlineExceeded)
	}
	if slices.Contains(device.calls, "power-cycle") {
		t.Fatalf("device calls = %v, want no automatic power cycle", device.calls)
	}
}

func TestReadCurrentSIMRequiresMatchingReadyState(t *testing.T) {
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
	if snapshot := modem.Snapshot(); snapshot.SIM != nil {
		t.Fatalf("cached SIM = %+v, want no update before the SIM is ready", snapshot.SIM)
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

func TestReadCurrentModemUsesReadyActiveSIMSnapshot(t *testing.T) {
	const path = "/sys/devices/modem-1"
	const iccid = "8901000000000000002"
	modem := simRefreshTestModem(path, 1, true)
	registry := &Registry{modems: map[string]*Modem{path: modem}, started: true}
	device := &fakeDeviceControl{state: devicewwan.SIMState{
		Supported: true,
		Matches:   true,
		Ready:     true,
		ICCID:     iccid,
		Slot:      2,
	}}
	registry.openDevice = fakeDeviceOpener(t, device, nil)
	target := SIMTarget{Slot: 2, ICCID: iccid, RequireActiveSlot: true}

	read, err := registry.readCurrentModem(context.Background(), modem, target)
	if err != nil {
		t.Fatalf("readCurrentModem() before slot switch error = %v", err)
	}
	if read.SIMVisible {
		t.Fatal("SIMVisible = true before the active slot changed")
	}
	if len(device.calls) != 0 {
		t.Fatalf("device calls before active slot changed = %v, want none", device.calls)
	}

	modem.applySIMInfo(wwanmodem.SIMInfo{Slot: 2, State: wwanmodem.SIMStateReady, ICCID: iccid})
	read, err = registry.readCurrentModem(context.Background(), modem, target)
	if err != nil {
		t.Fatalf("readCurrentModem() after slot switch error = %v", err)
	}
	if !read.SIMVisible {
		t.Fatal("SIMVisible = false after target SIM became ready")
	}
	if len(device.calls) != 0 {
		t.Fatalf("device calls after active SIM became ready = %v, want none", device.calls)
	}
}

func TestReadCurrentModemUsesReadyEUICCSnapshotWithoutSlotTarget(t *testing.T) {
	const path = "/sys/devices/modem-1"
	const iccid = "8901000000000000002"
	modem := simRefreshTestModem(path, 1, false)
	modem.applySIMInfo(wwanmodem.SIMInfo{
		Slot:  1,
		State: wwanmodem.SIMStateReady,
		ICCID: iccid,
		ATR:   []byte{0x3B, 0x80, 0x81, 0x2F, 0x82, 0xAC},
	})
	registry := &Registry{modems: map[string]*Modem{path: modem}, started: true}
	registry.openDevice = func(devicewwan.Config) (deviceControl, error) {
		t.Fatal("ready eUICC snapshot opened a device probe")
		return nil, nil
	}

	read, err := registry.readCurrentModem(context.Background(), modem, SIMTarget{ICCID: iccid, RequireEUICC: true})
	if err != nil {
		t.Fatalf("readCurrentModem() error = %v", err)
	}
	if !read.SIMVisible {
		t.Fatal("SIMVisible = false, want ready eUICC snapshot")
	}
}

func TestEnsureSIMVisibleWaitsForReadyActiveSIMSnapshot(t *testing.T) {
	restoreSIMRefreshTiming(t, 0, time.Millisecond)
	const path = "/sys/devices/modem-1"
	const iccid = "8901000000000000002"
	modem := simRefreshTestModem(path, 1, true)
	modem.applyActiveSIMIdentity(2, "")
	registry := &Registry{modems: map[string]*Modem{path: modem}, started: true}
	device := &simRefreshDevice{fakeDeviceControl: &fakeDeviceControl{}}
	go func() {
		time.Sleep(time.Millisecond)
		modem.applySIMInfo(wwanmodem.SIMInfo{Slot: 2, State: wwanmodem.SIMStateReady, ICCID: iccid})
	}()
	registry.openDevice = fakeDeviceOpener(t, device, nil)

	result, err := registry.EnsureSIMVisible(context.Background(), modem, SIMTarget{
		Slot:              2,
		ICCID:             iccid,
		RequireActiveSlot: true,
	})
	if err != nil {
		t.Fatalf("EnsureSIMVisible() error = %v", err)
	}
	if result != modem {
		t.Fatalf("result modem = %p, want %p", result, modem)
	}
	if len(device.calls) != 0 {
		t.Fatalf("device calls = %v, want none for a physical slot switch", device.calls)
	}
}

func TestEnsureSIMVisibleWaitsForActiveSIMClassification(t *testing.T) {
	restoreSIMRefreshTiming(t, 0, time.Millisecond)
	const path = "/sys/devices/modem-1"
	const iccid = "8901000000000000002"
	modem := simRefreshTestModem(path, 1, true)
	modem.applyActiveSIMIdentity(2, iccid)
	modem.applySIMInfo(wwanmodem.SIMInfo{Slot: 2, State: wwanmodem.SIMStateReady, ICCID: iccid})
	registry := &Registry{modems: map[string]*Modem{path: modem}, started: true}
	go func() {
		time.Sleep(time.Millisecond)
		modem.applySIMInfo(wwanmodem.SIMInfo{
			Slot:  2,
			State: wwanmodem.SIMStateReady,
			ICCID: iccid,
			ATR:   []byte{0x3B, 0x80, 0x81, 0x2F, 0x82, 0xAC},
		})
	}()

	result, err := registry.EnsureSIMVisible(context.Background(), modem, SIMTarget{
		Slot:              2,
		ICCID:             iccid,
		RequireActiveSlot: true,
		RequireClassified: true,
	})
	if err != nil {
		t.Fatalf("EnsureSIMVisible() error = %v", err)
	}
	if result != modem {
		t.Fatalf("result modem = %p, want %p", result, modem)
	}
	if kind := modem.Snapshot().SIMKind(); kind != SIMKindEUICC {
		t.Fatalf("SIM kind = %q, want %q", kind, SIMKindEUICC)
	}
}

func TestPublishSIMChangedForDuplicateICCIDInDifferentSlot(t *testing.T) {
	const (
		path  = "/sys/devices/modem-1"
		iccid = "8901000000000000001"
	)
	current := simRefreshTestModem(path, 7, true)
	current.applyActiveSIMIdentity(2, iccid)
	registry := &Registry{modems: map[string]*Modem{path: current}, started: true}
	var events []ModemEvent
	registry.subs = []subscription{{id: 1, fn: func(event ModemEvent) error {
		events = append(events, event)
		return nil
	}}}

	registry.publishSIMChanged(current, 1, iccid)

	if len(events) != 1 {
		t.Fatalf("published events = %d, want 1", len(events))
	}
	if events[0].PreviousSIMSlot != 1 || events[0].SIMSlot != 2 {
		t.Fatalf("event slot transition = %d -> %d, want 1 -> 2", events[0].PreviousSIMSlot, events[0].SIMSlot)
	}

	registry.publishSIMChanged(current, 1, iccid)
	if len(events) != 1 {
		t.Fatalf("duplicate published events = %d, want 1", len(events))
	}
}

func TestRuntimeSIMChangePublishesFromTrackedIdentity(t *testing.T) {
	const (
		path     = "/sys/devices/modem-1"
		previous = "8901000000000000001"
		next     = "8901000000000000002"
	)
	current := simRefreshTestModem(path, 7, true)
	registry := &Registry{modems: map[string]*Modem{path: current}, started: true}
	registry.trackSIMIdentity(current)
	var events []ModemEvent
	registry.subs = []subscription{{id: 1, fn: func(event ModemEvent) error {
		events = append(events, event)
		return nil
	}}}
	current.onSIMChange = func(previousSlot uint32, previousIdentifier string) {
		registry.publishSIMChanged(current, previousSlot, previousIdentifier)
	}

	previousSlot, previousIdentifier := activeSIMIdentity(current)
	current.applyActiveSIMIdentity(1, next)
	current.notifySIMChanged(previousSlot, previousIdentifier)

	if len(events) != 1 {
		t.Fatalf("published events = %d, want 1", len(events))
	}
	if events[0].PreviousSIMIdentifier != previous || events[0].SIMIdentifier != next {
		t.Fatalf("event SIM transition = %q -> %q, want %q -> %q", events[0].PreviousSIMIdentifier, events[0].SIMIdentifier, previous, next)
	}
}

func TestRuntimeSIMRemovalPublishesFromTrackedIdentity(t *testing.T) {
	const (
		path     = "/sys/devices/modem-1"
		previous = "8901000000000000001"
	)
	current := simRefreshTestModem(path, 7, true)
	registry := &Registry{modems: map[string]*Modem{path: current}, started: true}
	registry.trackSIMIdentity(current)
	var events []ModemEvent
	registry.subs = []subscription{{id: 1, fn: func(event ModemEvent) error {
		events = append(events, event)
		return nil
	}}}
	current.onSIMChange = func(previousSlot uint32, previousIdentifier string) {
		registry.publishSIMChanged(current, previousSlot, previousIdentifier)
	}

	previousSlot, previousIdentifier := activeSIMIdentity(current)
	current.applySIMInfo(wwanmodem.SIMInfo{})
	current.notifySIMChanged(previousSlot, previousIdentifier)

	if len(events) != 1 {
		t.Fatalf("published events = %d, want 1", len(events))
	}
	event := events[0]
	if event.PreviousSIMSlot != 1 || event.SIMSlot != 0 {
		t.Fatalf("event slot transition = %d -> %d, want 1 -> 0", event.PreviousSIMSlot, event.SIMSlot)
	}
	if event.PreviousSIMIdentifier != previous || event.SIMIdentifier != "" {
		t.Fatalf("event SIM transition = %q -> %q, want %q -> empty", event.PreviousSIMIdentifier, event.SIMIdentifier, previous)
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

func TestReadCurrentModemPreservesProbeErrorDuringGenerationChange(t *testing.T) {
	const path = "/sys/devices/modem-1"
	current := simRefreshTestModem(path, 1, false)
	replacement := simRefreshTestModem(path, 2, false)
	registry := &Registry{modems: map[string]*Modem{path: current}, started: true}
	wantErr := errors.New("UIM transport stopped")
	device := &simRefreshDevice{fakeDeviceControl: &fakeDeviceControl{}}
	device.onSIMState = func(context.Context, devicewwan.Target) (devicewwan.SIMState, error) {
		registry.mu.Lock()
		registry.modems[path] = replacement
		registry.mu.Unlock()
		return devicewwan.SIMState{}, wantErr
	}
	registry.openDevice = fakeDeviceOpener(t, device, nil)

	_, err := registry.readCurrentModem(context.Background(), current, SIMTarget{ICCID: "8901000000000000001"})
	if !errors.Is(err, wantErr) {
		t.Fatalf("readCurrentModem() error = %v, want %v", err, wantErr)
	}
}

func restoreSIMRefreshTiming(t *testing.T, settle, poll time.Duration) {
	t.Helper()
	previousSettle := simSettleDelay
	previousPoll := simVisiblePollInterval
	simSettleDelay = settle
	simVisiblePollInterval = poll
	t.Cleanup(func() {
		simSettleDelay = previousSettle
		simVisiblePollInterval = previousPoll
	})
}

func restoreSIMProbeTimeout(t *testing.T, timeout time.Duration) {
	t.Helper()
	previous := simProbeTimeout
	simProbeTimeout = timeout
	t.Cleanup(func() { simProbeTimeout = previous })
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
