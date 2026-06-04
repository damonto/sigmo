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
		access := modemAccess{modem: modem}
		profileID := access.profileID()
		if profileID == "" {
			continue
		}
		prefs, ok := states[profileID]
		if !ok || !prefs.AlwaysOn {
			continue
		}
		if err := c.restoreAlwaysOn(ctx, access, prefs); err != nil {
			slog.Warn("restore internet always on connection", "modem", modem.EquipmentIdentifier, "error", err)
		}
	}
}

func (c *Connector) restoreAlwaysOn(ctx context.Context, modem internetModem, prefs Preferences) error {
	modemID := modem.id()
	defer c.lockModem(modemID)()

	profileID := modem.profileID()
	if profileID == "" {
		return nil
	}
	latest, ok, err := c.loadAlwaysOnStateForProfile(ctx, profileID)
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
const profileScopePrefix = "profile:"

func profileScope(profileID string) string {
	return profileScopePrefix + strings.TrimSpace(profileID)
}

func (c *Connector) loadAlwaysOnStates(ctx context.Context) (map[string]Preferences, error) {
	raw, err := c.state.ListRaw(ctx, profileScopePrefix, alwaysOnKVKey)
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
		profileID := strings.TrimPrefix(scope, profileScopePrefix)
		if strings.TrimSpace(profileID) != "" {
			states[profileID] = prefs
		}
	}
	return states, nil
}

func (c *Connector) loadAlwaysOnStateForProfile(ctx context.Context, profileID string) (Preferences, bool, error) {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return Preferences{}, false, nil
	}
	var prefs Preferences
	err := c.state.Get(ctx, profileScope(profileID), alwaysOnKVKey, &prefs)
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
	profileID := modem.profileID()
	tracked, _, ok, err := recoverTrackedConnection(ctx, c.persistence, modemID, bearer, prefs)
	if err != nil {
		return err
	}
	if !ok {
		return ErrUnsupportedIPMethod
	}
	tracked.profileID = profileID
	tracked.prefs.AlwaysOn = true
	if err := c.syncProxyPreference(ctx, modemID, tracked.interfaceName, tracked.prefs); err != nil {
		return err
	}
	if err := c.syncAlwaysOnState(ctx, profileID, tracked.prefs); err != nil {
		return fmt.Errorf("sync always on state: %w", err)
	}
	c.setConnectionAndPreference(modemID, tracked, tracked.prefs)
	return nil
}
