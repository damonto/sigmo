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
	modemlink "github.com/damonto/sigmo/internal/pkg/modem/link"
	"github.com/damonto/sigmo/internal/pkg/netlink"
	wwanmodem "github.com/damonto/wwan-go/modem"
	"github.com/damonto/wwan-go/qcom"
)

const (
	internetQMAPMuxID = 1
	imsQMAPMuxID      = 2
)

type qmapConnection struct {
	modem      *mmodem.Modem
	generation uint64
	sessions   []*modemlink.QMAPSession
	tracked    trackedConnection
}

type qmapSessionResult struct {
	preference qcom.WDSIPPreference
	session    *modemlink.QMAPSession
	err        error
}

type qmapOps struct {
	openSessions     func(context.Context, *mmodem.Modem, []modemlink.QMAPConfig) ([]modemlink.QMAPSessionResult, error)
	configureNetwork func(context.Context, connectionStateStore, string, Preferences, qmapLinkConfig, defaultRouteOps) (trackedConnection, error)
	removeMuxes      func(*mmodem.Modem, ...uint8) error
}

func defaultQMAPOps() qmapOps {
	return qmapOps{
		openSessions:     modemlink.OpenQMAPSessions,
		configureNetwork: configureQMAPNetwork,
		removeMuxes:      modemlink.RemoveQMAPMuxes,
	}
}

func (o qmapOps) withDefaults() qmapOps {
	defaults := defaultQMAPOps()
	if o.openSessions == nil {
		o.openSessions = defaults.openSessions
	}
	if o.configureNetwork == nil {
		o.configureNetwork = defaults.configureNetwork
	}
	if o.removeMuxes == nil {
		o.removeMuxes = defaults.removeMuxes
	}
	return o
}

func (c *Connector) qmapOperationSet() qmapOps {
	if c == nil {
		return defaultQMAPOps()
	}
	return c.qmap.withDefaults()
}

func (c *Connector) removeQMAPMuxes(modem *mmodem.Modem, muxIDs ...uint8) error {
	return c.qmapOperationSet().removeMuxes(modem, muxIDs...)
}

func (c *Connector) qmapConnection(modem *mmodem.Modem) *qmapConnection {
	if modem == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	connection := c.qmapConnections[modem.EquipmentIdentifier]
	if connection == nil || connection.generation != modem.Generation() {
		return nil
	}
	return connection
}

func (c *Connector) qmapEnabledFor(modemID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.qmapEnabled[modemID]
}

func (c *Connector) setQMAPEnabled(modemID string, enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if enabled {
		c.qmapEnabled[modemID] = true
		return
	}
	delete(c.qmapEnabled, modemID)
}

func (c *Connector) qmapPendingNormalFor(modemID string) (Preferences, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	prefs, ok := c.qmapPendingNormal[modemID]
	return prefs, ok
}

func (c *Connector) setQMAPPendingNormal(modemID string, prefs Preferences) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.qmapPendingNormal[modemID] = prefs
}

func (c *Connector) deleteQMAPPendingNormal(modemID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.qmapPendingNormal, modemID)
}

