package modem

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	wwanmodem "github.com/damonto/wwan-go/modem"
)

var (
	registryOpenTimeout     = 15 * time.Second
	registryWatchRetryDelay = time.Second
)

var (
	ErrNotFound      = errors.New("modem not found")
	errModemRequired = errors.New("modem is required")
)

type Registry struct {
	mu             sync.RWMutex
	startMu        sync.Mutex
	modems         map[string]*Modem
	subs           []subscription
	nextSubID      uint64
	nextGeneration uint64
	started        bool
	closed         bool
	cancel         context.CancelFunc
	wg             sync.WaitGroup

	discover     func(context.Context) ([]wwanmodem.Device, error)
	watchDevices func(context.Context) (<-chan wwanmodem.Result[wwanmodem.DeviceEvent], error)
	open         func(context.Context, wwanmodem.Device, uint64) (*Modem, error)
	openDevice   deviceControlOpener
}

type ModemEventType int

const (
	ModemEventAdded ModemEventType = iota
	ModemEventRemoved
	ModemEventChanged
)

func (t ModemEventType) String() string {
	switch t {
	case ModemEventAdded:
		return "added"
	case ModemEventRemoved:
		return "removed"
	case ModemEventChanged:
		return "changed"
	default:
		return "unknown"
	}
}

type ModemEvent struct {
	Type         ModemEventType
	Modem        *Modem
	Previous     *Modem
	Path         string
	PreviousPath string
	Generation   uint64
	Snapshot     map[string]*Modem
}

type subscription struct {
	id uint64
	fn func(ModemEvent) error
}

func NewRegistry() (*Registry, error) {
	return &Registry{
		modems:       make(map[string]*Modem),
		discover:     wwanmodem.Discover,
		watchDevices: wwanmodem.WatchDevices,
		open:         openDiscoveredModem,
	}, nil
}

func (r *Registry) Modems(ctx context.Context) (map[string]*Modem, error) {
	if err := r.ensureStarted(ctx); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.copyModemsLocked(), nil
}

func (r *Registry) Find(ctx context.Context, id string) (*Modem, error) {
	if err := r.ensureStarted(ctx); err != nil {
		return nil, err
	}
	return r.findModem(id)
}

func (r *Registry) Subscribe(fn func(ModemEvent) error) (func(), error) {
	if fn == nil {
		return nil, errors.New("modem subscriber is required")
	}
	if err := r.ensureStarted(context.Background()); err != nil {
		return nil, err
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil, errors.New("modem registry is closed")
	}
	r.nextSubID++
	id := r.nextSubID
	r.subs = append(r.subs, subscription{id: id, fn: fn})
	r.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			r.mu.Lock()
			for i := range r.subs {
				if r.subs[i].id == id {
					r.subs = append(r.subs[:i], r.subs[i+1:]...)
					break
				}
			}
			r.mu.Unlock()
		})
	}, nil
}

func (r *Registry) ensureStarted(ctx context.Context) error {
	r.startMu.Lock()
	defer r.startMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.RLock()
	started, closed := r.started, r.closed
	r.mu.RUnlock()
	if closed {
		return errors.New("modem registry is closed")
	}
	if started {
		return nil
	}
	runCtx, cancel := context.WithCancel(context.Background())
	devices, err := r.discover(ctx)
	if err != nil {
		cancel()
		return fmt.Errorf("discover modems: %w", err)
	}
	opened := make(map[string]*Modem, len(devices))
	for _, device := range devices {
		key := physicalDeviceKey(device)
		generation := r.nextGenerationToken()
		modem, err := r.open(ctx, device, generation)
		if err != nil {
			slog.Warn("open discovered modem", "device", device.Path, "physical_path", key, "error", err)
			continue
		}
		modem.startRuntimeWatchers(runCtx)
		if previous := opened[key]; previous != nil {
			_ = previous.Close()
		}
		opened[key] = modem
	}
	stream, err := r.watchDevices(runCtx)
	if err == nil && stream == nil {
		err = errors.New("modem device watcher returned a nil stream")
	}
	if err != nil {
		cancel()
		for _, modem := range opened {
			_ = modem.Close()
		}
		return fmt.Errorf("watch modem devices: %w", err)
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		cancel()
		for _, modem := range opened {
			_ = modem.Close()
		}
		return errors.New("modem registry is closed")
	}
	r.modems = opened
	r.started = true
	r.cancel = cancel
	r.mu.Unlock()
	r.wg.Add(1)
	go r.watchLoop(runCtx, stream)
	return nil
}

