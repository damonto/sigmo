package internet

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/godbus/dbus/v5"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/netlink"
)

const (
	StatusConnected    = "connected"
	StatusDisconnected = "disconnected"
)

var (
	ErrModemRequired       = errors.New("modem is required")
	ErrUnsupportedIPMethod = errors.New("only static bearer IP configuration is supported")
)

type Preferences struct {
	APN          string
	IPType       string
	APNUsername  string
	APNPassword  string
	APNAuth      string
	DefaultRoute bool
	ProxyEnabled bool
	AlwaysOn     bool
}

type Connection struct {
	Status          string
	APN             string
	IPType          string
	APNUsername     string
	APNPassword     string
	APNAuth         string
	DefaultRoute    bool
	ProxyEnabled    bool
	AlwaysOn        bool
	Proxy           ProxyStatus
	InterfaceName   string
	Bearer          string
	IPv4Addresses   []string
	IPv6Addresses   []string
	DNS             []string
	DurationSeconds uint32
	TXBytes         uint64
	RXBytes         uint64
	RouteMetric     int
}

type Connector struct {
	mu           sync.Mutex
	operationMu  sync.Mutex
	connections  map[string]trackedConnection
	preferences  map[string]Preferences
	operations   map[string]*sync.Mutex
	proxy        *Proxy
	alwaysOnPath string
	proxyPath    string
	routePath    string
}

type ConnectorConfig struct {
	Proxy        *Proxy
	AlwaysOnPath string
	ProxyPath    string
	RoutePath    string
}

type internetModem interface {
	id() string
	operatorIdentifier() string
	bearer(context.Context, dbus.ObjectPath) (*mmodem.Bearer, error)
	bearers(context.Context) ([]*mmodem.Bearer, error)
	connectBearer(context.Context, mmodem.BearerProperties) (*mmodem.Bearer, error)
	disconnectBearer(context.Context, dbus.ObjectPath) error
	deleteBearer(context.Context, dbus.ObjectPath) error
	restart(context.Context, bool) error
}

type modemAccess struct {
	modem *mmodem.Modem
}

func (m modemAccess) id() string {
	if m.modem == nil {
		return ""
	}
	return m.modem.EquipmentIdentifier
}

func (m modemAccess) operatorIdentifier() string {
	if m.modem == nil || m.modem.Sim == nil {
		return ""
	}
	return strings.TrimSpace(m.modem.Sim.OperatorIdentifier)
}

func (m modemAccess) bearer(ctx context.Context, path dbus.ObjectPath) (*mmodem.Bearer, error) {
	if m.modem == nil {
		return nil, ErrModemRequired
	}
	return m.modem.Bearer(ctx, path)
}

func (m modemAccess) bearers(ctx context.Context) ([]*mmodem.Bearer, error) {
	if m.modem == nil {
		return nil, ErrModemRequired
	}
	return m.modem.Bearers(ctx)
}

func (m modemAccess) connectBearer(ctx context.Context, properties mmodem.BearerProperties) (*mmodem.Bearer, error) {
	if m.modem == nil {
		return nil, ErrModemRequired
	}
	return m.modem.ConnectBearer(ctx, properties)
}

func (m modemAccess) disconnectBearer(ctx context.Context, path dbus.ObjectPath) error {
	if m.modem == nil {
		return ErrModemRequired
	}
	return m.modem.DisconnectBearer(ctx, path)
}

func (m modemAccess) deleteBearer(ctx context.Context, path dbus.ObjectPath) error {
	if m.modem == nil {
		return ErrModemRequired
	}
	return m.modem.DeleteBearer(ctx, path)
}

func (m modemAccess) restart(ctx context.Context, compatible bool) error {
	if m.modem == nil {
		return ErrModemRequired
	}
	return m.modem.Restart(ctx, compatible)
}

