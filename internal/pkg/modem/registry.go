package modem

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	ModemManagerManagedObjects = "org.freedesktop.DBus.ObjectManager.GetManagedObjects"
	ModemManagerObjectPath     = "/org/freedesktop/ModemManager1"

	ModemManagerInterface = "org.freedesktop.ModemManager1"

	ModemManagerInterfacesAdded   = "org.freedesktop.DBus.ObjectManager.InterfacesAdded"
	ModemManagerInterfacesRemoved = "org.freedesktop.DBus.ObjectManager.InterfacesRemoved"
)

var waitForModemRefreshInterval = time.Second

const modemSignalLoadTimeout = 5 * time.Second

type Registry struct {
	dbusConn   *dbus.Conn
	dbusObject dbus.BusObject
	modems     map[dbus.ObjectPath]*Modem
	mu         sync.RWMutex
	startMu    sync.Mutex
	signalChan chan *dbus.Signal
	done       chan struct{}
	subs       []subscription
	nextSubID  uint64
	subscribed bool
	closed     bool
}

var (
	ErrNotFound      = errors.New("modem not found")
	errModemRequired = errors.New("modem is required")
)

type ModemEventType int

const (
	ModemEventAdded ModemEventType = iota
	ModemEventRemoved
)

func (t ModemEventType) String() string {
	switch t {
	case ModemEventAdded:
		return "added"
	case ModemEventRemoved:
		return "removed"
	default:
		return "unknown"
	}
}

type ModemEvent struct {
	Type     ModemEventType
	Modem    *Modem
	Path     dbus.ObjectPath
	Snapshot map[dbus.ObjectPath]*Modem
}

type subscription struct {
	id uint64
	fn func(ModemEvent) error
}

func NewRegistry() (*Registry, error) {
	m := &Registry{
		modems: make(map[dbus.ObjectPath]*Modem, 16),
	}
	var err error
	m.dbusConn, err = dbus.SystemBus()
	if err != nil {
		return nil, fmt.Errorf("connect system bus: %w", err)
	}
	m.dbusObject = m.dbusConn.Object(ModemManagerInterface, ModemManagerObjectPath)
	return m, nil
}

func (m *Registry) ScanDevices(ctx context.Context) error {
	return m.dbusObject.CallWithContext(ctx, ModemManagerInterface+".ScanDevices", 0).Err
}

func (m *Registry) InhibitDevice(ctx context.Context, uid string, inhibit bool) error {
	return m.dbusObject.CallWithContext(ctx, ModemManagerInterface+".InhibitDevice", 0, uid, inhibit).Err
}

func (m *Registry) Modems(ctx context.Context) (map[dbus.ObjectPath]*Modem, error) {
	managedObjects := make(map[dbus.ObjectPath]map[string]map[string]dbus.Variant)
	if err := m.dbusObject.CallWithContext(ctx, ModemManagerManagedObjects, 0).Store(&managedObjects); err != nil {
		return nil, err
	}
	modems := make(map[dbus.ObjectPath]*Modem, len(managedObjects))
	for objectPath, data := range managedObjects {
		modemData, ok := data[ModemInterface]
		if !ok {
			continue
		}
		modem, err := m.createModem(ctx, objectPath, modemData)
		if err != nil {
			slog.Error("create modem", "error", err)
			continue
		}
		modems[objectPath] = modem
	}
	m.mu.Lock()
	m.modems = modems
	snapshot := m.copyModemsLocked()
	m.mu.Unlock()
	return snapshot, nil
}

