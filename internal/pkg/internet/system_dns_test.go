package internet

import (
	"context"
	"errors"
	"maps"
	"net/netip"
	"os"
	"slices"
	"testing"

	"github.com/godbus/dbus/v5"
	"golang.org/x/sys/unix"

	"github.com/damonto/sigmo/internal/pkg/netlink"
)

type testSystemDNS struct {
	links            map[string]linkIdentity
	configs          map[int32]linkDNS
	checkErr         error
	failDomains      bool
	failClearServers bool
}

func newTestSystemDNS(names ...string) *testSystemDNS {
	resolver := &testSystemDNS{links: make(map[string]linkIdentity), configs: make(map[int32]linkDNS)}
	for i, name := range names {
		resolver.links[name] = linkIdentity{Boot: "boot", Index: int32(i + 1), Path: "/devices/" + name}
	}
	return resolver
}

func (r *testSystemDNS) linkID(name string) (linkIdentity, error) {
	link, ok := r.links[name]
	if !ok {
		return linkIdentity{Boot: "boot"}, os.ErrNotExist
	}
	return link, nil
}

func (r *testSystemDNS) check(ctx context.Context, _ string) error {
	return errors.Join(ctx.Err(), r.checkErr)
}

func (r *testSystemDNS) configuration(ctx context.Context) (map[int32]linkDNS, error) {
	return maps.Clone(r.configs), ctx.Err()
}

func (r *testSystemDNS) setServers(ctx context.Context, index int32, servers []netip.Addr) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.failClearServers && len(servers) == 0 {
		return errors.New("denied DNS server cleanup")
	}
	config := r.configs[index]
	config.servers = slices.Clone(servers)
	r.configs[index] = config
	return nil
}

func (r *testSystemDNS) setDomains(ctx context.Context, index int32, domains []dnsDomain) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.failDomains && len(domains) > 0 {
		return errors.New("denied DNS domain write")
	}
	config := r.configs[index]
	config.domains = slices.Clone(domains)
	r.configs[index] = config
	return nil
}

func dnsTestRoute(name string, family, metric int) netlink.DefaultRoute {
	return netlink.DefaultRoute{Interface: name, Family: family, Metric: metric}
}

func dnsTestConnection(route netlink.DefaultRoute, dns string) trackedConnection {
	return trackedConnection{interfaceName: route.Interface, routes: []netlink.DefaultRoute{route}, dns: []string{dns}, routeMetric: route.Metric}
}

func newDNSConnector(t *testing.T, resolver *testSystemDNS, routes *[]netlink.DefaultRoute) *Connector {
	t.Helper()
	c, err := NewConnector(ConnectorConfig{State: testStore(t)})
	if err != nil {
		t.Fatal(err)
	}
	c.systemDNS = resolver
	c.routes = defaultRouteOps{
		linkID:        resolver.linkID,
		defaultRoutes: func() ([]netlink.DefaultRoute, error) { return slices.Clone(*routes), nil },
		addDefaultRoute: func(route netlink.DefaultRoute) error {
			if defaultRouteExists(route, *routes) {
				return netlink.ErrDefaultRouteExists
			}
			*routes = append(*routes, route)
			return nil
		},
		deleteDefaultRoute: func(route netlink.DefaultRoute) error {
			*routes = slices.DeleteFunc(*routes, func(candidate netlink.DefaultRoute) bool { return sameDefaultRoute(candidate, route) })
			return nil
		},
	}
	return c
}

func TestCorruptDNSStateDoesNotPreventDefaultRoute(t *testing.T) {
	ctx := t.Context()
	route := dnsTestRoute("wwan0", netlink.FamilyIPv4, secondaryRouteMetric)
	routes := []netlink.DefaultRoute{route}
	resolver := newTestSystemDNS(route.Interface)
	c := newDNSConnector(t, resolver, &routes)
	if err := c.state.Put(ctx, "system", systemDNSKey, "invalid DNS record"); err != nil {
		t.Fatal(err)
	}
	tracked, err := updateTestDefaultRoute(ctx, c, "modem", dnsTestConnection(route, "10.0.0.53"), true)
	route.Metric = defaultRouteMetric
	if err != nil || !tracked.prefs.DefaultRoute || !slices.Equal(routes, []netlink.DefaultRoute{route}) {
		t.Fatalf("DNS state prevented promotion: %+v, %v", routes, err)
	}
	if dnsConfigured(resolver.configs[1]) {
		t.Fatalf("wrote DNS without a valid record: %+v", resolver.configs[1])
	}
}

