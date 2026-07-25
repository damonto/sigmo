package internet

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"slices"
	"strings"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/netlink"
	"github.com/damonto/wwan-go/qcom"
)

type qualcomm410Connection struct {
	link               *mmodem.BAMDMUXLink
	tracked            trackedConnection
	interfaceState     qualcomm410InterfaceState
	proxyInterfaceName string
	prefs              Preferences
	dns                []string
}

// qualcomm410InterfaceState records host settings changed while the raw-IP
// Internet path owns wwan0. Keeping the original values with the connection
// lets a successful switch back to ModemManager restore the interface, not
// only a failed setup attempt.
type qualcomm410InterfaceState struct {
	originalIPv6 netlink.IPv6Autoconfiguration
	originalMTU  uint32
	restoreIPv6  bool
	restoreMTU   bool
}

type qualcomm410State struct {
	// enabled describes the currently selected Qualcomm 410 path. A pending
	// restore records the normal bearer that must be retried before this state
	// can be discarded.
	enabled            bool
	connection         *qualcomm410Connection
	restorePending     bool
	restorePreferences Preferences
}

func (s qualcomm410State) active() bool {
	return s.enabled || s.connection != nil || s.restorePending
}

type qualcomm410Network struct {
	prefix  netip.Prefix
	peer    netip.Addr
	gateway netip.Addr
	family  int
}

var (
	openInternetQualcomm410Link         = mmodem.OpenBAMDMUXLink
	configureInternetQualcomm410Network = configureQualcomm410Network
	validateInternetQualcomm410Layout   = mmodem.ValidateQualcomm410Layout
	currentQualcomm410Bearer            = currentBearer
	restoreQualcomm410NormalBearer      = func(ctx context.Context, c *Connector, modem internetModem, prefs Preferences) error {
		_, err := c.connect(ctx, modem, prefs, false)
		return err
	}
	cleanupInternetQualcomm410State = func(ctx context.Context, connector *Connector, modemID string) error {
		return connector.cleanupStaleConnectionState(ctx, modemID, mmodem.Qualcomm410InternetInterface)
	}
)

type qualcomm410PDNLink interface {
	OpenPDN(context.Context, mmodem.BAMDMUXPDNConfig) (qcom.PDNInfo, error)
}

type qualcomm410NetworkOps struct {
	interfaceByName              func(string) (*net.Interface, error)
	readIPv6Autoconfiguration    func(string) (netlink.IPv6Autoconfiguration, error)
	setIPv6Autoconfiguration     func(string, netlink.IPv6Autoconfiguration) error
	disableIPv6Autoconfiguration func(string) error
	flushDefaultRoutes           func(string) error
	flushGlobalAddresses         func(string) error
	setUp                        func(string) error
	setMTU                       func(string, uint32) error
	defaultRoutes                func() ([]netlink.DefaultRoute, error)
	addPointToPointAddress       func(string, netip.Addr, netip.Addr) error
	addDefaultRoute              func(netlink.DefaultRoute) error
	deleteDefaultRoute           func(netlink.DefaultRoute) error
}

var systemQualcomm410NetworkOps = qualcomm410NetworkOps{
	interfaceByName:              net.InterfaceByName,
	readIPv6Autoconfiguration:    netlink.ReadIPv6Autoconfiguration,
	setIPv6Autoconfiguration:     netlink.SetIPv6Autoconfiguration,
	disableIPv6Autoconfiguration: netlink.DisableIPv6Autoconfiguration,
	flushDefaultRoutes:           netlink.FlushDefaultRoutes,
	flushGlobalAddresses:         netlink.FlushGlobalAddresses,
	setUp:                        netlink.SetUp,
	setMTU:                       netlink.SetMTU,
	defaultRoutes:                netlink.DefaultRoutes,
	addPointToPointAddress:       netlink.AddPointToPointAddress,
	addDefaultRoute:              netlink.AddDefaultRoute,
	deleteDefaultRoute:           netlink.DeleteDefaultRoute,
}

