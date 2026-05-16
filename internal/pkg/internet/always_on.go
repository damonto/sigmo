package internet

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

const alwaysOnMonitorInterval = 10 * time.Second

func (c *Connector) RunAlwaysOn(ctx context.Context, registry *mmodem.Registry) {
	c.restoreAlwaysOnModems(ctx, registry)

	ticker := time.NewTicker(alwaysOnMonitorInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.restoreAlwaysOnModems(ctx, registry)
		}
	}
}

func (c *Connector) restoreAlwaysOnModems(ctx context.Context, registry *mmodem.Registry) {
	if err := ctx.Err(); err != nil {
		return
	}
	states, err := loadAlwaysOnStates(c.alwaysOnPath)
	if err != nil {
		slog.Warn("load internet always on state", "error", err)
		return
	}
	if len(states) == 0 {
		return
	}

	modems, err := registry.Modems(ctx)
	if err != nil {
		slog.Warn("list modems for internet always on", "error", err)
		return
	}
	for _, modem := range modems {
		if modem == nil {
			continue
		}
		prefs, ok := states[modem.EquipmentIdentifier]
		if !ok || !prefs.AlwaysOn {
			continue
		}
		if err := c.restoreAlwaysOn(ctx, modemAccess{modem: modem}, prefs); err != nil {
			slog.Warn("restore internet always on connection", "modem", modem.EquipmentIdentifier, "error", err)
		}
	}
}

func (c *Connector) restoreAlwaysOn(ctx context.Context, modem internetModem, prefs Preferences) error {
	modemID := modem.id()
	defer c.lockModem(modemID)()

	latest, ok, err := loadAlwaysOnStateForModem(c.alwaysOnPath, modemID)
	if err != nil {
		return fmt.Errorf("load always on state: %w", err)
	}
	if !ok || !latest.AlwaysOn {
		return nil
	}
	prefs = latest
	prefs.AlwaysOn = true
	current, err := currentBearer(ctx, modem)
	if err != nil {
		return err
	}
	if current.bearer != nil && current.connected {
		return c.recoverAlwaysOn(ctx, modem, current.bearer, prefs)
	}

	_, err = c.connect(ctx, modem, prefs, false)
	if err != nil {
		return fmt.Errorf("connect always on bearer: %w", err)
	}
	return nil
}

func (c *Connector) recoverAlwaysOn(ctx context.Context, modem internetModem, bearer *mmodem.Bearer, prefs Preferences) error {
	modemID := modem.id()
	tracked, _, ok, err := recoverTrackedConnection(ctx, c.proxyPath, c.routePath, modemID, bearer, prefs)
	if err != nil {
		return err
	}
	if !ok {
		return ErrUnsupportedIPMethod
	}
	tracked.prefs.AlwaysOn = true
	if err := c.syncProxyPreference(modemID, tracked.interfaceName, tracked.prefs); err != nil {
		return err
	}
	if err := c.syncAlwaysOnState(modemID, tracked.prefs); err != nil {
		return fmt.Errorf("sync always on state: %w", err)
	}
	c.setConnectionAndPreference(modemID, tracked, tracked.prefs)
	return nil
}