func updateTestDefaultRoute(ctx context.Context, c *Connector, modemID string, tracked trackedConnection, enabled bool) (trackedConnection, error) {
	return c.updateTrackedPreferences(ctx, fakeInternetModem{modemID: modemID}, tracked, ConnectionPreferences{
		DefaultRoute: enabled,
		AlwaysOn:     tracked.prefs.AlwaysOn,
		ProxyEnabled: tracked.prefs.ProxyEnabled,
	})
}

func TestDefaultRouteDoesNotRequireDNS(t *testing.T) {
	for _, test := range []struct {
		name  string
		setup func(*testSystemDNS, *trackedConnection)
	}{
		{"unsupported resolver", func(r *testSystemDNS, _ *trackedConnection) { r.checkErr = errSystemDNSUnavailable }},
		{"no carrier DNS", func(_ *testSystemDNS, tracked *trackedConnection) { tracked.dns = nil }},
		{"external default DNS", func(r *testSystemDNS, _ *trackedConnection) {
			r.configs[2] = linkDNS{servers: []netip.Addr{netip.MustParseAddr("192.0.2.1")}, domains: []dnsDomain{rootDNSDomain}}
		}},
		{"failed DNS domain write", func(r *testSystemDNS, _ *trackedConnection) { r.failDomains = true }},
	} {
		t.Run(test.name, func(t *testing.T) {
			original := dnsTestRoute("wwan0", netlink.FamilyIPv4, secondaryRouteMetric+42)
			routes := []netlink.DefaultRoute{original}
			resolver := newTestSystemDNS("wwan0", "eth0")
			tracked := dnsTestConnection(original, "10.0.0.53")
			test.setup(resolver, &tracked)
			hostDNS := resolver.configs[2]
			c := newDNSConnector(t, resolver, &routes)
			updated, err := updateTestDefaultRoute(t.Context(), c, "modem", tracked, true)
			if err != nil || !updated.prefs.DefaultRoute || updated.routeMetric != defaultRouteMetric {
				t.Fatalf("promotion = %t, %d, %v; want preferred route", updated.prefs.DefaultRoute, updated.routeMetric, err)
			}
			expected := original
			expected.Metric = defaultRouteMetric
			if !slices.Equal(routes, []netlink.DefaultRoute{expected}) || dnsConfigured(resolver.configs[1]) {
				t.Fatalf("preferred routes %+v or fallback DNS %+v", routes, resolver.configs[1])
			}
			if got := resolver.configs[2]; !slices.Equal(got.servers, hostDNS.servers) || !slices.Equal(got.domains, hostDNS.domains) {
				t.Fatalf("host DNS changed: %+v", got)
			}
			state, err := c.loadSystemDNS(t.Context())
			if err != nil || state.Pending != nil || len(state.Owners) != 0 {
				t.Fatalf("rollback state = %+v, %v", state, err)
			}
		})
	}
}

func TestDefaultRouteRollbackRetriesIncompleteRouteRestoration(t *testing.T) {
	ctx := t.Context()
	a := dnsTestRoute("wwan0", netlink.FamilyIPv4, secondaryRouteMetric)
	b := dnsTestRoute("qmimux0", netlink.FamilyIPv4, secondaryRouteMetric+7)
	routes := []netlink.DefaultRoute{a, b}
	resolver := newTestSystemDNS(a.Interface, b.Interface)
	resolver.checkErr = errSystemDNSUnavailable
	c := newDNSConnector(t, resolver, &routes)
	active, err := c.updateTrackedDefaultRoute(ctx, "a", dnsTestConnection(a, "10.0.0.53"), true)
	if err != nil {
		t.Fatal(err)
	}
	c.setConnectionAndPreference("a", active, active.prefs)

	addRoute := c.routes.addDefaultRoute
	canRestoreA := false
	c.routes.addDefaultRoute = func(route netlink.DefaultRoute) error {
		if route.Metric == defaultRouteMetric && (route.Interface == b.Interface || !canRestoreA) {
			return errors.New("host temporarily rejected the preferred route")
		}
		return addRoute(route)
	}
	if _, err := c.updateTrackedDefaultRoute(ctx, "b", dnsTestConnection(b, "10.0.0.54"), true); err == nil {
		t.Fatal("promotion succeeded despite route write failure")
	}
	pending, err := c.loadRoutePromotion(ctx)
	if err != nil || pending == nil {
		t.Fatalf("pending rollback = %+v, %v", pending, err)
	}

	canRestoreA = true
	if err := c.recoverDefaultRoutes(ctx); err != nil {
		t.Fatal(err)
	}
	if len(routes) != 2 || !defaultRouteExists(active.routes[0], routes) || !defaultRouteExists(b, routes) {
		t.Fatalf("restored routes = %+v, want previous routes and metrics", routes)
	}
	pending, err = c.loadRoutePromotion(ctx)
	if err != nil || pending != nil {
		t.Fatalf("recovered route state = %+v, %v", pending, err)
	}
}