func NewConnector(cfg ConnectorConfig) (*Connector, error) {
	if cfg.AlwaysOnPath == "" {
		path, err := defaultAlwaysOnStatePath()
		if err != nil {
			return nil, fmt.Errorf("resolve always on state path: %w", err)
		}
		cfg.AlwaysOnPath = path
	}
	if cfg.ProxyPath == "" {
		cfg.ProxyPath = proxyStatePath
	}
	if cfg.RoutePath == "" {
		cfg.RoutePath = defaultRouteStatePath
	}
	return &Connector{
		connections:  make(map[string]trackedConnection),
		preferences:  make(map[string]Preferences),
		operations:   make(map[string]*sync.Mutex),
		proxy:        cfg.Proxy,
		alwaysOnPath: cfg.AlwaysOnPath,
		proxyPath:    cfg.ProxyPath,
		routePath:    cfg.RoutePath,
	}, nil
}

func (c *Connector) UpdateProxyConfig(cfg ProxyConfig) error {
	c.mu.Lock()
	if c.proxy == nil {
		c.proxy = NewProxy(cfg)
		c.mu.Unlock()
		return nil
	}
	proxy := c.proxy
	c.mu.Unlock()
	return proxy.UpdateConfig(cfg)
}

func (c *Connector) Recover(ctx context.Context, modems []*mmodem.Modem) error {
	var result error
	for _, modem := range modems {
		if modem == nil {
			continue
		}
		access := modemAccess{modem: modem}
		unlock := c.lockModem(access.id())
		err := c.recover(ctx, access)
		unlock()
		if err != nil {
			result = errors.Join(result, fmt.Errorf("recover internet connection for modem %s: %w", access.id(), err))
		}
	}
	return result
}

func (c *Connector) Current(ctx context.Context, modem *mmodem.Modem) (*Connection, error) {
	return c.current(ctx, modemAccess{modem: modem})
}

func (c *Connector) current(ctx context.Context, modem internetModem) (*Connection, error) {
	modemID := modem.id()
	defer c.lockModem(modemID)()

	prefs := c.preferenceWithAlwaysOn(modemID)
	var staleInterfaces []string
	if tracked, ok := c.connection(modemID); ok {
		bearer, err := modem.bearer(ctx, tracked.bearerPath)
		if err == nil {
			connected, err := bearer.Connected(ctx)
			if err == nil {
				if !connected {
					err := c.cleanupTracked(ctx, modemID, tracked)
					if err == nil {
						err = c.syncCleanedUpDefaultRouteState(tracked)
					}
					err = errors.Join(err, restoreStaleDefaultRouteStatesWithState(c.routePath, routeStateRestoreTarget{modemID: modemID}, netlinkDefaultRouteOps))
					if err != nil {
						return nil, fmt.Errorf("cleanup disconnected bearer: %w", err)
					}
					c.deleteConnection(modemID)
					prefs := bearerPreferences(ctx, bearer, tracked.prefs)
					prefs = preferencesWithSelectedAPN(modem, prefs)
					c.setPreference(modemID, prefs)
					return disconnectedConnection(prefs), nil
				}
				prefs := preferencesWithDefaultAPNCredentials(modem, tracked.prefs)
				tracked.prefs = prefs
				c.setConnectionAndPreference(modemID, tracked, prefs)
				connection, err := c.connectionFromBearer(ctx, modemID, bearer, prefs, tracked.routeMetric)
				if err == nil {
					return connection, nil
				}
			}
		}
		staleInterfaces = append(staleInterfaces, tracked.interfaceName)
		c.deleteConnection(modemID)
		prefs = tracked.prefs
	}

	current, err := currentBearer(ctx, modem)
	if err != nil {
		return nil, err
	}
	if current.bearer == nil {
		if err := c.cleanupStaleConnectionState(modemID, staleInterfaces...); err != nil {
			return nil, err
		}
		prefs = preferencesWithSelectedAPN(modem, prefs)
		return disconnectedConnection(prefs), nil
	}
	if !current.connected {
		if interfaceName, err := current.bearer.Interface(ctx); err == nil {
			staleInterfaces = append(staleInterfaces, interfaceName)
		}
		if err := c.cleanupStaleConnectionState(modemID, staleInterfaces...); err != nil {
			return nil, err
		}
		prefs = bearerPreferences(ctx, current.bearer, prefs)
		prefs = preferencesWithSelectedAPN(modem, prefs)
		c.setPreference(modemID, prefs)
		return disconnectedConnection(prefs), nil
	}
	bearer := current.bearer
	tracked, metric, ok, err := recoverTrackedConnection(ctx, c.proxyPath, c.routePath, modemID, bearer, prefs)
	if err != nil {
		return nil, err
	}
	if ok {
		tracked.prefs = preferencesWithDefaultAPNCredentials(modem, tracked.prefs)
		c.setConnectionAndPreference(modemID, tracked, tracked.prefs)
		return c.connectionFromBearer(ctx, modemID, bearer, tracked.prefs, metric)
	}
	return nil, ErrUnsupportedIPMethod
}