func (o qualcomm410NetworkOps) routeOps() defaultRouteOps {
	return defaultRouteOps{
		defaultRoutes:      o.defaultRoutes,
		addDefaultRoute:    o.addDefaultRoute,
		deleteDefaultRoute: o.deleteDefaultRoute,
	}
}

func (c *Connector) qualcomm410Connection(modem *mmodem.Modem) *qualcomm410Connection {
	if modem == nil {
		return nil
	}
	return c.qualcomm410ConnectionFor(modem.EquipmentIdentifier)
}

func (c *Connector) qualcomm410ConnectionFor(modemID string) *qualcomm410Connection {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.qualcomm410States[modemID].connection
}

func (c *Connector) qualcomm410EnabledFor(modemID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.qualcomm410States[modemID].enabled
}

func (c *Connector) setQualcomm410Enabled(modemID string, enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.qualcomm410States[modemID]
	state.enabled = enabled
	c.setQualcomm410StateLocked(modemID, state)
}

func (c *Connector) setQualcomm410RestorePending(modemID string, prefs Preferences) {
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.qualcomm410States[modemID]
	state.enabled = false
	state.restorePending = true
	state.restorePreferences = prefs
	c.setQualcomm410StateLocked(modemID, state)
}

func (c *Connector) clearQualcomm410State(modemID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.qualcomm410States, modemID)
}

func (c *Connector) clearQualcomm410RestorePending(modemID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.qualcomm410States[modemID]
	state.restorePending = false
	state.restorePreferences = Preferences{}
	state.enabled = state.connection != nil
	c.setQualcomm410StateLocked(modemID, state)
}

func (c *Connector) setQualcomm410ConnectionAndPreference(modemID string, connection *qualcomm410Connection, prefs Preferences) {
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.qualcomm410States[modemID]
	state.connection = connection
	if connection != nil {
		state.enabled = true
	}
	c.setQualcomm410StateLocked(modemID, state)
	if c.preferences == nil {
		c.preferences = make(map[string]Preferences)
	}
	c.preferences[modemID] = prefs
}

func (c *Connector) deleteQualcomm410Connection(modemID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.qualcomm410States[modemID]
	state.connection = nil
	c.setQualcomm410StateLocked(modemID, state)
}

func (c *Connector) qualcomm410StateFor(modemID string) qualcomm410State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.qualcomm410States[modemID]
}

func (c *Connector) setQualcomm410StateLocked(modemID string, state qualcomm410State) {
	if c.qualcomm410States == nil {
		c.qualcomm410States = make(map[string]qualcomm410State)
	}
	if !state.enabled && state.connection == nil && !state.restorePending {
		delete(c.qualcomm410States, modemID)
		return
	}
	c.qualcomm410States[modemID] = state
}