func TestDefaultRouteRecoveryWithoutDNS(t *testing.T) {
	ctx := t.Context()
	cellular := dnsTestRoute("wwan0", netlink.FamilyIPv4, secondaryRouteMetric)
	routes := []netlink.DefaultRoute{cellular}
	connector := newDNSConnector(t, newTestSystemDNS(cellular.Interface), &routes)
	connector.systemDNS = nil
	active, err := connector.updateTrackedDefaultRoute(ctx, "modem", dnsTestConnection(cellular, ""), true)
	if err != nil {
		t.Fatal(err)
	}
	connector.setConnectionAndPreference("modem", active, active.prefs)
	wired := dnsTestRoute("eth0", netlink.FamilyIPv4, 1)
	routes = append(routes, wired)
	if err := connector.recoverDefaultRoutes(ctx); err != nil {
		t.Fatal(err)
	}
	current, _ := connector.connection("modem")
	if current.prefs.DefaultRoute || len(routes) != 2 || !defaultRouteExists(cellular, routes) || !defaultRouteExists(wired, routes) {
		t.Fatalf("route recovery without DNS = %+v, configured default = %t", routes, current.prefs.DefaultRoute)
	}
}

func TestSystemDNSHandoffAndRecovery(t *testing.T) {
	a := dnsTestRoute("wwan0", netlink.FamilyIPv4, secondaryRouteMetric)
	b := dnsTestRoute("qmimux0", netlink.FamilyIPv4, secondaryRouteMetric+1)
	routes := []netlink.DefaultRoute{a, b}
	resolver := newTestSystemDNS(a.Interface, b.Interface)
	c := newDNSConnector(t, resolver, &routes)
	aTracked, err := updateTestDefaultRoute(t.Context(), c, "a", dnsTestConnection(a, "10.0.0.53"), true)
	if err != nil {
		t.Fatal(err)
	}
	c.setConnectionAndPreference("a", aTracked, aTracked.prefs)
	bTracked, err := updateTestDefaultRoute(t.Context(), c, "b", dnsTestConnection(b, "10.0.0.54"), true)
	if err != nil {
		t.Fatal(err)
	}
	if dnsConfigured(resolver.configs[1]) || !dnsConfigured(resolver.configs[2]) {
		t.Fatalf("handoff DNS = %+v", resolver.configs)
	}
	clear(resolver.configs) // Simulate resolved restarting without its link state.
	if err := c.recoverSystemDNS(t.Context()); err != nil || !dnsConfigured(resolver.configs[2]) {
		t.Fatalf("recover DNS = %+v, %v", resolver.configs, err)
	}
	if _, err := updateTestDefaultRoute(t.Context(), c, "b", bTracked, false); err != nil {
		t.Fatal(err)
	}
	if !dnsConfigured(resolver.configs[1]) || dnsConfigured(resolver.configs[2]) {
		t.Fatalf("restored DNS = %+v", resolver.configs)
	}
}

