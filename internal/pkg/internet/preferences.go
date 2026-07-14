package internet

import (
	"context"
	"errors"
	"fmt"
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
	defer c.lockModem(modemID)()

	if connection := c.qmapConnectionFor(modemID); connection != nil {
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
		if err := c.applyProxyPreference(ctx, modem.id(), updated.interfaceName, wanted.ProxyEnabled); err != nil {
			rollbackErr := c.rollbackTrackedDefaultRoute(ctx, modem.id(), updated, previous.DefaultRoute)
			return tracked, errors.Join(fmt.Errorf("update proxy preference: %w", err), rollbackErr)
		}
	}

	if previous.AlwaysOn != wanted.AlwaysOn {
		if err := c.syncAlwaysOnState(ctx, modem.profileID(), wanted); err != nil {
			var rollbackErr error
			if proxyChanged {
				rollbackErr = errors.Join(rollbackErr, c.applyProxyPreference(ctx, modem.id(), updated.interfaceName, previous.ProxyEnabled))
			}
			rollbackErr = errors.Join(rollbackErr, c.rollbackTrackedDefaultRoute(ctx, modem.id(), updated, previous.DefaultRoute))
			return tracked, errors.Join(fmt.Errorf("update always on preference: %w", err), rollbackErr)
		}
	}

	updated.prefs = wanted
	return updated, nil
}