// SetQualcomm410Enabled migrates Internet between a ModemManager bearer and
// the dual-QMI, endpoint-routed BAM-DMUX path used by Qualcomm 410 devices.
func (c *Connector) SetQualcomm410Enabled(ctx context.Context, modem *mmodem.Modem, enabled bool) error {
	if modem == nil {
		return ErrModemRequired
	}
	modemID := modem.EquipmentIdentifier
	defer c.lockModem(modemID)()

	state := c.qualcomm410StateFor(modemID)
	access := modemAccess{modem: modem}
	if enabled {
		if err := validateInternetQualcomm410Layout(); err != nil {
			return fmt.Errorf("validate Qualcomm 410 layout: %w", err)
		}
		if state.restorePending {
			prefs := state.restorePreferences
			if state.connection != nil {
				c.clearQualcomm410RestorePending(modemID)
				return nil
			}
			if err := cleanupInternetQualcomm410State(ctx, c, modemID); err != nil {
				return fmt.Errorf("cleanup stale Qualcomm 410 state before re-enabling: %w", err)
			}
			if _, err := c.connectQualcomm410Locked(ctx, modem, prefs); err != nil {
				return fmt.Errorf("reconnect Internet over Qualcomm 410: %w", err)
			}
			c.clearQualcomm410RestorePending(modemID)
			return nil
		}
		if state.enabled {
			return nil
		}
		current, err := currentBearer(ctx, access)
		if err != nil {
			return fmt.Errorf("read Internet bearer before enabling Qualcomm 410: %w", err)
		}
		if !current.connected {
			if err := cleanupInternetQualcomm410State(ctx, c, modemID); err != nil {
				return fmt.Errorf("cleanup stale Qualcomm 410 state before enabling: %w", err)
			}
			c.setQualcomm410Enabled(modemID, true)
			return nil
		}
		prefs := c.qmapMigrationPreferences(ctx, access, current.bearer)
		if err := c.disconnect(ctx, access, false); err != nil {
			return fmt.Errorf("disconnect Internet bearer before enabling Qualcomm 410: %w", err)
		}
		c.setQualcomm410Enabled(modemID, true)
		if _, err := c.connectQualcomm410Locked(ctx, modem, prefs); err != nil {
			c.setQualcomm410Enabled(modemID, false)
			c.setQualcomm410RestorePending(modemID, prefs)
			restoreErr := restoreQualcomm410NormalBearer(ctx, c, access, prefs)
			if restoreErr == nil {
				c.clearQualcomm410State(modemID)
			}
			return errors.Join(fmt.Errorf("connect Internet over Qualcomm 410: %w", err), restoreErr)
		}
		return nil
	}

	if state.restorePending {
		return c.disableQualcomm410PendingRestore(ctx, modem, access, state.restorePreferences)
	}
	if state.connection == nil {
		if state.active() {
			if err := cleanupInternetQualcomm410State(ctx, c, modemID); err != nil {
				return fmt.Errorf("cleanup Qualcomm 410 state before disabling: %w", err)
			}
			c.setQualcomm410Enabled(modemID, false)
			return nil
		}
		current, err := currentQualcomm410Bearer(ctx, access)
		if err != nil {
			return fmt.Errorf("inspect Internet bearer before disabling Qualcomm 410: %w", err)
		}
		// Never flush an interface that currently belongs to a normal bearer.
		// This also makes disabling safe after a process restart, when the
		// in-memory Qualcomm 410 ownership marker is unavailable.
		if current.connected {
			return nil
		}
		if err := cleanupInternetQualcomm410State(ctx, c, modemID); err != nil {
			return fmt.Errorf("cleanup Qualcomm 410 state before disabling: %w", err)
		}
		c.setQualcomm410Enabled(modemID, false)
		return nil
	}
	prefs := state.connection.prefs
	c.setQualcomm410RestorePending(modemID, prefs)
	return c.disableQualcomm410PendingRestore(ctx, modem, access, prefs)
}

func (c *Connector) disableQualcomm410PendingRestore(ctx context.Context, modem *mmodem.Modem, access internetModem, prefs Preferences) error {
	modemID := modem.EquipmentIdentifier
	state := c.qualcomm410StateFor(modemID)
	if state.connection != nil {
		if err := c.disconnectQualcomm410Locked(ctx, modem); err != nil {
			return fmt.Errorf("disconnect Qualcomm 410 Internet before restoring bearer: %w", err)
		}
	} else {
		if err := cleanupInternetQualcomm410State(ctx, c, modemID); err != nil {
			return fmt.Errorf("cleanup Qualcomm 410 state before restoring bearer: %w", err)
		}
	}
	restoreErr := restoreQualcomm410NormalBearer(ctx, c, access, prefs)
	if restoreErr == nil {
		c.clearQualcomm410State(modemID)
		return nil
	}
	c.setQualcomm410Enabled(modemID, false)
	_, rollbackErr := c.connectQualcomm410Locked(ctx, modem, prefs)
	if rollbackErr != nil {
		rollbackErr = fmt.Errorf("restore Qualcomm 410 fallback: %w", rollbackErr)
	}
	return errors.Join(fmt.Errorf("restore Internet bearer after Qualcomm 410: %w", restoreErr), rollbackErr)
}

