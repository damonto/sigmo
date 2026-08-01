package modem

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	wwanmodem "github.com/damonto/wwan-go/modem"
	"github.com/damonto/wwan-go/qcom"
)

func TestRegistryReplacesAndRemovesPhysicalDeviceGeneration(t *testing.T) {
	registry, err := NewRegistry()
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	device := qmiRegistryDevice("/dev/cdc-wdm0", "/sys/devices/modem-1")
	events := make(chan wwanmodem.Result[wwanmodem.DeviceEvent], 2)
	registry.discover = func(context.Context) ([]wwanmodem.Device, error) {
		return []wwanmodem.Device{device}, nil
	}
	registry.watchDevices = func(context.Context) (<-chan wwanmodem.Result[wwanmodem.DeviceEvent], error) {
		return events, nil
	}
	registry.open = func(_ context.Context, candidate wwanmodem.Device, generation uint64) (*Modem, error) {
		return &Modem{
			deviceInfo:          candidate,
			deviceKey:           physicalDeviceKey(candidate),
			generation:          generation,
			EquipmentIdentifier: "imei-1",
			PrimaryPort:         controlPortPath(candidate),
		}, nil
	}

	modems, err := registry.Modems(context.Background())
	if err != nil {
		t.Fatalf("Modems() error = %v", err)
	}
	initial := modems[device.PhysicalPath]
	if initial == nil || initial.Generation() != 1 {
		t.Fatalf("initial modem = %+v, want generation 1", initial)
	}

	published := make(chan ModemEvent, 2)
	unsubscribe, err := registry.Subscribe(func(event ModemEvent) error {
		published <- event
		return nil
	})
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	defer unsubscribe()

	replacementDevice := withRegistryControlPath(device, "/dev/cdc-wdm1")
	events <- wwanmodem.Result[wwanmodem.DeviceEvent]{Value: wwanmodem.DeviceEvent{
		Type:   wwanmodem.DeviceChanged,
		Device: replacementDevice,
	}}
	changed := receiveModemEvent(t, published)
	if changed.Type != ModemEventChanged || changed.Generation != 2 || changed.Previous != initial {
		t.Fatalf("changed event = %+v", changed)
	}
	if changed.Modem == nil || changed.Modem.PrimaryPort != controlPortPath(replacementDevice) {
		t.Fatalf("replacement modem = %+v, want primary port %s", changed.Modem, controlPortPath(replacementDevice))
	}
	if changed.Snapshot[device.PhysicalPath] != changed.Modem {
		t.Fatalf("changed snapshot = %+v, want replacement", changed.Snapshot)
	}

	events <- wwanmodem.Result[wwanmodem.DeviceEvent]{Value: wwanmodem.DeviceEvent{
		Type:   wwanmodem.DeviceRemoved,
		Device: replacementDevice,
	}}
	removed := receiveModemEvent(t, published)
	if removed.Type != ModemEventRemoved || removed.Generation != 2 || removed.Modem != changed.Modem {
		t.Fatalf("removed event = %+v", removed)
	}
	if len(removed.Snapshot) != 0 {
		t.Fatalf("removed snapshot = %+v, want empty", removed.Snapshot)
	}

	close(events)
	if err := registry.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestRegistryRetriesAfterWatcherStartupFailure(t *testing.T) {
	registry, err := NewRegistry()
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	device := qmiRegistryDevice("/dev/cdc-wdm0", "/sys/devices/modem-1")
	registry.discover = func(context.Context) ([]wwanmodem.Device, error) {
		return []wwanmodem.Device{device}, nil
	}
	openCalls := 0
	registry.open = func(_ context.Context, candidate wwanmodem.Device, generation uint64) (*Modem, error) {
		openCalls++
		return &Modem{deviceKey: physicalDeviceKey(candidate), generation: generation}, nil
	}
	watchCalls := 0
	registry.watchDevices = func(context.Context) (<-chan wwanmodem.Result[wwanmodem.DeviceEvent], error) {
		watchCalls++
		if watchCalls == 1 {
			return nil, errors.New("watch unavailable")
		}
		stream := make(chan wwanmodem.Result[wwanmodem.DeviceEvent])
		close(stream)
		return stream, nil
	}

	if _, err := registry.Modems(context.Background()); err == nil {
		t.Fatal("first Modems() error = nil, want watcher error")
	}
	modems, err := registry.Modems(context.Background())
	if err != nil {
		t.Fatalf("second Modems() error = %v", err)
	}
	if len(modems) != 1 || watchCalls != 2 || openCalls != 2 {
		t.Fatalf("modems/watch/open = %d/%d/%d, want 1/2/2", len(modems), watchCalls, openCalls)
	}
	if err := registry.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestRegistryRecoversCurrentGenerationAfterTransportFailure(t *testing.T) {
	tests := []struct {
		name string
	}{
		{name: "same control path"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			device := qmiRegistryDevice("/dev/cdc-wdm0", "/sys/devices/modem-1")
			key := physicalDeviceKey(device)
			initial := &Modem{
				deviceInfo:          device,
				deviceKey:           key,
				generation:          1,
				EquipmentIdentifier: "imei-1",
				PrimaryPort:         controlPortPath(device),
			}
			openCalls := 0
			registry := &Registry{
				modems:         map[string]*Modem{key: initial},
				nextGeneration: 1,
				failures:       make(chan modemFailure, 1),
				discover: func(context.Context) ([]wwanmodem.Device, error) {
					return []wwanmodem.Device{device}, nil
				},
				open: func(_ context.Context, candidate wwanmodem.Device, generation uint64) (*Modem, error) {
					openCalls++
					return &Modem{
						deviceInfo:          candidate,
						deviceKey:           physicalDeviceKey(candidate),
						generation:          generation,
						EquipmentIdentifier: "imei-1",
						PrimaryPort:         controlPortPath(candidate),
					}, nil
				},
			}
			var published []ModemEvent
			registry.subs = []subscription{{id: 1, fn: func(event ModemEvent) error {
				published = append(published, event)
				return nil
			}}}

			failure := modemFailure{modem: initial, err: errors.New("terminal transport error")}
			registry.handleModemFailure(context.Background(), failure)

			replacement := registry.modems[key]
			if replacement == nil || replacement == initial || replacement.Generation() != 2 {
				t.Fatalf("replacement = %+v, want generation 2", replacement)
			}
			if openCalls != 1 {
				t.Fatalf("open calls = %d, want 1", openCalls)
			}
			if len(published) != 2 || published[0].Type != ModemEventRemoved || published[1].Type != ModemEventAdded {
				t.Fatalf("published events = %+v, want removed then added", published)
			}

			registry.handleModemFailure(context.Background(), failure)
			if openCalls != 1 || registry.modems[key] != replacement {
				t.Fatal("stale generation failure replaced the current modem")
			}
		})
	}
}

func TestRegistryBoundsClientIDExhaustionRecovery(t *testing.T) {
	tests := []struct {
		name              string
		reconnectByEvents bool
	}{
		{name: "device events", reconnectByEvents: true},
		{name: "discovery snapshot"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			device := qmiRegistryDevice("/dev/cdc-wdm0", "/sys/devices/modem-1")
			key := physicalDeviceKey(device)
			initial := &Modem{
				deviceInfo:          device,
				deviceKey:           key,
				generation:          1,
				EquipmentIdentifier: "imei-1",
				PrimaryPort:         controlPortPath(device),
			}
			openCalls := 0
			devicePresent := true
			registry := &Registry{
				modems:         map[string]*Modem{key: initial},
				nextGeneration: 1,
				discover: func(context.Context) ([]wwanmodem.Device, error) {
					if !devicePresent {
						return nil, nil
					}
					return []wwanmodem.Device{device}, nil
				},
				open: func(_ context.Context, candidate wwanmodem.Device, generation uint64) (*Modem, error) {
					openCalls++
					return &Modem{
						deviceInfo:          candidate,
						deviceKey:           physicalDeviceKey(candidate),
						generation:          generation,
						EquipmentIdentifier: "imei-1",
						PrimaryPort:         controlPortPath(candidate),
					}, nil
				},
			}
			exhausted := func(modem *Modem) modemFailure {
				return modemFailure{modem: modem, err: errors.Join(errors.New("read serving system"), qcom.QMIErrorClientIdsExhausted)}
			}

			registry.handleModemFailure(context.Background(), exhausted(initial))
			replacement := registry.modems[key]
			if replacement == nil || replacement == initial {
				t.Fatalf("replacement = %+v, want one coordinated recovery", replacement)
			}
			if openCalls != 1 || registry.cidRecoveryState(key) != cidRecoveryRetried {
				t.Fatalf("open calls = %d, recovery state = %d, want 1 and retried", openCalls, registry.cidRecoveryState(key))
			}

			registry.handleModemFailure(context.Background(), modemFailure{
				modem: replacement,
				err:   errors.New("terminal transport error"),
			})
			replacement = registry.modems[key]
			if replacement == nil {
				t.Fatal("ordinary transport failure did not recover the modem")
			}
			if openCalls != 2 || registry.cidRecoveryState(key) != cidRecoveryRetried {
				t.Fatalf("open calls = %d, recovery state = %d, want 2 and retried", openCalls, registry.cidRecoveryState(key))
			}

			registry.handleModemFailure(context.Background(), exhausted(replacement))
			if registry.modems[key] != nil {
				t.Fatalf("modem = %+v, want suspended recovery", registry.modems[key])
			}
			if openCalls != 2 || registry.cidRecoveryState(key) != cidRecoverySuspended {
				t.Fatalf("open calls = %d, recovery state = %d, want 2 and suspended", openCalls, registry.cidRecoveryState(key))
			}

			if err := registry.reconcile(context.Background()); err != nil {
				t.Fatalf("reconcile() error = %v", err)
			}
			if openCalls != 2 || registry.modems[key] != nil {
				t.Fatal("present device bypassed client ID recovery circuit")
			}

			if tt.reconnectByEvents {
				registry.applyDeviceEvent(context.Background(), wwanmodem.DeviceEvent{Type: wwanmodem.DeviceRemoved, Device: device})
				registry.applyDeviceEvent(context.Background(), wwanmodem.DeviceEvent{Type: wwanmodem.DeviceAdded, Device: device})
			} else {
				devicePresent = false
				if err := registry.reconcile(context.Background()); err != nil {
					t.Fatalf("reconcile() absent device error = %v", err)
				}
				devicePresent = true
				if err := registry.reconcile(context.Background()); err != nil {
					t.Fatalf("reconcile() reconnected device error = %v", err)
				}
			}
			if openCalls != 3 || registry.modems[key] == nil {
				t.Fatalf("open calls = %d, modem = %+v, want recovery after reconnect", openCalls, registry.modems[key])
			}
			if registry.cidRecoveryState(key) != cidRecoveryUnused {
				t.Fatal("client ID recovery state remains set after reconnect")
			}
		})
	}
}