func (c *Connector) recover(ctx context.Context, modem internetModem) error {
	modemID := modem.id()
	prefs := c.preferenceWithAlwaysOn(modemID)
	current, err := currentBearer(ctx, modem)
	if err != nil {
		return err
	}
	if current.bearer == nil {
		return c.cleanupStaleConnectionState(modemID)
	}
	if !current.connected {
		var staleInterfaces []string
		if interfaceName, err := current.bearer.Interface(ctx); err == nil {
			staleInterfaces = append(staleInterfaces, interfaceName)
		}
		if err := c.cleanupStaleConnectionState(modemID, staleInterfaces...); err != nil {
			return err
		}
		c.setPreference(modemID, preferencesWithSelectedAPN(modem, bearerPreferences(ctx, current.bearer, prefs)))
		return nil
	}

	tracked, _, ok, err := recoverTrackedConnection(ctx, c.proxyPath, c.routePath, modemID, current.bearer, prefs)
	if err != nil {
		return err
	}
	if !ok {
		return ErrUnsupportedIPMethod
	}
	tracked.prefs = preferencesWithDefaultAPNCredentials(modem, tracked.prefs)
	if err := c.syncProxyPreference(modemID, tracked.interfaceName, tracked.prefs); err != nil {
		return err
	}
	c.setConnectionAndPreference(modemID, tracked, tracked.prefs)
	return nil
}

func (c *Connector) Connect(ctx context.Context, modem *mmodem.Modem, prefs Preferences) (*Connection, error) {
	access := modemAccess{modem: modem}
	defer c.lockModem(access.id())()

	return c.connect(ctx, access, prefs, true)
}

func (c *Connector) connect(ctx context.Context, modem internetModem, prefs Preferences, clearAlwaysOnBefore bool) (*Connection, error) {
	modemID := modem.id()
	prefs = normalizePreferences(prefs)
	if prefs.APN == "" {
		apn, err := apnFromBearers(ctx, modem)
		if err != nil {
			return nil, err
		}
		prefs.APN = apnForModem(modem, "", apn, c.preferenceWithAlwaysOn(modemID).APN)
	}
	prefs = preferencesWithDefaultAPNCredentials(modem, prefs)
	if err := c.disconnect(ctx, modem, clearAlwaysOnBefore); err != nil {
		return nil, fmt.Errorf("disconnect previous bearer: %w", err)
	}

	bearer, err := modem.connectBearer(ctx, bearerPropertiesFromPreferences(prefs))
	if err != nil {
		bearer, err = c.connectBearerAfterRecovery(ctx, modem, prefs, err)
		if err != nil {
			return nil, err
		}
	}
	prefs = bearerPreferences(ctx, bearer, prefs)

	tracked, err := configureBearer(ctx, c.routePath, modemID, bearer, prefs)
	if err != nil {
		disconnectErr := bearer.Disconnect(ctx)
		return nil, errors.Join(err, disconnectErr)
	}
	tracked.bearerPath = bearer.Path()
	tracked.prefs = prefs

	if err := c.syncProxyPreference(modemID, tracked.interfaceName, prefs); err != nil {
		cleanupErr := c.cleanupTracked(ctx, modemID, tracked)
		disconnectErr := bearer.Disconnect(ctx)
		return nil, errors.Join(err, cleanupErr, disconnectErr)
	}
	if prefs.ProxyEnabled {
		if err := saveProxyStateForModem(c.proxyPath, modemID, tracked.interfaceName); err != nil {
			cleanupErr := c.cleanupTracked(ctx, modemID, tracked)
			disconnectErr := bearer.Disconnect(ctx)
			return nil, errors.Join(fmt.Errorf("save proxy state: %w", err), cleanupErr, disconnectErr)
		}
	}

	connection, err := c.connectionFromBearer(ctx, modemID, bearer, prefs, tracked.routeMetric)
	if err != nil {
		cleanupErr := c.cleanupTracked(ctx, modemID, tracked)
		disconnectErr := bearer.Disconnect(ctx)
		return nil, errors.Join(err, cleanupErr, disconnectErr)
	}
	if err := c.syncAlwaysOnState(modemID, prefs); err != nil {
		cleanupErr := c.cleanupTracked(ctx, modemID, tracked)
		disconnectErr := bearer.Disconnect(ctx)
		return nil, errors.Join(fmt.Errorf("sync always on state: %w", err), cleanupErr, disconnectErr)
	}
	if err := c.syncDefaultRouteTakeover(c.routePath, modemID, &tracked); err != nil {
		cleanupErr := c.cleanupTracked(ctx, modemID, tracked)
		disconnectErr := bearer.Disconnect(ctx)
		return nil, errors.Join(fmt.Errorf("sync default route takeover: %w", err), cleanupErr, disconnectErr)
	}
	c.setConnectionAndPreference(modemID, tracked, prefs)

	return connection, nil
}

