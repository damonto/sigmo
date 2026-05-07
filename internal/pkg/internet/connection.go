package internet

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

const (
	StatusConnected    = "connected"
	StatusDisconnected = "disconnected"
)

var ErrUnsupportedIPMethod = errors.New("only static bearer IP configuration is supported")

type Preferences struct {
	APN          string
	DefaultRoute bool
	ProxyEnabled bool
	AlwaysOn     bool
}

type Connection struct {
	Status          string
	APN             string
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
					err := c.cleanupTracked(tracked)
					err = errors.Join(err, restoreStaleDefaultRouteStatesForModem(modem.EquipmentIdentifier))
					if err != nil {
						return nil, fmt.Errorf("cleanup disconnected bearer: %w", err)
					}
					delete(c.connections, modem.EquipmentIdentifier)
					prefs := bearerPreferences(bearer, tracked.prefs)
					prefs.APN = apnForModem(modem, "", "", prefs.APN)
					c.preferences[modem.EquipmentIdentifier] = prefs
					return disconnectedConnection(prefs), nil
				}
				connection, err := c.connectionFromBearer(bearer, tracked.prefs, tracked.routeMetric)
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
		prefs.APN = apnForModem(modem, "", "", prefs.APN)
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
		prefs.APN = apnForModem(modem, "", "", prefs.APN)
		c.preferences[modem.EquipmentIdentifier] = prefs
		return disconnectedConnection(prefs), nil
	}
	bearer := current.bearer
	tracked, metric, ok, err := recoverTrackedConnection(modem.EquipmentIdentifier, bearer, prefs)
	if err != nil {
		return nil, err
	}
	if ok {
		c.connections[modem.EquipmentIdentifier] = tracked
		c.preferences[modem.EquipmentIdentifier] = tracked.prefs
		return c.connectionFromBearer(bearer, tracked.prefs, metric)
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
		c.preferences[modem.EquipmentIdentifier] = bearerPreferences(current.bearer, prefs)
		return nil
	}

	tracked, _, ok, err := recoverTrackedConnection(modem.EquipmentIdentifier, current.bearer, prefs)
	if err != nil {
		return err
	}
	if !ok {
		return ErrUnsupportedIPMethod
	}
	if err := c.syncProxyPreference(tracked.interfaceName, tracked.prefs); err != nil {
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
	prefs.APN = strings.TrimSpace(prefs.APN)
	if prefs.APN == "" {
		apn, err := apnFromBearers(modem)
		if err != nil {
			return nil, err
		}
		prefs.APN = apnForModem(modem, "", apn, c.preferenceWithAlwaysOn(modem.EquipmentIdentifier).APN)
	}
	if err := c.disconnectLocked(modem, clearAlwaysOnBefore); err != nil {
		return nil, fmt.Errorf("disconnect previous bearer: %w", err)
	}

	bearer, err := modem.ConnectBearer(prefs.APN)
	if err != nil {
		return nil, fmt.Errorf("connect bearer: %w", err)
	}
	prefs = bearerPreferences(bearer, prefs)

	tracked, err := configureBearer(modem.EquipmentIdentifier, bearer, prefs)
	if err != nil {
		disconnectErr := bearer.Disconnect()
		return nil, errors.Join(err, disconnectErr)
	}
	tracked.bearerPath = bearer.Path()
	tracked.prefs = prefs
	tracked.routeMetric = routeMetric(prefs.DefaultRoute)

	if err := c.syncProxyPreference(tracked.interfaceName, prefs); err != nil {
		cleanupErr := c.cleanupTracked(tracked)
		disconnectErr := bearer.Disconnect()
		return nil, errors.Join(err, cleanupErr, disconnectErr)
	}
	if prefs.ProxyEnabled {
		if err := saveProxyStateForModem(proxyStatePath, modem.EquipmentIdentifier, tracked.interfaceName); err != nil {
			cleanupErr := c.cleanupTracked(tracked)
			disconnectErr := bearer.Disconnect()
			return nil, errors.Join(fmt.Errorf("save proxy state: %w", err), cleanupErr, disconnectErr)
		}
	}

	connection, err := c.connectionFromBearer(bearer, prefs, routeMetric(prefs.DefaultRoute))
	if err != nil {
		cleanupErr := c.cleanupTracked(tracked)
		disconnectErr := bearer.Disconnect()
		return nil, errors.Join(err, cleanupErr, disconnectErr)
	}
	if err := c.syncAlwaysOnState(modem.EquipmentIdentifier, prefs); err != nil {
		cleanupErr := c.cleanupTracked(tracked)
		disconnectErr := bearer.Disconnect()
		return nil, errors.Join(fmt.Errorf("sync always on state: %w", err), cleanupErr, disconnectErr)
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
		err := c.cleanupTracked(tracked)
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
		err = errors.Join(err, c.cleanupProxy(interfaceName))
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

func (c *Connector) cleanupTracked(tracked trackedConnection) error {
	err := c.cleanupProxy(tracked.interfaceName)
	cleanupErr := cleanupApplied(tracked)
	if cleanupErr == nil {
		cleanupErr = deleteRouteState(defaultRouteStatePath, tracked.interfaceName)
	}
	err = errors.Join(err, cleanupErr)
	return err
}

func (c *Connector) preference(modemID string) Preferences {
	if prefs, ok := c.preferences[modemID]; ok {
		prefs.APN = strings.TrimSpace(prefs.APN)
		return prefs
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
	if strings.TrimSpace(prefs.APN) != "" || prefs.DefaultRoute || prefs.ProxyEnabled {
		c.preferences[modemID] = prefs
	} else {
		delete(c.preferences, modemID)
	}
	return deleteAlwaysOnStateForModem(c.alwaysOnPath, modemID)
}

func (c *Connector) connectionFromBearer(bearer *mmodem.Bearer, prefs Preferences, metric int) (*Connection, error) {
	connection, err := connectionFromBearer(bearer, prefs, metric)
	if err != nil {
		return nil, err
	}
	if c.proxy != nil && strings.TrimSpace(connection.InterfaceName) != "" {
		connection.Proxy = c.proxy.Status(connection.InterfaceName)
	}
	return connection, nil
}

func (c *Connector) syncProxyPreference(interfaceName string, prefs Preferences) error {
	if c.proxy == nil {
		if prefs.ProxyEnabled {
			return ErrProxyPasswordRequired
		}
		return nil
	}
	if !prefs.ProxyEnabled {
		return c.cleanupProxy(interfaceName)
	}
	_, err := c.proxy.Register(interfaceName)
	return err
}

func (c *Connector) cleanupProxy(interfaceName string) error {
	err := deleteProxyState(proxyStatePath, interfaceName)
	if c.proxy != nil {
		err = errors.Join(err, c.proxy.Unregister(interfaceName))
	}
	return err
}

func (c *Connector) cleanupProxyInterfaces(interfaceNames []string) error {
	var result error
	for _, interfaceName := range interfaceNames {
		result = errors.Join(result, c.cleanupProxy(interfaceName))
	}
	return result
}

func (c *Connector) cleanupProxyForModem(modemID string) error {
	interfaceNames, err := proxyInterfacesForModem(proxyStatePath, modemID)
	if err != nil {
		return err
	}
	return c.cleanupProxyInterfaces(interfaceNames)
}

func (c *Connector) cleanupStaleConnectionState(modemID string, interfaceNames ...string) error {
	err := c.cleanupProxyInterfaces(interfaceNames)
	err = errors.Join(err, c.cleanupProxyForModem(modemID))
	err = errors.Join(err, restoreStaleDefaultRouteStatesForModem(modemID, interfaceNames...))
	return err
}
