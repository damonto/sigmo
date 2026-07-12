package internet

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/netlink"
	"github.com/damonto/wwan-go/qcom"
)

const (
	internetQMAPMuxID = 1
	imsQMAPMuxID      = 2
)

type qmapConnection struct {
	sessions []*mmodem.QMAPSession
	tracked  []trackedConnection
	prefs    Preferences
	dns      []string
}

func (c *Connector) qmapConnection(modem *mmodem.Modem) *qmapConnection {
	if modem == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.qmapConnections[modem.EquipmentIdentifier]
}

func (c *Connector) connectQMAP(ctx context.Context, modem *mmodem.Modem, prefs Preferences) (*Connection, error) {
	if modem == nil {
		return nil, ErrModemRequired
	}
	defer c.lockModem(modem.EquipmentIdentifier)()
	prefs = normalizePreferences(prefs)
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
	connection := &qmapConnection{prefs: prefs}
	for i, preference := range preferences {
		muxID := uint8(internetQMAPMuxID)
		if i > 0 {
			muxID = uint8(imsQMAPMuxID + i)
		}
		session, err := mmodem.OpenQMAPSession(ctx, modem, mmodem.QMAPConfig{
			APN: prefs.APN, IPPreference: preference, MuxID: muxID,
		})
		if err != nil {
			return nil, errors.Join(err, connection.close())
		}
		connection.sessions = append(connection.sessions, session)
	}
	for _, session := range connection.sessions {
		tracked, dns, err := configureQMAPNetwork(prefs, session)
		if err != nil {
			return nil, errors.Join(err, connection.cleanup(ctx, c.persistence), connection.close())
		}
		connection.tracked = append(connection.tracked, tracked)
		connection.dns = append(connection.dns, dns...)
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
	return errors.Join(connection.cleanup(ctx, c.persistence), connection.close())
}

func (c *qmapConnection) close() error {
	var result error
	for i := len(c.sessions) - 1; i >= 0; i-- {
		result = errors.Join(result, c.sessions[i].Close())
	}
	c.sessions = nil
	return result
}

func (c *qmapConnection) cleanup(ctx context.Context, state connectionStateStore) error {
	var result error
	for i := len(c.tracked) - 1; i >= 0; i-- {
		result = errors.Join(result, cleanupApplied(ctx, state, c.tracked[i]))
	}
	c.tracked = nil
	return result
}

func (c *qmapConnection) response() *Connection {
	response := &Connection{
		Status: StatusConnected, APN: c.prefs.APN, IPType: c.prefs.IPType,
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

func configureQMAPNetwork(prefs Preferences, session *mmodem.QMAPSession) (trackedConnection, []string, error) {
	tracked := trackedConnection{prefs: prefs, routeMetric: routeMetric(prefs.DefaultRoute)}
	if session == nil {
		return tracked, nil, errors.New("QMAP session is required")
	}
	tracked.interfaceName = session.InterfaceName
	if err := netlink.SetUp(tracked.interfaceName); err != nil {
		return tracked, nil, err
	}
	var dns []string
	info := session.Info
	if info.MTU > 0 {
		if err := netlink.SetMTU(tracked.interfaceName, info.MTU); err != nil {
			return tracked, dns, err
		}
	}
	for _, network := range qmapNetworks(info) {
		prefix, gateway := network.prefix, network.gateway
		if err := netlink.AddAddress(tracked.interfaceName, prefix); err != nil {
			return tracked, dns, err
		}
		tracked.addresses = append(tracked.addresses, prefix)
		route := netlink.DefaultRoute{Interface: tracked.interfaceName, Source: prefix.Addr(), Gateway: gateway, Metric: tracked.routeMetric}
		if prefix.Addr().Is4() {
			route.Family = netlink.FamilyIPv4
		} else {
			route.Family = netlink.FamilyIPv6
		}
		if err := netlink.AddDefaultRoute(route); err != nil {
			return tracked, dns, err
		}
		tracked.routes = append(tracked.routes, route)
	}
	for _, ip := range info.DNS {
		if value := strings.TrimSpace(ip.String()); value != "" {
			dns = append(dns, value)
		}
	}
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
		return []qcom.WDSIPPreference{qcom.WDSIPPreferenceUnspecified}, nil
	case "ipv4":
		return []qcom.WDSIPPreference{qcom.WDSIPPreferenceIPv4}, nil
	case "ipv6":
		return []qcom.WDSIPPreference{qcom.WDSIPPreferenceIPv6}, nil
	default:
		return nil, fmt.Errorf("%w: %s", mmodem.ErrUnsupportedBearerIPType, ipType)
	}
}