func (c *Connector) Disconnect(ctx context.Context, modem *mmodem.Modem) error {
	access := modemAccess{modem: modem}
	defer c.lockModem(access.id())()

	return c.disconnect(ctx, access, true)
}

func (c *Connector) Restore(ctx context.Context, modem *mmodem.Modem) error {
	access := modemAccess{modem: modem}
	defer c.lockModem(access.id())()

	err := c.disconnect(ctx, access, true)
	c.deleteConnectionAndPreference(access.id())
	return err
}

func (c *Connector) disconnect(ctx context.Context, modem internetModem, clearAlwaysOn bool) error {
	modemID := modem.id()
	var result error
	if clearAlwaysOn {
		result = errors.Join(result, c.clearAlwaysOnState(modemID))
	}

	if tracked, ok := c.connection(modemID); ok {
		err := c.cleanupTracked(ctx, modemID, tracked)
		if err == nil {
			err = c.syncCleanedUpDefaultRouteState(tracked)
		}
		err = errors.Join(err, modem.disconnectBearer(ctx, tracked.bearerPath))
		err = errors.Join(err, restoreStaleDefaultRouteStatesWithState(c.routePath, routeStateRestoreTarget{modemID: modemID}, netlinkDefaultRouteOps))
		c.deleteConnection(modemID)
		err = errors.Join(result, err)
		if err != nil {
			return fmt.Errorf("disconnect bearer: %w", err)
		}
		return nil
	}

	current, err := currentBearer(ctx, modem)
	if err != nil {
		err = errors.Join(result, err)
		return fmt.Errorf("disconnect bearer: %w", err)
	}
	if !current.connected {
		if current.bearer == nil {
			err := errors.Join(result, c.cleanupStaleConnectionState(modemID))
			if err != nil {
				return fmt.Errorf("disconnect bearer: %w", err)
			}
			return nil
		}
		interfaceName, err := current.bearer.Interface(ctx)
		if err != nil {
			err = errors.Join(result, fmt.Errorf("read bearer interface: %w", err))
			return fmt.Errorf("disconnect bearer: %w", err)
		}
		err = errors.Join(result, c.cleanupStaleConnectionState(modemID, interfaceName))
		if err != nil {
			return fmt.Errorf("disconnect bearer: %w", err)
		}
		return nil
	}
	bearer := current.bearer
	prefs := recoverPreferences(ctx, bearer, c.preference(modemID))
	interfaceName, interfaceErr := bearer.Interface(ctx)
	err = cleanupBearer(ctx, c.routePath, modemID, bearer, prefs)
	if err == nil && interfaceErr == nil {
		err = deleteRouteState(c.routePath, interfaceName)
	}
	if interfaceErr == nil {
		err = errors.Join(err, c.cleanupProxy(modemID, interfaceName))
	} else {
		err = errors.Join(err, c.cleanupProxyForModem(modemID))
	}
	err = errors.Join(err, bearer.Disconnect(ctx))
	err = errors.Join(err, restoreStaleDefaultRouteStatesWithState(c.routePath, routeStateRestoreTarget{modemID: modemID}, netlinkDefaultRouteOps))
	err = errors.Join(result, err)
	if err != nil {
		return fmt.Errorf("disconnect bearer: %w", err)
	}
	return nil
}