func (m *Registry) Find(ctx context.Context, id string) (*Modem, error) {
	modems, err := m.Modems(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing modems: %w", err)
	}
	for _, modem := range modems {
		if modem.EquipmentIdentifier == id {
			return modem, nil
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
}

func (m *Registry) createModem(ctx context.Context, objectPath dbus.ObjectPath, data map[string]dbus.Variant) (*Modem, error) {
	drivers := variantStrings(data, "Drivers")
	driver := ""
	if len(drivers) > 0 {
		driver = drivers[0]
	}
	primaryPort := devicePath(variantString(data, "PrimaryPort"))
	modem := Modem{
		dbusConn:            m.dbusConn,
		inhibitDevice:       m.InhibitDevice,
		objectPath:          objectPath,
		dbusObject:          m.dbusConn.Object(ModemManagerInterface, objectPath),
		Device:              variantString(data, "Device"),
		Manufacturer:        variantString(data, "Manufacturer"),
		EquipmentIdentifier: variantString(data, "EquipmentIdentifier"),
		Driver:              driver,
		Model:               variantString(data, "Model"),
		FirmwareRevision:    variantString(data, "Revision"),
		HardwareRevision:    variantString(data, "HardwareRevision"),
		State:               ModemState(variantInt32(data, "State")),
		PrimaryPort:         primaryPort,
		PrimarySimSlot:      variantUint[uint32](data, "PrimarySimSlot"),
	}
	var err error
	primarySIMPath := variantObjectPath(data, "Sim")
	if validSIMObjectPath(primarySIMPath) {
		modem.Sim, err = modem.SIMs().Get(ctx, primarySIMPath)
		if err != nil {
			if ctx.Err() != nil {
				return nil, fmt.Errorf("read primary SIM: %w", err)
			}
			slog.Warn("read primary SIM", "path", primarySIMPath, "modem", modem.EquipmentIdentifier, "error", err)
		}
	}
	if numbers := variantStrings(data, "OwnNumbers"); len(numbers) > 0 {
		modem.Number = numbers[0]
	}
	for _, port := range variantAnySlices(data, "Ports") {
		if len(port) < 2 {
			continue
		}
		name, _ := port[0].(string)
		portType, _ := port[1].(uint32)
		device := devicePath(name)
		if device == "" {
			continue
		}
		modem.Ports = append(modem.Ports, ModemPort{
			PortType: ModemPortType(portType),
			Device:   device,
		})
	}
	modem.SimSlots = simSlotPaths(data, primarySIMPath)
	return &modem, nil
}

func devicePath(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if strings.HasPrefix(name, "/dev/") {
		return name
	}
	return fmt.Sprintf("/dev/%s", name)
}

func simSlotPaths(data map[string]dbus.Variant, primarySIMPath dbus.ObjectPath) []dbus.ObjectPath {
	slots := variantObjectPaths(data, "SimSlots")
	paths := make([]dbus.ObjectPath, 0, len(slots))
	for _, slot := range slots {
		if !validSIMObjectPath(slot) {
			continue
		}
		paths = append(paths, slot)
	}
	if len(paths) > 0 {
		return paths
	}
	if !validSIMObjectPath(primarySIMPath) {
		return nil
	}
	return []dbus.ObjectPath{primarySIMPath}
}

func validSIMObjectPath(path dbus.ObjectPath) bool {
	return path != "" && path != "/"
}

func (m *Registry) Subscribe(subscriber func(ModemEvent) error) (func(), error) {
	if subscriber == nil {
		return nil, errors.New("subscriber is required")
	}
	m.mu.Lock()
	m.nextSubID++
	id := m.nextSubID
	m.subs = append(m.subs, subscription{id: id, fn: subscriber})
	m.mu.Unlock()

	if err := m.ensureSubscriptionStarted(); err != nil {
		m.mu.Lock()
		for i, sub := range m.subs {
			if sub.id == id {
				m.subs = append(m.subs[:i], m.subs[i+1:]...)
				break
			}
		}
		m.mu.Unlock()
		return nil, err
	}

	unsubscribe := func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		for i, sub := range m.subs {
			if sub.id == id {
				m.subs = append(m.subs[:i], m.subs[i+1:]...)
				break
			}
		}
	}
	return unsubscribe, nil
}

func (m *Registry) ensureSubscriptionStarted() error {
	m.startMu.Lock()
	defer m.startMu.Unlock()

	if m.closed {
		return errors.New("modem registry is closed")
	}
	m.mu.RLock()
	started := m.subscribed
	m.mu.RUnlock()
	if started {
		return nil
	}

	if err := m.startSubscription(); err != nil {
		return err
	}
	m.mu.Lock()
	m.subscribed = true
	m.mu.Unlock()
	return nil
}

func (m *Registry) Close() error {
	m.startMu.Lock()
	defer m.startMu.Unlock()

	m.closed = true
	if !m.subscribed {
		return nil
	}
	m.mu.Lock()
	m.subscribed = false
	m.subs = nil
	m.mu.Unlock()

	var result error
	if m.dbusConn != nil {
		if m.signalChan != nil {
			m.dbusConn.RemoveSignal(m.signalChan)
			m.signalChan = nil
		}
		result = errors.Join(result, m.dbusConn.RemoveMatchSignal(modemAddedMatchOptions()...))
		result = errors.Join(result, m.dbusConn.RemoveMatchSignal(modemRemovedMatchOptions()...))
	}
	if m.done != nil {
		close(m.done)
		m.done = nil
	}
	return result
}

func (m *Registry) WaitForModem(ctx context.Context, current *Modem) (*Modem, error) {
	return m.WaitForModemAfter(ctx, current, nil)
}

func (m *Registry) WaitForModemAfter(ctx context.Context, current *Modem, action func() error) (*Modem, error) {
	if current == nil {
		return nil, errModemRequired
	}
	ready := make(chan *Modem, 1)
	reload := newModemReloadState()
	notify := func(event ModemEvent) error {
		switch event.Type {
		case ModemEventRemoved:
			if isCurrentModemEvent(current, event) {
				reload.mark()
			}
			return nil
		case ModemEventAdded:
			if !readyModemEvent(current, event.Modem, reload.observed()) {
				return nil
			}
			select {
			case ready <- event.Modem:
			default:
			}
			return nil
		default:
			return nil
		}
	}

	unsubscribe, err := m.Subscribe(notify)
	if err != nil {
		return nil, err
	}
	defer unsubscribe()

	if action != nil {
		if err := action(); err != nil {
			if !isReloadStarted(err) {
				return nil, err
			}
			reload.mark()
			slog.Info("waiting for modem after reload started", "modem", current.EquipmentIdentifier, "error", err)
		}
	} else if modem := m.findReadyModem(current, reload.observed()); modem != nil {
		return modem, nil
	}

	return m.waitForReadyModem(ctx, current, ready, reload)
}

type modemReloadState struct {
	mu       sync.RWMutex
	reloaded bool
}

func newModemReloadState() *modemReloadState {
	return &modemReloadState{}
}

func (s *modemReloadState) mark() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reloaded = true
}

