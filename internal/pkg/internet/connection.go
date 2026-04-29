package internet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/godbus/dbus/v5"
	"golang.org/x/sys/unix"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/netlink"
)

const (
	defaultRouteMetric   = 10
	secondaryRouteMetric = 9000
	StatusConnected      = "connected"
	StatusDisconnected   = "disconnected"
	ipInfoURL            = "https://ipinfo.io/json"
	dnsServer            = "1.1.1.1:53"
	ipInfoTimeout        = 4 * time.Second
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

type IPInfo struct {
	IP           string
	Country      string
	Organization string
}

type recoveredRoute struct {
	Found        bool
	Metric       int
	DefaultRoute bool
}

type Connector struct {
	mu          sync.Mutex
	connections map[string]trackedConnection
	preferences map[string]Preferences
}

type trackedConnection struct {
	bearerPath    dbus.ObjectPath
	interfaceName string
	prefs         Preferences
	routeMetric   int
	addresses     []netip.Prefix
	routes        []netlink.DefaultRoute
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
	if tracked, ok := c.connections[modem.EquipmentIdentifier]; ok {
		bearer, err := modem.Bearer(tracked.bearerPath)
		if err == nil {
			connected, err := bearer.Connected()
			if err == nil {
				if !connected {
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
		delete(c.connections, modem.EquipmentIdentifier)
		prefs = tracked.prefs
	}

	current, err := currentBearer(modem)
	if err != nil {
		return nil, err
	}
	if current.bearer == nil {
		return disconnectedConnection(prefs), nil
	}
	if !current.connected {
		prefs = bearerPreferences(current.bearer, prefs)
		c.preferences[modem.EquipmentIdentifier] = prefs
		return disconnectedConnection(prefs), nil
	}
	bearer := current.bearer
	tracked, metric, ok, err := recoverTrackedConnection(bearer, prefs)
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

	tracked, err := configureBearer(bearer, prefs)
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

func (c *Connector) Public(ctx context.Context, modem *mmodem.Modem) (IPInfo, error) {
	connection, err := c.Current(modem)
	if err != nil {
		return IPInfo{}, err
	}
	if connection.Status != StatusConnected {
		return IPInfo{}, nil
	}
	interfaceName := strings.TrimSpace(connection.InterfaceName)
	if interfaceName == "" {
		return IPInfo{}, nil
	}
	return fetchIPInfo(ctx, interfaceName)
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
		delete(c.connections, modem.EquipmentIdentifier)
		if err != nil {
			return fmt.Errorf("disconnect bearer: %w", err)
		}
		return nil
	}

	bearer, err := connectedBearer(modem)
	if err != nil {
		return err
	}
	if bearer == nil {
		return nil
	}
	prefs := recoverPreferences(bearer, c.preference(modem.EquipmentIdentifier))
	err = cleanupBearer(bearer, prefs)
	err = errors.Join(err, bearer.Disconnect())
	if err != nil {
		return fmt.Errorf("disconnect bearer: %w", err)
	}
	return nil
}

func (c *Connector) cleanupTracked(tracked trackedConnection) error {
	var err error
	for i := len(tracked.routes) - 1; i >= 0; i-- {
		err = errors.Join(err, netlink.DeleteDefaultRoute(tracked.routes[i]))
	}
	for i := len(tracked.addresses) - 1; i >= 0; i-- {
		err = errors.Join(err, netlink.DeleteAddress(tracked.interfaceName, tracked.addresses[i]))
	}
	return err
}

func (c *Connector) preference(modemID string) Preferences {
	if prefs, ok := c.preferences[modemID]; ok {
		prefs.APN = strings.TrimSpace(prefs.APN)
		return prefs
	}
	return Preferences{}
}

func configureBearer(bearer *mmodem.Bearer, prefs Preferences) (trackedConnection, error) {
	var tracked trackedConnection

	interfaceName, err := bearer.Interface()
	if err != nil {
		return tracked, fmt.Errorf("read bearer interface: %w", err)
	}
	if strings.TrimSpace(interfaceName) == "" {
		return tracked, errors.New("bearer interface is empty")
	}
	tracked.interfaceName = interfaceName

	ip4, err := bearer.IP4Config()
	if err != nil {
		return tracked, fmt.Errorf("read ipv4 config: %w", err)
	}
	ip6, err := bearer.IP6Config()
	if err != nil {
		return tracked, fmt.Errorf("read ipv6 config: %w", err)
	}

	addresses, routes, err := addressesAndRoutes(interfaceName, prefs, ip4, ip6)
	if err != nil {
		return tracked, err
	}

	if err := netlink.SetUp(interfaceName); err != nil {
		return tracked, err
	}
	if err := netlink.SetMTU(interfaceName, max(ip4.MTU, ip6.MTU)); err != nil {
		return tracked, err
	}

	cleanup := func() {
		// Best effort: the original netlink error is returned to the caller.
		_ = cleanupApplied(tracked)
	}
	for _, address := range addresses {
		if err := netlink.AddAddress(interfaceName, address); err != nil {
			cleanup()
			return tracked, fmt.Errorf("add address: %w", err)
		}
		tracked.addresses = append(tracked.addresses, address)
	}
	for _, route := range routes {
		if err := netlink.AddDefaultRoute(route); err != nil {
			cleanup()
			return tracked, fmt.Errorf("add default route: %w", err)
		}
		tracked.routes = append(tracked.routes, route)
	}

	return tracked, nil
}

func cleanupBearer(bearer *mmodem.Bearer, prefs Preferences) error {
	interfaceName, err := bearer.Interface()
	if err != nil {
		return fmt.Errorf("read bearer interface: %w", err)
	}
	ip4, err := bearer.IP4Config()
	if err != nil {
		return fmt.Errorf("read ipv4 config: %w", err)
	}
	ip6, err := bearer.IP6Config()
	if err != nil {
		return fmt.Errorf("read ipv6 config: %w", err)
	}
	state := routeStateForInterface(interfaceName)
	includeRoutes := state.Found
	metric := routeMetric(prefs.DefaultRoute)
	if state.Found {
		prefs.DefaultRoute = state.DefaultRoute
		metric = state.Metric
	}
	addresses, routes, err := addressesAndRoutesWithMetric(interfaceName, metric, includeRoutes, ip4, ip6)
	if err != nil {
		if errors.Is(err, ErrUnsupportedIPMethod) {
			return nil
		}
		return err
	}
	return cleanupApplied(trackedConnection{
		interfaceName: interfaceName,
		addresses:     addresses,
		routes:        routes,
	})
}

func cleanupApplied(tracked trackedConnection) error {
	var err error
	for i := len(tracked.routes) - 1; i >= 0; i-- {
		err = errors.Join(err, netlink.DeleteDefaultRoute(tracked.routes[i]))
	}
	for i := len(tracked.addresses) - 1; i >= 0; i-- {
		err = errors.Join(err, netlink.DeleteAddress(tracked.interfaceName, tracked.addresses[i]))
	}
	return err
}

func addressesAndRoutes(interfaceName string, prefs Preferences, ip4, ip6 mmodem.BearerIPConfig) ([]netip.Prefix, []netlink.DefaultRoute, error) {
	return addressesAndRoutesWithMetric(interfaceName, routeMetric(prefs.DefaultRoute), true, ip4, ip6)
}

func addressesAndRoutesWithMetric(interfaceName string, metric int, includeRoutes bool, ip4, ip6 mmodem.BearerIPConfig) ([]netip.Prefix, []netlink.DefaultRoute, error) {
	var (
		addresses []netip.Prefix
		routes    []netlink.DefaultRoute
	)

	if address, ok, err := prefixFromIPConfig(ip4, netlink.FamilyIPv4); err != nil {
		return nil, nil, err
	} else if ok {
		addresses = append(addresses, address)
		if includeRoutes {
			routes = append(routes, netlink.DefaultRoute{
				Interface: interfaceName,
				Family:    netlink.FamilyIPv4,
				Gateway:   addrFromString(ip4.Gateway),
				Metric:    metric,
			})
		}
	}

	if address, ok, err := prefixFromIPConfig(ip6, netlink.FamilyIPv6); err != nil {
		return nil, nil, err
	} else if ok {
		addresses = append(addresses, address)
		if includeRoutes {
			routes = append(routes, netlink.DefaultRoute{
				Interface: interfaceName,
				Family:    netlink.FamilyIPv6,
				Gateway:   addrFromString(ip6.Gateway),
				Metric:    metric,
			})
		}
	}

	if len(addresses) == 0 {
		return nil, nil, ErrUnsupportedIPMethod
	}
	return addresses, routes, nil
}

func prefixFromIPConfig(cfg mmodem.BearerIPConfig, family int) (netip.Prefix, bool, error) {
	if !cfg.StaticAddress() {
		return netip.Prefix{}, false, nil
	}
	addr, err := netip.ParseAddr(cfg.Address)
	if err != nil {
		return netip.Prefix{}, false, fmt.Errorf("parse bearer address: %w", err)
	}
	if family == netlink.FamilyIPv4 && !addr.Is4() {
		return netip.Prefix{}, false, errors.New("ipv4 bearer address is not ipv4")
	}
	if family == netlink.FamilyIPv6 && !addr.Is6() {
		return netip.Prefix{}, false, errors.New("ipv6 bearer address is not ipv6")
	}
	bits := int(cfg.Prefix)
	if bits == 0 {
		if addr.Is4() {
			bits = 32
		} else {
			bits = 128
		}
	}
	prefix := netip.PrefixFrom(addr, bits)
	if !prefix.IsValid() {
		return netip.Prefix{}, false, errors.New("bearer address prefix is invalid")
	}
	return prefix, true, nil
}

func connectionFromBearer(bearer *mmodem.Bearer, prefs Preferences, metric int) (*Connection, error) {
	prefs = bearerPreferences(bearer, prefs)

	connected, err := bearer.Connected()
	if err != nil {
		return nil, fmt.Errorf("read bearer state: %w", err)
	}
	if !connected {
		return disconnectedConnection(prefs), nil
	}

	interfaceName, err := bearer.Interface()
	if err != nil {
		return nil, fmt.Errorf("read bearer interface: %w", err)
	}
	ip4, err := bearer.IP4Config()
	if err != nil {
		return nil, fmt.Errorf("read ipv4 config: %w", err)
	}
	ip6, err := bearer.IP6Config()
	if err != nil {
		return nil, fmt.Errorf("read ipv6 config: %w", err)
	}
	stats, err := bearer.Stats()
	if err != nil {
		// Some devices omit Stats while the bearer is otherwise usable.
		stats = mmodem.BearerStats{}
	}

	ipv4Addresses, ipv6Addresses, err := connectionAddressStrings(ip4, ip6)
	if err != nil {
		return nil, err
	}
	if len(ipv4Addresses) == 0 && len(ipv6Addresses) == 0 {
		return nil, ErrUnsupportedIPMethod
	}

	return &Connection{
		Status:          StatusConnected,
		APN:             prefs.APN,
		DefaultRoute:    prefs.DefaultRoute,
		InterfaceName:   interfaceName,
		Bearer:          string(bearer.Path()),
		IPv4Addresses:   ipv4Addresses,
		IPv6Addresses:   ipv6Addresses,
		DNS:             mergeDNS(ip4.DNS, ip6.DNS),
		DurationSeconds: stats.Duration,
		TXBytes:         stats.TXBytes,
		RXBytes:         stats.RXBytes,
		RouteMetric:     metric,
	}, nil
}

type ipInfoResponse struct {
	IP      string `json:"ip"`
	Country string `json:"country"`
	Org     string `json:"org"`
}

func fetchIPInfo(ctx context.Context, interfaceName string) (IPInfo, error) {
	interfaceName = strings.TrimSpace(interfaceName)
	if interfaceName == "" {
		return IPInfo{}, errors.New("interface name is empty")
	}

	return requestIPInfo(ctx, interfaceName)
}

func requestIPInfo(ctx context.Context, interfaceName string) (IPInfo, error) {
	client := &http.Client{
		Timeout:   ipInfoTimeout,
		Transport: boundTransport(interfaceName),
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ipInfoURL, nil)
	if err != nil {
		return IPInfo{}, fmt.Errorf("create ipinfo request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return IPInfo{}, fmt.Errorf("request ipinfo: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return IPInfo{}, fmt.Errorf("ipinfo status: %d", resp.StatusCode)
	}

	var payload ipInfoResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&payload); err != nil {
		return IPInfo{}, fmt.Errorf("decode ipinfo response: %w", err)
	}
	country := strings.ToUpper(strings.TrimSpace(payload.Country))
	return IPInfo{
		IP:           strings.TrimSpace(payload.IP),
		Country:      country,
		Organization: strings.TrimSpace(payload.Org),
	}, nil
}

func boundResolver(interfaceName string) *net.Resolver {
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			return rawBoundDialer(interfaceName).DialContext(ctx, dnsNetwork(network), dnsServer)
		},
	}
}

func dnsNetwork(network string) string {
	if strings.HasPrefix(network, "tcp") {
		return "tcp4"
	}
	return "udp4"
}

func boundTransport(interfaceName string) *http.Transport {
	return &http.Transport{
		Proxy:       nil,
		DialContext: boundDialer(interfaceName).DialContext,
	}
}

func boundDialer(interfaceName string) *net.Dialer {
	dialer := rawBoundDialer(interfaceName)
	dialer.Resolver = boundResolver(interfaceName)
	return dialer
}

func rawBoundDialer(interfaceName string) *net.Dialer {
	dialer := &net.Dialer{
		Timeout: ipInfoTimeout,
		Control: func(network, address string, connection syscall.RawConn) error {
			var controlErr error
			if err := connection.Control(func(fd uintptr) {
				controlErr = unix.SetsockoptString(int(fd), unix.SOL_SOCKET, unix.SO_BINDTODEVICE, interfaceName)
			}); err != nil {
				return err
			}
			return controlErr
		},
	}
	return dialer
}

type bearerState struct {
	bearer    *mmodem.Bearer
	connected bool
}

func currentBearer(modem *mmodem.Modem) (bearerState, error) {
	bearers, err := modem.Bearers()
	if err != nil {
		return bearerState{}, fmt.Errorf("list bearers: %w", err)
	}
	var fallback *mmodem.Bearer
	for _, bearer := range bearers {
		connected, err := bearer.Connected()
		if err != nil {
			return bearerState{}, fmt.Errorf("read bearer state: %w", err)
		}
		if connected {
			return bearerState{bearer: bearer, connected: true}, nil
		}
		if fallback != nil {
			continue
		}
		apn, err := bearer.APN()
		if err == nil && strings.TrimSpace(apn) != "" {
			fallback = bearer
		}
	}
	return bearerState{bearer: fallback}, nil
}

func connectedBearer(modem *mmodem.Modem) (*mmodem.Bearer, error) {
	current, err := currentBearer(modem)
	if err != nil {
		return nil, err
	}
	if !current.connected {
		return nil, nil
	}
	return current.bearer, nil
}

func apnFromBearers(modem *mmodem.Modem) (string, error) {
	current, err := currentBearer(modem)
	if err != nil {
		return "", err
	}
	if current.bearer == nil {
		return "", nil
	}
	apn, err := current.bearer.APN()
	if err != nil {
		return "", nil
	}
	return strings.TrimSpace(apn), nil
}

func disconnectedConnection(prefs Preferences) *Connection {
	return &Connection{
		Status:          StatusDisconnected,
		APN:             prefs.APN,
		DefaultRoute:    prefs.DefaultRoute,
		IPv4Addresses:   []string{},
		IPv6Addresses:   []string{},
		DNS:             []string{},
		DurationSeconds: 0,
		TXBytes:         0,
		RXBytes:         0,
		RouteMetric:     0,
	}
}

func bearerPreferences(bearer *mmodem.Bearer, fallback Preferences) Preferences {
	fallback.APN = strings.TrimSpace(fallback.APN)
	apn, err := bearer.APN()
	if err != nil || strings.TrimSpace(apn) == "" {
		return fallback
	}
	fallback.APN = strings.TrimSpace(apn)
	return fallback
}

func recoverTrackedConnection(bearer *mmodem.Bearer, fallback Preferences) (trackedConnection, int, bool, error) {
	prefs := recoverPreferences(bearer, fallback)
	metric := 0

	interfaceName, err := bearer.Interface()
	if err != nil {
		return trackedConnection{}, 0, false, fmt.Errorf("read bearer interface: %w", err)
	}
	if strings.TrimSpace(interfaceName) == "" {
		return trackedConnection{}, 0, false, nil
	}

	ip4, err := bearer.IP4Config()
	if err != nil {
		return trackedConnection{}, 0, false, fmt.Errorf("read ipv4 config: %w", err)
	}
	ip6, err := bearer.IP6Config()
	if err != nil {
		return trackedConnection{}, 0, false, fmt.Errorf("read ipv6 config: %w", err)
	}

	state := routeStateForInterface(interfaceName)
	includeRoutes := state.Found
	if state.Found {
		metric = state.Metric
		prefs.DefaultRoute = state.DefaultRoute
	} else {
		metric = 0
	}

	addresses, routes, err := addressesAndRoutesWithMetric(interfaceName, metric, includeRoutes, ip4, ip6)
	if err != nil {
		if errors.Is(err, ErrUnsupportedIPMethod) {
			return trackedConnection{}, metric, false, nil
		}
		return trackedConnection{}, 0, false, err
	}

	return trackedConnection{
		bearerPath:    bearer.Path(),
		interfaceName: interfaceName,
		prefs:         prefs,
		routeMetric:   metric,
		addresses:     addresses,
		routes:        routes,
	}, metric, true, nil
}

func recoverPreferences(bearer *mmodem.Bearer, fallback Preferences) Preferences {
	prefs := bearerPreferences(bearer, fallback)
	interfaceName, err := bearer.Interface()
	if err != nil || strings.TrimSpace(interfaceName) == "" {
		return prefs
	}
	state := routeStateForInterface(interfaceName)
	if state.Found {
		prefs.DefaultRoute = state.DefaultRoute
	}
	return prefs
}

func routeStateForInterface(interfaceName string) recoveredRoute {
	routes, err := netlink.DefaultRoutes()
	if err != nil {
		return recoveredRoute{}
	}
	return routeStateFromRoutes(routes, interfaceName)
}

func routeStateFromRoutes(routes []netlink.DefaultRoute, interfaceName string) recoveredRoute {
	var state recoveredRoute
	for _, route := range routes {
		if route.Interface != interfaceName {
			continue
		}
		if !state.Found || route.Metric < state.Metric {
			state.Found = true
			state.Metric = route.Metric
		}
	}
	if !state.Found {
		return state
	}
	state.DefaultRoute = state.Metric <= defaultRouteMetric
	return state
}

func connectionAddressStrings(ip4, ip6 mmodem.BearerIPConfig) ([]string, []string, error) {
	ipv4Addresses, err := addressStrings(ip4, netlink.FamilyIPv4)
	if err != nil {
		return nil, nil, err
	}
	ipv6Addresses, err := addressStrings(ip6, netlink.FamilyIPv6)
	if err != nil {
		return nil, nil, err
	}
	return ipv4Addresses, ipv6Addresses, nil
}

func addressStrings(cfg mmodem.BearerIPConfig, family int) ([]string, error) {
	prefix, ok, err := prefixFromIPConfig(cfg, family)
	if err != nil || !ok {
		return []string{}, err
	}
	return []string{prefix.String()}, nil
}

func mergeDNS(groups ...[]string) []string {
	var result []string
	for _, group := range groups {
		for _, dns := range group {
			dns = strings.TrimSpace(dns)
			if dns == "" || slices.Contains(result, dns) {
				continue
			}
			result = append(result, dns)
		}
	}
	return result
}

func addrFromString(value string) netip.Addr {
	addr, err := netip.ParseAddr(strings.TrimSpace(value))
	if err != nil {
		return netip.Addr{}
	}
	return addr
}

func routeMetric(defaultRoute bool) int {
	if defaultRoute {
		return defaultRouteMetric
	}
	return secondaryRouteMetric
}