func TestRegistryBoundsClientIDExhaustionForAddedDevice(t *testing.T) {
	clientIDsExhausted := errors.Join(errors.New("open NAS client"), qcom.QMIErrorClientIdsExhausted)
	tests := []struct {
		name       string
		openErrors []error
		wantModem  bool
		wantState  cidRecoveryState
	}{
		{
			name:       "retry succeeds",
			openErrors: []error{clientIDsExhausted, nil},
			wantModem:  true,
			wantState:  cidRecoveryRetried,
		},
		{
			name:       "retry also exhausts client IDs",
			openErrors: []error{clientIDsExhausted, clientIDsExhausted},
			wantState:  cidRecoverySuspended,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			device := qmiRegistryDevice("/dev/cdc-wdm0", "/sys/devices/modem-1")
			key := physicalDeviceKey(device)
			openCalls := 0
			registry := &Registry{
				modems:            make(map[string]*Modem),
				cidRecoveryStates: map[string]cidRecoveryState{key: cidRecoverySuspended},
				open: func(_ context.Context, candidate wwanmodem.Device, generation uint64) (*Modem, error) {
					if openCalls >= len(tt.openErrors) {
						t.Fatalf("open calls exceed configured outcomes: %d", openCalls+1)
					}
					err := tt.openErrors[openCalls]
					openCalls++
					if err != nil {
						return nil, err
					}
					return &Modem{
						deviceInfo:          candidate,
						deviceKey:           physicalDeviceKey(candidate),
						generation:          generation,
						EquipmentIdentifier: "imei-1",
						PrimaryPort:         controlPortPath(candidate),
					}, nil
				},
			}

			registry.applyDeviceEvent(t.Context(), wwanmodem.DeviceEvent{
				Type:   wwanmodem.DeviceAdded,
				Device: device,
			})

			if openCalls != 2 {
				t.Fatalf("open calls = %d, want 2", openCalls)
			}
			if got := registry.modems[key] != nil; got != tt.wantModem {
				t.Fatalf("modem present = %t, want %t", got, tt.wantModem)
			}
			if got := registry.cidRecoveryState(key); got != tt.wantState {
				t.Fatalf("recovery state = %d, want %d", got, tt.wantState)
			}
		})
	}
}

