package modem

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
)

// EnableDisabledPolicy decides whether an otherwise disabled modem should stay disabled.
type EnableDisabledPolicy func(context.Context, *Modem) (bool, error)

func EnableDisabled(ctx context.Context, registry *Registry, policy EnableDisabledPolicy) error {
	if registry == nil {
		return errors.New("modem registry is required")
	}
	modems, err := registry.Modems(ctx)
	if err != nil {
		return fmt.Errorf("list modems: %w", err)
	}
	var result error
	for _, modem := range modems {
		result = errors.Join(result, enableDisabledModem(ctx, modem, policy))
	}
	return result
}

func RunEnableDisabled(ctx context.Context, registry *Registry, policy EnableDisabledPolicy) error {
	task := newPresenceTask(registry, func(modemCtx context.Context, modem *Modem) {
		if err := enableDisabledModem(modemCtx, modem, policy); err != nil && modemCtx.Err() == nil {
			slog.Warn("enable modem", "imei", modem.EquipmentIdentifier, "error", err)
		}
	})
	return task.Run(ctx)
}

func enableDisabledModem(ctx context.Context, modem *Modem, policy EnableDisabledPolicy) error {
	if modem == nil {
		return errModemRequired
	}
	if modem.State != ModemStateDisabled {
		return nil
	}
	if policy != nil {
		skip, err := policy(ctx, modem)
		if err != nil {
			return err
		}
		if skip {
			slog.Info("skip enabling modem", "imei", modem.EquipmentIdentifier, "path", modem.objectPath)
			return nil
		}
	}
	slog.Info("enabling modem", "imei", modem.EquipmentIdentifier, "path", modem.objectPath)
	if err := modem.Enable(ctx); err != nil {
		return fmt.Errorf("enable modem: %w", err)
	}
	return nil
}
