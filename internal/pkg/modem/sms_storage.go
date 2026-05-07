package modem

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

const smsStorageRetryInterval = 5 * time.Second

func (manager *Manager) RunSMSStorageDefaults(ctx context.Context, storage SMSStorage) error {
	var mu sync.Mutex
	cancels := make(map[dbus.ObjectPath]context.CancelFunc)
	equipment := make(map[string]dbus.ObjectPath)
	modems := make(map[dbus.ObjectPath]string)

	addModem := func(path dbus.ObjectPath, m *Modem) {
		if ctx.Err() != nil {
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
		go setDefaultSMSStorage(modemCtx, m, storage)
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

	unsubscribe, err := manager.Subscribe(func(event ModemEvent) error {
		switch event.Type {
		case ModemEventAdded:
			if event.Modem == nil {
				return nil
			}
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

	modemMap, err := manager.Modems()
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

func setDefaultSMSStorage(ctx context.Context, m *Modem, storage SMSStorage) {
	warned := false
	for {
		if err := setDefaultSMSStorageOnce(m, storage); err != nil {
			if ctx.Err() != nil {
				return
			}
			if warned {
				slog.Debug("retry SMS default storage", "modem", m.EquipmentIdentifier, "storage", storage.String(), "error", err)
			} else {
				slog.Warn("set SMS default storage", "modem", m.EquipmentIdentifier, "storage", storage.String(), "error", err)
				warned = true
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(smsStorageRetryInterval):
			}
			continue
		}
		return
	}
}

func setDefaultSMSStorageOnce(m *Modem, storage SMSStorage) error {
	messaging := m.Messaging()
	supported, err := messaging.SupportedStorages()
	if err != nil {
		return err
	}
	if !slices.Contains(supported, storage) {
		slog.Info("SMS default storage unsupported", "modem", m.EquipmentIdentifier, "storage", storage.String(), "supported", supported)
		return nil
	}

	current, err := messaging.DefaultStorage()
	if err != nil {
		return err
	}
	if current == storage {
		slog.Debug("SMS default storage already configured", "modem", m.EquipmentIdentifier, "storage", storage.String())
		return nil
	}

	if err := messaging.SetDefaultStorage(storage); err != nil {
		return err
	}
	slog.Info("SMS default storage set", "modem", m.EquipmentIdentifier, "storage", storage.String())
	return nil
}