func (s *modemReloadState) observed() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.reloaded
}

func (m *Registry) waitForReadyModem(ctx context.Context, current *Modem, ready <-chan *Modem, reload *modemReloadState) (*Modem, error) {
	ticker := time.NewTicker(waitForModemRefreshInterval)
	defer ticker.Stop()

	for {
		if modem := m.findReadyModem(current, reload.observed()); modem != nil {
			return modem, nil
		}

		select {
		case modem := <-ready:
			return modem, nil
		case <-ticker.C:
			modem, missing, err := m.refreshReadyModem(ctx, current, reload.observed())
			if err != nil {
				slog.Warn("refresh modem while waiting", "modem", current.EquipmentIdentifier, "error", err)
				continue
			}
			if missing {
				reload.mark()
				continue
			}
			if modem != nil {
				return modem, nil
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (m *Registry) findReadyModem(current *Modem, reloadObserved bool) *Modem {
	m.mu.RLock()
	defer m.mu.RUnlock()
	modem, _ := readyModemIn(current, m.modems, reloadObserved)
	return modem
}

func (m *Registry) refreshReadyModem(ctx context.Context, current *Modem, reloadObserved bool) (*Modem, bool, error) {
	if m.dbusObject == nil {
		m.mu.RLock()
		defer m.mu.RUnlock()
		modem, found := readyModemIn(current, m.modems, reloadObserved)
		return modem, !found, nil
	}
	modems, err := m.Modems(ctx)
	if err != nil {
		return nil, false, err
	}
	modem, found := readyModemIn(current, modems, reloadObserved)
	return modem, !found, nil
}

func readyModemIn(current *Modem, modems map[dbus.ObjectPath]*Modem, reloadObserved bool) (*Modem, bool) {
	found := false
	for _, modem := range modems {
		if !sameEquipmentIdentifier(current, modem) {
			continue
		}
		found = true
		if reloadObserved && modem != current {
			return modem, true
		}
		if !reloadObserved && isReplacementObjectPath(current, modem) {
			return modem, true
		}
	}
	return nil, found
}

func readyModemEvent(current *Modem, candidate *Modem, reloadObserved bool) bool {
	if reloadObserved {
		return sameEquipmentIdentifier(current, candidate) && candidate != current
	}
	return isReplacementObjectPath(current, candidate)
}

func isReplacementObjectPath(current *Modem, candidate *Modem) bool {
	if !sameEquipmentIdentifier(current, candidate) {
		return false
	}
	return current.objectPath != "" && candidate.objectPath != "" && candidate.objectPath != current.objectPath
}

func sameEquipmentIdentifier(current *Modem, candidate *Modem) bool {
	if current == nil || candidate == nil {
		return false
	}
	id := strings.TrimSpace(current.EquipmentIdentifier)
	return id != "" && strings.TrimSpace(candidate.EquipmentIdentifier) == id
}

func isCurrentModemEvent(current *Modem, event ModemEvent) bool {
	if current == nil {
		return false
	}
	if event.Modem != nil && sameEquipmentIdentifier(current, event.Modem) {
		return true
	}
	return event.Path != "" && event.Path == current.objectPath
}

type reloadStartedError struct {
	err error
}

// ReloadStarted marks an action error as evidence that ModemManager started replacing the modem.
func ReloadStarted(err error) error {
	if err == nil {
		return nil
	}
	return reloadStartedError{err: err}
}

func (e reloadStartedError) Error() string {
	return e.err.Error()
}

func (e reloadStartedError) Unwrap() error {
	return e.err
}

func (e reloadStartedError) reloadStarted() {}

func isReloadStarted(err error) bool {
	var target interface {
		reloadStarted()
	}
	return errors.As(err, &target)
}

func (m *Registry) deleteAndUpdate(modem *Modem) {
	// If user restart the ModemManager manually, Dbus will not send the InterfacesRemoved signal
	// But it will send the InterfacesAdded signal again.
	// So we need to remove the duplicate modem manually and update it.
	if modem.EquipmentIdentifier != "" {
		for path, existing := range m.modems {
			if existing.EquipmentIdentifier == modem.EquipmentIdentifier {
				slog.Info("removing duplicate modem", "path", path, "equipmentIdentifier", modem.EquipmentIdentifier)
				delete(m.modems, path)
			}
		}
	}
	m.modems[modem.objectPath] = modem
}

func (m *Registry) startSubscription() error {
	if m.dbusConn == nil {
		return errors.New("dbus connection is required")
	}
	if err := m.dbusConn.AddMatchSignal(modemAddedMatchOptions()...); err != nil {
		return err
	}
	if err := m.dbusConn.AddMatchSignal(modemRemovedMatchOptions()...); err != nil {
		_ = m.dbusConn.RemoveMatchSignal(modemAddedMatchOptions()...)
		return err
	}

	m.signalChan = make(chan *dbus.Signal, 10)
	m.done = make(chan struct{})
	m.dbusConn.Signal(m.signalChan)
	go m.handleSignals(m.signalChan, m.done)
	return nil
}

func modemAddedMatchOptions() []dbus.MatchOption {
	return []dbus.MatchOption{
		dbus.WithMatchInterface("org.freedesktop.DBus.ObjectManager"),
		dbus.WithMatchMember("InterfacesAdded"),
		dbus.WithMatchPathNamespace("/org/freedesktop/ModemManager1"),
	}
}

func modemRemovedMatchOptions() []dbus.MatchOption {
	return []dbus.MatchOption{
		dbus.WithMatchInterface("org.freedesktop.DBus.ObjectManager"),
		dbus.WithMatchMember("InterfacesRemoved"),
		dbus.WithMatchPathNamespace("/org/freedesktop/ModemManager1"),
	}
}

func (m *Registry) handleSignals(sig <-chan *dbus.Signal, done <-chan struct{}) {
	for {
		var event *dbus.Signal
		select {
		case <-done:
			return
		case next, ok := <-sig:
			if !ok {
				return
			}
			event = next
		}
		modemPath, ok := modemPathFromSignal(event)
		if !ok {
			name, body := signalDetails(event)
			slog.Warn("ignore modem signal with invalid body", "name", name, "body", body)
			continue
		}
		var (
			modem     *Modem
			eventType ModemEventType
		)
		switch event.Name {
		case ModemManagerInterfacesAdded:
			eventType = ModemEventAdded
			slog.Info("new modem plugged in", "path", modemPath)
			raw, ok := managedInterfacesFromSignal(event)
			if !ok {
				_, body := signalDetails(event)
				slog.Warn("ignore modem added signal with invalid interfaces", "path", modemPath, "body", body)
				continue
			}
			modemData, ok := raw[ModemInterface]
			if !ok {
				continue
			}
			loadCtx, cancel := context.WithTimeout(context.Background(), modemSignalLoadTimeout)
			var err error
			modem, err = m.createModem(loadCtx, modemPath, modemData)
			cancel()
			if err != nil {
				slog.Error("create modem", "error", err)
				continue
			}
		case ModemManagerInterfacesRemoved:
			eventType = ModemEventRemoved
			slog.Info("modem unplugged", "path", modemPath)
		default:
			continue
		}

		m.mu.Lock()
		if eventType == ModemEventAdded {
			m.deleteAndUpdate(modem)
		} else {
			modem = m.modems[modemPath]
			delete(m.modems, modemPath)
		}
		snapshot := m.copyModemsLocked()
		subscribers := append([]subscription(nil), m.subs...)
		m.mu.Unlock()

		for _, subscriber := range subscribers {
			if err := subscriber.fn(ModemEvent{
				Type:     eventType,
				Modem:    modem,
				Path:     modemPath,
				Snapshot: snapshot,
			}); err != nil {
				slog.Error("process modem event", "error", err)
			}
		}
	}
}

func signalDetails(event *dbus.Signal) (string, []any) {
	if event == nil {
		return "", nil
	}
	return event.Name, event.Body
}

func modemPathFromSignal(event *dbus.Signal) (dbus.ObjectPath, bool) {
	if event == nil || len(event.Body) == 0 {
		return "", false
	}
	path, ok := event.Body[0].(dbus.ObjectPath)
	return path, ok && path != ""
}

func managedInterfacesFromSignal(event *dbus.Signal) (map[string]map[string]dbus.Variant, bool) {
	if event == nil || len(event.Body) < 2 {
		return nil, false
	}
	raw, ok := event.Body[1].(map[string]map[string]dbus.Variant)
	return raw, ok
}

func (m *Registry) copyModemsLocked() map[dbus.ObjectPath]*Modem {
	snapshot := make(map[dbus.ObjectPath]*Modem, len(m.modems))
	maps.Copy(snapshot, m.modems)
	return snapshot
}
