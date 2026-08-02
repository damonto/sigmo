package modemtask

import (
	"context"
	"log/slog"
	"slices"
	"time"

	wwanmodem "github.com/damonto/wwan-go/modem"

	"github.com/damonto/sigmo/internal/pkg/modem"
)

const smsStorageRetryInterval = 5 * time.Second

func RunSMSStorageDefaults(ctx context.Context, registry Registry, storage wwanmodem.MessageStorage) error {
	return Run(ctx, registry, func(modemCtx context.Context, m *modem.Modem) {
		setDefaultSMSStorage(modemCtx, m, storage)
	})
}

func setDefaultSMSStorage(ctx context.Context, m *modem.Modem, storage wwanmodem.MessageStorage) {
	warned := false
	for {
		if err := setDefaultSMSStorageOnce(ctx, m, storage); err != nil {
			if ctx.Err() != nil {
				return
			}
			if warned {
				slog.Debug("retry SMS default storage", "imei", m.EquipmentIdentifier, "storage", messageStorageName(storage), "error", err)
			} else {
				slog.Warn("set SMS default storage", "imei", m.EquipmentIdentifier, "storage", messageStorageName(storage), "error", err)
				warned = true
			}
			if err := sleepContext(ctx, smsStorageRetryInterval); err != nil {
				return
			}
			continue
		}
		return
	}
}

func setDefaultSMSStorageOnce(ctx context.Context, m *modem.Modem, storage wwanmodem.MessageStorage) error {
	messaging := m.Messaging()
	supported, err := messaging.SupportedStorages(ctx)
	if err != nil {
		return err
	}
	if !slices.Contains(supported, storage) {
		slog.Info("SMS default storage unsupported", "imei", m.EquipmentIdentifier, "storage", messageStorageName(storage), "supported", supported)
		return nil
	}

	current, err := messaging.DefaultStorage(ctx)
	if err != nil {
		return err
	}
	if current == storage {
		slog.Debug("SMS default storage already configured", "imei", m.EquipmentIdentifier, "storage", messageStorageName(storage))
		return nil
	}

	if err := messaging.SetDefaultStorage(ctx, storage); err != nil {
		return err
	}
	slog.Info("SMS default storage set", "imei", m.EquipmentIdentifier, "storage", messageStorageName(storage))
	return nil
}

func messageStorageName(storage wwanmodem.MessageStorage) string {
	switch storage {
	case wwanmodem.MessageStorageDevice:
		return "device"
	case wwanmodem.MessageStorageSIM:
		return "sim"
	default:
		return "unknown"
	}
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