// SetQMAPEnabled migrates an existing Internet connection between the normal
// process-owned bearer and QMAP. Firmware that cannot leave QMAP in place is
// reset, then the replacement-device path restores the normal bearer.
func (c *Connector) SetQMAPEnabled(ctx context.Context, modem *mmodem.Modem, enabled bool) error {
	if modem == nil {
		return ErrModemRequired
	}
	if modem.PrimaryPortType() != wwanmodem.PortQMI {
		return nil
	}
	modemID := modem.EquipmentIdentifier
	defer c.lockRouteTransaction(modemID)()
	if !enabled {
		if prefs, ok := c.qmapPendingNormalFor(modemID); ok {
			if err := c.removeManagedQMAPMuxes(modem); err != nil {
				return err
			}
			if err := modemlink.RestoreNonQMAPDataFormat(ctx, modem); err != nil {
				return err
			}
			if _, err := c.connect(ctx, modemAccess{modem: modem}, prefs, false); err != nil {
				return err
			}
			c.deleteQMAPPendingNormal(modemID)
			c.setQMAPEnabled(modemID, false)
			return nil
		}
	}
	if c.qmapEnabledFor(modemID) == enabled {
		return nil
	}

	access := modemAccess{modem: modem}
	if enabled {
		current, err := currentBearer(ctx, access)
		if err != nil {
			return fmt.Errorf("read Internet bearer before enabling QMAP: %w", err)
		}
		if !current.connected {
			if err := c.cleanupStaleQMAPInternet(ctx, modem); err != nil {
				return fmt.Errorf("cleanup stale QMAP Internet before enabling: %w", err)
			}
			c.setQMAPEnabled(modemID, true)
			return nil
		}
		prefs := c.qmapMigrationPreferences(ctx, access, current.bearer)
		if err := c.disconnect(ctx, access, false); err != nil {
			return fmt.Errorf("disconnect Internet bearer before enabling QMAP: %w", err)
		}
		c.setQMAPEnabled(modemID, true)
		if _, err := c.connectQMAPLocked(ctx, modem, prefs); err != nil {
			c.setQMAPEnabled(modemID, false)
			_, restoreErr := c.connect(ctx, access, prefs, false)
			return errors.Join(fmt.Errorf("connect Internet over QMAP: %w", err), restoreErr)
		}
		return nil
	}

	connection := c.qmapConnection(modem)
	if connection == nil {
		if err := c.removeManagedQMAPMuxes(modem); err != nil {
			return err
		}
		if err := modemlink.RestoreNonQMAPDataFormat(ctx, modem); err != nil {
			if errors.Is(err, qcom.QMIErrorInternal) {
				c.setQMAPEnabled(modemID, false)
				if resetErr := modem.Reset(ctx); resetErr != nil {
					c.setQMAPEnabled(modemID, true)
					return fmt.Errorf("reset modem for non-QMAP mode: %w", resetErr)
				}
				return nil
			}
			return err
		}
		c.setQMAPEnabled(modemID, false)
		return nil
	}
	prefs := connection.tracked.prefs
	if err := c.disconnectQMAPLocked(ctx, modem); err != nil {
		return fmt.Errorf("disconnect QMAP Internet before restoring bearer: %w", err)
	}
	if err := c.removeManagedQMAPMuxes(modem); err != nil {
		c.setQMAPEnabled(modemID, true)
		_, restoreErr := c.connectQMAPLocked(ctx, modem, prefs)
		return errors.Join(fmt.Errorf("remove QMAP mux interfaces: %w", err), restoreErr)
	}
	if err := modemlink.RestoreNonQMAPDataFormat(ctx, modem); err != nil {
		if errors.Is(err, qcom.QMIErrorInternal) {
			c.setQMAPPendingNormal(modemID, prefs)
			c.setQMAPEnabled(modemID, false)
			if resetErr := modem.Reset(ctx); resetErr != nil {
				c.deleteQMAPPendingNormal(modemID)
				c.setQMAPEnabled(modemID, true)
				_, restoreErr := c.connectQMAPLocked(ctx, modem, prefs)
				return errors.Join(fmt.Errorf("reset modem for non-QMAP Internet: %w", resetErr), restoreErr)
			}
			return nil
		}
		_, restoreErr := c.connectQMAPLocked(ctx, modem, prefs)
		return errors.Join(fmt.Errorf("restore non-QMAP data format: %w", err), restoreErr)
	}
	c.setQMAPEnabled(modemID, false)
	if _, err := c.connect(ctx, access, prefs, false); err != nil {
		c.setQMAPEnabled(modemID, true)
		_, restoreErr := c.connectQMAPLocked(ctx, modem, prefs)
		return errors.Join(fmt.Errorf("restore Internet bearer: %w", err), restoreErr)
	}
	return nil
}

