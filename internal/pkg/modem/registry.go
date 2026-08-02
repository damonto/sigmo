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
	"github.com/damonto/wwan-go/qcom"
)

var (
	registryOpenTimeout     = 15 * time.Second
	registryWatchRetryDelay = time.Second
)

const (
	qualcomm410ControlPortResolveAttempts = 10
	qualcomm410ControlPortResolveInterval = 50 * time.Millisecond
)

var (
	ErrNotFound                          = errors.New("modem not found")
	errModemRequired                     = errors.New("modem is required")
	errQualcomm410Data5PortNotDiscovered = errors.New("Qualcomm 410 DATA5 QMI control port is not mapped to a discovered port")
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
	failures     chan modemFailure
	// A physical reconnect resets the bounded CID-exhaustion recovery state.
	cidRecoveryStates map[string]cidRecoveryState
}

type cidRecoveryState uint8

const (
	cidRecoveryUnused cidRecoveryState = iota
	cidRecoveryRetried
	cidRecoverySuspended
)

type modemFailure struct {
	modem *Modem
	err   error
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
		modems:            make(map[string]*Modem),
		discover:          wwanmodem.Discover,
		watchDevices:      wwanmodem.WatchDevices,
		open:              openDiscoveredModem,
		failures:          make(chan modemFailure, 32),
		cidRecoveryStates: make(map[string]cidRecoveryState),
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
	present := make(map[string]wwanmodem.Device, len(devices))
	for _, device := range devices {
		key := physicalDeviceKey(device)
		if key != "" {
			present[key] = device
		}
	}
	r.clearAbsentCIDRecoveryStates(present)
	opened := make(map[string]*Modem, len(devices))
	for _, device := range devices {
		key := physicalDeviceKey(device)
		if key == "" {
			continue
		}
		if r.cidRecoveryState(key) == cidRecoverySuspended {
			slog.Warn("skip opening modem while QMI client ID recovery is suspended", "device", controlPortPath(device), "physical_path", key)
			continue
		}
		generation := r.nextGenerationToken()
		modem, err := r.open(ctx, device, generation)
		if err != nil {
			if errors.Is(err, qcom.QMIErrorClientIdsExhausted) {
				r.advanceCIDRecoveryState(key)
			}
			slog.Warn("open discovered modem", "device", controlPortPath(device), "physical_path", key, "error", err)
			continue
		}
		r.watchModem(runCtx, modem)
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
		case failure := <-r.failures:
			r.handleModemFailure(ctx, failure)
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
	}
	r.clearAbsentCIDRecoveryStates(present)
	for _, device := range devices {
		if physicalDeviceKey(device) == "" {
			continue
		}
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
		r.clearCIDRecoveryState(key)
		r.mu.RLock()
		existingKey, existing := r.findByDeviceLocked(event.Device)
		r.mu.RUnlock()
		r.removeModem(existingKey, existing)
		return
	}
	if event.Type == wwanmodem.DeviceAdded {
		r.clearCIDRecoveryState(key)
	}

	r.mu.RLock()
	existingKey, existing := r.findByDeviceLocked(event.Device)
	r.mu.RUnlock()
	if event.Type == wwanmodem.DevicePresent && existing != nil && sameDeviceDescription(existing.deviceInfo, event.Device) {
		return
	}
	if existing == nil && r.cidRecoveryState(key) == cidRecoverySuspended {
		return
	}

	var (
		generation  uint64
		replacement *Modem
	)
	for {
		generation = r.nextGenerationToken()
		openCtx, cancel := context.WithTimeout(ctx, registryOpenTimeout)
		var err error
		replacement, err = r.open(openCtx, event.Device, generation)
		cancel()
		if err == nil {
			break
		}

		slog.Warn("open changed modem", "device", controlPortPath(event.Device), "physical_path", key, "error", err)
		if !errors.Is(err, qcom.QMIErrorClientIdsExhausted) {
			return
		}
		state := r.advanceCIDRecoveryState(key)
		if state == cidRecoverySuspended || ctx.Err() != nil {
			return
		}
		// Do not allocate more client IDs while the current generation still owns its IDs.
		if existing != nil {
			return
		}
	}
	r.watchModem(ctx, replacement)

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

func (r *Registry) watchModem(ctx context.Context, modem *Modem) {
	modem.startRuntimeWatchers(ctx, func(err error) {
		if r.failures == nil {
			return
		}
		select {
		case r.failures <- modemFailure{modem: modem, err: err}:
		case <-ctx.Done():
		}
	})
}

func (r *Registry) handleModemFailure(ctx context.Context, failure modemFailure) {
	if failure.modem == nil {
		return
	}
	r.mu.RLock()
	key := r.keyForModemLocked(failure.modem)
	r.mu.RUnlock()
	if key == "" {
		return
	}

	clientIDsExhausted := errors.Is(failure.err, qcom.QMIErrorClientIdsExhausted)
	recoverySuspended := clientIDsExhausted && r.advanceCIDRecoveryState(key) == cidRecoverySuspended
	message := "modem transport stopped"
	if clientIDsExhausted {
		message = "modem QMI client IDs exhausted"
	}
	slog.Warn(message, "imei", failure.modem.EquipmentIdentifier, "generation", failure.modem.Generation(), "error", failure.err)
	r.removeModem(key, failure.modem)
	if ctx.Err() != nil {
		return
	}
	if recoverySuspended {
		slog.Error("suspend modem recovery until device reconnects", "imei", failure.modem.EquipmentIdentifier, "generation", failure.modem.Generation(), "error", failure.err)
		return
	}
	if err := r.reconcile(ctx); err != nil {
		slog.Error("recover modem after transport stop", "imei", failure.modem.EquipmentIdentifier, "generation", failure.modem.Generation(), "error", err)
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

func (r *Registry) cidRecoveryState(key string) cidRecoveryState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.cidRecoveryStates[key]
}

func (r *Registry) advanceCIDRecoveryState(key string) cidRecoveryState {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cidRecoveryStates == nil {
		r.cidRecoveryStates = make(map[string]cidRecoveryState)
	}
	state := cidRecoveryRetried
	if r.cidRecoveryStates[key] >= cidRecoveryRetried {
		state = cidRecoverySuspended
	}
	r.cidRecoveryStates[key] = state
	return state
}

func (r *Registry) clearCIDRecoveryState(key string) {
	r.mu.Lock()
	delete(r.cidRecoveryStates, key)
	r.mu.Unlock()
}

func (r *Registry) clearAbsentCIDRecoveryStates(present map[string]wwanmodem.Device) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for key := range r.cidRecoveryStates {
		if _, ok := present[key]; !ok {
			delete(r.cidRecoveryStates, key)
		}
	}
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

func (r *Registry) WaitForReloadedModem(ctx context.Context, current *Modem) (*Modem, error) {
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
	for _, port := range device.Ports {
		if path := strings.TrimSpace(port.SysPath); path != "" {
			return path
		}
	}
	return controlPortPath(device)
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
	for _, aPort := range controlPorts(a) {
		for _, bPort := range controlPorts(b) {
			if strings.TrimSpace(aPort.Path) != "" && strings.TrimSpace(aPort.Path) == strings.TrimSpace(bPort.Path) {
				return true
			}
		}
	}
	return physicalDeviceKey(a) != "" && physicalDeviceKey(a) == physicalDeviceKey(b)
}

func sameDeviceDescription(a, b wwanmodem.Device) bool {
	return a.PhysicalPath == b.PhysicalPath && slices.Equal(a.Ports, b.Ports)
}

func openDiscoveredModem(ctx context.Context, device wwanmodem.Device, generation uint64) (*Modem, error) {
	ports, resolveErr := controlPortsForOpen(ctx, device)
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("resolve modem control ports: %w", err)
	}
	if resolveErr != nil {
		// Preserve modem availability on other dual-QMI Qualcomm platforms,
		// while making the fallback visible instead of silently choosing DATA6.
		slog.Warn("use discovered QMI control port order", "physical_path", physicalDeviceKey(device), "error", resolveErr)
	}
	if len(ports) == 0 {
		return nil, errors.New("open discovered modem: no QMI or MBIM control port")
	}
	var openErrs []error
	for _, port := range ports {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("open discovered modem: %w", err)
		}
		core, err := wwanmodem.Open(ctx, port, wwanmodem.AccessAuto)
		if err != nil {
			openErrs = append(openErrs, fmt.Errorf("open control port %s: %w", port.Path, err))
			continue
		}
		info, err := core.Info(ctx)
		if err != nil {
			openErrs = append(openErrs, fmt.Errorf("read modem info from %s: %w", port.Path, errors.Join(err, core.Close())))
			continue
		}
		m := newDiscoveredModem(ctx, discoveredModemConfig{
			core:       core,
			device:     device,
			port:       port,
			info:       info,
			generation: generation,
		})
		return m, nil
	}
	return nil, fmt.Errorf("open discovered modem: %w", errors.Join(openErrs...))
}

