package internet

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/netlink"
)

type ConnectionPreferences struct {
	DefaultRoute bool
	ProxyEnabled bool
	AlwaysOn     bool
}

func (c *Connector) UpdatePreferences(ctx context.Context, modem *mmodem.Modem, next ConnectionPreferences) (*Connection, error) {
	if modem == nil {
		return nil, ErrModemRequired
	}
	access := modemAccess{modem: modem}
	modemID := access.id()
	defer c.lockRouteTransaction(modemID)()
	if err := c.rejectAirplaneMode(ctx, modem); err != nil {
		return nil, err
	}

	if connection := c.qmapConnectionFor(modemID, access.generation()); connection != nil {
		updated, err := c.updateQMAPPreferences(ctx, access, connection, next)
		if err != nil {
			return nil, err
		}
		return c.qmapConnectionResponse(modemID, updated), nil
	}
	current, err := c.currentLocked(ctx, access)
	if err != nil {
		return nil, err
	}
	if current.Status != StatusConnected {
		return nil, ErrNotConnected
	}
	tracked, ok := c.connection(modemID)
	if !ok {
		return nil, ErrNotConnected
	}
	updated, err := c.updateTrackedPreferences(ctx, access, tracked, next)
	if err != nil {
		return nil, err
	}
	c.setConnectionAndPreference(modemID, updated, updated.prefs)
	current.DefaultRoute = updated.prefs.DefaultRoute
	current.ProxyEnabled = updated.prefs.ProxyEnabled
	current.AlwaysOn = updated.prefs.AlwaysOn
	current.RouteMetric = updated.routeMetric
	current.Proxy = ProxyStatus{}
	if proxy := c.proxyInstance(); proxy != nil && current.InterfaceName != "" {
		current.Proxy = proxy.Status(modemID)
	}
	return current, nil
}

func (c *Connector) updateTrackedPreferences(ctx context.Context, modem internetModem, tracked trackedConnection, next ConnectionPreferences) (trackedConnection, error) {
	previous := tracked.prefs
	wanted := previous
	wanted.DefaultRoute = next.DefaultRoute
	wanted.ProxyEnabled = next.ProxyEnabled
	wanted.AlwaysOn = next.AlwaysOn
	if wanted.AlwaysOn && modem.profileID() == "" {
		return tracked, ErrProfileIDRequired
	}

	updated := cloneTrackedConnection(tracked)
	var err error
	if previous.DefaultRoute != wanted.DefaultRoute {
		updated, err = c.updateTrackedDefaultRoute(ctx, modem.id(), updated, wanted.DefaultRoute)
		if err != nil {
			return tracked, fmt.Errorf("update default route preference: %w", err)
		}
	}

	proxyChanged := previous.ProxyEnabled != wanted.ProxyEnabled
	if proxyChanged {
		if err := c.applyProxyPreference(ctx, modem.id(), updated.interfaceName, updated.dns, wanted.ProxyEnabled); err != nil {
			rollbackErr := c.rollbackTrackedDefaultRoute(ctx, modem.id(), updated, previous.DefaultRoute)
			return tracked, errors.Join(fmt.Errorf("update proxy preference: %w", err), rollbackErr)
		}
	}

	if previous.AlwaysOn != wanted.AlwaysOn {
		if err := c.syncAlwaysOnState(ctx, modem.profileID(), wanted); err != nil {
			var rollbackErr error
			if proxyChanged {
				rollbackErr = errors.Join(rollbackErr, c.applyProxyPreference(ctx, modem.id(), updated.interfaceName, tracked.dns, previous.ProxyEnabled))
			}
			rollbackErr = errors.Join(rollbackErr, c.rollbackTrackedDefaultRoute(ctx, modem.id(), updated, previous.DefaultRoute))
			return tracked, errors.Join(fmt.Errorf("update always on preference: %w", err), rollbackErr)
		}
	}

	updated.prefs = wanted
	return updated, nil
}

func (c *Connector) updateQMAPPreferences(ctx context.Context, modem internetModem, connection *qmapConnection, next ConnectionPreferences) (*qmapConnection, error) {
	tracked := cloneTrackedConnection(connection.tracked)
	tracked, err := c.updateTrackedPreferences(ctx, modem, tracked, next)
	if err != nil {
		return connection, err
	}
	updated := cloneQMAPConnection(connection)
	updated.tracked = tracked
	c.mu.Lock()
	c.qmapConnections[modem.id()] = updated
	c.preferences[modem.id()] = updated.tracked.prefs
	c.mu.Unlock()
	return updated, nil
}

func (c *Connector) updateTrackedDefaultRoute(ctx context.Context, modemID string, tracked trackedConnection, enabled bool) (trackedConnection, error) {
	if tracked.prefs.DefaultRoute == enabled {
		return tracked, nil
	}
	previous := cloneTrackedConnection(tracked)
	routeTemplate := slices.Clone(tracked.routes)
	if err := c.removeTrackedRoutes(ctx, tracked); err != nil {
		_, rollbackErr := c.installTrackedRoutes(ctx, modemID, previous, routeTemplate, previous.prefs.DefaultRoute)
		return previous, errors.Join(err, rollbackErr)
	}
	updated, err := c.installTrackedRoutes(ctx, modemID, tracked, routeTemplate, enabled)
	if err == nil {
		return updated, nil
	}
	_, rollbackErr := c.installTrackedRoutes(ctx, modemID, previous, routeTemplate, previous.prefs.DefaultRoute)
	return previous, errors.Join(err, rollbackErr)
}