func TestRegistrySuspendsWhenCIDRecoveryOpenAlsoExhausts(t *testing.T) {
	tests := []struct {
		name string
	}{
		{name: "reopen returns client IDs exhausted"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			device := qmiRegistryDevice("/dev/cdc-wdm0", "/sys/devices/modem-1")
			key := physicalDeviceKey(device)
			initial := &Modem{
				deviceInfo:          device,
				deviceKey:           key,
				generation:          1,
				EquipmentIdentifier: "imei-1",
				PrimaryPort:         controlPortPath(device),
			}
			openCalls := 0
			registry := &Registry{
				modems: map[string]*Modem{key: initial},
				discover: func(context.Context) ([]wwanmodem.Device, error) {
					return []wwanmodem.Device{device}, nil
				},
				open: func(context.Context, wwanmodem.Device, uint64) (*Modem, error) {
					openCalls++
					return nil, errors.Join(errors.New("open NAS client"), qcom.QMIErrorClientIdsExhausted)
				},
			}

			registry.handleModemFailure(context.Background(), modemFailure{
				modem: initial,
				err:   errors.Join(errors.New("read serving system"), qcom.QMIErrorClientIdsExhausted),
			})

			if openCalls != 1 {
				t.Fatalf("open calls = %d, want one coordinated reopen", openCalls)
			}
			if registry.modems[key] != nil {
				t.Fatalf("modem = %+v, want recovery suspended", registry.modems[key])
			}
			if registry.cidRecoveryState(key) != cidRecoverySuspended {
				t.Fatalf("recovery state = %d, want suspended", registry.cidRecoveryState(key))
			}

			if err := registry.reconcile(context.Background()); err != nil {
				t.Fatalf("reconcile() error = %v", err)
			}
			if openCalls != 1 {
				t.Fatalf("open calls = %d, suspended recovery retried again", openCalls)
			}
		})
	}
}

