package internet

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

const alwaysOnMonitorInterval = 10 * time.Second

func (c *Connector) RunAlwaysOn(ctx context.Context, manager *mmodem.Manager) {
	c.restoreAlwaysOnModems(ctx, manager)

	ticker := time.NewTicker(alwaysOnMonitorInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.restoreAlwaysOnModems(ctx, manager)
		}
	}
}

func (c *Connector) restoreAlwaysOnModems(ctx context.Context, manager *mmodem.Manager) {
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

	modems, err := manager.Modems()
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
		if err := c.restoreAlwaysOn(modem, prefs); err != nil {
			slog.Warn("restore internet always on connection", "modem", modem.EquipmentIdentifier, "error", err)
		}
	}
}

func (c *Connector) restoreAlwaysOn(modem *mmodem.Modem, prefs Preferences) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	latest, ok, err := loadAlwaysOnStateForModem(c.alwaysOnPath, modem.EquipmentIdentifier)
	if err != nil {
		return fmt.Errorf("load always on state: %w", err)
	}
	if !ok || !latest.AlwaysOn {
		return nil
	}
	prefs = latest
	prefs.AlwaysOn = true
	current, err := currentBearer(modem)
	if err != nil {
		return err
	}
	if current.bearer != nil && current.connected {
		return c.recoverAlwaysOnLocked(modem, current.bearer, prefs)
	}

	_, err = c.connectLocked(modem, prefs, false)
	if err != nil {
		return fmt.Errorf("connect always on bearer: %w", err)
	}
	return nil
}

func (c *Connector) recoverAlwaysOnLocked(modem *mmodem.Modem, bearer *mmodem.Bearer, prefs Preferences) error {
	tracked, _, ok, err := recoverTrackedConnection(modem.EquipmentIdentifier, bearer, prefs)
	if err != nil {
		return err
	}
	if !ok {
		return ErrUnsupportedIPMethod
	}
	tracked.prefs.AlwaysOn = true
	if err := c.syncProxyPreference(tracked.interfaceName, tracked.prefs); err != nil {
		return err
	}
	if err := c.syncAlwaysOnState(modem.EquipmentIdentifier, tracked.prefs); err != nil {
		return fmt.Errorf("sync always on state: %w", err)
	}
	c.connections[modem.EquipmentIdentifier] = tracked
	c.preferences[modem.EquipmentIdentifier] = tracked.prefs
	return nil
}
