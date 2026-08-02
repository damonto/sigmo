package modemtask

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	wwanmodem "github.com/damonto/wwan-go/modem"

	"github.com/damonto/sigmo/internal/pkg/modem"
)

// EnableDisabledPolicy decides whether an otherwise disabled modem should stay disabled.
type EnableDisabledPolicy func(context.Context, *modem.Modem) (bool, error)

func EnableDisabled(ctx context.Context, registry Registry, policy EnableDisabledPolicy) error {
	if registry == nil {
		return errors.New("modem registry is required")
	}
	present, err := registry.Modems(ctx)
	if err != nil {
		return fmt.Errorf("list modems: %w", err)
	}
	var result error
	for _, m := range present {
		result = errors.Join(result, enableDisabled(ctx, m, policy))
	}
	return result
}

func RunEnableDisabled(ctx context.Context, registry Registry, policy EnableDisabledPolicy) error {
	return Run(ctx, registry, func(modemCtx context.Context, m *modem.Modem) {
		if err := enableDisabled(modemCtx, m, policy); err != nil && modemCtx.Err() == nil {
			slog.Warn("enable modem", "imei", m.EquipmentIdentifier, "error", err)
		}
	})
}

func enableDisabled(ctx context.Context, m *modem.Modem, policy EnableDisabledPolicy) error {
	if m == nil {
		return errors.New("modem is required")
	}
	status := m.Snapshot().Status
	if status.SIM == wwanmodem.SIMStateLocked || status.SIM == wwanmodem.SIMStateFailure {
		return nil
	}
	if status.Power != wwanmodem.PowerStateOff && status.Power != wwanmodem.PowerStateLow {
		return nil
	}
	if policy != nil {
		skip, err := policy(ctx, m)
		if err != nil {
			return fmt.Errorf("evaluate modem enable policy: %w", err)
		}
		if skip {
			slog.Info("skip enabling modem", "imei", m.EquipmentIdentifier, "path", m.Path())
			return nil
		}
	}
	slog.Info("enabling modem", "imei", m.EquipmentIdentifier, "path", m.Path())
	if err := m.Enable(ctx); err != nil {
		return fmt.Errorf("enable modem: %w", err)
	}
	return nil
}