type discoveredModemConfig struct {
	core       *wwanmodem.Modem
	device     wwanmodem.Device
	port       wwanmodem.Port
	info       wwanmodem.Info
	generation uint64
}

func newDiscoveredModem(ctx context.Context, cfg discoveredModemConfig) *Modem {
	m := &Modem{
		core:                cfg.core,
		deviceInfo:          cfg.device,
		deviceKey:           physicalDeviceKey(cfg.device),
		generation:          cfg.generation,
		Device:              cfg.device.PhysicalPath,
		Manufacturer:        cfg.info.Manufacturer,
		EquipmentIdentifier: strings.TrimSpace(cfg.info.EquipmentID),
		Driver:              cfg.port.Driver,
		Model:               cfg.info.Model,
		FirmwareRevision:    cfg.info.Revision,
		HardwareRevision:    cfg.info.HardwareRevision,
		PrimaryPort:         cfg.port.Path,
		ussd:                wwanmodem.USSDMessage{State: wwanmodem.USSDStateIdle},
	}
	if m.Device == "" {
		m.Device = cfg.port.Path
	}
	if len(cfg.info.OwnNumbers) > 0 {
		m.runtimeMu.Lock()
		m.Number = strings.TrimSpace(cfg.info.OwnNumbers[0])
		m.runtimeMu.Unlock()
	}
	for _, candidate := range cfg.device.Ports {
		path := candidate.Path
		if candidate.Type == wwanmodem.PortNetwork {
			path = candidate.Name
		}
		m.Ports = append(m.Ports, ModemPort{PortType: legacyPortType(candidate.Type), Device: path})
	}
	// Initial snapshots are best effort; runtime watchers refresh these fields.
	if status, err := cfg.core.Status(ctx); err == nil {
		m.applyStatus(status)
	}
	if simInfo, err := cfg.core.SIMInfo(ctx); err == nil {
		m.applySIMInfo(simInfo)
	}
	if slots, err := cfg.core.SIMSlots(ctx); err == nil {
		m.applySIMSlots(slots)
	}
	return m
}