func hasQMIControlPort(modem *mmodem.Modem) bool {
	if modem == nil {
		return false
	}
	if modem.PrimaryPortType() == mmodem.ModemPortTypeQmi {
		return true
	}
	for _, port := range modem.Ports {
		if port.PortType == mmodem.ModemPortTypeQmi && strings.TrimSpace(port.Device) != "" {
			return true
		}
	}
	return false
}

func (c *Connector) connectQualcomm410(ctx context.Context, modem *mmodem.Modem, prefs Preferences) (*Connection, error) {
	if modem == nil {
		return nil, ErrModemRequired
	}
	defer c.lockModem(modem.EquipmentIdentifier)()
	connection, err := c.connectQualcomm410Locked(ctx, modem, prefs)
	if err == nil && c.qualcomm410StateFor(modem.EquipmentIdentifier).restorePending {
		c.clearQualcomm410RestorePending(modem.EquipmentIdentifier)
	}
	return connection, err
}

func (c *Connector) connectQualcomm410Locked(ctx context.Context, modem *mmodem.Modem, prefs Preferences) (*Connection, error) {
	prefs = normalizePreferences(prefs)
	if err := ValidatePreferences(prefs); err != nil {
		return nil, err
	}
	if prefs.APNUsername != "" || prefs.APNPassword != "" || prefs.APNAuth != "" {
		return nil, errors.New("Qualcomm 410 Internet authentication is not supported")
	}
	profileID := modemAccess{modem: modem}.profileID()
	if prefs.AlwaysOn && profileID == "" {
		return nil, ErrProfileIDRequired
	}
	if err := c.disconnectQualcomm410Locked(ctx, modem); err != nil {
		return nil, err
	}
	preferences, err := qualcomm410IPPreferences(prefs.IPType)
	if err != nil {
		return nil, err
	}

	link, err := openInternetQualcomm410Link(ctx, mmodem.BAMDMUXLinkConfig{
		ControlPort:   mmodem.Qualcomm410InternetQMI,
		InterfaceName: mmodem.Qualcomm410InternetInterface,
	})
	if err != nil {
		return nil, fmt.Errorf("open Qualcomm 410 Internet link: %w", err)
	}
	connection := &qualcomm410Connection{link: link, prefs: prefs}
	infos, legErrors := openQualcomm410PDNs(ctx, link, prefs.APN, preferences)
	if len(infos) == 0 {
		if legErrors == nil {
			legErrors = errors.New("Qualcomm 410 Internet has no available data leg")
		}
		return nil, errors.Join(legErrors, connection.close())
	}

	tracked, dns, interfaceState, err := configureInternetQualcomm410Network(ctx, c.persistence, modem.EquipmentIdentifier, prefs, mmodem.Qualcomm410InternetInterface, infos)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("configure Qualcomm 410 Internet: %w", err), connection.close())
	}
	connection.tracked = tracked
	connection.interfaceState = interfaceState
	connection.proxyInterfaceName = tracked.interfaceName
	connection.dns = dns
	if err := c.syncDefaultRouteTakeover(ctx, modem.EquipmentIdentifier, &connection.tracked); err != nil {
		return nil, errors.Join(fmt.Errorf("sync Qualcomm 410 default route takeover: %w", err), connection.cleanup(ctx, c), connection.close())
	}
	if err := c.applyProxyPreference(ctx, modem.EquipmentIdentifier, tracked.interfaceName, prefs.ProxyEnabled); err != nil {
		return nil, errors.Join(fmt.Errorf("configure Qualcomm 410 proxy: %w", err), connection.cleanup(ctx, c), connection.close())
	}
	if err := c.syncAlwaysOnState(ctx, profileID, prefs); err != nil {
		return nil, errors.Join(
			fmt.Errorf("sync Qualcomm 410 always on state: %w", err),
			c.cleanupProxy(ctx, modem.EquipmentIdentifier, tracked.interfaceName),
			connection.cleanup(ctx, c),
			connection.close(),
		)
	}

	c.setQualcomm410ConnectionAndPreference(modem.EquipmentIdentifier, connection, prefs)
	if legErrors != nil {
		slog.Warn("Qualcomm 410 Internet connected with unavailable data leg", "imei", modem.EquipmentIdentifier, "error", legErrors)
	}
	return c.qualcomm410ConnectionResponse(modem.EquipmentIdentifier, connection), nil
}