func (r *Registry) watchLoop(ctx context.Context, stream <-chan wwanmodem.Result[wwanmodem.DeviceEvent]) {
	defer r.wg.Done()
	for ctx.Err() == nil {
		if stream != nil {
			err := r.consumeDeviceStream(ctx, stream)
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				slog.Error("modem device watcher stopped", "error", err)
			}
		}
		if err := r.reconcile(ctx); err != nil && ctx.Err() == nil {
			slog.Error("reconcile modems after watcher stop", "error", err)
		}
		if err := sleepContext(ctx, registryWatchRetryDelay); err != nil {
			return
		}
		next, err := r.watchDevices(ctx)
		if err != nil {
			if ctx.Err() == nil {
				slog.Error("restart modem device watcher", "error", err)
			}
			stream = nil
			continue
		}
		if next == nil {
			slog.Error("restart modem device watcher", "error", errors.New("watcher returned a nil stream"))
			stream = nil
			continue
		}
		stream = next
	}
}

func (r *Registry) consumeDeviceStream(ctx context.Context, stream <-chan wwanmodem.Result[wwanmodem.DeviceEvent]) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case result, ok := <-stream:
			if !ok {
				return errors.New("modem device stream closed")
			}
			if result.Err != nil {
				return result.Err
			}
			r.applyDeviceEvent(ctx, result.Value)
		}
	}
}

func (r *Registry) reconcile(ctx context.Context) error {
	devices, err := r.discover(ctx)
	if err != nil {
		return fmt.Errorf("discover modems: %w", err)
	}
	present := make(map[string]wwanmodem.Device, len(devices))
	for _, device := range devices {
		key := physicalDeviceKey(device)
		if key == "" {
			continue
		}
		present[key] = device
		r.applyDeviceEvent(ctx, wwanmodem.DeviceEvent{Type: wwanmodem.DevicePresent, Device: device})
	}

	r.mu.RLock()
	current := r.copyModemsLocked()
	r.mu.RUnlock()
	for key, modem := range current {
		if _, ok := present[key]; ok {
			continue
		}
		if modem != nil && devicePresentInSnapshot(modem.deviceInfo, present) {
			continue
		}
		r.removeModem(key, modem)
	}
	return nil
}

func devicePresentInSnapshot(device wwanmodem.Device, devices map[string]wwanmodem.Device) bool {
	for _, candidate := range devices {
		if sameControlDevice(device, candidate) {
			return true
		}
	}
	return false
}

func (r *Registry) applyDeviceEvent(ctx context.Context, event wwanmodem.DeviceEvent) {
	key := physicalDeviceKey(event.Device)
	if key == "" {
		return
	}
	if event.Type == wwanmodem.DeviceRemoved {
		r.mu.RLock()
		existingKey, existing := r.findByDeviceLocked(event.Device)
		r.mu.RUnlock()
		r.removeModem(existingKey, existing)
		return
	}

	r.mu.RLock()
	existingKey, existing := r.findByDeviceLocked(event.Device)
	r.mu.RUnlock()
	if event.Type == wwanmodem.DevicePresent && existing != nil && sameDeviceDescription(existing.deviceInfo, event.Device) {
		return
	}

	generation := r.nextGenerationToken()
	openCtx, cancel := context.WithTimeout(ctx, registryOpenTimeout)
	replacement, err := r.open(openCtx, event.Device, generation)
	cancel()
	if err != nil {
		slog.Warn("open changed modem", "device", event.Device.Path, "physical_path", key, "error", err)
		return
	}
	replacement.startRuntimeWatchers(ctx)

	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		_ = replacement.Close()
		return
	}
	previousKey, previous := r.findReplacementLocked(key, replacement)
	if previous == nil {
		previousKey, previous = existingKey, existing
	}
	if previousKey != "" && previousKey != key {
		delete(r.modems, previousKey)
	}
	r.modems[key] = replacement
	snapshot := r.copyModemsLocked()
	subscribers := append([]subscription(nil), r.subs...)
	r.mu.Unlock()

	typeOfEvent := ModemEventAdded
	if previous != nil {
		if samePhysicalModem(previous, replacement) {
			typeOfEvent = ModemEventChanged
		} else {
			r.publish(subscribers, ModemEvent{
				Type: ModemEventRemoved, Modem: previous, Path: previousKey,
				Generation: previous.Generation(), Snapshot: snapshot,
			})
		}
	}
	r.publish(subscribers, ModemEvent{
		Type: typeOfEvent, Modem: replacement, Previous: previous, Path: key,
		PreviousPath: previousKey, Generation: generation, Snapshot: snapshot,
	})
	if previous != nil {
		if err := previous.Close(); err != nil {
			slog.Warn("close replaced modem", "path", previousKey, "error", err)
		}
	}
}

