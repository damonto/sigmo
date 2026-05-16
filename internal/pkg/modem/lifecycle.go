package modem

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
)

func EnableDisabled(ctx context.Context, registry *Registry) error {
	if registry == nil {
		return errors.New("modem registry is required")
	}
	modems, err := registry.Modems(ctx)
	if err != nil {
		return fmt.Errorf("list modems: %w", err)
	}
	var result error
	for _, modem := range modems {
		result = errors.Join(result, enableDisabledModem(ctx, modem))
	}
	return result
}

func RunEnableDisabled(ctx context.Context, registry *Registry) error {
	task := newPresenceTask(registry, func(modemCtx context.Context, modem *Modem) {
		if err := enableDisabledModem(modemCtx, modem); err != nil && modemCtx.Err() == nil {
			slog.Warn("enable modem", "modem", modem.EquipmentIdentifier, "error", err)
		}
	})
	return task.Run(ctx)
}

func enableDisabledModem(ctx context.Context, modem *Modem) error {
	if modem == nil {
		return errModemRequired
	}
	if modem.State != ModemStateDisabled {
		return nil
	}
	slog.Info("enabling modem", "modem", modem.EquipmentIdentifier, "path", modem.objectPath)
	if err := modem.Enable(ctx); err != nil {
		return fmt.Errorf("enable modem: %w", err)
	}
	return nil
}
