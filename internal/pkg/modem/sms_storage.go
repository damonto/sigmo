package modem

import (
	"context"
	"log/slog"
	"slices"
	"time"
)

const smsStorageRetryInterval = 5 * time.Second

func (manager *Manager) RunSMSStorageDefaults(ctx context.Context, storage SMSStorage) error {
	runner := newPresenceRunner(manager, func(modemCtx context.Context, m *Modem) {
		setDefaultSMSStorage(modemCtx, m, storage)
	})
	return runner.Run(ctx)
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