func (c *Connector) cleanupTracked(ctx context.Context, modemID string, tracked trackedConnection) error {
	err := c.cleanupProxy(modemID, tracked.interfaceName)
	cleanup := tracked
	if !tracked.prefs.DefaultRoute {
		cleanup.routeChanges = nil
	}
	cleanupErr := cleanupApplied(ctx, c.routePath, cleanup)
	if cleanupErr == nil {
		cleanupErr = deleteRouteState(c.routePath, tracked.interfaceName)
	}
	err = errors.Join(err, cleanupErr)
	return err
}

func (c *Connector) syncDefaultRouteTakeover(path string, modemID string, tracked *trackedConnection) error {
	if tracked == nil || len(tracked.routeChanges) == 0 {
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	affected := make(map[string]struct{})
	owners := make(map[string]trackedConnection, len(c.connections))
	for ownerID, owner := range c.connections {
		owners[ownerID] = owner
	}

	for _, change := range tracked.routeChanges {
		ownerID, owner, ok := routeChangeOwner(owners, change)
		if !ok {
			continue
		}
		updated := demoteTrackedRoute(&owner, change)
		if !updated {
			continue
		}
		owners[ownerID] = owner
		affected[ownerID] = struct{}{}
	}
	if len(affected) == 0 {
		return nil
	}

	if err := putRouteStateForModem(path, modemID, tracked.interfaceName, tracked.routes, tracked.routeChanges); err != nil {
		return fmt.Errorf("save takeover route state: %w", err)
	}
	for ownerID := range affected {
		owner := owners[ownerID]
		if owner.prefs.AlwaysOn {
			if err := c.syncAlwaysOnState(ownerID, owner.prefs); err != nil {
				return fmt.Errorf("sync demoted always-on state for %s: %w", ownerID, err)
			}
		}
	}

	for ownerID := range affected {
		owner := owners[ownerID]
		c.connections[ownerID] = owner
		c.preferences[ownerID] = owner.prefs
	}
	return nil
}

func (c *Connector) syncDefaultRouteRestore(changes []defaultRouteChange) error {
	if len(changes) == 0 {
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	affected := make(map[string]trackedConnection)
	owners := make(map[string]trackedConnection, len(c.connections))
	for ownerID, owner := range c.connections {
		owners[ownerID] = owner
	}

	for i := len(changes) - 1; i >= 0; i-- {
		change := changes[i]
		ownerID, owner, ok := restoredRouteOwner(owners, change)
		if !ok {
			continue
		}
		updated := restoreTrackedRoute(&owner, change)
		if !updated {
			continue
		}
		owners[ownerID] = owner
		affected[ownerID] = owner
	}
	if len(affected) == 0 {
		return nil
	}

	var result error
	for ownerID, owner := range affected {
		c.connections[ownerID] = owner
		c.preferences[ownerID] = owner.prefs
		if owner.prefs.AlwaysOn {
			result = errors.Join(result, c.syncAlwaysOnState(ownerID, owner.prefs))
		}
	}
	return result
}

func (c *Connector) syncCleanedUpDefaultRouteState(tracked trackedConnection) error {
	if tracked.prefs.DefaultRoute {
		return c.syncDefaultRouteRestore(tracked.routeChanges)
	}
	return c.syncDefaultRouteRemoval(c.routePath, tracked)
}

func (c *Connector) syncDefaultRouteRemoval(path string, removed trackedConnection) error {
	if len(removed.routes) == 0 {
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	affected := make(map[string]trackedConnection)
	for ownerID, owner := range c.connections {
		if owner.interfaceName == removed.interfaceName {
			continue
		}
		nextChanges, changed := defaultRouteChangesAfterRemoval(owner.routeChanges, removed)
		if !changed {
			continue
		}
		owner.routeChanges = nextChanges
		affected[ownerID] = owner
	}
	if len(affected) == 0 {
		return nil
	}

	for ownerID, owner := range affected {
		if len(owner.routeChanges) > 0 {
			if err := putRouteStateForModem(path, ownerID, owner.interfaceName, owner.routes, owner.routeChanges); err != nil {
				return fmt.Errorf("save inherited route state for %s: %w", owner.interfaceName, err)
			}
		} else if err := deleteRouteState(path, owner.interfaceName); err != nil {
			return fmt.Errorf("delete empty route state for %s: %w", owner.interfaceName, err)
		}
	}
	for ownerID, owner := range affected {
		c.connections[ownerID] = owner
	}
	return nil
}

func defaultRouteChangesAfterRemoval(changes []defaultRouteChange, removed trackedConnection) ([]defaultRouteChange, bool) {
	var next []defaultRouteChange
	changed := false
	inherited := false
	for _, change := range changes {
		if routeRemoved(change.Replacement, removed.routes) {
			changed = true
			if !inherited {
				next = append(next, removed.routeChanges...)
				inherited = true
			}
			continue
		}
		next = append(next, change)
	}
	return next, changed
}

func routeRemoved(route netlink.DefaultRoute, removed []netlink.DefaultRoute) bool {
	return slices.ContainsFunc(removed, func(candidate netlink.DefaultRoute) bool {
		return sameDefaultRoute(route, candidate)
	})
}

func routeChangeOwner(connections map[string]trackedConnection, change defaultRouteChange) (string, trackedConnection, bool) {
	for modemID, tracked := range connections {
		for _, route := range tracked.routes {
			if sameDefaultRoute(route, change.Original) {
				return modemID, tracked, true
			}
		}
	}
	return "", trackedConnection{}, false
}

func restoredRouteOwner(connections map[string]trackedConnection, change defaultRouteChange) (string, trackedConnection, bool) {
	for modemID, tracked := range connections {
		for _, route := range tracked.routes {
			if sameDefaultRoute(route, change.Replacement) {
				return modemID, tracked, true
			}
		}
	}
	return "", trackedConnection{}, false
}

func demoteTrackedRoute(tracked *trackedConnection, change defaultRouteChange) bool {
	updated := false
	for i, route := range tracked.routes {
		if !sameDefaultRoute(route, change.Original) {
			continue
		}
		tracked.routes[i] = change.Replacement
		updated = true
	}
	if !updated {
		return false
	}
	state := routeStateFromRoutes(tracked.routes, tracked.interfaceName)
	tracked.routeMetric = state.Metric
	tracked.prefs.DefaultRoute = state.DefaultRoute
	return true
}

func restoreTrackedRoute(tracked *trackedConnection, change defaultRouteChange) bool {
	updated := false
	for i, route := range tracked.routes {
		if !sameDefaultRoute(route, change.Replacement) {
			continue
		}
		tracked.routes[i] = change.Original
		updated = true
	}
	if !updated {
		return false
	}
	state := routeStateFromRoutes(tracked.routes, tracked.interfaceName)
	tracked.routeMetric = state.Metric
	tracked.prefs.DefaultRoute = state.DefaultRoute
	return true
}

func (c *Connector) preference(modemID string) Preferences {
	c.mu.Lock()
	defer c.mu.Unlock()

	if prefs, ok := c.preferences[modemID]; ok {
		return normalizePreferences(prefs)
	}
	return Preferences{}
}

func (c *Connector) preferenceWithAlwaysOn(modemID string) Preferences {
	prefs := c.preference(modemID)
	alwaysOn, ok, err := loadAlwaysOnStateForModem(c.alwaysOnPath, modemID)
	if err != nil || !ok {
		return prefs
	}
	return alwaysOn
}

func (c *Connector) syncAlwaysOnState(modemID string, prefs Preferences) error {
	if prefs.AlwaysOn {
		return saveAlwaysOnStateForModem(c.alwaysOnPath, modemID, prefs)
	}
	return deleteAlwaysOnStateForModem(c.alwaysOnPath, modemID)
}

func (c *Connector) clearAlwaysOnState(modemID string) error {
	prefs := c.preferenceWithAlwaysOn(modemID)
	prefs.AlwaysOn = false
	if hasInternetPreference(prefs) {
		c.setPreference(modemID, prefs)
	} else {
		c.deletePreference(modemID)
	}
	return deleteAlwaysOnStateForModem(c.alwaysOnPath, modemID)
}

func (c *Connector) lockModem(modemID string) func() {
	modemID = strings.TrimSpace(modemID)
	c.operationMu.Lock()
	lock := c.operations[modemID]
	if lock == nil {
		lock = new(sync.Mutex)
		c.operations[modemID] = lock
	}
	c.operationMu.Unlock()

	lock.Lock()
	return lock.Unlock
}

func (c *Connector) connection(modemID string) (trackedConnection, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	tracked, ok := c.connections[modemID]
	return tracked, ok
}

func (c *Connector) setConnectionAndPreference(modemID string, tracked trackedConnection, prefs Preferences) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.connections[modemID] = tracked
	c.preferences[modemID] = normalizePreferences(prefs)
}

func (c *Connector) deleteConnection(modemID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.connections, modemID)
}

func (c *Connector) setPreference(modemID string, prefs Preferences) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.preferences[modemID] = normalizePreferences(prefs)
}