func (c *Connector) removeManagedQMAPMuxes(modem *mmodem.Modem) error {
	return c.removeQMAPMuxes(modem, internetQMAPMuxID, imsQMAPMuxID)
}

func (c *Connector) cleanupStaleQMAPInternet(ctx context.Context, modem *mmodem.Modem) error {
	modemID := ""
	if modem != nil {
		modemID = modem.EquipmentIdentifier
	}
	routeErr := restoreStaleDefaultRouteStatesWithStore(ctx, c.persistence, routeStateRestoreTarget{
		modemID: modemID,
	}, c.routeOperationSet())
	muxErr := c.removeQMAPMuxes(modem, internetQMAPMuxID)
	return errors.Join(routeErr, muxErr)
}

func (c *Connector) qmapMigrationPreferences(ctx context.Context, modem internetModem, bearer *mmodem.Bearer) Preferences {
	if tracked, ok := c.connection(modem.id()); ok {
		return tracked.prefs
	}
	return recoverPreferences(ctx, bearer, c.preferenceWithAlwaysOn(ctx, modem))
}

func (c *Connector) connectQMAPLocked(ctx context.Context, modem *mmodem.Modem, prefs Preferences) (*Connection, error) {
	prefs = normalizePreferences(prefs)
	if err := ValidatePreferences(prefs); err != nil {
		return nil, err
	}
	profileID := modemAccess{modem: modem}.profileID()
	if prefs.AlwaysOn && profileID == "" {
		return nil, ErrProfileIDRequired
	}
	if prefs.APNUsername != "" || prefs.APNPassword != "" || prefs.APNAuth != "" {
		return nil, errors.New("QMAP Internet authentication is not supported")
	}
	if err := c.disconnectQMAPLocked(ctx, modem); err != nil {
		return nil, err
	}
	preferences, err := qmapIPPreferences(prefs.IPType)
	if err != nil {
		return nil, err
	}
	connection := &qmapConnection{modem: modem, generation: modem.Generation()}
	var combined qmapLinkConfig
	var familyErrors error
	familyResults, err := c.openQMAPFamilySessions(ctx, modem, prefs.APN, preferences)
	if err != nil {
		return nil, errors.Join(
			fmt.Errorf("open QMAP mux %d sessions: %w", internetQMAPMuxID, err),
			c.removeQMAPMuxes(modem, internetQMAPMuxID),
		)
	}
	for _, result := range familyResults {
		preference := result.preference
		session := result.session
		if result.err != nil {
			familyErrors = errors.Join(familyErrors,
				fmt.Errorf("open QMAP mux %d for IP preference %d: %w", internetQMAPMuxID, preference, result.err),
			)
			continue
		}
		if session == nil {
			familyErrors = errors.Join(familyErrors,
				fmt.Errorf("open QMAP mux %d for IP preference %d: session is nil", internetQMAPMuxID, preference),
			)
			continue
		}
		candidateSessions := append(slices.Clone(connection.sessions), session)
		candidateConfig := qmapLinkConfig{}
		sessionErr := validateQMAPSessionFamily(preference, session.Info)
		if sessionErr == nil {
			candidateConfig, sessionErr = combineQMAPSessions(candidateSessions)
		}
		if sessionErr != nil {
			familyErr := fmt.Errorf("validate QMAP mux %d for IP preference %d: %w", internetQMAPMuxID, preference, sessionErr)
			if closeErr := session.Close(); closeErr != nil {
				return nil, errors.Join(
					familyErr,
					fmt.Errorf("close rejected QMAP session: %w", closeErr),
					connection.close(),
					c.removeQMAPMuxes(modem, internetQMAPMuxID),
				)
			}
			familyErrors = errors.Join(familyErrors, familyErr)
			continue
		}
		connection.sessions = candidateSessions
		combined = candidateConfig
	}
	if len(connection.sessions) == 0 {
		if familyErrors == nil {
			familyErrors = errors.New("QMAP Internet has no available data family")
		}
		return nil, errors.Join(
			familyErrors,
			connection.close(),
			c.removeQMAPMuxes(modem, internetQMAPMuxID),
		)
	}
	tracked, err := c.qmapOperationSet().configureNetwork(ctx, c.persistence, modem.EquipmentIdentifier, prefs, combined, c.routeOperationSet())
	if err != nil {
		return nil, errors.Join(
			fmt.Errorf("configure QMAP mux %d: %w", internetQMAPMuxID, err),
			connection.close(),
			c.removeQMAPMuxes(modem, internetQMAPMuxID),
		)
	}
	tracked.modemGeneration = modem.Generation()
	tracked.profileID = profileID
	connection.tracked = tracked
	if err := c.syncDefaultRouteTakeover(ctx, modem.EquipmentIdentifier, &connection.tracked); err != nil {
		return nil, errors.Join(
			fmt.Errorf("sync QMAP default route takeover: %w", err),
			connection.cleanup(ctx, c),
			connection.close(),
			c.removeQMAPMuxes(modem, internetQMAPMuxID),
		)
	}
	if familyErrors != nil {
		slog.Warn("QMAP Internet connected with unavailable IP family", "imei", modem.EquipmentIdentifier, "error", familyErrors)
	}
	if prefs.ProxyEnabled {
		if err := c.applyProxyPreference(
			ctx,
			modem.EquipmentIdentifier,
			connection.tracked.interfaceName,
			connection.tracked.dns,
			true,
		); err != nil {
			return nil, errors.Join(
				fmt.Errorf("configure QMAP proxy: %w", err),
				connection.cleanup(ctx, c),
				connection.close(),
				c.removeQMAPMuxes(modem, internetQMAPMuxID),
			)
		}
	}
	if err := c.syncAlwaysOnState(ctx, profileID, prefs); err != nil {
		var proxyErr error
		if prefs.ProxyEnabled {
			proxyErr = c.cleanupProxy(ctx, modem.EquipmentIdentifier, connection.tracked.interfaceName)
		}
		return nil, errors.Join(
			fmt.Errorf("sync QMAP always on state: %w", err),
			proxyErr,
			connection.cleanup(ctx, c),
			connection.close(),
			c.removeQMAPMuxes(modem, internetQMAPMuxID),
		)
	}
	c.mu.Lock()
	c.qmapConnections[modem.EquipmentIdentifier] = connection
	c.preferences[modem.EquipmentIdentifier] = prefs
	c.mu.Unlock()
	return c.qmapConnectionResponse(modem.EquipmentIdentifier, connection), nil
}

