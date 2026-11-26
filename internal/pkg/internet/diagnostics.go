package internet

import (
	"context"
	"strings"
)

// Diagnostic work lives only as long as the tracked cellular connection.
// Copies of trackedConnection share its diagnostics, even when preferences change.
type connectionDiagnostics struct {
	lifetime context.Context
	cancel   context.CancelFunc
	publicIP publicIPCache
}

func (s *connectionDiagnostics) retire() {
	if s != nil {
		s.cancel()
	}
}

func (c *Connector) startConnectionMonitoring(tracked *trackedConnection) {
	tracked.traffic = c.startConnectionTraffic(tracked.interfaceName)
	lifetime, cancel := context.WithCancel(context.Background())
	tracked.diagnostics = &connectionDiagnostics{lifetime: lifetime, cancel: cancel}
}

// The caller holds the modem operation lock until it has captured the route.
func (c *Connector) diagnosticsLocked(ctx context.Context, access modemAccess) (*Connection, *connectionDiagnostics, error) {
	connection, err := c.currentModemLocked(ctx, access)
	if err != nil {
		return nil, nil, err
	}
	if connection.Status != StatusConnected || strings.TrimSpace(connection.InterfaceName) == "" {
		return nil, nil, ErrNotConnected
	}
	var tracked trackedConnection
	if qmap := c.qmapConnection(access.modem); qmap != nil {
		tracked = qmap.tracked
	} else {
		tracked, _ = c.connection(access.id())
	}
	if tracked.modemGeneration != access.generation() || tracked.profileID != access.profileID() ||
		tracked.diagnostics == nil || tracked.diagnostics.lifetime.Err() != nil {
		return nil, nil, ErrNotConnected
	}
	return connection, tracked.diagnostics, nil
}
