package internet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/storage"
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
	states, err := c.loadAlwaysOnStates(ctx)
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

	latest, ok, err := c.loadAlwaysOnStateForModem(ctx, modemID)
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

const alwaysOnKVKey = "internet.always_on"
const modemScopePrefix = "modem:"

func modemScope(modemID string) string {
	return modemScopePrefix + strings.TrimSpace(modemID)
}

func (c *Connector) loadAlwaysOnStates(ctx context.Context) (map[string]Preferences, error) {
	raw, err := c.state.ListRaw(ctx, modemScopePrefix, alwaysOnKVKey)
	if err != nil {
		return nil, err
	}
	states := make(map[string]Preferences, len(raw))
	for scope, value := range raw {
		var prefs Preferences
		if err := json.Unmarshal([]byte(value), &prefs); err != nil {
			return nil, fmt.Errorf("decode always on state for %s: %w", scope, err)
		}
		if !prefs.AlwaysOn {
			continue
		}
		modemID := strings.TrimPrefix(scope, modemScopePrefix)
		if strings.TrimSpace(modemID) != "" {
			states[modemID] = prefs
		}
	}
	return states, nil
}

func (c *Connector) loadAlwaysOnStateForModem(ctx context.Context, modemID string) (Preferences, bool, error) {
	var prefs Preferences
	err := c.state.Get(ctx, modemScope(modemID), alwaysOnKVKey, &prefs)
	if errors.Is(err, storage.ErrNotFound) {
		return Preferences{}, false, nil
	}
	if err != nil {
		return Preferences{}, false, err
	}
	if !prefs.AlwaysOn {
		return Preferences{}, false, nil
	}
	return prefs, true, nil
}

func (c *Connector) recoverAlwaysOn(ctx context.Context, modem internetModem, bearer *mmodem.Bearer, prefs Preferences) error {
	modemID := modem.id()
	tracked, _, ok, err := recoverTrackedConnection(ctx, c.persistence, modemID, bearer, prefs)
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
