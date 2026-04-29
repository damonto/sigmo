package modem

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"sync"

	"github.com/godbus/dbus/v5"
)

const (
	ModemManagerManagedObjects = "org.freedesktop.DBus.ObjectManager.GetManagedObjects"
	ModemManagerObjectPath     = "/org/freedesktop/ModemManager1"

	ModemManagerInterface = "org.freedesktop.ModemManager1"

	ModemManagerInterfacesAdded   = "org.freedesktop.DBus.ObjectManager.InterfacesAdded"
	ModemManagerInterfacesRemoved = "org.freedesktop.DBus.ObjectManager.InterfacesRemoved"
)

type Manager struct {
	dbusConn   *dbus.Conn
	dbusObject dbus.BusObject
	modems     map[dbus.ObjectPath]*Modem
	mu         sync.RWMutex
	subs       []subscription
	nextSubID  uint64
	subscribe  sync.Once
}

var errModemRequired = errors.New("modem is required")

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

func NewManager() (*Manager, error) {
	m := &Manager{
		modems: make(map[dbus.ObjectPath]*Modem, 16),
	}
	var err error
	m.dbusConn, err = dbus.SystemBus()
	if err != nil {
		return nil, err
	}
	m.dbusObject = m.dbusConn.Object(ModemManagerInterface, ModemManagerObjectPath)
	return m, nil
}

func (m *Manager) ScanDevices() error {
	return m.dbusObject.Call(ModemManagerInterface+".ScanDevices", 0).Err
}

func (m *Manager) InhibitDevice(uid string, inhibit bool) error {
	return m.dbusObject.Call(ModemManagerInterface+".InhibitDevice", 0, uid, inhibit).Err
}