func controlPorts(device wwanmodem.Device) []wwanmodem.Port {
	return controlPortsWithSameDevice(device, sameDeviceNode)
}

func controlPortsWithSameDevice(device wwanmodem.Device, sameDevice func(string, string) (bool, error)) []wwanmodem.Port {
	ports := listedControlPorts(device)
	if device.Bus != wwanmodem.BusPlatform || !hasMultipleQMIControlPorts(ports) {
		return ports
	}
	// This best-effort path is also used by logging and identity checks. The
	// modem open path performs the bounded retry needed during device reload.
	_ = preferQualcomm410InternetControlPort(ports, sameDevice)
	return ports
}

func listedControlPorts(device wwanmodem.Device) []wwanmodem.Port {
	ports := make([]wwanmodem.Port, 0, len(device.Ports))
	for _, portType := range []wwanmodem.PortType{wwanmodem.PortQMI, wwanmodem.PortMBIM} {
		for _, port := range device.Ports {
			if port.Type == portType && strings.TrimSpace(port.Path) != "" {
				ports = append(ports, port)
			}
		}
	}
	return ports
}

func hasMultipleQMIControlPorts(ports []wwanmodem.Port) bool {
	count := 0
	for _, port := range ports {
		if port.Type != wwanmodem.PortQMI {
			break
		}
		count++
		if count == 2 {
			return true
		}
	}
	return false
}