func openQualcomm410PDNs(ctx context.Context, link qualcomm410PDNLink, apn string, preferences []qcom.WDSIPPreference) ([]qcom.PDNInfo, error) {
	infos := make([]qcom.PDNInfo, 0, len(preferences))
	var result error
	for _, preference := range preferences {
		info, err := link.OpenPDN(ctx, mmodem.BAMDMUXPDNConfig{
			APN:          apn,
			IPPreference: preference,
		})
		if err != nil {
			result = errors.Join(result, fmt.Errorf("open Qualcomm 410 Internet %s leg: %w", preference, err))
			continue
		}
		infos = append(infos, info)
	}
	return infos, result
}

func (c *Connector) disconnectQualcomm410Locked(ctx context.Context, modem *mmodem.Modem) error {
	state := c.qualcomm410StateFor(modem.EquipmentIdentifier)
	connection := state.connection
	if connection == nil {
		return nil
	}
	err := errors.Join(
		connection.cleanupProxy(ctx, c, modem.EquipmentIdentifier),
		connection.cleanup(ctx, c),
		connection.close(),
	)
	if err != nil {
		return err
	}
	c.deleteQualcomm410Connection(modem.EquipmentIdentifier)
	return nil
}

// disconnectQualcomm410ForUser stops the current 410 data session without
// accidentally turning a transient restore failure into a future automatic
// reconnect. A normal bearer that appeared while the process was recovering
// is left untouched because it is not owned by the 410 path.
func (c *Connector) disconnectQualcomm410ForUser(ctx context.Context, modem *mmodem.Modem, access internetModem, state qualcomm410State) error {
	modemID := modem.EquipmentIdentifier
	if state.connection != nil {
		if err := c.disconnectQualcomm410Locked(ctx, modem); err != nil {
			return err
		}
		if state.restorePending {
			c.clearQualcomm410RestorePending(modemID)
			c.setQualcomm410Enabled(modemID, true)
		}
		return nil
	}

	current, err := currentQualcomm410Bearer(ctx, access)
	if err != nil {
		return fmt.Errorf("inspect Internet bearer: %w", err)
	}
	if !current.connected {
		if err := cleanupInternetQualcomm410State(ctx, c, modemID); err != nil {
			return fmt.Errorf("cleanup disconnected Qualcomm 410 Internet: %w", err)
		}
	}
	if state.restorePending {
		c.clearQualcomm410State(modemID)
	}
	return nil
}

func (c *qualcomm410Connection) close() error {
	if c == nil || c.link == nil {
		return nil
	}
	link := c.link
	c.link = nil
	return link.Close()
}

func (c *qualcomm410Connection) cleanupProxy(ctx context.Context, connector *Connector, modemID string) error {
	if c.proxyInterfaceName == "" {
		return nil
	}
	if err := connector.cleanupProxy(ctx, modemID, c.proxyInterfaceName); err != nil {
		return err
	}
	c.proxyInterfaceName = ""
	return nil
}

func (c *qualcomm410Connection) cleanup(ctx context.Context, connector *Connector) error {
	if c.tracked.interfaceName == "" {
		return nil
	}
	err := cleanupApplied(ctx, connector.persistence, c.tracked)
	if err == nil {
		err = connector.syncCleanedUpDefaultRouteState(ctx, c.tracked)
	}
	if err == nil {
		err = restoreQualcomm410InterfaceState(c.tracked.interfaceName, c.interfaceState, systemQualcomm410NetworkOps)
	}
	if err == nil {
		c.tracked = trackedConnection{}
		c.interfaceState = qualcomm410InterfaceState{}
	}
	return err
}