func (c *Connector) disconnectQMAPLocked(ctx context.Context, modem *mmodem.Modem) error {
	c.mu.Lock()
	connection := c.qmapConnections[modem.EquipmentIdentifier]
	delete(c.qmapConnections, modem.EquipmentIdentifier)
	c.mu.Unlock()
	if connection == nil {
		return nil
	}
	var proxyErr error
	if connection.tracked.prefs.ProxyEnabled {
		proxyErr = c.cleanupProxy(ctx, modem.EquipmentIdentifier, connection.tracked.interfaceName)
	}
	cleanupErr := connection.cleanup(ctx, c)
	closeErr := connection.close()
	muxErr := c.removeQMAPMuxes(modem, internetQMAPMuxID)
	return errors.Join(proxyErr, cleanupErr, closeErr, muxErr)
}

func (c *Connector) openQMAPFamilySessions(
	ctx context.Context,
	modem *mmodem.Modem,
	apn string,
	preferences []qcom.WDSIPPreference,
) ([]qmapSessionResult, error) {
	results := make([]qmapSessionResult, len(preferences))
	configs := make([]modemlink.QMAPConfig, len(preferences))
	for i, preference := range preferences {
		results[i].preference = preference
		configs[i] = modemlink.QMAPConfig{APN: apn, IPPreference: preference, MuxID: internetQMAPMuxID}
	}
	opened, err := c.qmapOperationSet().openSessions(ctx, modem, configs)
	if err != nil {
		return nil, err
	}
	if len(opened) != len(results) {
		var closeErr error
		for _, result := range opened {
			if result.Session != nil {
				closeErr = errors.Join(closeErr, result.Session.Close())
			}
		}
		return nil, errors.Join(
			fmt.Errorf("open QMAP family sessions returned %d results, want %d", len(opened), len(results)),
			closeErr,
		)
	}
	for i, result := range opened {
		switch {
		case result.Session != nil && result.Err != nil:
			results[i].err = errors.Join(
				fmt.Errorf("open QMAP family session %d returned both session and error: %w", i, result.Err),
				result.Session.Close(),
			)
		case result.Session == nil && result.Err == nil:
			results[i].err = fmt.Errorf("open QMAP family session %d returned neither session nor error", i)
		default:
			results[i].session = result.Session
			results[i].err = result.Err
		}
	}
	return results, nil
}