func (c *Connector) rollbackTrackedDefaultRoute(ctx context.Context, modemID string, tracked trackedConnection, enabled bool) error {
	if tracked.prefs.DefaultRoute == enabled {
		return nil
	}
	_, err := c.updateTrackedDefaultRoute(ctx, modemID, tracked, enabled)
	if err != nil {
		return fmt.Errorf("rollback default route preference: %w", err)
	}
	return nil
}

func (c *Connector) removeTrackedRoutes(ctx context.Context, tracked trackedConnection) error {
	routeOps := c.routeOperationSet()
	var result error
	for i := len(tracked.routes) - 1; i >= 0; i-- {
		result = errors.Join(result, routeOps.deleteDefaultRoute(tracked.routes[i]))
	}
	if result != nil {
		return fmt.Errorf("remove current routes: %w", result)
	}
	if err := cleanupDefaultRouteChangesWithStore(ctx, c.persistence, tracked.interfaceName, tracked.routeChanges, routeOps); err != nil {
		return fmt.Errorf("restore replaced routes: %w", err)
	}
	if err := c.syncDefaultRouteRestore(ctx, tracked.routeChanges); err != nil {
		return fmt.Errorf("sync restored route state: %w", err)
	}
	return nil
}

func (c *Connector) installTrackedRoutes(ctx context.Context, modemID string, tracked trackedConnection, routeTemplate []netlink.DefaultRoute, enabled bool) (trackedConnection, error) {
	routeOps := c.routeOperationSet()
	desired := slices.Clone(routeTemplate)
	metric := defaultRouteMetric
	if !enabled {
		current, err := routeOps.defaultRoutes()
		if err != nil {
			return tracked, fmt.Errorf("list default routes: %w", err)
		}
		metric = secondaryRouteMetricFor(desired, current)
	}
	setRouteMetric(desired, metric)

	var changes []defaultRouteChange
	if enabled {
		if err := restoreStaleDefaultRouteStatesWithStore(ctx, c.persistence, routeStateRestoreTarget{
			modemID:        modemID,
			interfaceNames: []string{tracked.interfaceName},
		}, routeOps); err != nil {
			return tracked, fmt.Errorf("restore stale route state: %w", err)
		}
		var err error
		changes, err = takeoverDefaultRoutesWithStore(ctx, c.persistence, modemID, tracked.interfaceName, desired, routeOps)
		if err != nil {
			return tracked, fmt.Errorf("take over default routes: %w", err)
		}
	}

	var added []netlink.DefaultRoute
	for _, route := range desired {
		if err := restoreOriginalDefaultRouteWithOps(route, routeOps); err != nil {
			cleanupErr := deleteDefaultRoutesWithOps(added, routeOps)
			cleanupErr = errors.Join(cleanupErr, cleanupDefaultRouteChangesWithStore(ctx, c.persistence, tracked.interfaceName, changes, routeOps))
			return tracked, errors.Join(fmt.Errorf("add updated route: %w", err), cleanupErr)
		}
		added = append(added, route)
	}

	updated := cloneTrackedConnection(tracked)
	updated.routes = desired
	updated.routeChanges = changes
	updated.routeMetric = metric
	updated.prefs.DefaultRoute = enabled
	if enabled {
		if err := c.syncDefaultRouteTakeover(ctx, modemID, &updated); err != nil {
			cleanupErr := c.removeTrackedRoutes(ctx, updated)
			return tracked, errors.Join(fmt.Errorf("sync default route takeover: %w", err), cleanupErr)
		}
	}
	return updated, nil
}

func (c *Connector) applyProxyPreference(ctx context.Context, modemID, interfaceName string, dns []string, enabled bool) error {
	prefs := Preferences{ProxyEnabled: enabled}
	if err := c.syncProxyPreference(ctx, modemID, interfaceName, dns, prefs); err != nil {
		return err
	}
	if !enabled {
		return nil
	}
	if err := c.persistence.saveProxyStateForModem(ctx, modemID, interfaceName); err != nil {
		return errors.Join(fmt.Errorf("save proxy state: %w", err), c.cleanupProxy(ctx, modemID, interfaceName))
	}
	return nil
}

func (c *Connector) qmapConnectionFor(modemID string, generation uint64) *qmapConnection {
	c.mu.Lock()
	defer c.mu.Unlock()
	connection := c.qmapConnections[modemID]
	if connection == nil || connection.generation != generation {
		return nil
	}
	return connection
}

func (c *Connector) qmapConnectionResponse(modemID string, connection *qmapConnection) *Connection {
	response := connection.response()
	if proxy := c.proxyInstance(); proxy != nil && response.InterfaceName != "" {
		response.Proxy = proxy.Status(modemID)
	}
	return response
}

func cloneTrackedConnection(tracked trackedConnection) trackedConnection {
	tracked.addresses = slices.Clone(tracked.addresses)
	tracked.dns = slices.Clone(tracked.dns)
	tracked.peers = maps.Clone(tracked.peers)
	tracked.routes = slices.Clone(tracked.routes)
	tracked.routeChanges = slices.Clone(tracked.routeChanges)
	return tracked
}

func cloneQMAPConnection(connection *qmapConnection) *qmapConnection {
	if connection == nil {
		return nil
	}
	cloned := *connection
	cloned.sessions = slices.Clone(connection.sessions)
	cloned.tracked = cloneTrackedConnection(connection.tracked)
	return &cloned
}
