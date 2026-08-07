package internet

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

// InvalidateModem releases process-owned resources tied to one modem
// generation. Preferences remain available so a replacement generation can
// restore the connection without inheriting stale bearer or host state.
func (c *Connector) InvalidateModem(ctx context.Context, modem *mmodem.Modem) error {
	if modem == nil {
		return nil
	}
	return c.invalidateModemGeneration(ctx, modem.EquipmentIdentifier, modem.Generation())
}

func (c *Connector) invalidateModemGeneration(ctx context.Context, modemID string, generation uint64) error {
	modemID = strings.TrimSpace(modemID)
	if modemID == "" {
		return nil
	}
	defer c.lockModem(modemID)()

	var (
		tracked    *trackedConnection
		qmap       *qmapConnection
		lease      qualcomm410DataFormatLease
		interfaces []string
	)
	c.mu.Lock()
	if current, ok := c.connections[modemID]; ok && current.modemGeneration == generation {
		copy := current
		tracked = &copy
		delete(c.connections, modemID)
	}
	if current := c.qmapConnections[modemID]; current != nil && current.generation == generation {
		qmap = current
		delete(c.qmapConnections, modemID)
	}
	state, hasQualcomm410State := c.qualcomm410States[modemID]
	if hasQualcomm410State && state.generation == generation && (state.selected || state.lease != nil) {
		if !state.reconnectPending {
			switch {
			case tracked != nil:
				state.scheduleReconnect(tracked.prefs)
			case qmap != nil:
				state.scheduleReconnect(qmap.prefs)
			}
		}
		state.generation = generation
		state.reloadPending = true
		lease = state.lease
		state.lease = nil
		c.qualcomm410States[modemID] = state
	}
	c.mu.Unlock()

	appendInterface := func(name string) {
		name = strings.TrimSpace(name)
		if name != "" && !slices.Contains(interfaces, name) {
			interfaces = append(interfaces, name)
		}
	}
	var result error
	if lease != nil {
		if err := lease.Close(); err != nil {
			result = errors.Join(result, fmt.Errorf("release invalidated Qualcomm 410 Internet WDA lease: %w", err))
		}
	}
	if tracked != nil {
		appendInterface(tracked.interfaceName)
		err := c.cleanupTracked(ctx, modemID, *tracked)
		if err == nil {
			err = c.syncCleanedUpDefaultRouteState(ctx, *tracked)
		}
		result = errors.Join(result, err)
	}
	if qmap != nil {
		for _, value := range qmap.tracked {
			appendInterface(value.interfaceName)
		}
		result = errors.Join(result, qmap.cleanup(ctx, c), qmap.close())
		if qmap.modem != nil && len(qmap.muxIDs) > 0 {
			result = errors.Join(result, removeInternetQMAPMuxes(qmap.modem, qmap.muxIDs...))
		}
	}
	if tracked != nil || qmap != nil {
		result = errors.Join(result, c.cleanupStaleConnectionState(ctx, modemID, interfaces...))
	}
	if result != nil {
		return fmt.Errorf("invalidate modem %s generation %d: %w", modemID, generation, result)
	}
	return nil
}