type controlPortResolver struct {
	sameDevice func(string, string) (bool, error)
	wait       func(context.Context) error
}

func controlPortsForOpen(ctx context.Context, device wwanmodem.Device) ([]wwanmodem.Port, error) {
	return controlPortsForOpenWithResolver(ctx, device, controlPortResolver{
		sameDevice: sameDeviceNode,
		wait: func(ctx context.Context) error {
			return sleepContext(ctx, qualcomm410ControlPortResolveInterval)
		},
	})
}

func controlPortsForOpenWithResolver(ctx context.Context, device wwanmodem.Device, resolver controlPortResolver) ([]wwanmodem.Port, error) {
	ports := listedControlPorts(device)
	if device.Bus != wwanmodem.BusPlatform || !hasMultipleQMIControlPorts(ports) {
		return ports, nil
	}

	attempts := 0
	var resolveErr error
	for attempts < qualcomm410ControlPortResolveAttempts {
		attempts++
		resolveErr = preferQualcomm410InternetControlPort(ports, resolver.sameDevice)
		if resolveErr == nil {
			return ports, nil
		}
		if attempts == qualcomm410ControlPortResolveAttempts || resolver.wait == nil {
			break
		}
		if err := resolver.wait(ctx); err != nil {
			return ports, fmt.Errorf("wait for Qualcomm 410 DATA5 QMI control port: %w", err)
		}
	}
	return ports, fmt.Errorf("resolve Qualcomm 410 DATA5 QMI control port after %d attempts: %w", attempts, resolveErr)
}

func preferQualcomm410InternetControlPort(ports []wwanmodem.Port, sameDevice func(string, string) (bool, error)) error {
	preferred := slices.IndexFunc(ports, func(port wwanmodem.Port) bool {
		return port.Type == wwanmodem.PortQMI && strings.TrimSpace(port.Path) == Qualcomm410InternetQMI
	})
	if preferred < 0 && sameDevice != nil {
		var compareErr error
		for i, port := range ports {
			if port.Type != wwanmodem.PortQMI {
				break
			}
			match, err := sameDevice(Qualcomm410InternetQMI, strings.TrimSpace(port.Path))
			if err != nil {
				compareErr = errors.Join(compareErr, fmt.Errorf("compare DATA5 with QMI control port %s: %w", port.Path, err))
				continue
			}
			if match {
				preferred = i
				break
			}
		}
		if preferred < 0 && compareErr != nil {
			return errors.Join(errQualcomm410Data5PortNotDiscovered, compareErr)
		}
	}
	if preferred < 0 {
		return errQualcomm410Data5PortNotDiscovered
	}
	if preferred <= 0 {
		return nil
	}
	port := ports[preferred]
	copy(ports[1:preferred+1], ports[:preferred])
	ports[0] = port
	return nil
}

func controlPortPath(device wwanmodem.Device) string {
	ports := controlPorts(device)
	if len(ports) == 0 {
		return ""
	}
	return strings.TrimSpace(ports[0].Path)
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