func TestRegistryStartupHonorsCIDRecoverySuspension(t *testing.T) {
	tests := []struct {
		name string
	}{
		{name: "repeated startup while device watcher is unavailable"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			device := qmiRegistryDevice("/dev/cdc-wdm0", "/sys/devices/modem-1")
			key := physicalDeviceKey(device)
			devicePresent := true
			openCalls := 0
			watchErr := errors.New("device watcher unavailable")
			registry, err := NewRegistry()
			if err != nil {
				t.Fatalf("NewRegistry() error = %v", err)
			}
			t.Cleanup(func() { _ = registry.Close() })
			registry.discover = func(context.Context) ([]wwanmodem.Device, error) {
				if !devicePresent {
					return nil, nil
				}
				return []wwanmodem.Device{device}, nil
			}
			registry.watchDevices = func(context.Context) (<-chan wwanmodem.Result[wwanmodem.DeviceEvent], error) {
				return nil, watchErr
			}
			registry.open = func(context.Context, wwanmodem.Device, uint64) (*Modem, error) {
				openCalls++
				return nil, errors.Join(errors.New("open NAS client"), qcom.QMIErrorClientIdsExhausted)
			}

			steps := []struct {
				name         string
				present      bool
				wantCalls    int
				wantRecovery cidRecoveryState
			}{
				{name: "first exhaustion", present: true, wantCalls: 1, wantRecovery: cidRecoveryRetried},
				{name: "second exhaustion", present: true, wantCalls: 2, wantRecovery: cidRecoverySuspended},
				{name: "suspended startup", present: true, wantCalls: 2, wantRecovery: cidRecoverySuspended},
				{name: "observed absence", present: false, wantCalls: 2, wantRecovery: cidRecoveryUnused},
				{name: "opening after reconnect", present: true, wantCalls: 3, wantRecovery: cidRecoveryRetried},
			}
			for _, step := range steps {
				t.Run(step.name, func(t *testing.T) {
					devicePresent = step.present
					if err := registry.ensureStarted(t.Context()); !errors.Is(err, watchErr) {
						t.Fatalf("ensureStarted() error = %v, want watcher error", err)
					}
					if openCalls != step.wantCalls {
						t.Fatalf("open calls = %d, want %d", openCalls, step.wantCalls)
					}
					if got := registry.cidRecoveryState(key); got != step.wantRecovery {
						t.Fatalf("recovery state = %d, want %d", got, step.wantRecovery)
					}
				})
			}
		})
	}
}