func (c *Connector) deletePreference(modemID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.preferences, modemID)
}

func (c *Connector) deleteConnectionAndPreference(modemID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.connections, modemID)
	delete(c.preferences, modemID)
}

func (c *Connector) proxyInstance() *Proxy {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.proxy
}

func normalizePreferences(prefs Preferences) Preferences {
	prefs.APN = strings.TrimSpace(prefs.APN)
	prefs.IPType = strings.ToLower(strings.TrimSpace(prefs.IPType))
	prefs.APNUsername = strings.TrimSpace(prefs.APNUsername)
	prefs.APNAuth = strings.ToLower(strings.TrimSpace(prefs.APNAuth))
	return prefs
}

func hasInternetPreference(prefs Preferences) bool {
	prefs = normalizePreferences(prefs)
	return prefs.APN != "" ||
		(prefs.IPType != "" && prefs.IPType != "ipv4v6") ||
		prefs.APNUsername != "" ||
		prefs.APNPassword != "" ||
		prefs.APNAuth != "" ||
		prefs.DefaultRoute ||
		prefs.ProxyEnabled
}

func (c *Connector) connectionFromBearer(ctx context.Context, modemID string, bearer *mmodem.Bearer, prefs Preferences, metric int) (*Connection, error) {
	connection, err := connectionFromBearer(ctx, bearer, prefs, metric)
	if err != nil {
		return nil, err
	}
	if proxy := c.proxyInstance(); proxy != nil && strings.TrimSpace(connection.InterfaceName) != "" {
		connection.Proxy = proxy.Status(modemID)
	}
	return connection, nil
}

