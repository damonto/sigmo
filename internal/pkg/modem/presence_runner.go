package modem

import (
	"context"
	"fmt"
	"sync"

	"github.com/godbus/dbus/v5"
)

type presenceRunner struct {
	manager *Manager
	start   func(context.Context, *Modem)
}

func newPresenceRunner(manager *Manager, start func(context.Context, *Modem)) *presenceRunner {
	return &presenceRunner{
		manager: manager,
		start:   start,
	}
}

func (r *presenceRunner) Run(ctx context.Context) error {
	var mu sync.Mutex
	cancels := make(map[dbus.ObjectPath]context.CancelFunc)
	equipment := make(map[string]dbus.ObjectPath)
	modems := make(map[dbus.ObjectPath]string)

	addModem := func(path dbus.ObjectPath, m *Modem) {
		if ctx.Err() != nil || m == nil {
			return
		}

		modemCtx, cancel := context.WithCancel(ctx)
		var oldCancels []context.CancelFunc

		mu.Lock()
		if existingCancel, ok := cancels[path]; ok {
			oldCancels = append(oldCancels, existingCancel)
			delete(cancels, path)
			if equipmentID, ok := modems[path]; ok {
				delete(modems, path)
				delete(equipment, equipmentID)
			}
		}
		if m.EquipmentIdentifier != "" {
			if existingPath, ok := equipment[m.EquipmentIdentifier]; ok && existingPath != path {
				if existingCancel, ok := cancels[existingPath]; ok {
					oldCancels = append(oldCancels, existingCancel)
				}
				delete(cancels, existingPath)
				delete(modems, existingPath)
				delete(equipment, m.EquipmentIdentifier)
			}
		}
		cancels[path] = cancel
		if m.EquipmentIdentifier != "" {
			equipment[m.EquipmentIdentifier] = path
			modems[path] = m.EquipmentIdentifier
		}
		mu.Unlock()

		for _, oldCancel := range oldCancels {
			oldCancel()
		}
		go r.start(modemCtx, m)
	}

	removeModem := func(path dbus.ObjectPath) {
		var cancel context.CancelFunc
		mu.Lock()
		cancel = cancels[path]
		delete(cancels, path)
		if equipmentID, ok := modems[path]; ok {
			delete(modems, path)
			delete(equipment, equipmentID)
		}
		mu.Unlock()
		if cancel != nil {
			cancel()
		}
	}

	stopAll := func() {
		mu.Lock()
		allCancels := make([]context.CancelFunc, 0, len(cancels))
		for _, cancel := range cancels {
			allCancels = append(allCancels, cancel)
		}
		cancels = make(map[dbus.ObjectPath]context.CancelFunc)
		equipment = make(map[string]dbus.ObjectPath)
		modems = make(map[dbus.ObjectPath]string)
		mu.Unlock()

		for _, cancel := range allCancels {
			cancel()
		}
	}

	unsubscribe, err := r.manager.Subscribe(func(event ModemEvent) error {
		switch event.Type {
		case ModemEventAdded:
			addModem(event.Path, event.Modem)
		case ModemEventRemoved:
			removeModem(event.Path)
		}
		return nil
	})
	if err != nil {
		stopAll()
		return fmt.Errorf("subscribe modem manager: %w", err)
	}
	defer unsubscribe()

	modemMap, err := r.manager.Modems()
	if err != nil {
		stopAll()
		return fmt.Errorf("list modems: %w", err)
	}
	for path, m := range modemMap {
		addModem(path, m)
	}

	<-ctx.Done()
	stopAll()
	return nil
}
