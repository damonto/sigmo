package internet

import (
	"context"
	"errors"
	"fmt"
	"time"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	wwanmodem "github.com/damonto/wwan-go/modem"
)

const connectionCleanupTimeout = 15 * time.Second

// ErrAutomationProfileChanged means the modem no longer has the expected SIM activation.
var ErrAutomationProfileChanged = errors.New("internet SIM has changed")

// WithAutomationConnection uses the current cellular connection or owns a temporary one.
// The callback runs with the SIM and connector reserved until cleanup completes.
func (c *Connector) WithAutomationConnection(ctx context.Context, modem *mmodem.Modem, profile mmodem.SIMIdentity, use func(*Connection) error) (result error) {
	release, err := modem.ReserveSIMSlot(ctx)
	if err != nil {
		return err
	}
	defer release()
	access := modemAccess{modem: modem}
	unlock, err := c.acquireModem(ctx, access.id())
	if err != nil {
		return err
	}
	defer unlock()
	if !modem.MatchesSIMActivation(profile) {
		return ErrAutomationProfileChanged
	}
	if err := c.rejectAirplaneMode(ctx, modem); err != nil {
		return err
	}
	current, err := c.automationCurrent(ctx, access)
	if err != nil {
		return err
	}
	if current.Status == StatusConnected {
		return use(current)
	}

	prefs := c.preferenceWithAlwaysOn(ctx, access)
	prefs.AlwaysOn = false
	prefs.DefaultRoute = false
	prefs.ProxyEnabled = false
	current, err = c.automationConnect(ctx, access, prefs, connectTemporary)
	if err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), connectionCleanupTimeout)
		defer cancel()
		var err error
		if c.qmapConnection(modem) != nil {
			err = c.disconnectQMAPLocked(cleanup, modem)
		} else {
			err = c.disconnect(cleanup, access, false)
		}
		if err != nil {
			result = errors.Join(result, fmt.Errorf("%w: %w", ErrAutomationConnectionCleanup, err))
		}
	}()
	return use(current)
}

// ErrAutomationConnectionCleanup means the task-owned connection could not be released.
var ErrAutomationConnectionCleanup = errors.New("temporary cellular connection cleanup failed")

// RestoreAutomationConnection restores a previously connected line if it is still selected and disconnected.
func (c *Connector) RestoreAutomationConnection(ctx context.Context, modem *mmodem.Modem, profile mmodem.SIMIdentity, prefs Preferences) error {
	release, err := modem.ReserveSIMSlot(ctx)
	if err != nil {
		return err
	}
	defer release()
	access := modemAccess{modem: modem}
	unlock, err := c.acquireModem(ctx, access.id())
	if err != nil {
		return err
	}
	defer unlock()
	if !modem.MatchesSIMActivation(profile) {
		return ErrAutomationProfileChanged
	}
	if err := c.rejectAirplaneMode(ctx, modem); err != nil {
		return err
	}
	current, err := c.automationCurrent(ctx, access)
	if err != nil {
		return err
	}
	if current.Status == StatusConnected {
		return nil
	}
	_, err = c.automationConnect(ctx, access, prefs, connectRestore)
	return err
}

func (c *Connector) automationCurrent(ctx context.Context, access modemAccess) (*Connection, error) {
	if connection := c.qmapConnection(access.modem); connection != nil {
		if connection.tracked.profileID != access.profileID() {
			return nil, ErrAutomationProfileChanged
		}
		return c.qmapConnectionResponse(access.id(), connection), nil
	}
	return c.currentLocked(ctx, access)
}

func (c *Connector) automationConnect(ctx context.Context, access modemAccess, prefs Preferences, mode connectionMode) (*Connection, error) {
	if access.modem.PrimaryPortType() == wwanmodem.PortQMI && c.qmapEnabledFor(access.id()) {
		return c.connectQMAPSessionLocked(ctx, access.modem, prefs, mode)
	}
	return c.connect(ctx, access, prefs, mode)
}
