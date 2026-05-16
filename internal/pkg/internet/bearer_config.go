package internet

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"slices"
	"strings"

	"github.com/godbus/dbus/v5"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/netlink"
)

type recoveredRoute struct {
	Found        bool
	Metric       int
	DefaultRoute bool
}

type trackedConnection struct {
	bearerPath    dbus.ObjectPath
	interfaceName string
	prefs         Preferences
	routeMetric   int
	addresses     []netip.Prefix
	routes        []netlink.DefaultRoute
	routeChanges  []defaultRouteChange
}

func configureBearer(ctx context.Context, routePath string, modemID string, bearer *mmodem.Bearer, prefs Preferences) (trackedConnection, error) {
	var tracked trackedConnection

	interfaceName, err := bearer.Interface(ctx)
	if err != nil {
		return tracked, fmt.Errorf("read bearer interface: %w", err)
	}
	if strings.TrimSpace(interfaceName) == "" {
		return tracked, errors.New("bearer interface is empty")
	}
	tracked.interfaceName = interfaceName

	ip4, err := bearer.IP4Config(ctx)
	if err != nil {
		return tracked, fmt.Errorf("read ipv4 config: %w", err)
	}
	ip6, err := bearer.IP6Config(ctx)
	if err != nil {
		return tracked, fmt.Errorf("read ipv6 config: %w", err)
	}

	addresses, routes, err := addressesAndRoutes(interfaceName, prefs, ip4, ip6)
	if err != nil {
		return tracked, err
	}
	tracked.routeMetric = routeMetric(prefs.DefaultRoute)
	if len(routes) > 0 {
		tracked.routeMetric = routes[0].Metric
	}
	if !prefs.DefaultRoute && len(routes) > 0 {
		current, err := netlink.DefaultRoutes()
		if err != nil {
			return tracked, fmt.Errorf("list default routes: %w", err)
		}
		tracked.routeMetric = secondaryRouteMetricFor(routes, current)
		setRouteMetric(routes, tracked.routeMetric)
	}

	if err := netlink.SetUp(interfaceName); err != nil {
		return tracked, err
	}
	if err := netlink.SetMTU(interfaceName, max(ip4.MTU, ip6.MTU)); err != nil {
		return tracked, err
	}

	release := false
	defer func() {
		if !release {
			// Best effort: the original netlink error is returned to the caller.
			_ = cleanupApplied(ctx, routePath, tracked)
		}
	}()
	for _, address := range addresses {
		if err := netlink.AddAddress(interfaceName, address); err != nil {
			return tracked, fmt.Errorf("add address: %w", err)
		}
		tracked.addresses = append(tracked.addresses, address)
	}
	if prefs.DefaultRoute {
		if err := restoreStaleDefaultRouteStatesWithState(routePath, routeStateRestoreTarget{modemID: modemID, interfaceNames: []string{interfaceName}}, netlinkDefaultRouteOps); err != nil {
			return tracked, fmt.Errorf("restore previous default route state: %w", err)
		}
		changes, err := takeoverDefaultRoutesWithState(routePath, modemID, interfaceName, routes, netlinkDefaultRouteOps)
		tracked.routeChanges = changes
		if err != nil {
			return tracked, fmt.Errorf("take over default route: %w", err)
		}
	}
	for _, route := range routes {
		if err := netlink.AddDefaultRoute(route); err != nil {
			return tracked, fmt.Errorf("add default route: %w", err)
		}
		tracked.routes = append(tracked.routes, route)
	}

	release = true
	return tracked, nil
}

func cleanupBearer(ctx context.Context, routePath string, modemID string, bearer *mmodem.Bearer, prefs Preferences) error {
	interfaceName, err := bearer.Interface(ctx)
	if err != nil {
		return fmt.Errorf("read bearer interface: %w", err)
	}
	ip4, err := bearer.IP4Config(ctx)
	if err != nil {
		return fmt.Errorf("read ipv4 config: %w", err)
	}
	ip6, err := bearer.IP6Config(ctx)
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
	routeChanges, _, err := loadRouteStateForModem(routePath, modemID, interfaceName)
	if err != nil {
		return fmt.Errorf("load default route state: %w", err)
	}
	return cleanupApplied(ctx, routePath, trackedConnection{
		interfaceName: interfaceName,
		addresses:     addresses,
		routes:        routes,
		routeChanges:  routeChanges,
	})
}