func (r *Registry) removeModem(key string, existing *Modem) {
	if existing == nil {
		return
	}
	r.mu.Lock()
	if current := r.modems[key]; current != existing {
		key = r.keyForModemLocked(existing)
	}
	if key == "" || r.modems[key] != existing {
		r.mu.Unlock()
		return
	}
	delete(r.modems, key)
	snapshot := r.copyModemsLocked()
	subscribers := append([]subscription(nil), r.subs...)
	r.mu.Unlock()
	r.publish(subscribers, ModemEvent{Type: ModemEventRemoved, Modem: existing, Path: key, Generation: existing.Generation(), Snapshot: snapshot})
	if err := existing.Close(); err != nil {
		slog.Warn("close removed modem", "path", key, "error", err)
	}
}

func (r *Registry) findByDeviceLocked(device wwanmodem.Device) (string, *Modem) {
	key := physicalDeviceKey(device)
	if modem := r.modems[key]; modem != nil {
		return key, modem
	}
	for candidateKey, modem := range r.modems {
		if modem != nil && sameControlDevice(modem.deviceInfo, device) {
			return candidateKey, modem
		}
	}
	return "", nil
}

func (r *Registry) findReplacementLocked(key string, replacement *Modem) (string, *Modem) {
	if modem := r.modems[key]; modem != nil {
		return key, modem
	}
	for candidateKey, modem := range r.modems {
		if samePhysicalModem(modem, replacement) {
			return candidateKey, modem
		}
	}
	return "", nil
}

func (r *Registry) keyForModemLocked(target *Modem) string {
	for key, modem := range r.modems {
		if modem == target {
			return key
		}
	}
	return ""
}

func (r *Registry) publish(subscribers []subscription, event ModemEvent) {
	for _, subscriber := range subscribers {
		if err := subscriber.fn(event); err != nil {
			slog.Error("process modem event", "type", event.Type, "path", event.Path, "error", err)
		}
	}
}

func (r *Registry) nextGenerationToken() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextGeneration++
	return r.nextGeneration
}

func (r *Registry) Close() error {
	r.startMu.Lock()
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		r.startMu.Unlock()
		return nil
	}
	r.closed = true
	if r.cancel != nil {
		r.cancel()
	}
	r.mu.Unlock()
	r.startMu.Unlock()
	r.wg.Wait()
	r.mu.Lock()
	modems := maps.Clone(r.modems)
	r.modems = make(map[string]*Modem)
	r.subs = nil
	r.mu.Unlock()
	var result error
	for _, modem := range modems {
		result = errors.Join(result, modem.Close())
	}
	return result
}

func (r *Registry) WaitForModem(ctx context.Context, current *Modem) (*Modem, error) {
	return r.waitForGeneration(ctx, current)
}

func (r *Registry) WaitForReloadedModem(ctx context.Context, current *Modem) (*Modem, error) {
	return r.waitForGeneration(ctx, current)
}

