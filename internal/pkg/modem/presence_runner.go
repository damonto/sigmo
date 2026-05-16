package modem

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/godbus/dbus/v5"
)

type presenceTask struct {
	registry *Registry
	start    func(context.Context, *Modem)
}

func newPresenceTask(registry *Registry, start func(context.Context, *Modem)) *presenceTask {
	return &presenceTask{
		registry: registry,
		start:    start,
	}
}

func (t *presenceTask) Run(ctx context.Context) error {
	if t.registry == nil {
		return errors.New("modem registry is required")
	}
	if t.start == nil {
		return errors.New("modem start function is required")
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	cancels := make(map[dbus.ObjectPath]context.CancelFunc)
	equipment := make(map[string]dbus.ObjectPath)
	modems := make(map[dbus.ObjectPath]string)
	stopped := false

	addModem := func(path dbus.ObjectPath, m *Modem) {
		if ctx.Err() != nil || m == nil {
			return
		}

		modemCtx, cancel := context.WithCancel(ctx)
		var oldCancels []context.CancelFunc

		mu.Lock()
		if stopped {
			mu.Unlock()
			cancel()
			return
		}
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
		wg.Add(1)
		mu.Unlock()

		for _, oldCancel := range oldCancels {
			oldCancel()
		}
		go func() {
			defer wg.Done()
			t.start(modemCtx, m)
		}()
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
		stopped = true
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

	unsubscribe, err := t.registry.Subscribe(func(event ModemEvent) error {
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
		return fmt.Errorf("subscribe modem registry: %w", err)
	}
	defer func() {
		unsubscribe()
		stopAll()
		wg.Wait()
	}()

	modemMap, err := t.registry.Modems(ctx)
	if err != nil {
		return fmt.Errorf("list modems: %w", err)
	}
	for path, m := range modemMap {
		addModem(path, m)
	}

	<-ctx.Done()
	stopAll()
	return nil
}
