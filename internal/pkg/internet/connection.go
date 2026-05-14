package internet

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/netlink"
)

const (
	StatusConnected    = "connected"
	StatusDisconnected = "disconnected"
)

var ErrUnsupportedIPMethod = errors.New("only static bearer IP configuration is supported")

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
	connections  map[string]trackedConnection
	preferences  map[string]Preferences
	proxy        *Proxy
	alwaysOnPath string
}

func NewConnector() *Connector {
	return NewConnectorWithProxy(nil)
}

func NewConnectorWithProxy(proxy *Proxy) *Connector {
	return &Connector{
		connections:  make(map[string]trackedConnection),
		preferences:  make(map[string]Preferences),
		proxy:        proxy,
		alwaysOnPath: alwaysOnStatePath,
	}
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

func (c *Connector) Recover(modems []*mmodem.Modem) error {
	var result error
	for _, modem := range modems {
		if modem == nil {
			continue
		}
		c.mu.Lock()
		err := c.recoverLocked(modem)
		c.mu.Unlock()
		if err != nil {
			result = errors.Join(result, fmt.Errorf("recover internet connection for modem %s: %w", modem.EquipmentIdentifier, err))
		}
	}
	return result
}

func (c *Connector) Current(modem *mmodem.Modem) (*Connection, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	prefs := c.preferenceWithAlwaysOn(modem.EquipmentIdentifier)
	var staleInterfaces []string
	if tracked, ok := c.connections[modem.EquipmentIdentifier]; ok {
		bearer, err := modem.Bearer(tracked.bearerPath)
		if err == nil {
			connected, err := bearer.Connected()
			if err == nil {
				if !connected {
					err := c.cleanupTracked(modem.EquipmentIdentifier, tracked)
					if err == nil {
						err = c.syncCleanedUpDefaultRouteState(tracked)
					}
					err = errors.Join(err, restoreStaleDefaultRouteStatesForModem(modem.EquipmentIdentifier))
					if err != nil {
						return nil, fmt.Errorf("cleanup disconnected bearer: %w", err)
					}
					delete(c.connections, modem.EquipmentIdentifier)
					prefs := bearerPreferences(bearer, tracked.prefs)
					prefs = preferencesWithSelectedAPN(modem, prefs)
					c.preferences[modem.EquipmentIdentifier] = prefs
					return disconnectedConnection(prefs), nil
				}
				prefs := preferencesWithDefaultAPNCredentials(modem, tracked.prefs)
				tracked.prefs = prefs
				c.connections[modem.EquipmentIdentifier] = tracked
				c.preferences[modem.EquipmentIdentifier] = prefs
				connection, err := c.connectionFromBearer(modem.EquipmentIdentifier, bearer, prefs, tracked.routeMetric)
				if err == nil {
					return connection, nil
				}
			}
		}
		staleInterfaces = append(staleInterfaces, tracked.interfaceName)
		delete(c.connections, modem.EquipmentIdentifier)
		prefs = tracked.prefs
	}

	current, err := currentBearer(modem)
	if err != nil {
		return nil, err
	}
	if current.bearer == nil {
		if err := c.cleanupStaleConnectionState(modem.EquipmentIdentifier, staleInterfaces...); err != nil {
			return nil, err
		}
		prefs = preferencesWithSelectedAPN(modem, prefs)
		return disconnectedConnection(prefs), nil
	}
	if !current.connected {
		if interfaceName, err := current.bearer.Interface(); err == nil {
			staleInterfaces = append(staleInterfaces, interfaceName)
		}
		if err := c.cleanupStaleConnectionState(modem.EquipmentIdentifier, staleInterfaces...); err != nil {
			return nil, err
		}
		prefs = bearerPreferences(current.bearer, prefs)
		prefs = preferencesWithSelectedAPN(modem, prefs)
		c.preferences[modem.EquipmentIdentifier] = prefs
		return disconnectedConnection(prefs), nil
	}
	bearer := current.bearer
	tracked, metric, ok, err := recoverTrackedConnection(modem.EquipmentIdentifier, bearer, prefs)
	if err != nil {
		return nil, err
	}
	if ok {
		tracked.prefs = preferencesWithDefaultAPNCredentials(modem, tracked.prefs)
		c.connections[modem.EquipmentIdentifier] = tracked
		c.preferences[modem.EquipmentIdentifier] = tracked.prefs
		return c.connectionFromBearer(modem.EquipmentIdentifier, bearer, tracked.prefs, metric)
	}
	return nil, ErrUnsupportedIPMethod
}

func (c *Connector) recoverLocked(modem *mmodem.Modem) error {
	prefs := c.preferenceWithAlwaysOn(modem.EquipmentIdentifier)
	current, err := currentBearer(modem)
	if err != nil {
		return err
	}
	if current.bearer == nil {
		return c.cleanupStaleConnectionState(modem.EquipmentIdentifier)
	}
	if !current.connected {
		var staleInterfaces []string
		if interfaceName, err := current.bearer.Interface(); err == nil {
			staleInterfaces = append(staleInterfaces, interfaceName)
		}
		if err := c.cleanupStaleConnectionState(modem.EquipmentIdentifier, staleInterfaces...); err != nil {
			return err
		}
		c.preferences[modem.EquipmentIdentifier] = preferencesWithSelectedAPN(modem, bearerPreferences(current.bearer, prefs))
		return nil
	}

	tracked, _, ok, err := recoverTrackedConnection(modem.EquipmentIdentifier, current.bearer, prefs)
	if err != nil {
		return err
	}
	if !ok {
		return ErrUnsupportedIPMethod
	}
	tracked.prefs = preferencesWithDefaultAPNCredentials(modem, tracked.prefs)
	if err := c.syncProxyPreference(modem.EquipmentIdentifier, tracked.interfaceName, tracked.prefs); err != nil {
		return err
	}
	c.connections[modem.EquipmentIdentifier] = tracked
	c.preferences[modem.EquipmentIdentifier] = tracked.prefs
	return nil
}

func (c *Connector) Connect(modem *mmodem.Modem, prefs Preferences) (*Connection, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.connectLocked(modem, prefs, true)
}

func (c *Connector) connectLocked(modem *mmodem.Modem, prefs Preferences, clearAlwaysOnBefore bool) (*Connection, error) {
	prefs = normalizePreferences(prefs)
	if prefs.APN == "" {
		apn, err := apnFromBearers(modem)
		if err != nil {
			return nil, err
		}
		prefs.APN = apnForModem(modem, "", apn, c.preferenceWithAlwaysOn(modem.EquipmentIdentifier).APN)
	}
	prefs = preferencesWithDefaultAPNCredentials(modem, prefs)
	if err := c.disconnectLocked(modem, clearAlwaysOnBefore); err != nil {
		return nil, fmt.Errorf("disconnect previous bearer: %w", err)
	}

	bearer, err := modem.ConnectBearer(bearerPropertiesFromPreferences(prefs))
	if err != nil {
		bearer, err = c.connectBearerAfterRecovery(modem, prefs, err)
		if err != nil {
			return nil, err
		}
	}
	prefs = bearerPreferences(bearer, prefs)

	tracked, err := configureBearer(modem.EquipmentIdentifier, bearer, prefs)
	if err != nil {
		disconnectErr := bearer.Disconnect()
		return nil, errors.Join(err, disconnectErr)
	}
	tracked.bearerPath = bearer.Path()
	tracked.prefs = prefs

	if err := c.syncProxyPreference(modem.EquipmentIdentifier, tracked.interfaceName, prefs); err != nil {
		cleanupErr := c.cleanupTracked(modem.EquipmentIdentifier, tracked)
		disconnectErr := bearer.Disconnect()
		return nil, errors.Join(err, cleanupErr, disconnectErr)
	}
	if prefs.ProxyEnabled {
		if err := saveProxyStateForModem(proxyStatePath, modem.EquipmentIdentifier, tracked.interfaceName); err != nil {
			cleanupErr := c.cleanupTracked(modem.EquipmentIdentifier, tracked)
			disconnectErr := bearer.Disconnect()
			return nil, errors.Join(fmt.Errorf("save proxy state: %w", err), cleanupErr, disconnectErr)
		}
	}

	connection, err := c.connectionFromBearer(modem.EquipmentIdentifier, bearer, prefs, tracked.routeMetric)
	if err != nil {
		cleanupErr := c.cleanupTracked(modem.EquipmentIdentifier, tracked)
		disconnectErr := bearer.Disconnect()
		return nil, errors.Join(err, cleanupErr, disconnectErr)
	}
	if err := c.syncAlwaysOnState(modem.EquipmentIdentifier, prefs); err != nil {
		cleanupErr := c.cleanupTracked(modem.EquipmentIdentifier, tracked)
		disconnectErr := bearer.Disconnect()
		return nil, errors.Join(fmt.Errorf("sync always on state: %w", err), cleanupErr, disconnectErr)
	}
	if err := c.syncDefaultRouteTakeover(defaultRouteStatePath, modem.EquipmentIdentifier, &tracked); err != nil {
		cleanupErr := c.cleanupTracked(modem.EquipmentIdentifier, tracked)
		disconnectErr := bearer.Disconnect()
		return nil, errors.Join(fmt.Errorf("sync default route takeover: %w", err), cleanupErr, disconnectErr)
	}
	c.connections[modem.EquipmentIdentifier] = tracked
	c.preferences[modem.EquipmentIdentifier] = prefs

	return connection, nil
}

func (c *Connector) Disconnect(modem *mmodem.Modem) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.disconnectLocked(modem, true)
}