func (c *qualcomm410Connection) response() *Connection {
	ipType := qmapActualIPType([]trackedConnection{c.tracked})
	if ipType == "" {
		ipType = c.prefs.IPType
	}
	response := &Connection{
		Status: StatusConnected, APN: c.prefs.APN, IPType: ipType,
		DefaultRoute: c.prefs.DefaultRoute, ProxyEnabled: c.prefs.ProxyEnabled,
		AlwaysOn: c.prefs.AlwaysOn, DNS: slices.Clone(c.dns),
		InterfaceName: c.tracked.interfaceName, RouteMetric: c.tracked.routeMetric,
	}
	for _, prefix := range c.tracked.addresses {
		if prefix.Addr().Is4() {
			response.IPv4Addresses = append(response.IPv4Addresses, prefix.String())
		} else {
			response.IPv6Addresses = append(response.IPv6Addresses, prefix.String())
		}
	}
	return response
}

func cloneQualcomm410Connection(connection *qualcomm410Connection) *qualcomm410Connection {
	if connection == nil {
		return nil
	}
	cloned := *connection
	cloned.tracked = cloneTrackedConnection(connection.tracked)
	cloned.dns = slices.Clone(connection.dns)
	return &cloned
}

func (c *Connector) qualcomm410ConnectionResponse(modemID string, connection *qualcomm410Connection) *Connection {
	response := connection.response()
	if proxy := c.proxyInstance(); proxy != nil && response.InterfaceName != "" {
		response.Proxy = proxy.Status(modemID)
	}
	return response
}

func configureQualcomm410Network(ctx context.Context, state connectionStateStore, modemID string, prefs Preferences, interfaceName string, infos []qcom.PDNInfo) (tracked trackedConnection, dns []string, interfaceState qualcomm410InterfaceState, err error) {
	return configureQualcomm410NetworkWithOps(ctx, state, modemID, prefs, interfaceName, infos, systemQualcomm410NetworkOps)
}