func (c *Connector) updateQMAPPreferences(ctx context.Context, modem internetModem, connection *qmapConnection, next ConnectionPreferences) (*qmapConnection, error) {
	previous := cloneQMAPConnection(connection)
	wanted := previous.prefs
	wanted.DefaultRoute = next.DefaultRoute
	wanted.ProxyEnabled = next.ProxyEnabled
	wanted.AlwaysOn = next.AlwaysOn
	if wanted.AlwaysOn && modem.profileID() == "" {
		return connection, ErrProfileIDRequired
	}

	updated := cloneQMAPConnection(connection)
	if previous.prefs.DefaultRoute != wanted.DefaultRoute {
		for _, i := range qmapRouteUpdateOrder(len(updated.tracked), wanted.DefaultRoute) {
			tracked, err := c.updateTrackedDefaultRoute(ctx, modem.id(), updated.tracked[i], wanted.DefaultRoute)
			if err != nil {
				rollbackErr := c.rollbackQMAPDefaultRoutes(ctx, modem.id(), updated.tracked, previous.tracked)
				return connection, errors.Join(fmt.Errorf("update QMAP default route preference: %w", err), rollbackErr)
			}
			updated.tracked[i] = tracked
		}
	}

	interfaceName := qmapPrimaryInterface(updated)
	proxyChanged := previous.prefs.ProxyEnabled != wanted.ProxyEnabled
	if proxyChanged {
		if err := c.applyProxyPreference(ctx, modem.id(), interfaceName, wanted.ProxyEnabled); err != nil {
			rollbackErr := c.rollbackQMAPDefaultRoutes(ctx, modem.id(), updated.tracked, previous.tracked)
			return connection, errors.Join(fmt.Errorf("update QMAP proxy preference: %w", err), rollbackErr)
		}
	}

	if previous.prefs.AlwaysOn != wanted.AlwaysOn {
		if err := c.syncAlwaysOnState(ctx, modem.profileID(), wanted); err != nil {
			var rollbackErr error
			if proxyChanged {
				rollbackErr = errors.Join(rollbackErr, c.applyProxyPreference(ctx, modem.id(), interfaceName, previous.prefs.ProxyEnabled))
			}
			rollbackErr = errors.Join(rollbackErr, c.rollbackQMAPDefaultRoutes(ctx, modem.id(), updated.tracked, previous.tracked))
			return connection, errors.Join(fmt.Errorf("update QMAP always on preference: %w", err), rollbackErr)
		}
	}

	updated.prefs = wanted
	for i := range updated.tracked {
		updated.tracked[i].prefs = wanted
	}
	c.mu.Lock()
	c.qmapConnections[modem.id()] = updated
	c.preferences[modem.id()] = wanted
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

func (c *Connector) rollbackQMAPDefaultRoutes(ctx context.Context, modemID string, current, previous []trackedConnection) error {
	count := min(len(current), len(previous))
	if count == 0 {
		return nil
	}
	var result error
	for _, i := range qmapRouteUpdateOrder(count, previous[0].prefs.DefaultRoute) {
		if current[i].prefs.DefaultRoute == previous[i].prefs.DefaultRoute {
			continue
		}
		_, err := c.updateTrackedDefaultRoute(ctx, modemID, current[i], previous[i].prefs.DefaultRoute)
		if err != nil {
			result = errors.Join(result, fmt.Errorf("rollback QMAP route %s: %w", current[i].interfaceName, err))
		}
	}
	return result
}

func qmapRouteUpdateOrder(count int, enabled bool) []int {
	order := make([]int, count)
	for i := range count {
		if enabled {
			order[i] = i
			continue
		}
		order[i] = count - 1 - i
	}
	return order
}

func (c *Connector) removeTrackedRoutes(ctx context.Context, tracked trackedConnection) error {
	var result error
	for i := len(tracked.routes) - 1; i >= 0; i-- {
		result = errors.Join(result, netlinkDefaultRouteOps.deleteDefaultRoute(tracked.routes[i]))
	}
	if result != nil {
		return fmt.Errorf("remove current routes: %w", result)
	}
	if err := cleanupDefaultRouteChangesWithStore(ctx, c.persistence, tracked.interfaceName, tracked.routeChanges, netlinkDefaultRouteOps); err != nil {
		return fmt.Errorf("restore replaced routes: %w", err)
	}
	if err := c.syncDefaultRouteRestore(ctx, tracked.routeChanges); err != nil {
		return fmt.Errorf("sync restored route state: %w", err)
	}
	return nil
}

func (c *Connector) installTrackedRoutes(ctx context.Context, modemID string, tracked trackedConnection, routeTemplate []netlink.DefaultRoute, enabled bool) (trackedConnection, error) {
	desired := slices.Clone(routeTemplate)
	metric := defaultRouteMetric
	if !enabled {
		current, err := netlinkDefaultRouteOps.defaultRoutes()
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
		}, netlinkDefaultRouteOps); err != nil {
			return tracked, fmt.Errorf("restore stale route state: %w", err)
		}
		var err error
		changes, err = takeoverDefaultRoutesWithStore(ctx, c.persistence, modemID, tracked.interfaceName, desired, netlinkDefaultRouteOps)
		if err != nil {
			return tracked, fmt.Errorf("take over default routes: %w", err)
		}
	}

	var added []netlink.DefaultRoute
	for _, route := range desired {
		if err := restoreOriginalDefaultRouteWithOps(route, netlinkDefaultRouteOps); err != nil {
			cleanupErr := deleteDefaultRoutesWithOps(added, netlinkDefaultRouteOps)
			cleanupErr = errors.Join(cleanupErr, cleanupDefaultRouteChangesWithStore(ctx, c.persistence, tracked.interfaceName, changes, netlinkDefaultRouteOps))
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

func (c *Connector) applyProxyPreference(ctx context.Context, modemID, interfaceName string, enabled bool) error {
	prefs := Preferences{ProxyEnabled: enabled}
	if err := c.syncProxyPreference(ctx, modemID, interfaceName, prefs); err != nil {
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

func (c *Connector) qmapConnectionFor(modemID string) *qmapConnection {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.qmapConnections[modemID]
}

func (c *Connector) qmapConnectionResponse(modemID string, connection *qmapConnection) *Connection {
	response := connection.response()
	if proxy := c.proxyInstance(); proxy != nil && response.InterfaceName != "" {
		response.Proxy = proxy.Status(modemID)
	}
	return response
}

func qmapPrimaryInterface(connection *qmapConnection) string {
	if connection == nil || len(connection.tracked) == 0 {
		return ""
	}
	return connection.tracked[0].interfaceName
}

func cloneTrackedConnection(tracked trackedConnection) trackedConnection {
	tracked.addresses = slices.Clone(tracked.addresses)
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
	cloned.tracked = slices.Clone(connection.tracked)
	for i := range cloned.tracked {
		cloned.tracked[i] = cloneTrackedConnection(cloned.tracked[i])
	}
	cloned.muxIDs = slices.Clone(connection.muxIDs)
	cloned.dns = slices.Clone(connection.dns)
	return &cloned
}