func TestRegistryRestartsWatcherAndReconcilesMissedChange(t *testing.T) {
	previousDelay := registryWatchRetryDelay
	registryWatchRetryDelay = time.Millisecond
	t.Cleanup(func() { registryWatchRetryDelay = previousDelay })

	registry, err := NewRegistry()
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	t.Cleanup(func() {
		if err := registry.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	initialDevice := qmiRegistryDevice("/dev/cdc-wdm0", "/sys/devices/modem-1")
	replacementDevice := withRegistryControlPath(initialDevice, "/dev/cdc-wdm1")
	var discoverMu sync.RWMutex
	discovered := initialDevice
	registry.discover = func(context.Context) ([]wwanmodem.Device, error) {
		discoverMu.RLock()
		defer discoverMu.RUnlock()
		return []wwanmodem.Device{discovered}, nil
	}
	firstStream := make(chan wwanmodem.Result[wwanmodem.DeviceEvent])
	secondStream := make(chan wwanmodem.Result[wwanmodem.DeviceEvent])
	var watchCalls atomic.Int32
	registry.watchDevices = func(context.Context) (<-chan wwanmodem.Result[wwanmodem.DeviceEvent], error) {
		switch watchCalls.Add(1) {
		case 1:
			return firstStream, nil
		case 2:
			return nil, errors.New("watch unavailable")
		default:
			return secondStream, nil
		}
	}
	registry.open = func(_ context.Context, candidate wwanmodem.Device, generation uint64) (*Modem, error) {
		return &Modem{
			deviceInfo:          candidate,
			deviceKey:           physicalDeviceKey(candidate),
			generation:          generation,
			EquipmentIdentifier: "imei-1",
			PrimaryPort:         controlPortPath(candidate),
		}, nil
	}

	if _, err := registry.Modems(context.Background()); err != nil {
		t.Fatalf("Modems() error = %v", err)
	}
	published := make(chan ModemEvent, 1)
	unsubscribe, err := registry.Subscribe(func(event ModemEvent) error {
		published <- event
		return nil
	})
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	defer unsubscribe()

	discoverMu.Lock()
	discovered = replacementDevice
	discoverMu.Unlock()
	close(firstStream)

	changed := receiveModemEvent(t, published)
	if changed.Type != ModemEventChanged || changed.Generation != 2 || changed.Modem == nil || changed.Modem.PrimaryPort != controlPortPath(replacementDevice) {
		t.Fatalf("reconciled event = %+v", changed)
	}
	deadline := time.Now().Add(time.Second)
	for watchCalls.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := watchCalls.Load(); got < 3 {
		t.Fatalf("watch calls = %d, want restarted watcher", got)
	}
}

func TestRegistryPublishesPathChangeBeforeClosingPreviousGeneration(t *testing.T) {
	oldDevice := qmiRegistryDevice("/dev/cdc-wdm0", "/sys/devices/old")
	newDevice := oldDevice
	newDevice.PhysicalPath = "/sys/devices/new"
	closed := false
	old := &Modem{
		deviceInfo:          oldDevice,
		deviceKey:           physicalDeviceKey(oldDevice),
		generation:          1,
		EquipmentIdentifier: "imei-1",
		PrimaryPort:         controlPortPath(oldDevice),
		watchCancel:         func() { closed = true },
	}
	registry := &Registry{
		modems:         map[string]*Modem{old.Path(): old},
		started:        true,
		nextGeneration: 1,
		open: func(_ context.Context, candidate wwanmodem.Device, generation uint64) (*Modem, error) {
			return &Modem{
				deviceInfo:          candidate,
				deviceKey:           physicalDeviceKey(candidate),
				generation:          generation,
				EquipmentIdentifier: "imei-1",
				PrimaryPort:         controlPortPath(candidate),
			}, nil
		},
	}
	var got ModemEvent
	closedAtPublish := true
	unsubscribe, err := registry.Subscribe(func(event ModemEvent) error {
		got = event
		closedAtPublish = closed
		return nil
	})
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	defer unsubscribe()

	registry.applyDeviceEvent(context.Background(), wwanmodem.DeviceEvent{Type: wwanmodem.DeviceChanged, Device: newDevice})

	if got.Type != ModemEventChanged || got.Previous != old || got.PreviousPath != old.Path() || got.Path != physicalDeviceKey(newDevice) {
		t.Fatalf("changed event = %+v", got)
	}
	if closedAtPublish {
		t.Fatal("previous generation was closed before subscribers handled the change")
	}
	if !closed {
		t.Fatal("previous generation was not closed after publishing the change")
	}
}

func TestRegistryPublishesRemovedAndAddedForDifferentIMEIOnSameDevice(t *testing.T) {
	device := qmiRegistryDevice("/dev/cdc-wdm0", "/sys/devices/modem-1")
	old := &Modem{
		deviceInfo:          device,
		deviceKey:           physicalDeviceKey(device),
		generation:          1,
		EquipmentIdentifier: "imei-old",
		PrimaryPort:         controlPortPath(device),
	}
	registry := &Registry{
		modems:         map[string]*Modem{old.Path(): old},
		started:        true,
		nextGeneration: 1,
		open: func(_ context.Context, candidate wwanmodem.Device, generation uint64) (*Modem, error) {
			return &Modem{
				deviceInfo:          candidate,
				deviceKey:           physicalDeviceKey(candidate),
				generation:          generation,
				EquipmentIdentifier: "imei-new",
				PrimaryPort:         controlPortPath(candidate),
			}, nil
		},
	}
	var events []ModemEvent
	unsubscribe, err := registry.Subscribe(func(event ModemEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	defer unsubscribe()

	registry.applyDeviceEvent(context.Background(), wwanmodem.DeviceEvent{Type: wwanmodem.DeviceChanged, Device: device})

	if len(events) != 2 || events[0].Type != ModemEventRemoved || events[1].Type != ModemEventAdded {
		t.Fatalf("events = %+v, want removed then added", events)
	}
	if events[0].Modem != old || events[0].Generation != 1 {
		t.Fatalf("removed event = %+v", events[0])
	}
	if events[1].Modem == nil || events[1].Modem.EquipmentIdentifier != "imei-new" || events[1].Generation != 2 {
		t.Fatalf("added event = %+v", events[1])
	}
}

func TestControlPortsPreferQMI(t *testing.T) {
	device := wwanmodem.Device{Ports: []wwanmodem.Port{
		{Type: wwanmodem.PortMBIM, Path: "/dev/wwan0mbim0"},
		{Type: wwanmodem.PortNetwork, Name: "wwan0"},
		{Type: wwanmodem.PortQMI, Path: "/dev/wwan0qmi0"},
		{Type: wwanmodem.PortQMI},
	}}

	got := controlPorts(device)
	want := []wwanmodem.Port{
		{Type: wwanmodem.PortQMI, Path: "/dev/wwan0qmi0"},
		{Type: wwanmodem.PortMBIM, Path: "/dev/wwan0mbim0"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("controlPorts() = %+v, want %+v", got, want)
	}
}

func receiveModemEvent(t *testing.T, events <-chan ModemEvent) ModemEvent {
	t.Helper()
	select {
	case event := <-events:
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for modem event")
		return ModemEvent{}
	}
}

func qmiRegistryDevice(path, physicalPath string) wwanmodem.Device {
	return wwanmodem.Device{
		PhysicalPath: physicalPath,
		Ports: []wwanmodem.Port{{
			Type: wwanmodem.PortQMI, Path: path, Driver: "qmi_wwan",
		}},
	}
}

func withRegistryControlPath(device wwanmodem.Device, path string) wwanmodem.Device {
	device.Ports = slices.Clone(device.Ports)
	for i := range device.Ports {
		if device.Ports[i].Protocol() != wwanmodem.ProtocolUnknown {
			device.Ports[i].Path = path
			break
		}
	}
	return device
}