func TestSystemDNSPendingCrashRecovery(t *testing.T) {
	route := dnsTestRoute("wwan0", netlink.FamilyIPv4, defaultRouteMetric)
	routes := []netlink.DefaultRoute{route}
	resolver := newTestSystemDNS(route.Interface)
	c := newDNSConnector(t, resolver, &routes)
	tracked := dnsTestConnection(route, "10.0.0.53")
	state, err := c.beginSystemDNS(t.Context(), "modem", tracked)
	if err != nil {
		t.Fatal(err)
	}
	resolver.configs[1] = linkDNS{servers: state.Pending.Servers}
	if err := c.recoverSystemDNS(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(routes, []netlink.DefaultRoute{route}) || dnsConfigured(resolver.configs[1]) {
		t.Fatalf("recovered routes %+v and DNS %+v", routes, resolver.configs)
	}
	state, err = c.loadSystemDNS(t.Context())
	if err != nil || state.Pending != nil {
		t.Fatalf("recovered state = %+v, %v", state, err)
	}
}

func TestSystemDNSRetryDoesNotRecreateRoutesAfterDisconnect(t *testing.T) {
	ctx := t.Context()
	route := dnsTestRoute("wwan0", netlink.FamilyIPv4, secondaryRouteMetric)
	routes := []netlink.DefaultRoute{route}
	resolver := newTestSystemDNS(route.Interface)
	resolver.failDomains = true
	resolver.failClearServers = true
	c := newDNSConnector(t, resolver, &routes)
	updated, err := updateTestDefaultRoute(ctx, c, "modem", dnsTestConnection(route, "10.0.0.53"), true)
	if err != nil || !updated.prefs.DefaultRoute {
		t.Fatalf("DNS cleanup error prevented default route: %+v, %v", updated, err)
	}
	state, err := c.loadSystemDNS(ctx)
	if err != nil || state.Pending == nil {
		t.Fatalf("pending DNS cleanup = %+v, %v", state, err)
	}
	if err := c.recoverSystemDNS(ctx); err == nil || !slices.Equal(routes, updated.routes) {
		t.Fatalf("DNS retry changed the preferred route: %+v, %v", routes, err)
	}

	// The bearer has gone away but the physical interface still exists.
	routes = nil
	resolver.failClearServers = false
	if err := c.recoverSystemDNS(ctx); err != nil {
		t.Fatal(err)
	}
	if len(routes) != 0 || dnsConfigured(resolver.configs[1]) {
		t.Fatalf("retry left routes %+v or DNS %+v", routes, resolver.configs[1])
	}
}

func TestSystemDNSCleanupFailureDoesNotPreventDemotion(t *testing.T) {
	ctx := t.Context()
	route := dnsTestRoute("wwan0", netlink.FamilyIPv4, secondaryRouteMetric)
	routes := []netlink.DefaultRoute{route}
	resolver := newTestSystemDNS(route.Interface)
	c := newDNSConnector(t, resolver, &routes)
	active, err := updateTestDefaultRoute(ctx, c, "modem", dnsTestConnection(route, "10.0.0.53"), true)
	if err != nil {
		t.Fatal(err)
	}
	resolver.failClearServers = true
	updated, err := updateTestDefaultRoute(ctx, c, "modem", active, false)
	if err != nil || updated.prefs.DefaultRoute || !slices.Equal(routes, []netlink.DefaultRoute{route}) {
		t.Fatalf("DNS cleanup blocked route demotion: %+v, %v", routes, err)
	}
	resolver.failClearServers = false
	if err := c.recoverSystemDNS(ctx); err != nil || dnsConfigured(resolver.configs[1]) {
		t.Fatalf("failed to retry DNS cleanup: %+v, %v", resolver.configs[1], err)
	}
}

func TestSystemDNSYieldsToVPNWithoutChangingRoutes(t *testing.T) {
	route := dnsTestRoute("wwan0", netlink.FamilyIPv4, secondaryRouteMetric)
	routes := []netlink.DefaultRoute{route}
	resolver := newTestSystemDNS(route.Interface, "tun0")
	c := newDNSConnector(t, resolver, &routes)
	tracked, err := updateTestDefaultRoute(t.Context(), c, "modem", dnsTestConnection(route, "10.0.0.53"), true)
	if err != nil {
		t.Fatal(err)
	}
	c.setConnectionAndPreference("modem", tracked, tracked.prefs)
	vpnDNS := []netip.Addr{netip.MustParseAddr("192.0.2.53")}
	resolver.configs[2] = linkDNS{servers: vpnDNS, domains: []dnsDomain{rootDNSDomain}}
	if err := c.recoverSystemDNS(t.Context()); err != nil {
		t.Fatal(err)
	}
	current, _ := c.connection("modem")
	if !current.prefs.DefaultRoute || !slices.Equal(routes, tracked.routes) || dnsConfigured(resolver.configs[1]) {
		t.Fatalf("DNS handoff changed the preferred route or kept cellular DNS: %+v, %+v", routes, resolver.configs[1])
	}
	if !slices.Equal(resolver.configs[2].servers, vpnDNS) || !slices.Equal(resolver.configs[2].domains, []dnsDomain{rootDNSDomain}) {
		t.Fatalf("VPN DNS changed: %+v", resolver.configs[2])
	}
}

func TestSystemDNSKeepsResolverOverridesOnRelease(t *testing.T) {
	ctx := t.Context()
	route := dnsTestRoute("wwan0", netlink.FamilyIPv4, secondaryRouteMetric)
	routes := []netlink.DefaultRoute{route}
	resolver := newTestSystemDNS(route.Interface)
	c := newDNSConnector(t, resolver, &routes)
	const server = "10.0.0.53"
	tracked, err := updateTestDefaultRoute(ctx, c, "modem", dnsTestConnection(route, server), true)
	if err != nil {
		t.Fatal(err)
	}
	c.setConnectionAndPreference("modem", tracked, tracked.prefs)

	// Another network manager configures the same address with TLS metadata.
	resolver.configs, err = decodeResolvedDNS(map[string]dbus.Variant{
		"DNSEx": dbus.MakeVariant([]resolvedLinkServerEx{{
			Index: 1, Family: unix.AF_INET, Address: netip.MustParseAddr(server).AsSlice(), Port: 853, ServerName: "dns.example",
		}}),
		"Domains": dbus.MakeVariant([]resolvedLinkDomain{{Index: 1, Domain: ".", RouteOnly: true}}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.recoverSystemDNS(ctx); err != nil {
		t.Fatal(err)
	}
	current, _ := c.connection("modem")
	if !current.prefs.DefaultRoute || !slices.Equal(routes, tracked.routes) {
		t.Fatalf("cellular route changed: %+v", routes)
	}
	got := resolver.configs[1]
	if !got.customServers || !slices.Equal(got.servers, []netip.Addr{netip.MustParseAddr(server)}) || !slices.Equal(got.domains, []dnsDomain{rootDNSDomain}) {
		t.Fatalf("externally configured DNS changed: %+v", got)
	}
}

func TestSystemDNSKeepsIPv6WhenIPv4RouteLosesPreference(t *testing.T) {
	ctx := t.Context()
	ipv4 := dnsTestRoute("wwan0", netlink.FamilyIPv4, secondaryRouteMetric)
	ipv6 := dnsTestRoute("wwan0", netlink.FamilyIPv6, secondaryRouteMetric)
	routes := []netlink.DefaultRoute{ipv4, ipv6}
	resolver := newTestSystemDNS(ipv4.Interface)
	c := newDNSConnector(t, resolver, &routes)
	tracked := dnsTestConnection(ipv4, "2001:db8::53")
	tracked.routes = append(tracked.routes, ipv6)
	tracked, err := updateTestDefaultRoute(ctx, c, "modem", tracked, true)
	if err != nil {
		t.Fatal(err)
	}
	c.setConnectionAndPreference("modem", tracked, tracked.prefs)
	routes = append(routes, dnsTestRoute("eth0", netlink.FamilyIPv4, 1))
	if err := c.recoverDefaultRoutes(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.recoverSystemDNS(ctx); err != nil {
		t.Fatal(err)
	}
	current, _ := c.connection("modem")
	if !current.prefs.DefaultRoute || !dnsConfigured(resolver.configs[1]) {
		t.Fatalf("reconciliation removed the IPv6 default and DNS: %+v, %+v", routes, resolver.configs[1])
	}
}

func TestDefaultDNSServersUsesAvailableFamilies(t *testing.T) {
	routes := []netlink.DefaultRoute{dnsTestRoute("wwan0", netlink.FamilyIPv4, defaultRouteMetric)}
	got := defaultDNSServers([]string{"invalid", "2001:db8::53", "10.0.0.53", "::ffff:10.0.0.53", "127.0.0.53"}, routes)
	if !slices.Equal(got, []netip.Addr{netip.MustParseAddr("10.0.0.53")}) {
		t.Fatalf("default DNS servers = %v", got)
	}
}

func TestResolvedStubConfiguration(t *testing.T) {
	for _, test := range []struct {
		conf string
		want bool
	}{
		{"# nameserver 192.0.2.1\nnameserver 127.0.0.53\noptions edns0", true},
		{"nameserver 127.0.0.53\nnameserver 192.0.2.1", false},
		{"nameserver 192.0.2.1", false},
		{"search example.com", false},
	} {
		if got := usesResolvedStub(test.conf); got != test.want {
			t.Errorf("usesResolvedStub(%q) = %t, want %t", test.conf, got, test.want)
		}
	}
}