func (c *Connector) Restore(modem *mmodem.Modem) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	err := c.disconnectLocked(modem, true)
	delete(c.connections, modem.EquipmentIdentifier)
	delete(c.preferences, modem.EquipmentIdentifier)
	return err
}

func (c *Connector) disconnectLocked(modem *mmodem.Modem, clearAlwaysOn bool) error {
	var result error
	if clearAlwaysOn {
		result = errors.Join(result, c.clearAlwaysOnStateLocked(modem.EquipmentIdentifier))
	}

	if tracked, ok := c.connections[modem.EquipmentIdentifier]; ok {
		err := c.cleanupTracked(modem.EquipmentIdentifier, tracked)
		if err == nil {
			err = c.syncCleanedUpDefaultRouteState(tracked)
		}
		err = errors.Join(err, modem.DisconnectBearer(tracked.bearerPath))
		err = errors.Join(err, restoreStaleDefaultRouteStatesForModem(modem.EquipmentIdentifier))
		delete(c.connections, modem.EquipmentIdentifier)
		err = errors.Join(result, err)
		if err != nil {
			return fmt.Errorf("disconnect bearer: %w", err)
		}
		return nil
	}

	current, err := currentBearer(modem)
	if err != nil {
		err = errors.Join(result, err)
		return fmt.Errorf("disconnect bearer: %w", err)
	}
	if !current.connected {
		if current.bearer == nil {
			err := errors.Join(result, c.cleanupStaleConnectionState(modem.EquipmentIdentifier))
			if err != nil {
				return fmt.Errorf("disconnect bearer: %w", err)
			}
			return nil
		}
		interfaceName, err := current.bearer.Interface()
		if err != nil {
			err = errors.Join(result, fmt.Errorf("read bearer interface: %w", err))
			return fmt.Errorf("disconnect bearer: %w", err)
		}
		err = errors.Join(result, c.cleanupStaleConnectionState(modem.EquipmentIdentifier, interfaceName))
		if err != nil {
			return fmt.Errorf("disconnect bearer: %w", err)
		}
		return nil
	}
	bearer := current.bearer
	prefs := recoverPreferences(bearer, c.preference(modem.EquipmentIdentifier))
	interfaceName, interfaceErr := bearer.Interface()
	err = cleanupBearer(modem.EquipmentIdentifier, bearer, prefs)
	if err == nil && interfaceErr == nil {
		err = deleteRouteState(defaultRouteStatePath, interfaceName)
	}
	if interfaceErr == nil {
		err = errors.Join(err, c.cleanupProxy(modem.EquipmentIdentifier, interfaceName))
	} else {
		err = errors.Join(err, c.cleanupProxyForModem(modem.EquipmentIdentifier))
	}
	err = errors.Join(err, bearer.Disconnect())
	err = errors.Join(err, restoreStaleDefaultRouteStatesForModem(modem.EquipmentIdentifier))
	err = errors.Join(result, err)
	if err != nil {
		return fmt.Errorf("disconnect bearer: %w", err)
	}
	return nil
}