func (r *Registry) waitForGeneration(ctx context.Context, current *Modem) (*Modem, error) {
	if current == nil {
		return nil, errModemRequired
	}
	ready := make(chan *Modem, 1)
	unsubscribe, err := r.Subscribe(func(event ModemEvent) error {
		if event.Modem != nil && samePhysicalModem(current, event.Modem) && event.Generation > current.Generation() {
			select {
			case ready <- event.Modem:
			default:
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	defer unsubscribe()
	if candidate, err := r.findModem(current.EquipmentIdentifier); err == nil && candidate.Generation() > current.Generation() {
		return candidate, nil
	}
	select {
	case modem := <-ready:
		return modem, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (r *Registry) findModem(id string) (*Modem, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("%w: equipment identifier is empty", ErrNotFound)
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, modem := range r.modems {
		if strings.TrimSpace(modem.EquipmentIdentifier) == id {
			return modem, nil
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
}

func (r *Registry) copyModemsLocked() map[string]*Modem { return maps.Clone(r.modems) }

func physicalDeviceKey(device wwanmodem.Device) string {
	if path := strings.TrimSpace(device.PhysicalPath); path != "" {
		return path
	}
	return strings.TrimSpace(device.Path)
}

func samePhysicalModem(a, b *Modem) bool {
	if a == nil || b == nil {
		return false
	}
	if a.EquipmentIdentifier != "" && b.EquipmentIdentifier != "" {
		return a.EquipmentIdentifier == b.EquipmentIdentifier
	}
	return a.Path() != "" && a.Path() == b.Path()
}

func sameControlDevice(a, b wwanmodem.Device) bool {
	if strings.TrimSpace(a.Path) != "" && strings.TrimSpace(b.Path) != "" {
		return strings.TrimSpace(a.Path) == strings.TrimSpace(b.Path)
	}
	return physicalDeviceKey(a) != "" && physicalDeviceKey(a) == physicalDeviceKey(b)
}

func sameDeviceDescription(a, b wwanmodem.Device) bool {
	if a.Path != b.Path || a.Protocol != b.Protocol || a.Driver != b.Driver || a.PhysicalPath != b.PhysicalPath {
		return false
	}
	if !slices.Equal(a.NetworkInterfaces, b.NetworkInterfaces) || !slices.Equal(a.Ports, b.Ports) {
		return false
	}
	return true
}

func openDiscoveredModem(ctx context.Context, device wwanmodem.Device, generation uint64) (*Modem, error) {
	core, err := wwanmodem.Open(ctx, device.Path, wwanmodem.AccessAuto)
	if err != nil {
		return nil, err
	}
	info, err := core.Info(ctx)
	if err != nil {
		_ = core.Close()
		return nil, fmt.Errorf("read modem info: %w", err)
	}
	m := &Modem{
		core: core, deviceInfo: device, deviceKey: physicalDeviceKey(device), generation: generation,
		Device: device.PhysicalPath, Manufacturer: info.Manufacturer, EquipmentIdentifier: strings.TrimSpace(info.EquipmentID),
		Driver: device.Driver, Model: info.Model, FirmwareRevision: info.Revision, HardwareRevision: info.HardwareRevision,
		PrimaryPort: device.Path, ussd: wwanmodem.USSDMessage{State: wwanmodem.USSDStateIdle},
	}
	if m.Device == "" {
		m.Device = device.Path
	}
	if len(info.OwnNumbers) > 0 {
		m.runtimeMu.Lock()
		m.Number = strings.TrimSpace(info.OwnNumbers[0])
		m.runtimeMu.Unlock()
	}
	for _, port := range device.Ports {
		path := port.Path
		if port.Type == wwanmodem.PortNetwork {
			path = port.Name
		}
		m.Ports = append(m.Ports, ModemPort{PortType: legacyPortType(port.Type), Device: path})
	}
	if status, statusErr := core.Status(ctx); statusErr == nil {
		m.applyStatus(status)
	}
	if simInfo, simErr := core.SIMInfo(ctx); simErr == nil {
		m.applySIMInfo(simInfo)
	}
	if slots, slotsErr := core.SIMSlots(ctx); slotsErr == nil {
		m.applySIMSlots(slots)
	}
	return m, nil
}

func legacyPortType(portType wwanmodem.PortType) ModemPortType {
	switch portType {
	case wwanmodem.PortQMI:
		return ModemPortTypeQmi
	case wwanmodem.PortMBIM:
		return ModemPortTypeMbim
	case wwanmodem.PortAT:
		return ModemPortTypeAt
	case wwanmodem.PortNetwork:
		return ModemPortTypeNet
	default:
		return ModemPortTypeUnknown
	}
}

func legacyModemState(status wwanmodem.Status) ModemState {
	if status.SIM == wwanmodem.SIMStateFailure {
		return ModemStateFailed
	}
	if status.SIM == wwanmodem.SIMStateLocked {
		return ModemStateLocked
	}
	if status.Power != wwanmodem.PowerStateOn {
		return ModemStateDisabled
	}
	if status.OwnBearers > 0 {
		return ModemStateConnected
	}
	switch status.Registration {
	case wwanmodem.RegistrationHome, wwanmodem.RegistrationRoaming:
		return ModemStateRegistered
	case wwanmodem.RegistrationSearching:
		return ModemStateSearching
	default:
		return ModemStateEnabled
	}
}