func (m *Manager) Modems() (map[dbus.ObjectPath]*Modem, error) {
	managedObjects := make(map[dbus.ObjectPath]map[string]map[string]dbus.Variant)
	if err := m.dbusObject.Call(ModemManagerManagedObjects, 0).Store(&managedObjects); err != nil {
		return nil, err
	}
	modems := make(map[dbus.ObjectPath]*Modem, len(managedObjects))
	for objectPath, data := range managedObjects {
		if _, ok := data["org.freedesktop.ModemManager1.Modem"]; !ok {
			continue
		}
		modem, err := m.createModem(objectPath, data["org.freedesktop.ModemManager1.Modem"])
		if err != nil {
			slog.Error("failed to create modem", "error", err)
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

func (m *Manager) createModem(objectPath dbus.ObjectPath, data map[string]dbus.Variant) (*Modem, error) {
	drivers := variantStrings(data, "Drivers")
	driver := ""
	if len(drivers) > 0 {
		driver = drivers[0]
	}
	modem := Modem{
		mmgr:                m,
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
		PrimaryPort:         fmt.Sprintf("/dev/%s", variantString(data, "PrimaryPort")),
		PrimarySimSlot:      variantUint[uint32](data, "PrimarySimSlot"),
	}
	if modem.State == ModemStateDisabled {
		slog.Info("enabling modem", "path", objectPath)
		if err := modem.Enable(); err != nil {
			slog.Error("failed to enable modem", "error", err)
			return nil, err
		}
	}
	var err error
	modem.Sim, err = modem.SIMs().Get(variantObjectPath(data, "Sim"))
	if err != nil {
		return nil, err
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
		modem.Ports = append(modem.Ports, ModemPort{
			PortType: ModemPortType(portType),
			Device:   fmt.Sprintf("/dev/%s", name),
		})
	}
	for _, slot := range variantObjectPaths(data, "SimSlots") {
		if slot != "/" {
			modem.SimSlots = append(modem.SimSlots, slot)
		}
	}
	return &modem, nil
}

func (m *Manager) Subscribe(subscriber func(ModemEvent) error) (func(), error) {
	if subscriber == nil {
		return nil, errors.New("subscriber is required")
	}
	m.mu.Lock()
	m.nextSubID++
	id := m.nextSubID
	m.subs = append(m.subs, subscription{id: id, fn: subscriber})
	m.mu.Unlock()

	var err error
	m.subscribe.Do(func() {
		err = m.startSubscription()
	})
	if err != nil {
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

func (m *Manager) WaitForModem(ctx context.Context, current *Modem) (*Modem, error) {
	if current == nil {
		return nil, errModemRequired
	}
	ready := make(chan *Modem, 1)
	notify := func(event ModemEvent) error {
		if event.Type != ModemEventAdded || event.Modem == nil {
			return nil
		}
		if !isReplacementModem(current, event.Modem) {
			return nil
		}
		select {
		case ready <- event.Modem:
		default:
		}
		return nil
	}

	unsubscribe, err := m.Subscribe(notify)
	if err != nil {
		return nil, err
	}
	defer unsubscribe()

	if modem := m.findReplacementModem(current); modem != nil {
		return modem, nil
	}

	select {
	case modem := <-ready:
		return modem, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (m *Manager) findReplacementModem(current *Modem) *Modem {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, modem := range m.modems {
		if isReplacementModem(current, modem) {
			return modem
		}
	}
	return nil
}

func isReplacementModem(current *Modem, candidate *Modem) bool {
	if current == nil || candidate == nil {
		return false
	}
	if candidate.EquipmentIdentifier != current.EquipmentIdentifier {
		return false
	}
	return candidate != current
}

func (m *Manager) deleteAndUpdate(modem *Modem) {
	// If user restart the ModemManager manually, Dbus will not send the InterfacesRemoved signal
	// But it will send the InterfacesAdded signal again.
	// So we need to remove the duplicate modem manually and update it.
	for path, v := range m.modems {
		if v.EquipmentIdentifier == modem.EquipmentIdentifier {
			slog.Info("removing duplicate modem", "path", path, "equipmentIdentifier", modem.EquipmentIdentifier)
			delete(m.modems, path)
		}
	}
	m.modems[modem.objectPath] = modem
}

func (m *Manager) startSubscription() error {
	if err := m.dbusConn.AddMatchSignal(
		dbus.WithMatchInterface("org.freedesktop.DBus.ObjectManager"),
		dbus.WithMatchMember("InterfacesAdded"),
		dbus.WithMatchPathNamespace("/org/freedesktop/ModemManager1"),
	); err != nil {
		return err
	}
	if err := m.dbusConn.AddMatchSignal(
		dbus.WithMatchInterface("org.freedesktop.DBus.ObjectManager"),
		dbus.WithMatchMember("InterfacesRemoved"),
		dbus.WithMatchPathNamespace("/org/freedesktop/ModemManager1"),
	); err != nil {
		return err
	}

	sig := make(chan *dbus.Signal, 10)
	m.dbusConn.Signal(sig)
	go m.handleSignals(sig)
	return nil
}

func (m *Manager) handleSignals(sig <-chan *dbus.Signal) {
	for event := range sig {
		modemPath := event.Body[0].(dbus.ObjectPath)
		var (
			modem     *Modem
			eventType ModemEventType
		)
		if event.Name == ModemManagerInterfacesAdded {
			eventType = ModemEventAdded
			slog.Info("new modem plugged in", "path", modemPath)
			raw := event.Body[1].(map[string]map[string]dbus.Variant)
			var err error
			modem, err = m.createModem(modemPath, raw["org.freedesktop.ModemManager1.Modem"])
			if err != nil {
				slog.Error("failed to create modem", "error", err)
				continue
			}
		} else {
			eventType = ModemEventRemoved
			slog.Info("modem unplugged", "path", modemPath)
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
				slog.Error("failed to process modem", "error", err)
			}
		}
	}
}

func (m *Manager) copyModemsLocked() map[dbus.ObjectPath]*Modem {
	snapshot := make(map[dbus.ObjectPath]*Modem, len(m.modems))
	maps.Copy(snapshot, m.modems)
	return snapshot
}