func (c *Connector) cleanupTracked(modemID string, tracked trackedConnection) error {
	err := c.cleanupProxy(modemID, tracked.interfaceName)
	cleanup := tracked
	if !tracked.prefs.DefaultRoute {
		cleanup.routeChanges = nil
	}
	cleanupErr := cleanupApplied(cleanup)
	if cleanupErr == nil {
		cleanupErr = deleteRouteState(defaultRouteStatePath, tracked.interfaceName)
	}
	err = errors.Join(err, cleanupErr)
	return err
}

func (c *Connector) syncDefaultRouteTakeover(path string, modemID string, tracked *trackedConnection) error {
	if tracked == nil || len(tracked.routeChanges) == 0 {
		return nil
	}

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
	return c.syncDefaultRouteRemoval(defaultRouteStatePath, tracked)
}

func (c *Connector) syncDefaultRouteRemoval(path string, removed trackedConnection) error {
	if len(removed.routes) == 0 {
		return nil
	}

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

func (c *Connector) clearAlwaysOnStateLocked(modemID string) error {
	prefs := c.preferenceWithAlwaysOn(modemID)
	prefs.AlwaysOn = false
	if hasInternetPreference(prefs) {
		c.preferences[modemID] = prefs
	} else {
		delete(c.preferences, modemID)
	}
	return deleteAlwaysOnStateForModem(c.alwaysOnPath, modemID)
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

func (c *Connector) connectionFromBearer(modemID string, bearer *mmodem.Bearer, prefs Preferences, metric int) (*Connection, error) {
	connection, err := connectionFromBearer(bearer, prefs, metric)
	if err != nil {
		return nil, err
	}
	if c.proxy != nil && strings.TrimSpace(connection.InterfaceName) != "" {
		connection.Proxy = c.proxy.Status(modemID)
	}
	return connection, nil
}

func (c *Connector) syncProxyPreference(modemID string, interfaceName string, prefs Preferences) error {
	if c.proxy == nil {
		if prefs.ProxyEnabled {
			return ErrProxyPasswordRequired
		}
		return nil
	}
	if !prefs.ProxyEnabled {
		return c.cleanupProxy(modemID, interfaceName)
	}
	_, err := c.proxy.Register(ProxyBinding{Username: modemID, InterfaceName: interfaceName})
	return err
}

func (c *Connector) cleanupProxy(modemID string, interfaceName string) error {
	err := deleteProxyState(proxyStatePath, interfaceName)
	if c.proxy != nil {
		err = errors.Join(err, c.proxy.Unregister(modemID))
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
	interfaceNames, err := proxyInterfacesForModem(proxyStatePath, modemID)
	if err != nil {
		return err
	}
	err = c.cleanupProxyInterfaces(modemID, interfaceNames)
	if c.proxy != nil {
		err = errors.Join(err, c.proxy.Unregister(modemID))
	}
	return err
}

func (c *Connector) cleanupStaleConnectionState(modemID string, interfaceNames ...string) error {
	err := c.cleanupProxyInterfaces(modemID, interfaceNames)
	err = errors.Join(err, c.cleanupProxyForModem(modemID))
	err = errors.Join(err, restoreStaleDefaultRouteStatesForModem(modemID, interfaceNames...))
	for _, interfaceName := range interfaceNames {
		if strings.TrimSpace(interfaceName) == "" {
			continue
		}
		err = errors.Join(err, netlink.FlushAddresses(interfaceName))
	}
	return err
}

func (c *Connector) connectBearerAfterRecovery(modem *mmodem.Modem, prefs Preferences, connectErr error) (*mmodem.Bearer, error) {
	recoverErr := c.cleanupConnectFailure(modem)
	if recoverErr != nil {
		return nil, errors.Join(fmt.Errorf("connect bearer: %w", connectErr), recoverErr)
	}
	bearer, err := modem.ConnectBearer(bearerPropertiesFromPreferences(prefs))
	if err == nil {
		return bearer, nil
	}

	resetErr := c.resetConnectFailure(modem)
	if resetErr == nil {
		bearer, err = modem.ConnectBearer(bearerPropertiesFromPreferences(prefs))
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

func (c *Connector) cleanupConnectFailure(modem *mmodem.Modem) error {
	interfaceNames, err := c.deleteDisconnectedBearers(modem)
	err = errors.Join(err, c.cleanupStaleConnectionState(modem.EquipmentIdentifier, interfaceNames...))
	return err
}

func (c *Connector) resetConnectFailure(modem *mmodem.Modem) error {
	err := c.cleanupConnectFailure(modem)
	if err != nil {
		return err
	}
	if err := modem.Restart(false); err != nil {
		return fmt.Errorf("restart modem: %w", err)
	}
	return c.cleanupConnectFailure(modem)
}

func (c *Connector) deleteDisconnectedBearers(modem *mmodem.Modem) ([]string, error) {
	bearers, err := modem.Bearers()
	if err != nil {
		return nil, fmt.Errorf("list bearers: %w", err)
	}
	var result error
	var interfaceNames []string
	for _, bearer := range bearers {
		connected, err := bearer.Connected()
		if err != nil {
			result = errors.Join(result, fmt.Errorf("read bearer state: %w", err))
			continue
		}
		if connected {
			continue
		}
		if interfaceName, err := bearer.Interface(); err == nil && strings.TrimSpace(interfaceName) != "" {
			interfaceName = strings.TrimSpace(interfaceName)
			if !slices.Contains(interfaceNames, interfaceName) {
				interfaceNames = append(interfaceNames, interfaceName)
			}
		}
		if err := modem.DeleteBearer(bearer.Path()); err != nil {
			result = errors.Join(result, fmt.Errorf("delete bearer %s: %w", bearer.Path(), err))
		}
	}
	return interfaceNames, result
}
