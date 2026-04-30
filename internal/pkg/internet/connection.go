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
}

type Connection struct {
	Status          string
	APN             string
	DefaultRoute    bool
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
	mu          sync.Mutex
	connections map[string]trackedConnection
	preferences map[string]Preferences
}

func NewConnector() *Connector {
	return &Connector{
		connections: make(map[string]trackedConnection),
		preferences: make(map[string]Preferences),
	}
}

func (c *Connector) Current(modem *mmodem.Modem) (*Connection, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	prefs := c.preference(modem.EquipmentIdentifier)
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
					c.preferences[modem.EquipmentIdentifier] = prefs
					return disconnectedConnection(prefs), nil
				}
				connection, err := connectionFromBearer(bearer, tracked.prefs, tracked.routeMetric)
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
		if err := restoreStaleDefaultRouteStatesForModem(modem.EquipmentIdentifier, staleInterfaces...); err != nil {
			return nil, err
		}
		return disconnectedConnection(prefs), nil
	}
	if !current.connected {
		if interfaceName, err := current.bearer.Interface(); err == nil {
			staleInterfaces = append(staleInterfaces, interfaceName)
		}
		if err := restoreStaleDefaultRouteStatesForModem(modem.EquipmentIdentifier, staleInterfaces...); err != nil {
			return nil, err
		}
		prefs = bearerPreferences(current.bearer, prefs)
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
		return connectionFromBearer(bearer, tracked.prefs, metric)
	}
	return nil, ErrUnsupportedIPMethod
}

func (c *Connector) Connect(modem *mmodem.Modem, prefs Preferences) (*Connection, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	prefs.APN = strings.TrimSpace(prefs.APN)
	if prefs.APN == "" {
		apn, err := apnFromBearers(modem)
		if err != nil {
			return nil, err
		}
		prefs.APN = apn
	}
	if prefs.APN == "" {
		prefs.APN = c.preference(modem.EquipmentIdentifier).APN
	}
	if err := c.disconnectLocked(modem); err != nil {
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
	c.connections[modem.EquipmentIdentifier] = tracked
	c.preferences[modem.EquipmentIdentifier] = prefs

	return connectionFromBearer(bearer, prefs, routeMetric(prefs.DefaultRoute))
}

func (c *Connector) Disconnect(modem *mmodem.Modem) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.disconnectLocked(modem)
}

func (c *Connector) Restore(modem *mmodem.Modem) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	err := c.disconnectLocked(modem)
	delete(c.connections, modem.EquipmentIdentifier)
	delete(c.preferences, modem.EquipmentIdentifier)
	return err
}

func (c *Connector) disconnectLocked(modem *mmodem.Modem) error {
	if tracked, ok := c.connections[modem.EquipmentIdentifier]; ok {
		err := c.cleanupTracked(tracked)
		err = errors.Join(err, modem.DisconnectBearer(tracked.bearerPath))
		err = errors.Join(err, restoreStaleDefaultRouteStatesForModem(modem.EquipmentIdentifier))
		delete(c.connections, modem.EquipmentIdentifier)
		if err != nil {
			return fmt.Errorf("disconnect bearer: %w", err)
		}
		return nil
	}

	current, err := currentBearer(modem)
	if err != nil {
		return err
	}
	if !current.connected {
		if current.bearer == nil {
			return restoreStaleDefaultRouteStatesForModem(modem.EquipmentIdentifier)
		}
		interfaceName, err := current.bearer.Interface()
		if err != nil {
			return fmt.Errorf("read bearer interface: %w", err)
		}
		return restoreStaleDefaultRouteStatesForModem(modem.EquipmentIdentifier, interfaceName)
	}
	bearer := current.bearer
	prefs := recoverPreferences(bearer, c.preference(modem.EquipmentIdentifier))
	err = cleanupBearer(modem.EquipmentIdentifier, bearer, prefs)
	err = errors.Join(err, bearer.Disconnect())
	err = errors.Join(err, restoreStaleDefaultRouteStatesForModem(modem.EquipmentIdentifier))
	if err != nil {
		return fmt.Errorf("disconnect bearer: %w", err)
	}
	return nil
}

func (c *Connector) cleanupTracked(tracked trackedConnection) error {
	return cleanupApplied(tracked)
}

func (c *Connector) preference(modemID string) Preferences {
	if prefs, ok := c.preferences[modemID]; ok {
		prefs.APN = strings.TrimSpace(prefs.APN)
		return prefs
	}
	return Preferences{}
}