func configureQualcomm410NetworkWithOps(ctx context.Context, state connectionStateStore, modemID string, prefs Preferences, interfaceName string, infos []qcom.PDNInfo, ops qualcomm410NetworkOps) (tracked trackedConnection, dns []string, interfaceState qualcomm410InterfaceState, err error) {
	tracked = trackedConnection{interfaceName: interfaceName, prefs: prefs, routeMetric: routeMetric(prefs.DefaultRoute)}
	networks, dns, mtu, err := qualcomm410Networks(infos)
	if err != nil {
		return tracked, nil, interfaceState, err
	}
	originalInterface, err := ops.interfaceByName(interfaceName)
	if err != nil {
		return tracked, nil, interfaceState, fmt.Errorf("find Qualcomm 410 interface %s: %w", interfaceName, err)
	}
	if originalInterface == nil {
		return tracked, nil, interfaceState, fmt.Errorf("find Qualcomm 410 interface %s: interface is nil", interfaceName)
	}
	originalIPv6, err := ops.readIPv6Autoconfiguration(interfaceName)
	if err != nil {
		return tracked, nil, interfaceState, fmt.Errorf("read Qualcomm 410 IPv6 interface state: %w", err)
	}
	interfaceState = qualcomm410InterfaceState{
		originalIPv6: originalIPv6,
		originalMTU:  uint32(originalInterface.MTU),
		restoreIPv6:  true,
	}

	routes := make([]netlink.DefaultRoute, 0, len(networks))
	for _, network := range networks {
		routes = append(routes, netlink.DefaultRoute{
			Interface: interfaceName,
			Family:    network.family,
			Gateway:   network.gateway,
			Source:    network.prefix.Addr(),
			Metric:    tracked.routeMetric,
		})
	}
	if !prefs.DefaultRoute && len(routes) > 0 {
		current, err := ops.defaultRoutes()
		if err != nil {
			return tracked, dns, interfaceState, fmt.Errorf("list default routes: %w", err)
		}
		tracked.routeMetric = secondaryRouteMetricFor(routes, current)
		setRouteMetric(routes, tracked.routeMetric)
	}

	release := false
	networkResetStarted := false
	defer func() {
		if release {
			return
		}
		rollbackCtx := context.WithoutCancel(ctx)
		var rollbackErr error
		if networkResetStarted {
			// Flush first so partially applied operations that were not yet
			// recorded in tracked are removed. Only route takeover state needs a
			// targeted rollback after the interface itself has been reset.
			rollbackErr = errors.Join(
				ops.flushDefaultRoutes(interfaceName),
				ops.flushGlobalAddresses(interfaceName),
				cleanupDefaultRouteChangesWithStore(rollbackCtx, state, tracked.interfaceName, tracked.routeChanges, ops.routeOps()),
			)
		}
		if interfaceState.restoreMTU {
			rollbackErr = errors.Join(rollbackErr, ops.setMTU(interfaceName, interfaceState.originalMTU))
		}
		if interfaceState.restoreIPv6 {
			rollbackErr = errors.Join(rollbackErr, ops.setIPv6Autoconfiguration(interfaceName, interfaceState.originalIPv6))
		}
		if rollbackErr != nil {
			rollbackErr = fmt.Errorf("roll back Qualcomm 410 interface: %w", rollbackErr)
		}
		err = errors.Join(err, rollbackErr)
	}()
	if err := ops.disableIPv6Autoconfiguration(interfaceName); err != nil {
		return tracked, dns, interfaceState, fmt.Errorf("disable Qualcomm 410 IPv6 autoconfiguration: %w", err)
	}
	networkResetStarted = true
	if err := errors.Join(ops.flushDefaultRoutes(interfaceName), ops.flushGlobalAddresses(interfaceName)); err != nil {
		return tracked, dns, interfaceState, fmt.Errorf("reset Qualcomm 410 interface network state: %w", err)
	}
	if err := ops.setUp(interfaceName); err != nil {
		return tracked, dns, interfaceState, fmt.Errorf("set Qualcomm 410 interface up: %w", err)
	}
	if mtu > 0 {
		interfaceState.restoreMTU = originalInterface.MTU > 0
		if err := ops.setMTU(interfaceName, mtu); err != nil {
			return tracked, dns, interfaceState, fmt.Errorf("set Qualcomm 410 interface MTU %d: %w", mtu, err)
		}
	}
	tracked.peers = make(map[netip.Prefix]netip.Addr, len(networks))
	for _, network := range networks {
		if err := ops.addPointToPointAddress(interfaceName, network.prefix.Addr(), network.peer); err != nil {
			return tracked, dns, interfaceState, fmt.Errorf("add Qualcomm 410 address %s: %w", network.prefix, err)
		}
		tracked.addresses = append(tracked.addresses, network.prefix)
		tracked.peers[network.prefix] = network.peer
	}
	if prefs.DefaultRoute {
		if err := restoreStaleDefaultRouteStatesWithStore(ctx, state, routeStateRestoreTarget{
			modemID: modemID, interfaceNames: []string{interfaceName},
		}, ops.routeOps()); err != nil {
			return tracked, dns, interfaceState, fmt.Errorf("restore previous default route state: %w", err)
		}
		changes, err := takeoverDefaultRoutesWithStore(ctx, state, modemID, interfaceName, routes, ops.routeOps())
		tracked.routeChanges = changes
		if err != nil {
			return tracked, dns, interfaceState, fmt.Errorf("take over default route: %w", err)
		}
	}
	for _, route := range routes {
		if err := ops.addDefaultRoute(route); err != nil {
			return tracked, dns, interfaceState, fmt.Errorf("add default route: %w", err)
		}
		tracked.routes = append(tracked.routes, route)
	}
	release = true
	return tracked, dns, interfaceState, nil
}

func restoreQualcomm410InterfaceState(interfaceName string, state qualcomm410InterfaceState, ops qualcomm410NetworkOps) error {
	var result error
	if state.restoreMTU {
		if err := ops.setMTU(interfaceName, state.originalMTU); err != nil {
			result = errors.Join(result, fmt.Errorf("restore Qualcomm 410 interface MTU: %w", err))
		}
	}
	if state.restoreIPv6 {
		if err := ops.setIPv6Autoconfiguration(interfaceName, state.originalIPv6); err != nil {
			result = errors.Join(result, fmt.Errorf("restore Qualcomm 410 IPv6 autoconfiguration: %w", err))
		}
	}
	return result
}