func (c *qmapConnection) close() error {
	var result error
	for i := len(c.sessions) - 1; i >= 0; i-- {
		result = errors.Join(result, c.sessions[i].Close())
	}
	c.sessions = nil
	return result
}

func (c *qmapConnection) cleanup(ctx context.Context, connector *Connector) error {
	tracked := c.tracked
	c.tracked = trackedConnection{}
	if strings.TrimSpace(tracked.interfaceName) == "" {
		return nil
	}
	if err := cleanupAppliedWithRouteOps(ctx, connector.persistence, tracked, connector.routeOperationSet()); err != nil {
		return err
	}
	return connector.syncCleanedUpDefaultRouteState(ctx, tracked)
}

func (c *qmapConnection) response() *Connection {
	prefs := c.tracked.prefs
	ipType := qmapActualIPType(c.tracked)
	if ipType == "" {
		ipType = prefs.IPType
	}
	response := &Connection{
		Status: StatusConnected, APN: prefs.APN, IPType: ipType,
		DefaultRoute: prefs.DefaultRoute, ProxyEnabled: prefs.ProxyEnabled,
		AlwaysOn: prefs.AlwaysOn, DNS: slices.Clone(c.tracked.dns),
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

func validateQMAPSessionFamily(preference qcom.WDSIPPreference, info qcom.PDNInfo) error {
	hasIPv4 := len(info.LocalIPv4) > 0
	hasIPv6 := len(info.LocalIPv6) > 0
	switch preference {
	case qcom.WDSIPPreferenceIPv4:
		if !hasIPv4 || hasIPv6 {
			return fmt.Errorf("IPv4 session returned IPv4=%t IPv6=%t", hasIPv4, hasIPv6)
		}
	case qcom.WDSIPPreferenceIPv6:
		if hasIPv4 || !hasIPv6 {
			return fmt.Errorf("IPv6 session returned IPv4=%t IPv6=%t", hasIPv4, hasIPv6)
		}
	default:
		return fmt.Errorf("unsupported QMAP IP preference %d", preference)
	}
	return nil
}

func qmapActualIPType(tracked trackedConnection) string {
	var ipv4, ipv6 bool
	for _, prefix := range tracked.addresses {
		ipv4 = ipv4 || prefix.Addr().Is4()
		ipv6 = ipv6 || prefix.Addr().Is6()
	}
	switch {
	case ipv4 && ipv6:
		return "ipv4v6"
	case ipv4:
		return "ipv4"
	case ipv6:
		return "ipv6"
	default:
		return ""
	}
}

type qmapLinkConfig struct {
	interfaceName string
	networks      []qmapNetwork
	dns           []string
	mtu           uint32
}

func configureQMAPNetwork(ctx context.Context, state connectionStateStore, modemID string, prefs Preferences, config qmapLinkConfig, routeOps defaultRouteOps) (tracked trackedConnection, err error) {
	routeOps = routeOps.withDefaults()
	tracked = trackedConnection{prefs: prefs, dns: slices.Clone(config.dns), routeMetric: routeMetric(prefs.DefaultRoute)}
	if strings.TrimSpace(config.interfaceName) == "" {
		return tracked, errors.New("QMAP interface is required")
	}
	if len(config.networks) == 0 {
		return tracked, errors.New("QMAP network configuration is empty")
	}
	tracked.interfaceName = config.interfaceName
	if err := netlink.DisableIPv6Autoconfiguration(tracked.interfaceName); err != nil {
		return tracked, err
	}
	if err := errors.Join(
		netlink.FlushDefaultRoutes(tracked.interfaceName),
		netlink.FlushGlobalAddresses(tracked.interfaceName),
	); err != nil {
		return tracked, err
	}
	if err := netlink.SetUp(tracked.interfaceName); err != nil {
		return tracked, err
	}
	if config.mtu > 0 {
		if err := netlink.SetMTU(tracked.interfaceName, config.mtu); err != nil {
			return tracked, err
		}
	}
	routes := make([]netlink.DefaultRoute, 0, len(config.networks))
	for _, network := range config.networks {
		route := netlink.DefaultRoute{Interface: tracked.interfaceName, Source: network.prefix.Addr(), Gateway: network.gateway, Metric: tracked.routeMetric}
		if network.prefix.Addr().Is4() {
			route.Family = netlink.FamilyIPv4
		} else {
			route.Family = netlink.FamilyIPv6
		}
		routes = append(routes, route)
	}
	if !prefs.DefaultRoute && len(routes) > 0 {
		current, err := routeOps.defaultRoutes()
		if err != nil {
			return tracked, fmt.Errorf("list default routes: %w", err)
		}
		tracked.routeMetric = secondaryRouteMetricFor(routes, current)
		setRouteMetric(routes, tracked.routeMetric)
	}

	release := false
	defer func() {
		if !release {
			err = errors.Join(err, cleanupAppliedWithRouteOps(context.WithoutCancel(ctx), state, tracked, routeOps))
		}
	}()
	for _, network := range config.networks {
		prefix := network.prefix
		if err := netlink.AddAddress(tracked.interfaceName, prefix); err != nil {
			return tracked, err
		}
		tracked.addresses = append(tracked.addresses, prefix)
	}
	if prefs.DefaultRoute {
		if err := restoreStaleDefaultRouteStatesWithStore(ctx, state, routeStateRestoreTarget{
			interfaceNames: []string{tracked.interfaceName},
		}, routeOps); err != nil {
			return tracked, fmt.Errorf("restore previous default route state: %w", err)
		}
		changes, err := takeoverDefaultRoutesWithStore(ctx, state, modemID, tracked.interfaceName, routes, routeOps)
		tracked.routeChanges = changes
		if err != nil {
			return tracked, fmt.Errorf("take over default route: %w", err)
		}
	}
	for _, route := range routes {
		if err := routeOps.addDefaultRoute(route); err != nil {
			return tracked, fmt.Errorf("add default route: %w", err)
		}
		tracked.routes = append(tracked.routes, route)
	}
	release = true
	return tracked, nil
}

type qmapNetwork struct {
	prefix  netip.Prefix
	gateway netip.Addr
}

func qmapNetworks(info qcom.PDNInfo) ([]qmapNetwork, error) {
	var networks []qmapNetwork
	if len(info.LocalIPv4) > 0 {
		raw := info.LocalIPv4.To4()
		if raw == nil {
			return nil, fmt.Errorf("IPv4 local address is invalid: %v", info.LocalIPv4)
		}
		addr, _ := netip.AddrFromSlice(raw)
		if addr.IsUnspecified() {
			return nil, errors.New("IPv4 local address is unspecified")
		}
		mask := info.IPv4SubnetMask.To4()
		if mask == nil {
			return nil, fmt.Errorf("IPv4 subnet mask is missing or invalid: %v", info.IPv4SubnetMask)
		}
		ones, bits := net.IPMask(mask).Size()
		if bits != net.IPv4len*8 {
			return nil, fmt.Errorf("IPv4 subnet mask is not contiguous: %v", info.IPv4SubnetMask)
		}
		if ones == 0 {
			return nil, errors.New("IPv4 subnet mask has zero prefix length")
		}
		rawGateway := info.IPv4Gateway.To4()
		if rawGateway == nil {
			return nil, fmt.Errorf("IPv4 gateway is missing or not IPv4: %v", info.IPv4Gateway)
		}
		gateway, _ := netip.AddrFromSlice(rawGateway)
		if gateway.IsUnspecified() {
			return nil, errors.New("IPv4 gateway is unspecified")
		}
		networks = append(networks, qmapNetwork{netip.PrefixFrom(addr, ones), gateway})
	}
	if len(info.LocalIPv6) > 0 {
		raw := info.LocalIPv6.To16()
		if raw == nil || info.LocalIPv6.To4() != nil {
			return nil, fmt.Errorf("IPv6 local address is invalid: %v", info.LocalIPv6)
		}
		if info.IPv6PrefixLength == 0 || info.IPv6PrefixLength > 128 {
			return nil, fmt.Errorf("IPv6 prefix length %d is outside range 1-128", info.IPv6PrefixLength)
		}
		rawGateway := info.IPv6Gateway.To16()
		if rawGateway == nil || info.IPv6Gateway.To4() != nil {
			return nil, fmt.Errorf("IPv6 gateway is missing or not IPv6: %v", info.IPv6Gateway)
		}
		addr, _ := netip.AddrFromSlice(raw)
		gateway, _ := netip.AddrFromSlice(rawGateway)
		if addr.IsUnspecified() {
			return nil, errors.New("IPv6 local address is unspecified")
		}
		if gateway.IsUnspecified() {
			return nil, errors.New("IPv6 gateway is unspecified")
		}
		networks = append(networks, qmapNetwork{netip.PrefixFrom(addr, int(info.IPv6PrefixLength)), gateway})
	}
	return networks, nil
}

func combineQMAPSessions(sessions []*modemlink.QMAPSession) (qmapLinkConfig, error) {
	var combined qmapLinkConfig
	for _, session := range sessions {
		if session == nil {
			return qmapLinkConfig{}, errors.New("QMAP session is required")
		}
		interfaceName := strings.TrimSpace(session.InterfaceName)
		if interfaceName == "" {
			return qmapLinkConfig{}, errors.New("QMAP session interface is required")
		}
		if combined.interfaceName == "" {
			combined.interfaceName = interfaceName
		} else if interfaceName != combined.interfaceName {
			return qmapLinkConfig{}, fmt.Errorf("QMAP sessions use different interfaces %s and %s", combined.interfaceName, interfaceName)
		}
		networks, err := qmapNetworks(session.Info)
		if err != nil {
			return qmapLinkConfig{}, fmt.Errorf("validate QMAP network on %s: %w", interfaceName, err)
		}
		if len(networks) == 0 {
			return qmapLinkConfig{}, fmt.Errorf("QMAP network configuration on %s is empty", interfaceName)
		}
		for _, network := range networks {
			if !slices.Contains(combined.networks, network) {
				combined.networks = append(combined.networks, network)
			}
		}
		for _, server := range session.Info.DNS {
			addr, ok := netip.AddrFromSlice(server)
			if !ok {
				continue
			}
			addr = addr.Unmap()
			if addr.IsUnspecified() {
				continue
			}
			value := addr.String()
			if !slices.Contains(combined.dns, value) {
				combined.dns = append(combined.dns, value)
			}
		}
		if mtu := session.Info.MTU; mtu > 0 && (combined.mtu == 0 || mtu < combined.mtu) {
			combined.mtu = mtu
		}
	}
	if len(sessions) == 0 {
		return qmapLinkConfig{}, errors.New("QMAP interface is unavailable")
	}
	if len(combined.networks) == 0 {
		return qmapLinkConfig{}, errors.New("QMAP network configuration is empty")
	}
	return combined, nil
}

func qmapIPPreferences(ipType string) ([]qcom.WDSIPPreference, error) {
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
