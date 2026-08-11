package internet

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	wwanmodem "github.com/damonto/wwan-go/modem"
)

const airplaneModeReloadTimeout = 30 * time.Second

var (
	ErrAirplaneMode               = errors.New("internet connection is unavailable in airplane mode")
	errAirplaneModeChangeRequired = errors.New("airplane mode change is required")
)

// ChangeAirplaneMode keeps the radio transition serialized with Internet
// operations. A suspended connection retains its Always-On policy so disabling
// Airplane mode can restore only connections the user asked Sigmo to maintain.
func (c *Connector) ChangeAirplaneMode(ctx context.Context, modem *mmodem.Modem, targetEnabled bool, apply func() (applied bool, err error)) error {
	if modem == nil {
		return ErrModemRequired
	}
	if apply == nil {
		return errAirplaneModeChangeRequired
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	applied, transitionErr := c.beginAndApplyAirplaneModeChange(ctx, modem, targetEnabled, apply)

	restoreModem := modem
	var reloadErr error
	if !targetEnabled && applied {
		reloadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), airplaneModeReloadTimeout)
		restoreModem, reloadErr = c.reloadAfterAirplaneMode(reloadCtx, modem)
		cancel()
	}

	completeErr := c.finishAirplaneModeChange(restoreModem, targetEnabled, applied)
	return errors.Join(transitionErr, reloadErr, completeErr)
}

func (c *Connector) beginAndApplyAirplaneModeChange(ctx context.Context, modem *mmodem.Modem, targetEnabled bool, apply func() (applied bool, err error)) (bool, error) {
	defer c.lockModem(modem.EquipmentIdentifier)()
	beginErr := c.beginAirplaneModeChangeLocked(ctx, modem, targetEnabled)
	applied, changeErr := apply()
	return applied, errors.Join(beginErr, changeErr)
}

func (c *Connector) finishAirplaneModeChange(modem *mmodem.Modem, targetEnabled bool, radioStateApplied bool) error {
	defer c.lockModem(modem.EquipmentIdentifier)()
	return c.completeAirplaneModeChangeLocked(modem, targetEnabled, radioStateApplied)
}

func (c *Connector) reloadAfterAirplaneMode(ctx context.Context, modem *mmodem.Modem) (*mmodem.Modem, error) {
	if modem == nil {
		return nil, ErrModemRequired
	}
	if modem.PrimaryPortType() != wwanmodem.PortQMI || c.reloadModem == nil {
		return modem, nil
	}
	replacement, err := c.reloadModem(ctx, modem)
	if err != nil {
		return modem, fmt.Errorf("reload QMI modem after airplane mode: %w", err)
	}
	if replacement == nil {
		return modem, errors.New("reload QMI modem after airplane mode: replacement is nil")
	}
	return replacement, nil
}

// BeginAirplaneModeChange blocks new Internet sessions for the transition. On
// enable it also removes the active data path without clearing Always-On.
func (c *Connector) BeginAirplaneModeChange(ctx context.Context, modem *mmodem.Modem, targetEnabled bool) error {
	if modem == nil {
		return ErrModemRequired
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	defer c.lockModem(modem.EquipmentIdentifier)()
	return c.beginAirplaneModeChangeLocked(ctx, modem, targetEnabled)
}

func (c *Connector) beginAirplaneModeChangeLocked(ctx context.Context, modem *mmodem.Modem, targetEnabled bool) error {
	if !targetEnabled {
		current, err := c.airplaneModeEnabled(ctx, modem)
		if err != nil {
			c.setAirplaneModeState(modem.EquipmentIdentifier, true)
			return fmt.Errorf("read airplane mode before Internet transition: %w", err)
		}
		c.setAirplaneModeState(modem.EquipmentIdentifier, current)
		return nil
	}

	c.setAirplaneModeState(modem.EquipmentIdentifier, true)
	access := modemAccess{modem: modem}
	if c.qmapConnectionFor(access.id(), access.generation()) != nil {
		return c.disconnectQMAPLocked(ctx, modem)
	}
	return c.disconnect(ctx, access, false)
}

// CompleteAirplaneModeChange records the applied radio state. When the final
// state is online it immediately retries the saved Always-On policy.
func (c *Connector) CompleteAirplaneModeChange(modem *mmodem.Modem, targetEnabled bool, radioStateApplied bool) error {
	if modem == nil {
		return ErrModemRequired
	}
	defer c.lockModem(modem.EquipmentIdentifier)()
	return c.completeAirplaneModeChangeLocked(modem, targetEnabled, radioStateApplied)
}

func (c *Connector) completeAirplaneModeChangeLocked(modem *mmodem.Modem, targetEnabled bool, radioStateApplied bool) error {
	// These two outcomes leave the radio offline: enabling succeeded, or
	// disabling was not applied.
	if targetEnabled == radioStateApplied {
		return nil
	}
	c.setAirplaneModeState(modem.EquipmentIdentifier, false)
	c.requestAlwaysOnRestore(modem)
	return nil
}

func (c *Connector) rejectAirplaneMode(ctx context.Context, modem *mmodem.Modem) error {
	enabled, err := c.airplaneModeEnabled(ctx, modem)
	if err != nil {
		return err
	}
	if enabled {
		return ErrAirplaneMode
	}
	return nil
}

func (c *Connector) airplaneModeEnabled(ctx context.Context, modem *mmodem.Modem) (bool, error) {
	if modem == nil {
		return false, ErrModemRequired
	}
	if enabled, ok := c.airplaneModeState(modem.EquipmentIdentifier); ok {
		return enabled, nil
	}
	return c.savedAirplaneMode(ctx, modem)
}

func (c *Connector) airplaneModeState(modemID string) (bool, bool) {
	modemID = strings.TrimSpace(modemID)
	if modemID == "" {
		return false, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	enabled, ok := c.airplaneModeStates[modemID]
	return enabled, ok
}

func (c *Connector) setAirplaneModeState(modemID string, enabled bool) {
	modemID = strings.TrimSpace(modemID)
	if modemID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.airplaneModeStates == nil {
		c.airplaneModeStates = make(map[string]bool)
	}
	c.airplaneModeStates[modemID] = enabled
}