func qualcomm410Networks(infos []qcom.PDNInfo) ([]qualcomm410Network, []string, uint32, error) {
	var networks []qualcomm410Network
	var dns []string
	var mtu uint32
	for _, info := range infos {
		validIPv4 := false
		validIPv6 := false
		if local, peer, ok := qcom410AddressPair(info.LocalIPv4, info.IPv4Gateway, true); ok {
			validIPv4 = true
			network := qualcomm410Network{
				prefix:  netip.PrefixFrom(local, local.BitLen()),
				peer:    peer,
				gateway: peer,
				family:  netlink.FamilyIPv4,
			}
			if !slices.ContainsFunc(networks, func(existing qualcomm410Network) bool { return existing.prefix == network.prefix }) {
				networks = append(networks, network)
			}
		}
		if local, peer, ok := qcom410AddressPair(info.LocalIPv6, info.IPv6Gateway, false); ok {
			validIPv6 = true
			network := qualcomm410Network{
				prefix: netip.PrefixFrom(local, local.BitLen()),
				peer:   peer,
				family: netlink.FamilyIPv6,
			}
			if !slices.ContainsFunc(networks, func(existing qualcomm410Network) bool { return existing.prefix == network.prefix }) {
				networks = append(networks, network)
			}
		}
		for _, address := range info.DNS {
			parsed, ok := netip.AddrFromSlice(address)
			if !ok {
				continue
			}
			parsed = parsed.Unmap()
			if parsed.IsUnspecified() {
				continue
			}
			if parsed.Is4() {
				if !validIPv4 {
					continue
				}
			} else if !validIPv6 {
				continue
			}
			value := parsed.String()
			if !slices.Contains(dns, value) {
				dns = append(dns, value)
			}
		}
		if (validIPv4 || validIPv6) && info.MTU > 0 && (mtu == 0 || info.MTU < mtu) {
			mtu = info.MTU
		}
	}
	if len(networks) == 0 {
		return nil, nil, 0, ErrUnsupportedIPMethod
	}
	return networks, dns, mtu, nil
}

func qcom410AddressPair(localIP, peerIP net.IP, ipv4 bool) (netip.Addr, netip.Addr, bool) {
	if ipv4 {
		localIP = localIP.To4()
		peerIP = peerIP.To4()
	} else {
		// net.IP.To16 also succeeds for IPv4 addresses. Reject that case so a
		// malformed QMI response cannot create an IPv4 address on an IPv6 leg.
		if localIP.To4() != nil || peerIP.To4() != nil {
			return netip.Addr{}, netip.Addr{}, false
		}
		localIP = localIP.To16()
		peerIP = peerIP.To16()
	}
	local, localOK := netip.AddrFromSlice(localIP)
	peer, peerOK := netip.AddrFromSlice(peerIP)
	if !localOK || !peerOK {
		return netip.Addr{}, netip.Addr{}, false
	}
	local = local.Unmap()
	peer = peer.Unmap()
	if local.IsUnspecified() || peer.IsUnspecified() {
		return netip.Addr{}, netip.Addr{}, false
	}
	return local, peer, true
}

func qualcomm410IPPreferences(ipType string) ([]qcom.WDSIPPreference, error) {
	switch strings.ToLower(strings.TrimSpace(ipType)) {
	case "", "ipv4v6":
		return []qcom.WDSIPPreference{qcom.WDSIPPreferenceIPv4, qcom.WDSIPPreferenceIPv6}, nil
	case "ipv4":
		return []qcom.WDSIPPreference{qcom.WDSIPPreferenceIPv4}, nil
	case "ipv6":
		return []qcom.WDSIPPreference{qcom.WDSIPPreferenceIPv6}, nil
	default:
		return nil, fmt.Errorf("%w: %s", mmodem.ErrUnsupportedBearerIPType, ipType)
	}
}