func (c *Connector) syncProxyPreference(modemID string, interfaceName string, prefs Preferences) error {
	proxy := c.proxyInstance()
	if proxy == nil {
		if prefs.ProxyEnabled {
			return ErrProxyNotConfigured
		}
		return nil
	}
	if !prefs.ProxyEnabled {
		return c.cleanupProxy(modemID, interfaceName)
	}
	_, err := proxy.Register(ProxyBinding{Username: modemID, InterfaceName: interfaceName})
	return err
}

func (c *Connector) cleanupProxy(modemID string, interfaceName string) error {
	err := deleteProxyState(c.proxyPath, interfaceName)
	if proxy := c.proxyInstance(); proxy != nil {
		err = errors.Join(err, proxy.Unregister(modemID))
	}
	return err
}

func (c *Connector) cleanupProxyInterfaces(modemID string, interfaceNames []string) error {
	var result error
	for _, interfaceName := range interfaceNames {
		result = errors.Join(result, c.cleanupProxy(modemID, interfaceName))
	}
	return result
}

func (c *Connector) cleanupProxyForModem(modemID string) error {
	interfaceNames, err := proxyInterfacesForModem(c.proxyPath, modemID)
	if err != nil {
		return err
	}
	err = c.cleanupProxyInterfaces(modemID, interfaceNames)
	if proxy := c.proxyInstance(); proxy != nil {
		err = errors.Join(err, proxy.Unregister(modemID))
	}
	return err
}