func cleanupApplied(ctx context.Context, routePath string, tracked trackedConnection) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var err error
	for i := len(tracked.routes) - 1; i >= 0; i-- {
		err = errors.Join(err, netlink.DeleteDefaultRoute(tracked.routes[i]))
	}
	err = errors.Join(err, cleanupDefaultRouteChanges(routePath, tracked.interfaceName, tracked.routeChanges, netlinkDefaultRouteOps))
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
				Source:    address.Addr(),
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
				Source:    address.Addr(),
				Metric:    metric,
			})
		}
	}

	if len(addresses) == 0 {
		return nil, nil, ErrUnsupportedIPMethod
	}
	return addresses, routes, nil
}

func secondaryRouteMetricFor(routes, current []netlink.DefaultRoute) int {
	metric := secondaryRouteMetric
	for routeMetricInUse(routes, current, metric) {
		metric++
	}
	return metric
}

func routeMetricInUse(routes, current []netlink.DefaultRoute, metric int) bool {
	return slices.ContainsFunc(routes, func(route netlink.DefaultRoute) bool {
		return slices.ContainsFunc(current, func(existing netlink.DefaultRoute) bool {
			return existing.Family == route.Family && existing.Metric == metric
		})
	})
}

func setRouteMetric(routes []netlink.DefaultRoute, metric int) {
	for i := range routes {
		routes[i].Metric = metric
	}
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

func connectionFromBearer(ctx context.Context, bearer *mmodem.Bearer, prefs Preferences, metric int) (*Connection, error) {
	prefs = normalizePreferences(bearerPreferences(ctx, bearer, prefs))

	connected, err := bearer.Connected(ctx)
	if err != nil {
		return nil, fmt.Errorf("read bearer state: %w", err)
	}
	if !connected {
		return disconnectedConnection(prefs), nil
	}

	interfaceName, err := bearer.Interface(ctx)
	if err != nil {
		return nil, fmt.Errorf("read bearer interface: %w", err)
	}
	ip4, err := bearer.IP4Config(ctx)
	if err != nil {
		return nil, fmt.Errorf("read ipv4 config: %w", err)
	}
	ip6, err := bearer.IP6Config(ctx)
	if err != nil {
		return nil, fmt.Errorf("read ipv6 config: %w", err)
	}
	stats, err := bearer.Stats(ctx)
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
		IPType:          prefs.IPType,
		APNUsername:     prefs.APNUsername,
		APNPassword:     prefs.APNPassword,
		APNAuth:         prefs.APNAuth,
		DefaultRoute:    prefs.DefaultRoute,
		ProxyEnabled:    prefs.ProxyEnabled,
		AlwaysOn:        prefs.AlwaysOn,
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

type bearerState struct {
	bearer    *mmodem.Bearer
	connected bool
}

func currentBearer(ctx context.Context, modem internetModem) (bearerState, error) {
	bearers, err := modem.bearers(ctx)
	if err != nil {
		return bearerState{}, fmt.Errorf("list bearers: %w", err)
	}
	var fallback *mmodem.Bearer
	for _, bearer := range bearers {
		connected, err := bearer.Connected(ctx)
		if err != nil {
			return bearerState{}, fmt.Errorf("read bearer state: %w", err)
		}
		if connected {
			return bearerState{bearer: bearer, connected: true}, nil
		}
		if fallback != nil {
			continue
		}
		apn, err := bearer.APN(ctx)
		if err == nil && strings.TrimSpace(apn) != "" {
			fallback = bearer
		}
	}
	return bearerState{bearer: fallback}, nil
}

func apnFromBearers(ctx context.Context, modem internetModem) (string, error) {
	current, err := currentBearer(ctx, modem)
	if err != nil {
		return "", err
	}
	if current.bearer == nil {
		return "", nil
	}
	apn, err := current.bearer.APN(ctx)
	if err != nil {
		return "", nil
	}
	return strings.TrimSpace(apn), nil
}

func modemOperatorIdentifier(modem internetModem) string {
	return strings.TrimSpace(modem.operatorIdentifier())
}

func apnForModem(modem internetModem, requested, bearer, remembered string) string {
	return selectAPN(apnSelection{
		Requested:          requested,
		Bearer:             bearer,
		Remembered:         remembered,
		OperatorIdentifier: modemOperatorIdentifier(modem),
		DefaultAPNs:        defaultAPNs,
	})
}

func preferencesWithSelectedAPN(modem internetModem, prefs Preferences) Preferences {
	prefs.APN = apnForModem(modem, "", "", prefs.APN)
	return preferencesWithDefaultAPNCredentials(modem, prefs)
}

func preferencesWithDefaultAPNCredentials(modem internetModem, prefs Preferences) Preferences {
	profile := defaultAPNProfileFrom(defaultAPNs, modemOperatorIdentifier(modem))
	if profile.APN == "" || !strings.EqualFold(strings.TrimSpace(prefs.APN), profile.APN) {
		return normalizePreferences(prefs)
	}
	if strings.TrimSpace(prefs.IPType) == "" {
		prefs.IPType = profile.IPType
	}
	if strings.TrimSpace(prefs.APNUsername) == "" {
		prefs.APNUsername = profile.Username
	}
	if prefs.APNPassword == "" {
		prefs.APNPassword = profile.Password
	}
	if strings.TrimSpace(prefs.APNAuth) == "" {
		prefs.APNAuth = profile.Auth
	}
	return normalizePreferences(prefs)
}

func disconnectedConnection(prefs Preferences) *Connection {
	prefs = normalizePreferences(prefs)
	return &Connection{
		Status:          StatusDisconnected,
		APN:             prefs.APN,
		IPType:          prefs.IPType,
		APNUsername:     prefs.APNUsername,
		APNPassword:     prefs.APNPassword,
		APNAuth:         prefs.APNAuth,
		DefaultRoute:    prefs.DefaultRoute,
		ProxyEnabled:    prefs.ProxyEnabled,
		AlwaysOn:        prefs.AlwaysOn,
		IPv4Addresses:   []string{},
		IPv6Addresses:   []string{},
		DNS:             []string{},
		DurationSeconds: 0,
		TXBytes:         0,
		RXBytes:         0,
		RouteMetric:     0,
	}
}

func bearerPreferences(ctx context.Context, bearer *mmodem.Bearer, fallback Preferences) Preferences {
	fallback = normalizePreferences(fallback)

	properties, err := bearer.Properties(ctx)
	if err != nil {
		return fallback
	}
	if properties.APN != "" {
		fallback.APN = properties.APN
	}
	if properties.IPType != "" {
		fallback.IPType = properties.IPType
	}
	if properties.Username != "" {
		fallback.APNUsername = properties.Username
	}
	if properties.Password != "" {
		fallback.APNPassword = properties.Password
	}
	if properties.AllowedAuth != "" {
		fallback.APNAuth = properties.AllowedAuth
	}
	return fallback
}

func recoverTrackedConnection(ctx context.Context, proxyPath string, routePath string, modemID string, bearer *mmodem.Bearer, fallback Preferences) (trackedConnection, int, bool, error) {
	prefs := recoverPreferences(ctx, bearer, fallback)
	metric := 0

	interfaceName, err := bearer.Interface(ctx)
	if err != nil {
		return trackedConnection{}, 0, false, fmt.Errorf("read bearer interface: %w", err)
	}
	if strings.TrimSpace(interfaceName) == "" {
		return trackedConnection{}, 0, false, nil
	}

	ip4, err := bearer.IP4Config(ctx)
	if err != nil {
		return trackedConnection{}, 0, false, fmt.Errorf("read ipv4 config: %w", err)
	}
	ip6, err := bearer.IP6Config(ctx)
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
	proxyEnabled, proxyStateFound, err := loadProxyStateForModem(proxyPath, modemID, interfaceName)
	if err != nil {
		return trackedConnection{}, 0, false, fmt.Errorf("load proxy state: %w", err)
	}
	if proxyStateFound {
		prefs.ProxyEnabled = proxyEnabled
	}

	addresses, routes, err := addressesAndRoutesWithMetric(interfaceName, metric, includeRoutes, ip4, ip6)
	if err != nil {
		if errors.Is(err, ErrUnsupportedIPMethod) {
			return trackedConnection{}, metric, false, nil
		}
		return trackedConnection{}, 0, false, err
	}
	routeChanges, routeStateFound, err := loadRouteStateForModem(routePath, modemID, interfaceName)
	if err != nil {
		return trackedConnection{}, 0, false, fmt.Errorf("load default route state: %w", err)
	}
	if prefs.DefaultRoute && !routeStateFound {
		slog.Debug("recovering connected bearer default route takeover", "modem", modemID, "interface", interfaceName)
		routeChanges, err = takeoverDefaultRoutesWithState(routePath, modemID, interfaceName, routes, netlinkDefaultRouteOps)
		if err != nil {
			return trackedConnection{}, 0, false, fmt.Errorf("take over recovered default route: %w", err)
		}
	}

	return trackedConnection{
		bearerPath:    bearer.Path(),
		interfaceName: interfaceName,
		prefs:         prefs,
		routeMetric:   metric,
		addresses:     addresses,
		routes:        routes,
		routeChanges:  routeChanges,
	}, metric, true, nil
}

func recoverPreferences(ctx context.Context, bearer *mmodem.Bearer, fallback Preferences) Preferences {
	prefs := bearerPreferences(ctx, bearer, fallback)
	interfaceName, err := bearer.Interface(ctx)
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
