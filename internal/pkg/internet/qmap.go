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

const (
	internetQMAPMuxID = 1
	imsQMAPMuxID      = 2
	ipv6QMAPMuxID     = 3
)

type qmapConnection struct {
	sessions []*mmodem.QMAPSession
	tracked  []trackedConnection
	muxIDs   []uint8
	prefs    Preferences
	dns      []string
}

var (
	openInternetQMAPSession      = mmodem.OpenQMAPSession
	configureInternetQMAPNetwork = configureQMAPNetwork
	removeInternetQMAPMuxes      = mmodem.RemoveQMAPMuxes
)

func (c *Connector) qmapConnection(modem *mmodem.Modem) *qmapConnection {
	if modem == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.qmapConnections[modem.EquipmentIdentifier]
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
// ModemManager bearer and QMAP. Firmware that cannot leave QMAP in place is
// reset, then the modem-added path finishes restoring the normal bearer.
func (c *Connector) SetQMAPEnabled(ctx context.Context, modem *mmodem.Modem, enabled bool) error {
	if modem == nil {
		return ErrModemRequired
	}
	if modem.PrimaryPortType() != mmodem.ModemPortTypeQmi {
		return nil
	}
	modemID := modem.EquipmentIdentifier
	defer c.lockModem(modemID)()
	if !enabled {
		if prefs, ok := c.qmapPendingNormalFor(modemID); ok {
			if err := removeManagedQMAPMuxes(modem); err != nil {
				return err
			}
			if err := mmodem.RestoreNonQMAPDataFormat(ctx, modem); err != nil {
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
		if err := removeManagedQMAPMuxes(modem); err != nil {
			return err
		}
		if err := mmodem.RestoreNonQMAPDataFormat(ctx, modem); err != nil {
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
	prefs := connection.prefs
	if err := c.disconnectQMAPLocked(ctx, modem); err != nil {
		return fmt.Errorf("disconnect QMAP Internet before restoring bearer: %w", err)
	}
	if err := removeManagedQMAPMuxes(modem); err != nil {
		c.setQMAPEnabled(modemID, true)
		_, restoreErr := c.connectQMAPLocked(ctx, modem, prefs)
		return errors.Join(fmt.Errorf("remove QMAP mux interfaces: %w", err), restoreErr)
	}
	if err := mmodem.RestoreNonQMAPDataFormat(ctx, modem); err != nil {
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

func removeManagedQMAPMuxes(modem *mmodem.Modem) error {
	return removeInternetQMAPMuxes(modem, internetQMAPMuxID, imsQMAPMuxID, ipv6QMAPMuxID)
}

func (c *Connector) cleanupStaleQMAPInternet(ctx context.Context, modem *mmodem.Modem) error {
	modemID := ""
	if modem != nil {
		modemID = modem.EquipmentIdentifier
	}
	routeErr := restoreStaleDefaultRouteStatesWithStore(ctx, c.persistence, routeStateRestoreTarget{
		modemID: modemID,
	}, netlinkDefaultRouteOps)
	muxErr := removeInternetQMAPMuxes(modem, internetQMAPMuxID, ipv6QMAPMuxID)
	return errors.Join(routeErr, muxErr)
}

func (c *Connector) qmapMigrationPreferences(ctx context.Context, modem internetModem, bearer *mmodem.Bearer) Preferences {
	if tracked, ok := c.connection(modem.id()); ok {
		return tracked.prefs
	}
	return recoverPreferences(ctx, bearer, c.preferenceWithAlwaysOn(ctx, modem))
}

func (c *Connector) connectQMAP(ctx context.Context, modem *mmodem.Modem, prefs Preferences) (*Connection, error) {
	if modem == nil {
		return nil, ErrModemRequired
	}
	defer c.lockModem(modem.EquipmentIdentifier)()
	return c.connectQMAPLocked(ctx, modem, prefs)
}

func (c *Connector) connectQMAPLocked(ctx context.Context, modem *mmodem.Modem, prefs Preferences) (*Connection, error) {
	prefs = normalizePreferences(prefs)
	if prefs.APNUsername != "" || prefs.APNPassword != "" || prefs.APNAuth != "" {
		return nil, errors.New("QMAP Internet authentication is not supported")
	}
	if err := c.disconnectQMAPLocked(ctx, modem); err != nil {
		return nil, err
	}
	legs, err := qmapDataLegs(prefs.IPType)
	if err != nil {
		return nil, err
	}
	connection := &qmapConnection{prefs: prefs}
	var legErrors error
	for _, leg := range legs {
		session, err := openInternetQMAPSession(ctx, modem, mmodem.QMAPConfig{
			APN: prefs.APN, IPPreference: leg.preference, MuxID: leg.muxID,
		})
		if err != nil {
			legErrors = errors.Join(legErrors,
				fmt.Errorf("open QMAP mux %d for IP preference %d: %w", leg.muxID, leg.preference, err),
				removeInternetQMAPMuxes(modem, leg.muxID),
			)
			continue
		}
		tracked, dns, err := configureInternetQMAPNetwork(ctx, c.persistence, modem.EquipmentIdentifier, prefs, session)
		if err != nil {
			legErrors = errors.Join(legErrors,
				fmt.Errorf("configure QMAP mux %d for IP preference %d: %w", leg.muxID, leg.preference, err),
				session.Close(),
				removeInternetQMAPMuxes(modem, leg.muxID),
			)
			continue
		}
		connection.sessions = append(connection.sessions, session)
		connection.tracked = append(connection.tracked, tracked)
		connection.muxIDs = append(connection.muxIDs, leg.muxID)
		if err := c.syncDefaultRouteTakeover(ctx, modem.EquipmentIdentifier, &connection.tracked[len(connection.tracked)-1]); err != nil {
			return nil, errors.Join(
				fmt.Errorf("sync QMAP default route takeover: %w", err),
				connection.cleanup(ctx, c),
				connection.close(),
				removeInternetQMAPMuxes(modem, connection.muxIDs...),
			)
		}
		for _, address := range dns {
			if !slices.Contains(connection.dns, address) {
				connection.dns = append(connection.dns, address)
			}
		}
	}
	if len(connection.sessions) == 0 {
		if legErrors == nil {
			legErrors = errors.New("QMAP Internet has no available data leg")
		}
		return nil, legErrors
	}
	if legErrors != nil {
		slog.Warn("QMAP Internet connected with unavailable data leg", "imei", modem.EquipmentIdentifier, "error", legErrors)
	}
	c.mu.Lock()
	c.qmapConnections[modem.EquipmentIdentifier] = connection
	c.preferences[modem.EquipmentIdentifier] = prefs
	c.mu.Unlock()
	return connection.response(), nil
}

func (c *Connector) disconnectQMAP(ctx context.Context, modem *mmodem.Modem) error {
	if modem == nil {
		return ErrModemRequired
	}
	defer c.lockModem(modem.EquipmentIdentifier)()
	return c.disconnectQMAPLocked(ctx, modem)
}

func (c *Connector) disconnectQMAPLocked(ctx context.Context, modem *mmodem.Modem) error {
	c.mu.Lock()
	connection := c.qmapConnections[modem.EquipmentIdentifier]
	delete(c.qmapConnections, modem.EquipmentIdentifier)
	c.mu.Unlock()
	if connection == nil {
		return nil
	}
	return errors.Join(connection.cleanup(ctx, c), connection.close())
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
	var result error
	for i := len(c.tracked) - 1; i >= 0; i-- {
		tracked := c.tracked[i]
		err := cleanupApplied(ctx, connector.persistence, tracked)
		if err == nil {
			err = connector.syncCleanedUpDefaultRouteState(ctx, tracked)
		}
		result = errors.Join(result, err)
	}
	c.tracked = nil
	return result
}

func (c *qmapConnection) response() *Connection {
	ipType := qmapActualIPType(c.tracked)
	if ipType == "" {
		ipType = c.prefs.IPType
	}
	response := &Connection{
		Status: StatusConnected, APN: c.prefs.APN, IPType: ipType,
		DefaultRoute: c.prefs.DefaultRoute, ProxyEnabled: c.prefs.ProxyEnabled,
		AlwaysOn: c.prefs.AlwaysOn, DNS: append([]string(nil), c.dns...),
	}
	for _, tracked := range c.tracked {
		if response.InterfaceName == "" {
			response.InterfaceName = tracked.interfaceName
			response.RouteMetric = tracked.routeMetric
		}
		for _, prefix := range tracked.addresses {
			if prefix.Addr().Is4() {
				response.IPv4Addresses = append(response.IPv4Addresses, prefix.String())
			} else {
				response.IPv6Addresses = append(response.IPv6Addresses, prefix.String())
			}
		}
	}
	return response
}

func qmapActualIPType(tracked []trackedConnection) string {
	var ipv4, ipv6 bool
	for _, connection := range tracked {
		for _, prefix := range connection.addresses {
			ipv4 = ipv4 || prefix.Addr().Is4()
			ipv6 = ipv6 || prefix.Addr().Is6()
		}
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

func configureQMAPNetwork(ctx context.Context, state connectionStateStore, modemID string, prefs Preferences, session *mmodem.QMAPSession) (tracked trackedConnection, dns []string, err error) {
	tracked = trackedConnection{prefs: prefs, routeMetric: routeMetric(prefs.DefaultRoute)}
	if session == nil {
		return tracked, nil, errors.New("QMAP session is required")
	}
	tracked.interfaceName = session.InterfaceName
	if err := netlink.DisableIPv6Autoconfiguration(tracked.interfaceName); err != nil {
		return tracked, nil, err
	}
	if err := errors.Join(
		netlink.FlushDefaultRoutes(tracked.interfaceName),
		netlink.FlushGlobalAddresses(tracked.interfaceName),
	); err != nil {
		return tracked, nil, err
	}
	if err := netlink.SetUp(tracked.interfaceName); err != nil {
		return tracked, nil, err
	}
	info := session.Info
	if info.MTU > 0 {
		if err := netlink.SetMTU(tracked.interfaceName, info.MTU); err != nil {
			return tracked, dns, err
		}
	}
	networks := qmapNetworks(info)
	routes := make([]netlink.DefaultRoute, 0, len(networks))
	for _, network := range networks {
		route := netlink.DefaultRoute{Interface: tracked.interfaceName, Source: network.prefix.Addr(), Gateway: network.gateway, Metric: tracked.routeMetric}
		if network.prefix.Addr().Is4() {
			route.Family = netlink.FamilyIPv4
		} else {
			route.Family = netlink.FamilyIPv6
		}
		routes = append(routes, route)
	}
	if !prefs.DefaultRoute && len(routes) > 0 {
		current, err := netlink.DefaultRoutes()
		if err != nil {
			return tracked, dns, fmt.Errorf("list default routes: %w", err)
		}
		tracked.routeMetric = secondaryRouteMetricFor(routes, current)
		setRouteMetric(routes, tracked.routeMetric)
	}

	release := false
	defer func() {
		if !release {
			err = errors.Join(err, cleanupApplied(context.WithoutCancel(ctx), state, tracked))
		}
	}()
	for _, network := range networks {
		prefix := network.prefix
		if err := netlink.AddAddress(tracked.interfaceName, prefix); err != nil {
			return tracked, dns, err
		}
		tracked.addresses = append(tracked.addresses, prefix)
	}
	if prefs.DefaultRoute {
		if err := restoreStaleDefaultRouteStatesWithStore(ctx, state, routeStateRestoreTarget{
			interfaceNames: []string{tracked.interfaceName},
		}, netlinkDefaultRouteOps); err != nil {
			return tracked, dns, fmt.Errorf("restore previous default route state: %w", err)
		}
		changes, err := takeoverDefaultRoutesWithStore(ctx, state, modemID, tracked.interfaceName, routes, netlinkDefaultRouteOps)
		tracked.routeChanges = changes
		if err != nil {
			return tracked, dns, fmt.Errorf("take over default route: %w", err)
		}
	}
	for _, route := range routes {
		if err := netlink.AddDefaultRoute(route); err != nil {
			return tracked, dns, fmt.Errorf("add default route: %w", err)
		}
		tracked.routes = append(tracked.routes, route)
	}
	for _, ip := range info.DNS {
		if value := strings.TrimSpace(ip.String()); value != "" {
			dns = append(dns, value)
		}
	}
	release = true
	return tracked, dns, nil
}

type qmapNetwork struct {
	prefix  netip.Prefix
	gateway netip.Addr
}

func qmapNetworks(info qcom.PDNInfo) []qmapNetwork {
	var networks []qmapNetwork
	if raw := info.LocalIPv4.To4(); raw != nil {
		addr, _ := netip.AddrFromSlice(raw)
		ones, _ := net.IPMask(info.IPv4SubnetMask.To4()).Size()
		gateway, _ := netip.AddrFromSlice(info.IPv4Gateway.To4())
		networks = append(networks, qmapNetwork{netip.PrefixFrom(addr, ones), gateway})
	}
	if raw := info.LocalIPv6.To16(); raw != nil {
		addr, _ := netip.AddrFromSlice(raw)
		gateway, _ := netip.AddrFromSlice(info.IPv6Gateway.To16())
		networks = append(networks, qmapNetwork{netip.PrefixFrom(addr, int(info.IPv6PrefixLength)), gateway})
	}
	return networks
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

type qmapDataLeg struct {
	preference qcom.WDSIPPreference
	muxID      uint8
}

func qmapDataLegs(ipType string) ([]qmapDataLeg, error) {
	preferences, err := qmapIPPreferences(ipType)
	if err != nil {
		return nil, err
	}
	legs := make([]qmapDataLeg, 0, len(preferences))
	for i, preference := range preferences {
		muxID := uint8(internetQMAPMuxID)
		if i == 1 {
			muxID = ipv6QMAPMuxID
		}
		legs = append(legs, qmapDataLeg{preference: preference, muxID: muxID})
	}
	return legs, nil
}