func (c *Connector) cleanupStaleConnectionState(modemID string, interfaceNames ...string) error {
	err := c.cleanupProxyInterfaces(modemID, interfaceNames)
	err = errors.Join(err, c.cleanupProxyForModem(modemID))
	err = errors.Join(err, restoreStaleDefaultRouteStatesWithState(c.routePath, routeStateRestoreTarget{modemID: modemID, interfaceNames: interfaceNames}, netlinkDefaultRouteOps))
	for _, interfaceName := range interfaceNames {
		if strings.TrimSpace(interfaceName) == "" {
			continue
		}
		err = errors.Join(err, netlink.FlushAddresses(interfaceName))
	}
	return err
}

func (c *Connector) connectBearerAfterRecovery(ctx context.Context, modem internetModem, prefs Preferences, connectErr error) (*mmodem.Bearer, error) {
	recoverErr := c.cleanupConnectFailure(ctx, modem)
	if recoverErr != nil {
		return nil, errors.Join(fmt.Errorf("connect bearer: %w", connectErr), recoverErr)
	}
	bearer, err := modem.connectBearer(ctx, bearerPropertiesFromPreferences(prefs))
	if err == nil {
		return bearer, nil
	}

	resetErr := c.resetConnectFailure(ctx, modem)
	if resetErr == nil {
		bearer, err = modem.connectBearer(ctx, bearerPropertiesFromPreferences(prefs))
		if err == nil {
			return bearer, nil
		}
	}
	return nil, errors.Join(fmt.Errorf("connect bearer: %w", err), resetErr)
}

func bearerPropertiesFromPreferences(prefs Preferences) mmodem.BearerProperties {
	return mmodem.BearerProperties{
		APN:         prefs.APN,
		IPType:      prefs.IPType,
		Username:    prefs.APNUsername,
		Password:    prefs.APNPassword,
		AllowedAuth: prefs.APNAuth,
	}
}

func (c *Connector) cleanupConnectFailure(ctx context.Context, modem internetModem) error {
	interfaceNames, err := c.deleteDisconnectedBearers(ctx, modem)
	err = errors.Join(err, c.cleanupStaleConnectionState(modem.id(), interfaceNames...))
	return err
}

func (c *Connector) resetConnectFailure(ctx context.Context, modem internetModem) error {
	err := c.cleanupConnectFailure(ctx, modem)
	if err != nil {
		return err
	}
	if err := modem.restart(ctx, false); err != nil {
		return fmt.Errorf("restart modem: %w", err)
	}
	return c.cleanupConnectFailure(ctx, modem)
}

func (c *Connector) deleteDisconnectedBearers(ctx context.Context, modem internetModem) ([]string, error) {
	bearers, err := modem.bearers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list bearers: %w", err)
	}
	var result error
	var interfaceNames []string
	for _, bearer := range bearers {
		connected, err := bearer.Connected(ctx)
		if err != nil {
			result = errors.Join(result, fmt.Errorf("read bearer state: %w", err))
			continue
		}
		if connected {
			continue
		}
		if interfaceName, err := bearer.Interface(ctx); err == nil && strings.TrimSpace(interfaceName) != "" {
			interfaceName = strings.TrimSpace(interfaceName)
			if !slices.Contains(interfaceNames, interfaceName) {
				interfaceNames = append(interfaceNames, interfaceName)
			}
		}
		if err := modem.deleteBearer(ctx, bearer.Path()); err != nil {
			result = errors.Join(result, fmt.Errorf("delete bearer %s: %w", bearer.Path(), err))
		}
	}
	return interfaceNames, result
}
