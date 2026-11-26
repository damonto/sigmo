package internet

import (
	"context"
	"errors"
	"time"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

// SpeedTestSize is the combined download and upload payload in decimal MB.
type SpeedTestSize int

// DefaultSpeedTestSize preserves the original data usage for callers that omit a size.
const DefaultSpeedTestSize SpeedTestSize = 10

// Validate rejects sizes outside the supported measurement presets.
func (size SpeedTestSize) Validate() error {
	switch size {
	case DefaultSpeedTestSize, 25, 50, 100:
		return nil
	default:
		return errors.New("speed test size must be 10, 25, 50, or 100 MB")
	}
}

func (size SpeedTestSize) timeout() time.Duration {
	return time.Duration(size) * 2 * time.Second
}

// SpeedTestStatus associates a saved result with the currently active SIM.
type SpeedTestStatus struct {
	ICCID  string
	Result *storage.InternetSpeedTest
}

// LatestSpeedTest reads the active SIM's last successful result, including when offline.
func (c *Connector) LatestSpeedTest(ctx context.Context, modem *mmodem.Modem) (SpeedTestStatus, error) {
	if modem == nil {
		return SpeedTestStatus{}, ErrModemRequired
	}
	identity := modem.SIMIdentity()
	if !modem.MatchesSIMActivation(identity) {
		return SpeedTestStatus{}, nil
	}
	result, err := c.state.LatestInternetSpeedTest(ctx, identity.ICCID)
	if err != nil {
		return SpeedTestStatus{}, err
	}
	if !modem.MatchesSIMActivation(identity) {
		return SpeedTestStatus{}, mmodem.ErrSIMChanged
	}
	return SpeedTestStatus{ICCID: identity.ICCID, Result: result}, nil
}

// RunSpeedTest measures the expected SIM's connection and saves both rates on success.
func (c *Connector) RunSpeedTest(ctx context.Context, modem *mmodem.Modem, iccid string, size SpeedTestSize) (SpeedTestStatus, error) {
	return c.runSpeedTest(ctx, modem, iccid, size, measureBoundSpeed)
}

func (c *Connector) runSpeedTest(ctx context.Context, modem *mmodem.Modem, iccid string, size SpeedTestSize, measure func(context.Context, *Connection, SpeedTestSize) (storage.InternetSpeedTest, error)) (SpeedTestStatus, error) {
	if err := size.Validate(); err != nil {
		return SpeedTestStatus{}, err
	}
	if modem == nil {
		return SpeedTestStatus{}, ErrModemRequired
	}
	identity := modem.SIMIdentity()
	if identity.ICCID != iccid || !modem.MatchesSIMActivation(identity) {
		return SpeedTestStatus{}, mmodem.ErrSIMChanged
	}
	if !c.beginSpeedTest(iccid) {
		return SpeedTestStatus{}, ErrOperationInProgress
	}
	defer c.endSpeedTest(iccid)
	ctx, cancel := context.WithTimeout(ctx, size.timeout())
	defer cancel()

	var connection *Connection
	var session *connectionDiagnostics
	if err := c.withSpeedTestTarget(ctx, modem, identity, func(current *Connection, diagnostics *connectionDiagnostics) error {
		connection, session = current, diagnostics
		return nil
	}); err != nil {
		return SpeedTestStatus{}, err
	}
	stop := context.AfterFunc(session.lifetime, cancel)
	defer stop()
	if session.lifetime.Err() != nil {
		return SpeedTestStatus{}, ErrNotConnected
	}

	result, err := measure(ctx, connection, size)
	if session.lifetime.Err() != nil {
		return SpeedTestStatus{}, ErrNotConnected
	}
	if err != nil {
		return SpeedTestStatus{}, err
	}
	if err := c.withSpeedTestTarget(ctx, modem, identity, func(_ *Connection, current *connectionDiagnostics) error {
		if current != session {
			return ErrNotConnected
		}
		return c.state.SaveInternetSpeedTest(ctx, iccid, result)
	}); err != nil {
		return SpeedTestStatus{}, err
	}
	return SpeedTestStatus{ICCID: iccid, Result: &result}, nil
}

func (c *Connector) withSpeedTestTarget(ctx context.Context, modem *mmodem.Modem, identity mmodem.SIMIdentity, use func(*Connection, *connectionDiagnostics) error) error {
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
	if !modem.MatchesSIMActivation(identity) {
		return mmodem.ErrSIMChanged
	}
	connection, diagnostics, err := c.diagnosticsLocked(ctx, access)
	if err != nil {
		return err
	}
	if !modem.MatchesSIMActivation(identity) {
		return mmodem.ErrSIMChanged
	}
	return use(connection, diagnostics)
}

func (c *Connector) beginSpeedTest(iccid string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, running := c.speedTests[iccid]; running {
		return false
	}
	if c.speedTests == nil {
		c.speedTests = make(map[string]struct{})
	}
	c.speedTests[iccid] = struct{}{}
	return true
}

func (c *Connector) endSpeedTest(iccid string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.speedTests, iccid)
}
