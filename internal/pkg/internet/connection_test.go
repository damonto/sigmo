package internet

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strings"
	"testing"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/netlink"
	"github.com/damonto/sigmo/internal/pkg/networkprefs"
)

func TestLockRouteTransactionHoldsGlobalRouteLock(t *testing.T) {
	t.Parallel()

	connector, err := NewConnector(ConnectorConfig{State: testStore(t)})
	if err != nil {
		t.Fatalf("NewConnector() error = %v", err)
	}
	unlock := connector.lockRouteTransaction("modem-1")
	if connector.routeMu.TryLock() {
		connector.routeMu.Unlock()
		unlock()
		t.Fatal("route transaction did not hold the global route lock")
	}
	unlock()

	if !connector.routeMu.TryLock() {
		t.Fatal("route transaction did not release the global route lock")
	}
	connector.routeMu.Unlock()
}

func TestRouteMetric(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		defaultRoute bool
		want         int
	}{
		{name: "default route", defaultRoute: true, want: defaultRouteMetric},
		{name: "secondary route", defaultRoute: false, want: secondaryRouteMetric},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := routeMetric(tt.defaultRoute); got != tt.want {
				t.Fatalf("routeMetric() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestCurrentReturnsDefaultAPNCredentials(t *testing.T) {
	t.Parallel()

	modem := fakeInternetModem{
		modemID:    "860588043408833",
		operatorID: "23491",
	}

	connector, err := NewConnector(ConnectorConfig{
		State: testStore(t),
	})
	if err != nil {
		t.Fatalf("NewConnector() error = %v", err)
	}

	got, err := connector.current(t.Context(), modem)
	if err != nil {
		t.Fatalf("Current() error = %v", err)
	}
	if got.Status != StatusDisconnected {
		t.Fatalf("Current() Status = %q, want %q", got.Status, StatusDisconnected)
	}
	if got.APN != "wap.vodafone.co.uk" {
		t.Fatalf("Current() APN = %q, want Vodafone APN", got.APN)
	}
	if got.IPType != "ipv4v6" {
		t.Fatalf("Current() IPType = %q, want ipv4v6", got.IPType)
	}
	if got.APNUsername != "wap" || got.APNPassword != "wap" || got.APNAuth != "pap" {
		t.Fatalf("Current() credentials = %q/%q/%q, want wap/wap/pap", got.APNUsername, got.APNPassword, got.APNAuth)
	}
}

func TestSecondaryRouteMetricFor(t *testing.T) {
	t.Parallel()

	ipv4Route := netlink.DefaultRoute{
		Interface: "wws27u2i4",
		Family:    netlink.FamilyIPv4,
		Gateway:   netip.MustParseAddr("10.9.15.132"),
		Metric:    secondaryRouteMetric,
	}
	ipv6Route := netlink.DefaultRoute{
		Interface: "wws27u2i4",
		Family:    netlink.FamilyIPv6,
		Gateway:   netip.MustParseAddr("2001:db8::1"),
		Metric:    secondaryRouteMetric,
	}

	tests := []struct {
		name    string
		routes  []netlink.DefaultRoute
		current []netlink.DefaultRoute
		want    int
	}{
		{
			name:   "keeps default secondary metric when unused",
			routes: []netlink.DefaultRoute{ipv4Route},
			current: []netlink.DefaultRoute{
				{Interface: "eth0", Family: netlink.FamilyIPv4, Metric: defaultRouteMetric},
			},
			want: secondaryRouteMetric,
		},
		{
			name:   "skips occupied ipv4 metric",
			routes: []netlink.DefaultRoute{ipv4Route},
			current: []netlink.DefaultRoute{
				{Interface: "wws27u1i4", Family: netlink.FamilyIPv4, Metric: secondaryRouteMetric},
			},
			want: secondaryRouteMetric + 1,
		},
		{
			name:   "ignores occupied metric in unrelated family",
			routes: []netlink.DefaultRoute{ipv4Route},
			current: []netlink.DefaultRoute{
				{Interface: "wws27u1i4", Family: netlink.FamilyIPv6, Metric: secondaryRouteMetric},
			},
			want: secondaryRouteMetric,
		},
		{
			name:   "dual stack skips when either family is occupied",
			routes: []netlink.DefaultRoute{ipv4Route, ipv6Route},
			current: []netlink.DefaultRoute{
				{Interface: "wws27u1i4", Family: netlink.FamilyIPv6, Metric: secondaryRouteMetric},
			},
			want: secondaryRouteMetric + 1,
		},
		{
			name:   "skips consecutive occupied metrics",
			routes: []netlink.DefaultRoute{ipv4Route},
			current: []netlink.DefaultRoute{
				{Interface: "wws27u1i4", Family: netlink.FamilyIPv4, Metric: secondaryRouteMetric},
				{Interface: "wws27u3i4", Family: netlink.FamilyIPv4, Metric: secondaryRouteMetric + 1},
			},
			want: secondaryRouteMetric + 2,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := secondaryRouteMetricFor(tt.routes, tt.current); got != tt.want {
				t.Fatalf("secondaryRouteMetricFor() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestEffectiveDNSServers(t *testing.T) {
	tests := []struct {
		name    string
		servers []string
		want    []string
	}{
		{name: "fallback", want: []string{fallbackDNSServer}},
		{name: "ipv4", servers: []string{" 1.1.1.1 "}, want: []string{"1.1.1.1:53"}},
		{name: "ipv6", servers: []string{"2001:4860:4860::8888"}, want: []string{"[2001:4860:4860::8888]:53"}},
		{name: "keeps port and removes duplicates", servers: []string{"9.9.9.9:5353", "9.9.9.9:5353"}, want: []string{"9.9.9.9:5353"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := effectiveDNSServers(tt.servers); !slices.Equal(got, tt.want) {
				t.Fatalf("effectiveDNSServers() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDNSNetworkForServer(t *testing.T) {
	tests := []struct {
		name    string
		network string
		server  string
		want    string
	}{
		{name: "udp ipv4", network: "udp", server: "1.1.1.1:53", want: "udp4"},
		{name: "tcp ipv4", network: "tcp", server: "1.1.1.1:53", want: "tcp4"},
		{name: "udp ipv6", network: "udp", server: "[2001:4860:4860::8888]:53", want: "udp6"},
		{name: "tcp ipv6", network: "tcp", server: "[2001:4860:4860::8888]:53", want: "tcp6"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := dnsNetworkForServer(tt.network, tt.server); got != tt.want {
				t.Fatalf("dnsNetworkForServer() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDNSResolverRotatesServers(t *testing.T) {
	type dialCall struct {
		network string
		address string
	}
	want := []dialCall{
		{network: "udp4", address: "1.1.1.1:53"},
		{network: "tcp6", address: "[2001:4860:4860::8888]:53"},
		{network: "udp4", address: "1.1.1.1:53"},
	}
	var got []dialCall
	dialErr := errors.New("stop DNS dial")
	resolver := newDNSResolver(
		[]string{"1.1.1.1:53", "[2001:4860:4860::8888]:53"},
		func(_ context.Context, network, address string) (net.Conn, error) {
			got = append(got, dialCall{network: network, address: address})
			return nil, dialErr
		},
	)

	for _, network := range []string{"udp", "tcp", "udp"} {
		if _, err := resolver.Dial(t.Context(), network, "ignored:53"); !errors.Is(err, dialErr) {
			t.Fatalf("Resolver.Dial() error = %v, want %v", err, dialErr)
		}
	}
	if !slices.Equal(got, want) {
		t.Fatalf("Resolver.Dial() calls = %+v, want %+v", got, want)
	}
}

func TestAddressesAndRoutes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		prefs      Preferences
		ip4        mmodem.BearerIPConfig
		ip6        mmodem.BearerIPConfig
		wantAddrs  []netip.Prefix
		wantRoutes []netlink.DefaultRoute
		wantErr    error
		errOnly    bool
	}{
		{
			name: "ipv4 secondary route",
			prefs: Preferences{
				APN:          "internet",
				DefaultRoute: false,
			},
			ip4: mmodem.BearerIPConfig{
				Method:  mmodem.BearerIPMethodStatic,
				Address: "10.0.0.2",
				Prefix:  30,
				Gateway: "10.0.0.1",
			},
			wantAddrs: []netip.Prefix{netip.MustParsePrefix("10.0.0.2/30")},
			wantRoutes: []netlink.DefaultRoute{
				{
					Interface: "wwan0",
					Family:    netlink.FamilyIPv4,
					Gateway:   netip.MustParseAddr("10.0.0.1"),
					Source:    netip.MustParseAddr("10.0.0.2"),
					Metric:    secondaryRouteMetric,
				},
			},
		},
		{
			name: "ipv6 default route",
			prefs: Preferences{
				APN:          "internet",
				DefaultRoute: true,
			},
			ip6: mmodem.BearerIPConfig{
				Method:  mmodem.BearerIPMethodStatic,
				Address: "2001:db8::2",
				Prefix:  64,
				Gateway: "2001:db8::1",
			},
			wantAddrs: []netip.Prefix{netip.MustParsePrefix("2001:db8::2/64")},
			wantRoutes: []netlink.DefaultRoute{
				{
					Interface: "wwan0",
					Family:    netlink.FamilyIPv6,
					Gateway:   netip.MustParseAddr("2001:db8::1"),
					Source:    netip.MustParseAddr("2001:db8::2"),
					Metric:    defaultRouteMetric,
				},
			},
		},
		{
			name: "unsupported when no static address",
			ip4: mmodem.BearerIPConfig{
				Method: mmodem.BearerIPMethodDHCP,
			},
			wantErr: ErrUnsupportedIPMethod,
		},
		{
			name: "dhcp method with complete ipv4 config",
			ip4: mmodem.BearerIPConfig{
				Method:  mmodem.BearerIPMethodDHCP,
				Address: "10.0.0.2",
				Prefix:  30,
				Gateway: "10.0.0.1",
			},
			wantAddrs: []netip.Prefix{netip.MustParsePrefix("10.0.0.2/30")},
			wantRoutes: []netlink.DefaultRoute{{
				Interface: "wwan0",
				Family:    netlink.FamilyIPv4,
				Gateway:   netip.MustParseAddr("10.0.0.1"),
				Source:    netip.MustParseAddr("10.0.0.2"),
				Metric:    secondaryRouteMetric,
			}},
		},
		{
			name: "invalid static address",
			ip4: mmodem.BearerIPConfig{
				Method:  mmodem.BearerIPMethodStatic,
				Address: "not-an-ip",
				Prefix:  24,
			},
			errOnly: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotAddrs, gotRoutes, err := addressesAndRoutes("wwan0", tt.prefs, tt.ip4, tt.ip6)
			if tt.wantErr != nil || tt.errOnly {
				if err == nil {
					t.Fatal("addressesAndRoutes() error = nil, want error")
				}
				if errors.Is(tt.wantErr, ErrUnsupportedIPMethod) && !errors.Is(err, ErrUnsupportedIPMethod) {
					t.Fatalf("addressesAndRoutes() error = %v, want unsupported", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("addressesAndRoutes() error = %v", err)
			}
			if !slices.Equal(gotAddrs, tt.wantAddrs) {
				t.Fatalf("addressesAndRoutes() addresses = %#v, want %#v", gotAddrs, tt.wantAddrs)
			}
			if !slices.Equal(gotRoutes, tt.wantRoutes) {
				t.Fatalf("addressesAndRoutes() routes = %#v, want %#v", gotRoutes, tt.wantRoutes)
			}
		})
	}
}

func TestAddressesAndRoutesWithMetric(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		metric        int
		includeRoutes bool
		wantRoutes    []netlink.DefaultRoute
	}{
		{
			name:          "recovered route keeps kernel metric",
			metric:        42,
			includeRoutes: true,
			wantRoutes: []netlink.DefaultRoute{
				{
					Interface: "wwan0",
					Family:    netlink.FamilyIPv4,
					Gateway:   netip.MustParseAddr("10.0.0.1"),
					Source:    netip.MustParseAddr("10.0.0.2"),
					Metric:    42,
				},
			},
		},
		{
			name:          "no recovered route only tracks address",
			metric:        0,
			includeRoutes: false,
			wantRoutes:    nil,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, gotRoutes, err := addressesAndRoutesWithMetric("wwan0", tt.metric, tt.includeRoutes, mmodem.BearerIPConfig{
				Method:  mmodem.BearerIPMethodStatic,
				Address: "10.0.0.2",
				Prefix:  30,
				Gateway: "10.0.0.1",
			}, mmodem.BearerIPConfig{})
			if err != nil {
				t.Fatalf("addressesAndRoutesWithMetric() error = %v", err)
			}
			if !slices.Equal(gotRoutes, tt.wantRoutes) {
				t.Fatalf("addressesAndRoutesWithMetric() routes = %#v, want %#v", gotRoutes, tt.wantRoutes)
			}
		})
	}
}

func TestDefaultRouteChanges(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		current   []netlink.DefaultRoute
		preferred []netlink.DefaultRoute
		want      []defaultRouteChange
	}{
		{
			name: "demotes lower metric ipv4 route",
			current: []netlink.DefaultRoute{
				{
					Interface: "ens18",
					Family:    netlink.FamilyIPv4,
					Gateway:   netip.MustParseAddr("10.10.10.201"),
					Metric:    0,
				},
				{
					Interface: "eth1",
					Family:    netlink.FamilyIPv4,
					Gateway:   netip.MustParseAddr("10.20.0.1"),
					Metric:    100,
				},
			},
			preferred: []netlink.DefaultRoute{
				{
					Interface: "wws27u1i4",
					Family:    netlink.FamilyIPv4,
					Gateway:   netip.MustParseAddr("10.9.15.132"),
					Metric:    defaultRouteMetric,
				},
			},
			want: []defaultRouteChange{
				{
					Original: netlink.DefaultRoute{
						Interface: "ens18",
						Family:    netlink.FamilyIPv4,
						Gateway:   netip.MustParseAddr("10.10.10.201"),
						Metric:    0,
					},
					Replacement: netlink.DefaultRoute{
						Interface: "ens18",
						Family:    netlink.FamilyIPv4,
						Gateway:   netip.MustParseAddr("10.10.10.201"),
						Metric:    defaultRouteMetric + 1,
					},
				},
			},
		},
		{
			name: "keeps unrelated family",
			current: []netlink.DefaultRoute{
				{
					Interface: "ens18",
					Family:    netlink.FamilyIPv6,
					Gateway:   netip.MustParseAddr("2001:db8::1"),
					Metric:    0,
				},
			},
			preferred: []netlink.DefaultRoute{
				{
					Interface: "wws27u1i4",
					Family:    netlink.FamilyIPv4,
					Gateway:   netip.MustParseAddr("10.9.15.132"),
					Metric:    defaultRouteMetric,
				},
			},
		},
		{
			name: "avoids replacement metric collision",
			current: []netlink.DefaultRoute{
				{
					Interface: "ens18",
					Family:    netlink.FamilyIPv4,
					Gateway:   netip.MustParseAddr("10.10.10.201"),
					Metric:    0,
				},
				{
					Interface: "ens18",
					Family:    netlink.FamilyIPv4,
					Gateway:   netip.MustParseAddr("10.10.10.201"),
					Metric:    defaultRouteMetric + 1,
				},
			},
			preferred: []netlink.DefaultRoute{
				{
					Interface: "wws27u1i4",
					Family:    netlink.FamilyIPv4,
					Gateway:   netip.MustParseAddr("10.9.15.132"),
					Metric:    defaultRouteMetric,
				},
			},
			want: []defaultRouteChange{
				{
					Original: netlink.DefaultRoute{
						Interface: "ens18",
						Family:    netlink.FamilyIPv4,
						Gateway:   netip.MustParseAddr("10.10.10.201"),
						Metric:    0,
					},
					Replacement: netlink.DefaultRoute{
						Interface: "ens18",
						Family:    netlink.FamilyIPv4,
						Gateway:   netip.MustParseAddr("10.10.10.201"),
						Metric:    defaultRouteMetric + 2,
					},
				},
			},
		},
		{
			name: "avoids replacement metric collision across interfaces",
			current: []netlink.DefaultRoute{
				{
					Interface: "ens18",
					Family:    netlink.FamilyIPv4,
					Gateway:   netip.MustParseAddr("10.10.10.201"),
					Metric:    0,
				},
				{
					Interface: "eth0",
					Family:    netlink.FamilyIPv4,
					Gateway:   netip.MustParseAddr("10.20.0.1"),
					Metric:    0,
				},
			},
			preferred: []netlink.DefaultRoute{
				{
					Interface: "wws27u1i4",
					Family:    netlink.FamilyIPv4,
					Gateway:   netip.MustParseAddr("10.9.15.132"),
					Metric:    defaultRouteMetric,
				},
			},
			want: []defaultRouteChange{
				{
					Original: netlink.DefaultRoute{
						Interface: "ens18",
						Family:    netlink.FamilyIPv4,
						Gateway:   netip.MustParseAddr("10.10.10.201"),
						Metric:    0,
					},
					Replacement: netlink.DefaultRoute{
						Interface: "ens18",
						Family:    netlink.FamilyIPv4,
						Gateway:   netip.MustParseAddr("10.10.10.201"),
						Metric:    defaultRouteMetric + 1,
					},
				},
				{
					Original: netlink.DefaultRoute{
						Interface: "eth0",
						Family:    netlink.FamilyIPv4,
						Gateway:   netip.MustParseAddr("10.20.0.1"),
						Metric:    0,
					},
					Replacement: netlink.DefaultRoute{
						Interface: "eth0",
						Family:    netlink.FamilyIPv4,
						Gateway:   netip.MustParseAddr("10.20.0.1"),
						Metric:    defaultRouteMetric + 2,
					},
				},
			},
		},
		{
			name: "keeps preferred route already present",
			current: []netlink.DefaultRoute{
				{
					Interface: "ens18",
					Family:    netlink.FamilyIPv4,
					Gateway:   netip.MustParseAddr("10.10.10.201"),
					Metric:    0,
				},
				{
					Interface: "wws27u1i4",
					Family:    netlink.FamilyIPv4,
					Gateway:   netip.MustParseAddr("10.9.15.132"),
					Metric:    defaultRouteMetric,
				},
			},
			preferred: []netlink.DefaultRoute{
				{
					Interface: "wws27u1i4",
					Family:    netlink.FamilyIPv4,
					Gateway:   netip.MustParseAddr("10.9.15.132"),
					Metric:    defaultRouteMetric,
				},
			},
			want: []defaultRouteChange{
				{
					Original: netlink.DefaultRoute{
						Interface: "ens18",
						Family:    netlink.FamilyIPv4,
						Gateway:   netip.MustParseAddr("10.10.10.201"),
						Metric:    0,
					},
					Replacement: netlink.DefaultRoute{
						Interface: "ens18",
						Family:    netlink.FamilyIPv4,
						Gateway:   netip.MustParseAddr("10.10.10.201"),
						Metric:    defaultRouteMetric + 1,
					},
				},
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := defaultRouteChanges(tt.current, tt.preferred); !slices.Equal(got, tt.want) {
				t.Fatalf("defaultRouteChanges() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestSyncDefaultRouteTakeoverTransfersDemotedConnectionState(t *testing.T) {
	t.Parallel()

	originalGatewayRoute := netlink.DefaultRoute{
		Interface: "eth0",
		Family:    netlink.FamilyIPv4,
		Gateway:   netip.MustParseAddr("10.20.0.1"),
		Metric:    0,
	}
	demotedGatewayRoute := originalGatewayRoute
	demotedGatewayRoute.Metric = defaultRouteMetric + 1
	oldDefaultRoute := netlink.DefaultRoute{
		Interface: "wws27u1i4",
		Family:    netlink.FamilyIPv4,
		Gateway:   netip.MustParseAddr("10.9.15.132"),
		Metric:    defaultRouteMetric,
	}
	demotedOldRoute := oldDefaultRoute
	demotedOldRoute.Metric = defaultRouteMetric + 2
	newDefaultRoute := netlink.DefaultRoute{
		Interface: "wws27u2i4",
		Family:    netlink.FamilyIPv4,
		Gateway:   netip.MustParseAddr("10.8.15.132"),
		Metric:    defaultRouteMetric,
	}
	oldGatewayChange := defaultRouteChange{
		Original:    originalGatewayRoute,
		Replacement: demotedGatewayRoute,
	}
	oldRouteChange := defaultRouteChange{
		Original:    oldDefaultRoute,
		Replacement: demotedOldRoute,
	}

	tests := []struct {
		name string
		qmap bool
	}{
		{name: "normal bearer owner"},
		{name: "QMAP owner", qmap: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			state := testDBConnectionState(t)
			if err := state.saveRouteStateForModem(t.Context(), "old-modem", "wws27u1i4", []netlink.DefaultRoute{oldDefaultRoute}, []defaultRouteChange{oldGatewayChange}); err != nil {
				t.Fatalf("save old route state: %v", err)
			}
			if err := state.saveRouteStateForModem(t.Context(), "new-modem", "wws27u2i4", []netlink.DefaultRoute{newDefaultRoute}, []defaultRouteChange{oldRouteChange}); err != nil {
				t.Fatalf("save new route state: %v", err)
			}
			store := testStore(t)
			const oldProfileID = "8901000000000000001"
			oldTracked := trackedConnection{
				interfaceName: "wws27u1i4",
				profileID:     oldProfileID,
				prefs:         Preferences{DefaultRoute: true, AlwaysOn: true},
				routeMetric:   defaultRouteMetric,
				routes:        []netlink.DefaultRoute{oldDefaultRoute},
				routeChanges:  []defaultRouteChange{oldGatewayChange},
			}
			connections := map[string]trackedConnection{"old-modem": oldTracked}
			qmapConnections := make(map[string]*qmapConnection)
			if tt.qmap {
				delete(connections, "old-modem")
				qmapConnections["old-modem"] = &qmapConnection{tracked: oldTracked}
			}
			c := &Connector{
				connections:     connections,
				qmapConnections: qmapConnections,
				preferences: map[string]Preferences{
					"old-modem": {DefaultRoute: true, AlwaysOn: true},
				},
				state:       store,
				persistence: state,
			}
			storedTracked := func() trackedConnection {
				if tt.qmap {
					return c.qmapConnections["old-modem"].tracked
				}
				return c.connections["old-modem"]
			}
			tracked := trackedConnection{
				interfaceName: "wws27u2i4",
				profileID:     "8901000000000000002",
				prefs:         Preferences{DefaultRoute: true},
				routeMetric:   defaultRouteMetric,
				routes:        []netlink.DefaultRoute{newDefaultRoute},
				routeChanges:  []defaultRouteChange{oldRouteChange},
			}

			if err := c.syncAlwaysOnState(t.Context(), oldProfileID, Preferences{DefaultRoute: true, AlwaysOn: true}); err != nil {
				t.Fatalf("sync old always-on state: %v", err)
			}
			if err := c.syncDefaultRouteTakeover(t.Context(), "new-modem", &tracked); err != nil {
				t.Fatalf("syncDefaultRouteTakeover() error = %v", err)
			}

			gotTracked := storedTracked()
			if gotTracked.prefs.DefaultRoute {
				t.Fatal("old tracked DefaultRoute = true, want false")
			}
			if gotTracked.routeMetric != demotedOldRoute.Metric {
				t.Fatalf("old tracked routeMetric = %d, want %d", gotTracked.routeMetric, demotedOldRoute.Metric)
			}
			if !slices.Equal(gotTracked.routes, []netlink.DefaultRoute{demotedOldRoute}) {
				t.Fatalf("old tracked routes = %#v, want %#v", gotTracked.routes, []netlink.DefaultRoute{demotedOldRoute})
			}
			if !slices.Equal(gotTracked.routeChanges, []defaultRouteChange{oldGatewayChange}) {
				t.Fatalf("old tracked routeChanges = %#v, want %#v", gotTracked.routeChanges, []defaultRouteChange{oldGatewayChange})
			}
			if c.preferences["old-modem"].DefaultRoute {
				t.Fatal("old preference DefaultRoute = true, want false")
			}
			oldAlwaysOn, ok, err := c.loadAlwaysOnStateForProfile(t.Context(), oldProfileID)
			if err != nil || !ok || oldAlwaysOn.DefaultRoute {
				t.Fatalf("load old always-on after takeover = %#v, ok = %t, err = %v; want non-default", oldAlwaysOn, ok, err)
			}
			if !slices.Equal(tracked.routeChanges, []defaultRouteChange{oldRouteChange}) {
				t.Fatalf("new tracked routeChanges = %#v, want %#v", tracked.routeChanges, []defaultRouteChange{oldRouteChange})
			}
			gotOldChanges, ok, err := state.loadRouteStateForModem(t.Context(), "old-modem", "wws27u1i4")
			if err != nil || !ok || !slices.Equal(gotOldChanges, []defaultRouteChange{oldGatewayChange}) {
				t.Fatalf("loadRouteStateForModem(old) = %#v, ok = %t, err = %v; want %#v, true, nil", gotOldChanges, ok, err, []defaultRouteChange{oldGatewayChange})
			}
			gotChanges, ok, err := state.loadRouteStateForModem(t.Context(), "new-modem", "wws27u2i4")
			if err != nil || !ok || !slices.Equal(gotChanges, []defaultRouteChange{oldRouteChange}) {
				t.Fatalf("loadRouteStateForModem(new) = %#v, ok = %t, err = %v; want %#v, true, nil", gotChanges, ok, err, []defaultRouteChange{oldRouteChange})
			}

			if err := c.syncDefaultRouteRestore(t.Context(), tracked.routeChanges); err != nil {
				t.Fatalf("syncDefaultRouteRestore() error = %v", err)
			}
			gotTracked = storedTracked()
			if !gotTracked.prefs.DefaultRoute {
				t.Fatal("old tracked DefaultRoute after restore = false, want true")
			}
			if gotTracked.routeMetric != oldDefaultRoute.Metric {
				t.Fatalf("old tracked routeMetric after restore = %d, want %d", gotTracked.routeMetric, oldDefaultRoute.Metric)
			}
			if !slices.Equal(gotTracked.routes, []netlink.DefaultRoute{oldDefaultRoute}) {
				t.Fatalf("old tracked routes after restore = %#v, want %#v", gotTracked.routes, []netlink.DefaultRoute{oldDefaultRoute})
			}
			oldAlwaysOn, ok, err = c.loadAlwaysOnStateForProfile(t.Context(), oldProfileID)
			if err != nil || !ok || !oldAlwaysOn.DefaultRoute {
				t.Fatalf("load old always-on after restore = %#v, ok = %t, err = %v; want default", oldAlwaysOn, ok, err)
			}
		})
	}
}

func TestSyncDefaultRouteRemovalTransfersOriginalRouteState(t *testing.T) {
	t.Parallel()

	originalGatewayRoute := netlink.DefaultRoute{
		Interface: "eth0",
		Family:    netlink.FamilyIPv4,
		Gateway:   netip.MustParseAddr("10.20.0.1"),
		Metric:    0,
	}
	demotedGatewayRoute := originalGatewayRoute
	demotedGatewayRoute.Metric = defaultRouteMetric + 1
	oldDefaultRoute := netlink.DefaultRoute{
		Interface: "wws27u1i4",
		Family:    netlink.FamilyIPv4,
		Gateway:   netip.MustParseAddr("10.9.15.132"),
		Metric:    defaultRouteMetric,
	}
	demotedOldRoute := oldDefaultRoute
	demotedOldRoute.Metric = defaultRouteMetric + 2
	newDefaultRoute := netlink.DefaultRoute{
		Interface: "wws27u2i4",
		Family:    netlink.FamilyIPv4,
		Gateway:   netip.MustParseAddr("10.8.15.132"),
		Metric:    defaultRouteMetric,
	}
	oldGatewayChange := defaultRouteChange{
		Original:    originalGatewayRoute,
		Replacement: demotedGatewayRoute,
	}
	oldRouteChange := defaultRouteChange{
		Original:    oldDefaultRoute,
		Replacement: demotedOldRoute,
	}

	tests := []struct {
		name string
		qmap bool
	}{
		{name: "normal bearer owner"},
		{name: "QMAP owner", qmap: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			state := testDBConnectionState(t)
			if err := state.saveRouteStateForModem(t.Context(), "new-modem", "wws27u2i4", []netlink.DefaultRoute{newDefaultRoute}, []defaultRouteChange{oldRouteChange}); err != nil {
				t.Fatalf("save new route state: %v", err)
			}
			removed := trackedConnection{
				interfaceName: "wws27u1i4",
				prefs:         Preferences{AlwaysOn: true},
				routeMetric:   demotedOldRoute.Metric,
				routes:        []netlink.DefaultRoute{demotedOldRoute},
				routeChanges:  []defaultRouteChange{oldGatewayChange},
			}
			newTracked := trackedConnection{
				interfaceName: "wws27u2i4",
				prefs:         Preferences{DefaultRoute: true},
				routeMetric:   defaultRouteMetric,
				routes:        []netlink.DefaultRoute{newDefaultRoute},
				routeChanges:  []defaultRouteChange{oldRouteChange},
			}
			connections := map[string]trackedConnection{
				"old-modem": removed,
				"new-modem": newTracked,
			}
			qmapConnections := make(map[string]*qmapConnection)
			if tt.qmap {
				delete(connections, "new-modem")
				qmapConnections["new-modem"] = &qmapConnection{tracked: newTracked}
			}
			c := &Connector{
				connections:     connections,
				qmapConnections: qmapConnections,
				preferences: map[string]Preferences{
					"old-modem": {AlwaysOn: true},
					"new-modem": {DefaultRoute: true},
				},
				persistence: state,
			}

			if err := c.syncDefaultRouteRemoval(t.Context(), removed); err != nil {
				t.Fatalf("syncDefaultRouteRemoval() error = %v", err)
			}

			gotTracked := c.connections["new-modem"]
			if tt.qmap {
				gotTracked = c.qmapConnections["new-modem"].tracked
			}
			if !slices.Equal(gotTracked.routeChanges, []defaultRouteChange{oldGatewayChange}) {
				t.Fatalf("new tracked routeChanges = %#v, want %#v", gotTracked.routeChanges, []defaultRouteChange{oldGatewayChange})
			}
			gotChanges, ok, err := state.loadRouteStateForModem(t.Context(), "new-modem", "wws27u2i4")
			if err != nil || !ok || !slices.Equal(gotChanges, []defaultRouteChange{oldGatewayChange}) {
				t.Fatalf("loadRouteStateForModem(new) = %#v, ok = %t, err = %v; want %#v, true, nil", gotChanges, ok, err, []defaultRouteChange{oldGatewayChange})
			}
		})
	}
}

func TestTakeoverDefaultRoutesKeepsStateWhenRollbackFails(t *testing.T) {
	t.Parallel()

	errAddFallback := errors.New("add fallback route")
	errRestoreOriginal := errors.New("restore original route")
	original := netlink.DefaultRoute{
		Interface: "ens18",
		Family:    netlink.FamilyIPv4,
		Gateway:   netip.MustParseAddr("10.10.10.201"),
		Metric:    0,
	}
	preferred := []netlink.DefaultRoute{
		{
			Interface: "wws27u1i4",
			Family:    netlink.FamilyIPv4,
			Gateway:   netip.MustParseAddr("10.9.15.132"),
			Metric:    defaultRouteMetric,
		},
	}
	replacement := original
	replacement.Metric = defaultRouteMetric + 1

	tests := []struct {
		name       string
		restoreErr error
		wantState  bool
	}{
		{name: "delete state after rollback succeeds"},
		{name: "keep state after rollback fails", restoreErr: errRestoreOriginal, wantState: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			state := testDBConnectionState(t)
			ops := defaultRouteOps{
				defaultRoutes: func() ([]netlink.DefaultRoute, error) {
					return []netlink.DefaultRoute{original}, nil
				},
				deleteDefaultRoute: func(route netlink.DefaultRoute) error {
					return nil
				},
				addDefaultRoute: func(route netlink.DefaultRoute) error {
					switch {
					case sameDefaultRoute(route, replacement):
						return errAddFallback
					case sameDefaultRoute(route, original):
						return tt.restoreErr
					default:
						return nil
					}
				},
			}

			if _, err := takeoverDefaultRoutesWithState(t.Context(), state, "modem-1", "wws27u1i4", preferred, ops); err == nil {
				t.Fatal("takeoverDefaultRoutesWithState() error = nil, want error")
			}
			_, ok, err := loadRouteState(t.Context(), state, "wws27u1i4")
			if err != nil {
				t.Fatalf("loadRouteState() error = %v", err)
			}
			if ok != tt.wantState {
				t.Fatalf("loadRouteState() ok = %t, want %t", ok, tt.wantState)
			}
		})
	}
}

func TestTakeoverDefaultRoutesReportsStateCleanupError(t *testing.T) {
	t.Parallel()

	errDeleteOriginal := errors.New("delete original route")
	errDeleteState := errors.New("delete route state")
	original := netlink.DefaultRoute{
		Interface: "ens18",
		Family:    netlink.FamilyIPv4,
		Gateway:   netip.MustParseAddr("10.10.10.201"),
		Metric:    0,
	}
	preferred := []netlink.DefaultRoute{
		{
			Interface: "wws27u1i4",
			Family:    netlink.FamilyIPv4,
			Gateway:   netip.MustParseAddr("10.9.15.132"),
			Metric:    defaultRouteMetric,
		},
	}

	tests := []struct {
		name string
	}{
		{name: "delete state failure is reported"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			state := deleteRouteStateErrorStore{
				connectionStateStore: testDBConnectionState(t),
				err:                  errDeleteState,
			}
			ops := defaultRouteOps{
				defaultRoutes: func() ([]netlink.DefaultRoute, error) {
					return []netlink.DefaultRoute{original}, nil
				},
				deleteDefaultRoute: func(route netlink.DefaultRoute) error {
					return errDeleteOriginal
				},
			}

			_, err := takeoverDefaultRoutesWithState(t.Context(), state, "modem-1", "wws27u1i4", preferred, ops)
			if err == nil {
				t.Fatal("takeoverDefaultRoutesWithState() error = nil, want error")
			}
			if !errors.Is(err, errDeleteOriginal) {
				t.Fatalf("takeoverDefaultRoutesWithState() error = %v, want %v", err, errDeleteOriginal)
			}
			if !errors.Is(err, errDeleteState) {
				t.Fatalf("takeoverDefaultRoutesWithState() error = %v, want %v", err, errDeleteState)
			}
			if !strings.Contains(err.Error(), "delete default route state") {
				t.Fatalf("takeoverDefaultRoutesWithState() error = %v, want state cleanup context", err)
			}
		})
	}
}

type deleteRouteStateErrorStore struct {
	connectionStateStore
	err error
}

func (s deleteRouteStateErrorStore) deleteRouteState(context.Context, string) error {
	return s.err
}

func TestTakeoverDefaultRoutesKeepsUnrestoredChangeInCleanup(t *testing.T) {
	t.Parallel()

	errAddFallback := errors.New("add fallback route")
	errRestoreOriginal := errors.New("restore original route")
	firstOriginal := netlink.DefaultRoute{
		Interface: "ens18",
		Family:    netlink.FamilyIPv4,
		Gateway:   netip.MustParseAddr("10.10.10.201"),
		Metric:    0,
	}
	secondOriginal := netlink.DefaultRoute{
		Interface: "eth0",
		Family:    netlink.FamilyIPv4,
		Gateway:   netip.MustParseAddr("10.20.0.1"),
		Metric:    0,
	}
	firstReplacement := firstOriginal
	firstReplacement.Metric = defaultRouteMetric + 1
	secondReplacement := secondOriginal
	secondReplacement.Metric = defaultRouteMetric + 2
	preferred := []netlink.DefaultRoute{
		{
			Interface: "wws27u1i4",
			Family:    netlink.FamilyIPv4,
			Gateway:   netip.MustParseAddr("10.9.15.132"),
			Metric:    defaultRouteMetric,
		},
	}
	wantChanges := []defaultRouteChange{
		{Original: firstOriginal, Replacement: firstReplacement},
		{Original: secondOriginal, Replacement: secondReplacement},
	}

	tests := []struct {
		name             string
		restoreFailures  int
		wantCleanupError bool
		wantState        bool
	}{
		{
			name:             "keep state when cleanup cannot restore deleted route",
			restoreFailures:  2,
			wantCleanupError: true,
			wantState:        true,
		},
		{
			name:            "delete state after cleanup restores deleted route",
			restoreFailures: 1,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			state := testDBConnectionState(t)
			restoreAttempts := 0
			ops := defaultRouteOps{
				defaultRoutes: func() ([]netlink.DefaultRoute, error) {
					return []netlink.DefaultRoute{firstOriginal, secondOriginal}, nil
				},
				deleteDefaultRoute: func(route netlink.DefaultRoute) error {
					return nil
				},
				addDefaultRoute: func(route netlink.DefaultRoute) error {
					switch {
					case sameDefaultRoute(route, secondReplacement):
						return errAddFallback
					case sameDefaultRoute(route, secondOriginal):
						restoreAttempts++
						if restoreAttempts <= tt.restoreFailures {
							return errRestoreOriginal
						}
					}
					return nil
				},
			}

			gotChanges, err := takeoverDefaultRoutesWithState(t.Context(), state, "modem-1", "wws27u1i4", preferred, ops)
			if err == nil {
				t.Fatal("takeoverDefaultRoutesWithState() error = nil, want error")
			}
			if !slices.Equal(gotChanges, wantChanges) {
				t.Fatalf("takeoverDefaultRoutesWithState() changes = %#v, want %#v", gotChanges, wantChanges)
			}

			cleanupErr := cleanupDefaultRouteChanges(t.Context(), state, "wws27u1i4", gotChanges, ops)
			if (cleanupErr != nil) != tt.wantCleanupError {
				t.Fatalf("cleanupDefaultRouteChanges() error = %v, want error %t", cleanupErr, tt.wantCleanupError)
			}
			_, ok, err := loadRouteState(t.Context(), state, "wws27u1i4")
			if err != nil {
				t.Fatalf("loadRouteState() error = %v", err)
			}
			if ok != tt.wantState {
				t.Fatalf("loadRouteState() ok = %t, want %t", ok, tt.wantState)
			}
		})
	}
}

func TestRestoreDefaultRoutesKeepsReplacementWhenOriginalRestoreFails(t *testing.T) {
	t.Parallel()

	errRestoreOriginal := errors.New("restore original route")
	original := netlink.DefaultRoute{
		Interface: "ens18",
		Family:    netlink.FamilyIPv4,
		Gateway:   netip.MustParseAddr("10.10.10.201"),
		Metric:    0,
	}
	replacement := original
	replacement.Metric = defaultRouteMetric + 1

	tests := []struct {
		name        string
		restoreErr  error
		wantErr     bool
		wantDeleted []netlink.DefaultRoute
	}{
		{
			name:       "keep replacement when restore fails",
			restoreErr: errRestoreOriginal,
			wantErr:    true,
		},
		{
			name:        "delete replacement after restore succeeds",
			wantDeleted: []netlink.DefaultRoute{replacement},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var deleted []netlink.DefaultRoute
			ops := defaultRouteOps{
				addDefaultRoute: func(route netlink.DefaultRoute) error {
					if sameDefaultRoute(route, original) {
						return tt.restoreErr
					}
					return nil
				},
				deleteDefaultRoute: func(route netlink.DefaultRoute) error {
					deleted = append(deleted, route)
					return nil
				},
			}

			err := restoreDefaultRoutesWithOps([]defaultRouteChange{
				{Original: original, Replacement: replacement},
			}, ops)
			if (err != nil) != tt.wantErr {
				t.Fatalf("restoreDefaultRoutesWithOps() error = %v, want error %t", err, tt.wantErr)
			}
			if !slices.Equal(deleted, tt.wantDeleted) {
				t.Fatalf("deleted routes = %#v, want %#v", deleted, tt.wantDeleted)
			}
		})
	}
}

func TestRestoreStaleDefaultRouteStates(t *testing.T) {
	t.Parallel()

	original := netlink.DefaultRoute{
		Interface: "ens18",
		Family:    netlink.FamilyIPv4,
		Gateway:   netip.MustParseAddr("10.10.10.201"),
		Metric:    0,
	}
	replacement := original
	replacement.Metric = defaultRouteMetric + 1
	preferred := []netlink.DefaultRoute{
		{
			Interface: "wws27u1i4",
			Family:    netlink.FamilyIPv4,
			Gateway:   netip.MustParseAddr("10.9.15.132"),
			Metric:    defaultRouteMetric,
		},
	}
	changes := []defaultRouteChange{
		{
			Original:    original,
			Replacement: replacement,
		},
	}

	tests := []struct {
		name        string
		target      routeStateRestoreTarget
		modemID     string
		current     []netlink.DefaultRoute
		wantDeleted []netlink.DefaultRoute
		wantAdded   []netlink.DefaultRoute
		wantState   bool
	}{
		{
			name:        "restore when preferred route is absent",
			current:     []netlink.DefaultRoute{replacement},
			wantDeleted: []netlink.DefaultRoute{replacement},
			wantAdded:   []netlink.DefaultRoute{original},
		},
		{
			name:      "skip unscoped restore when preferred route remains",
			current:   preferred,
			wantState: true,
		},
		{
			name:        "restore scoped interface when stale preferred route remains",
			target:      routeStateRestoreTarget{interfaceNames: []string{"wws27u1i4"}},
			current:     preferred,
			wantDeleted: []netlink.DefaultRoute{preferred[0], replacement},
			wantAdded:   []netlink.DefaultRoute{original},
		},
		{
			name:        "restore scoped modem when stale preferred route remains",
			target:      routeStateRestoreTarget{modemID: "modem-1"},
			modemID:     "modem-1",
			current:     preferred,
			wantDeleted: []netlink.DefaultRoute{preferred[0], replacement},
			wantAdded:   []netlink.DefaultRoute{original},
		},
		{
			name:      "skip interface fallback when state belongs to another modem",
			target:    routeStateRestoreTarget{modemID: "modem-1", interfaceNames: []string{"wws27u1i4"}},
			modemID:   "modem-2",
			current:   []netlink.DefaultRoute{preferred[0], replacement},
			wantState: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			state := testDBConnectionState(t)
			stateModemID := tt.modemID
			if stateModemID == "" {
				stateModemID = "modem-1"
			}
			if err := state.saveRouteStateForModem(t.Context(), stateModemID, "wws27u1i4", preferred, changes); err != nil {
				t.Fatalf("saveRouteState() error = %v", err)
			}
			var deleted []netlink.DefaultRoute
			var added []netlink.DefaultRoute
			ops := defaultRouteOps{
				defaultRoutes: func() ([]netlink.DefaultRoute, error) {
					return tt.current, nil
				},
				deleteDefaultRoute: func(route netlink.DefaultRoute) error {
					deleted = append(deleted, route)
					return nil
				},
				addDefaultRoute: func(route netlink.DefaultRoute) error {
					added = append(added, route)
					return nil
				},
			}

			if err := restoreStaleDefaultRouteStatesWithState(t.Context(), state, tt.target, ops); err != nil {
				t.Fatalf("restoreStaleDefaultRouteStatesWithState() error = %v", err)
			}
			if !slices.Equal(deleted, tt.wantDeleted) {
				t.Fatalf("deleted routes = %#v, want %#v", deleted, tt.wantDeleted)
			}
			if !slices.Equal(added, tt.wantAdded) {
				t.Fatalf("added routes = %#v, want %#v", added, tt.wantAdded)
			}
			_, ok, err := loadRouteState(t.Context(), state, "wws27u1i4")
			if err != nil {
				t.Fatalf("loadRouteState() error = %v", err)
			}
			if ok != tt.wantState {
				t.Fatalf("loadRouteState() ok = %t, want %t", ok, tt.wantState)
			}
		})
	}
}

func TestRestoreStaleDefaultRouteStatesScopesModem(t *testing.T) {
	t.Parallel()

	firstOriginal := netlink.DefaultRoute{
		Interface: "ens18",
		Family:    netlink.FamilyIPv4,
		Gateway:   netip.MustParseAddr("10.10.10.201"),
		Metric:    0,
	}
	firstReplacement := firstOriginal
	firstReplacement.Metric = defaultRouteMetric + 1
	firstPreferred := []netlink.DefaultRoute{
		{
			Interface: "wws0",
			Family:    netlink.FamilyIPv4,
			Gateway:   netip.MustParseAddr("10.9.15.132"),
			Metric:    defaultRouteMetric,
		},
	}
	firstChanges := []defaultRouteChange{
		{Original: firstOriginal, Replacement: firstReplacement},
	}

	secondOriginal := netlink.DefaultRoute{
		Interface: "eth0",
		Family:    netlink.FamilyIPv4,
		Gateway:   netip.MustParseAddr("10.20.0.1"),
		Metric:    0,
	}
	secondReplacement := secondOriginal
	secondReplacement.Metric = defaultRouteMetric + 2
	secondPreferred := []netlink.DefaultRoute{
		{
			Interface: "wws1",
			Family:    netlink.FamilyIPv4,
			Gateway:   netip.MustParseAddr("10.8.0.1"),
			Metric:    defaultRouteMetric,
		},
	}
	secondChanges := []defaultRouteChange{
		{Original: secondOriginal, Replacement: secondReplacement},
	}

	otherOriginal := netlink.DefaultRoute{
		Interface: "lan0",
		Family:    netlink.FamilyIPv4,
		Gateway:   netip.MustParseAddr("10.30.0.1"),
		Metric:    0,
	}
	otherReplacement := otherOriginal
	otherReplacement.Metric = defaultRouteMetric + 3
	otherPreferred := []netlink.DefaultRoute{
		{
			Interface: "wws2",
			Family:    netlink.FamilyIPv4,
			Gateway:   netip.MustParseAddr("10.7.0.1"),
			Metric:    defaultRouteMetric,
		},
	}
	otherChanges := []defaultRouteChange{
		{Original: otherOriginal, Replacement: otherReplacement},
	}

	tests := []struct {
		name string
	}{
		{name: "restore all entries owned by modem"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			state := testDBConnectionState(t)
			if err := state.saveRouteStateForModem(t.Context(), "modem-1", "wws0", firstPreferred, firstChanges); err != nil {
				t.Fatalf("saveRouteStateForModem(wws0) error = %v", err)
			}
			if err := state.saveRouteStateForModem(t.Context(), "modem-1", "wws1", secondPreferred, secondChanges); err != nil {
				t.Fatalf("saveRouteStateForModem(wws1) error = %v", err)
			}
			if err := state.saveRouteStateForModem(t.Context(), "modem-2", "wws2", otherPreferred, otherChanges); err != nil {
				t.Fatalf("saveRouteStateForModem(wws2) error = %v", err)
			}
			var deleted []netlink.DefaultRoute
			var added []netlink.DefaultRoute
			ops := defaultRouteOps{
				defaultRoutes: func() ([]netlink.DefaultRoute, error) {
					return []netlink.DefaultRoute{
						firstPreferred[0], firstReplacement,
						secondPreferred[0], secondReplacement,
						otherPreferred[0], otherReplacement,
					}, nil
				},
				deleteDefaultRoute: func(route netlink.DefaultRoute) error {
					deleted = append(deleted, route)
					return nil
				},
				addDefaultRoute: func(route netlink.DefaultRoute) error {
					added = append(added, route)
					return nil
				},
			}

			if err := restoreStaleDefaultRouteStatesWithState(t.Context(), state, routeStateRestoreTarget{modemID: "modem-1"}, ops); err != nil {
				t.Fatalf("restoreStaleDefaultRouteStatesWithState() error = %v", err)
			}
			wantDeleted := []netlink.DefaultRoute{firstPreferred[0], firstReplacement, secondPreferred[0], secondReplacement}
			if !slices.Equal(deleted, wantDeleted) {
				t.Fatalf("deleted routes = %#v, want %#v", deleted, wantDeleted)
			}
			wantAdded := []netlink.DefaultRoute{firstOriginal, secondOriginal}
			if !slices.Equal(added, wantAdded) {
				t.Fatalf("added routes = %#v, want %#v", added, wantAdded)
			}
			if _, ok, err := loadRouteState(t.Context(), state, "wws0"); err != nil || ok {
				t.Fatalf("loadRouteState(wws0) ok = %t, err = %v; want false, nil", ok, err)
			}
			if _, ok, err := loadRouteState(t.Context(), state, "wws1"); err != nil || ok {
				t.Fatalf("loadRouteState(wws1) ok = %t, err = %v; want false, nil", ok, err)
			}
			if got, ok, err := loadRouteState(t.Context(), state, "wws2"); err != nil || !ok || !slices.Equal(got, otherChanges) {
				t.Fatalf("loadRouteState(wws2) = %#v, ok = %t, err = %v; want %#v, true, nil", got, ok, err, otherChanges)
			}
		})
	}
}

func TestRestoreStaleDefaultRouteStatesScopesInterfaces(t *testing.T) {
	t.Parallel()

	firstOriginal := netlink.DefaultRoute{
		Interface: "ens18",
		Family:    netlink.FamilyIPv4,
		Gateway:   netip.MustParseAddr("10.10.10.201"),
		Metric:    0,
	}
	firstReplacement := firstOriginal
	firstReplacement.Metric = defaultRouteMetric + 1
	firstPreferred := []netlink.DefaultRoute{
		{
			Interface: "wws0",
			Family:    netlink.FamilyIPv4,
			Gateway:   netip.MustParseAddr("10.9.15.132"),
			Metric:    defaultRouteMetric,
		},
	}
	firstChanges := []defaultRouteChange{
		{Original: firstOriginal, Replacement: firstReplacement},
	}

	secondOriginal := netlink.DefaultRoute{
		Interface: "eth0",
		Family:    netlink.FamilyIPv4,
		Gateway:   netip.MustParseAddr("10.20.0.1"),
		Metric:    0,
	}
	secondReplacement := secondOriginal
	secondReplacement.Metric = defaultRouteMetric + 1
	secondPreferred := []netlink.DefaultRoute{
		{
			Interface: "wws1",
			Family:    netlink.FamilyIPv4,
			Gateway:   netip.MustParseAddr("10.8.0.1"),
			Metric:    defaultRouteMetric,
		},
	}
	secondChanges := []defaultRouteChange{
		{Original: secondOriginal, Replacement: secondReplacement},
	}

	tests := []struct {
		name string
	}{
		{name: "only target interface"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			state := testDBConnectionState(t)
			if err := saveRouteState(t.Context(), state, "wws0", firstPreferred, firstChanges); err != nil {
				t.Fatalf("saveRouteState(wws0) error = %v", err)
			}
			if err := saveRouteState(t.Context(), state, "wws1", secondPreferred, secondChanges); err != nil {
				t.Fatalf("saveRouteState(wws1) error = %v", err)
			}
			var deleted []netlink.DefaultRoute
			var added []netlink.DefaultRoute
			ops := defaultRouteOps{
				defaultRoutes: func() ([]netlink.DefaultRoute, error) {
					return []netlink.DefaultRoute{firstPreferred[0], firstReplacement, secondPreferred[0], secondReplacement}, nil
				},
				deleteDefaultRoute: func(route netlink.DefaultRoute) error {
					deleted = append(deleted, route)
					return nil
				},
				addDefaultRoute: func(route netlink.DefaultRoute) error {
					added = append(added, route)
					return nil
				},
			}

			if err := restoreStaleDefaultRouteStatesWithState(t.Context(), state, routeStateRestoreTarget{interfaceNames: []string{"wws0"}}, ops); err != nil {
				t.Fatalf("restoreStaleDefaultRouteStatesWithState() error = %v", err)
			}
			if want := []netlink.DefaultRoute{firstPreferred[0], firstReplacement}; !slices.Equal(deleted, want) {
				t.Fatalf("deleted routes = %#v, want %#v", deleted, want)
			}
			if want := []netlink.DefaultRoute{firstOriginal}; !slices.Equal(added, want) {
				t.Fatalf("added routes = %#v, want %#v", added, want)
			}
			if _, ok, err := loadRouteState(t.Context(), state, "wws0"); err != nil || ok {
				t.Fatalf("loadRouteState(wws0) ok = %t, err = %v; want false, nil", ok, err)
			}
			if got, ok, err := loadRouteState(t.Context(), state, "wws1"); err != nil || !ok || !slices.Equal(got, secondChanges) {
				t.Fatalf("loadRouteState(wws1) = %#v, ok = %t, err = %v; want %#v, true, nil", got, ok, err, secondChanges)
			}
		})
	}
}

func TestRestoreStaleDefaultRouteStateDeletesPreferredBeforeRestore(t *testing.T) {
	t.Parallel()

	original := netlink.DefaultRoute{
		Interface: "ens18",
		Family:    netlink.FamilyIPv4,
		Gateway:   netip.MustParseAddr("10.10.10.201"),
		Metric:    defaultRouteMetric,
	}
	replacement := original
	replacement.Metric = defaultRouteMetric + 1
	preferred := []netlink.DefaultRoute{
		{
			Interface: "wws27u1i4",
			Family:    netlink.FamilyIPv4,
			Gateway:   netip.MustParseAddr("10.9.15.132"),
			Metric:    defaultRouteMetric,
		},
	}
	changes := []defaultRouteChange{
		{
			Original:    original,
			Replacement: replacement,
		},
	}

	tests := []struct {
		name string
	}{
		{name: "same metric conflict"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			state := testDBConnectionState(t)
			if err := saveRouteState(t.Context(), state, "wws27u1i4", preferred, changes); err != nil {
				t.Fatalf("saveRouteState() error = %v", err)
			}
			preferredDeleted := false
			var deleted []netlink.DefaultRoute
			var added []netlink.DefaultRoute
			ops := defaultRouteOps{
				defaultRoutes: func() ([]netlink.DefaultRoute, error) {
					return []netlink.DefaultRoute{preferred[0], replacement}, nil
				},
				deleteDefaultRoute: func(route netlink.DefaultRoute) error {
					deleted = append(deleted, route)
					if sameDefaultRoute(route, preferred[0]) {
						preferredDeleted = true
					}
					return nil
				},
				addDefaultRoute: func(route netlink.DefaultRoute) error {
					if sameDefaultRoute(route, original) && !preferredDeleted {
						return fmt.Errorf("%w: stale preferred route", netlink.ErrDefaultRouteExists)
					}
					added = append(added, route)
					return nil
				},
			}

			if err := restoreStaleDefaultRouteStatesWithState(t.Context(), state, routeStateRestoreTarget{interfaceNames: []string{"wws27u1i4"}}, ops); err != nil {
				t.Fatalf("restoreStaleDefaultRouteStatesWithState() error = %v", err)
			}
			if want := []netlink.DefaultRoute{preferred[0], replacement}; !slices.Equal(deleted, want) {
				t.Fatalf("deleted routes = %#v, want %#v", deleted, want)
			}
			if want := []netlink.DefaultRoute{original}; !slices.Equal(added, want) {
				t.Fatalf("added routes = %#v, want %#v", added, want)
			}
			_, ok, err := loadRouteState(t.Context(), state, "wws27u1i4")
			if err != nil {
				t.Fatalf("loadRouteState() error = %v", err)
			}
			if ok {
				t.Fatal("loadRouteState() ok = true, want false")
			}
		})
	}
}

func TestRestoreOriginalDefaultRouteConfirmsExistingRoute(t *testing.T) {
	t.Parallel()

	original := netlink.DefaultRoute{
		Interface: "ens18",
		Family:    netlink.FamilyIPv4,
		Gateway:   netip.MustParseAddr("10.10.10.201"),
		Metric:    defaultRouteMetric,
	}
	conflict := netlink.DefaultRoute{
		Interface: "wws27u1i4",
		Family:    netlink.FamilyIPv4,
		Gateway:   netip.MustParseAddr("10.9.15.132"),
		Metric:    defaultRouteMetric,
	}
	wrongProtocol := original
	wrongProtocol.Protocol = 99
	originalWithProtocol := original
	originalWithProtocol.Protocol = 100

	tests := []struct {
		route   netlink.DefaultRoute
		name    string
		current []netlink.DefaultRoute
		wantErr bool
	}{
		{name: "original exists", route: original, current: []netlink.DefaultRoute{original}},
		{name: "only conflicting route exists", route: original, current: []netlink.DefaultRoute{conflict}, wantErr: true},
		{name: "same route with wrong protocol", route: originalWithProtocol, current: []netlink.DefaultRoute{wrongProtocol}, wantErr: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ops := defaultRouteOps{
				defaultRoutes: func() ([]netlink.DefaultRoute, error) {
					return tt.current, nil
				},
				addDefaultRoute: func(route netlink.DefaultRoute) error {
					return fmt.Errorf("%w: conflict", netlink.ErrDefaultRouteExists)
				},
			}

			err := restoreOriginalDefaultRouteWithOps(tt.route, ops)
			if (err != nil) != tt.wantErr {
				t.Fatalf("restoreOriginalDefaultRouteWithOps() error = %v, want error %t", err, tt.wantErr)
			}
		})
	}
}

func TestConnectionAddressStrings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		ip4      mmodem.BearerIPConfig
		ip6      mmodem.BearerIPConfig
		wantIPv4 []string
		wantIPv6 []string
		wantErr  bool
	}{
		{
			name: "static ipv4 and ipv6",
			ip4: mmodem.BearerIPConfig{
				Method:  mmodem.BearerIPMethodStatic,
				Address: "10.0.0.2",
				Prefix:  30,
			},
			ip6: mmodem.BearerIPConfig{
				Method:  mmodem.BearerIPMethodStatic,
				Address: "2001:db8::2",
				Prefix:  64,
			},
			wantIPv4: []string{"10.0.0.2/30"},
			wantIPv6: []string{"2001:db8::2/64"},
		},
		{
			name: "no static address",
			ip4: mmodem.BearerIPConfig{
				Method: mmodem.BearerIPMethodDHCP,
			},
			wantIPv4: []string{},
			wantIPv6: []string{},
		},
		{
			name: "dhcp method with configured address",
			ip4: mmodem.BearerIPConfig{
				Method:  mmodem.BearerIPMethodDHCP,
				Address: "10.0.0.2",
				Prefix:  30,
			},
			wantIPv4: []string{"10.0.0.2/30"},
			wantIPv6: []string{},
		},
		{
			name: "invalid static address",
			ip4: mmodem.BearerIPConfig{
				Method:  mmodem.BearerIPMethodStatic,
				Address: "not-an-ip",
				Prefix:  24,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotIPv4, gotIPv6, err := connectionAddressStrings(tt.ip4, tt.ip6)
			if tt.wantErr {
				if err == nil {
					t.Fatal("connectionAddressStrings() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("connectionAddressStrings() error = %v", err)
			}
			if !slices.Equal(gotIPv4, tt.wantIPv4) {
				t.Fatalf("connectionAddressStrings() ipv4 = %#v, want %#v", gotIPv4, tt.wantIPv4)
			}
			if !slices.Equal(gotIPv6, tt.wantIPv6) {
				t.Fatalf("connectionAddressStrings() ipv6 = %#v, want %#v", gotIPv6, tt.wantIPv6)
			}
		})
	}
}

func TestRouteStateFromRoutes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		routes []netlink.DefaultRoute
		want   recoveredRoute
	}{
		{
			name: "default metric",
			routes: []netlink.DefaultRoute{
				{Interface: "wwan0", Metric: defaultRouteMetric},
			},
			want: recoveredRoute{Found: true, Metric: defaultRouteMetric, DefaultRoute: true},
		},
		{
			name: "secondary metric",
			routes: []netlink.DefaultRoute{
				{Interface: "wwan0", Metric: secondaryRouteMetric},
			},
			want: recoveredRoute{Found: true, Metric: secondaryRouteMetric, DefaultRoute: false},
		},
		{
			name: "lowest metric on interface wins",
			routes: []netlink.DefaultRoute{
				{Interface: "wwan0", Metric: secondaryRouteMetric},
				{Interface: "wwan0", Metric: defaultRouteMetric},
			},
			want: recoveredRoute{Found: true, Metric: defaultRouteMetric, DefaultRoute: true},
		},
		{
			name: "missing interface",
			routes: []netlink.DefaultRoute{
				{Interface: "eth0", Metric: defaultRouteMetric},
			},
			want: recoveredRoute{},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := routeStateFromRoutes(tt.routes, "wwan0")
			if got != tt.want {
				t.Fatalf("routeStateFromRoutes() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestConnectorRequiresModem(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		run  func(context.Context, *Connector) error
	}{
		{
			name: "current",
			run: func(ctx context.Context, connector *Connector) error {
				_, err := connector.Current(ctx, nil)
				return err
			},
		},
		{
			name: "connect",
			run: func(ctx context.Context, connector *Connector) error {
				_, err := connector.Connect(ctx, nil, Preferences{})
				return err
			},
		},
		{
			name: "disconnect",
			run: func(ctx context.Context, connector *Connector) error {
				return connector.Disconnect(ctx, nil)
			},
		},
		{
			name: "restore",
			run: func(ctx context.Context, connector *Connector) error {
				return connector.Restore(ctx, nil)
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			connector, err := NewConnector(ConnectorConfig{
				State: testStore(t),
			})
			if err != nil {
				t.Fatalf("NewConnector() error = %v", err)
			}
			if err := tt.run(t.Context(), connector); !errors.Is(err, ErrModemRequired) {
				t.Fatalf("%s error = %v, want %v", tt.name, err, ErrModemRequired)
			}
		})
	}
}

func TestConnectRejectsInvalidPreferencesBeforeReadingBearers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		prefs Preferences
		err   error
	}{
		{name: "IP type", prefs: Preferences{IPType: "ipx"}, err: mmodem.ErrUnsupportedBearerIPType},
		{name: "APN authentication", prefs: Preferences{APNAuth: "oauth"}, err: mmodem.ErrUnsupportedBearerAuth},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			connector, err := NewConnector(ConnectorConfig{State: testStore(t)})
			if err != nil {
				t.Fatalf("NewConnector() error = %v", err)
			}
			bearerReads := 0
			_, err = connector.connect(t.Context(), fakeInternetModem{modemID: "modem-1", bearerReads: &bearerReads}, tt.prefs, true)
			if !errors.Is(err, tt.err) {
				t.Fatalf("connect() error = %v, want %v", err, tt.err)
			}
			if bearerReads != 0 {
				t.Fatalf("connect() read bearers %d times before validating preferences", bearerReads)
			}
		})
	}
}

func TestConnectPreparesBearerDataPath(t *testing.T) {
	errPrepare := errors.New("prepare data format")
	errConnect := errors.New("connect bearer")

	tests := []struct {
		name             string
		qualcomm410      bool
		prepareErr       error
		leaseErr         error
		wantErr          error
		wantPrepareCalls int
		wantLeaseCalls   int
		wantLeaseHeld    bool
		wantConnect      bool
	}{
		{
			name:             "preparation failure stops normal bearer",
			prepareErr:       errPrepare,
			wantErr:          errPrepare,
			wantPrepareCalls: 1,
		},
		{
			name:             "normal bearer connects after preparation",
			wantErr:          errConnect,
			wantPrepareCalls: 1,
			wantConnect:      true,
		},
		{
			name:           "Qualcomm 410 leases data format before bearer",
			qualcomm410:    true,
			prepareErr:     errPrepare,
			wantErr:        errConnect,
			wantLeaseCalls: 1,
			wantLeaseHeld:  true,
			wantConnect:    true,
		},
		{
			name:           "Qualcomm 410 lease failure stops bearer",
			qualcomm410:    true,
			leaseErr:       errPrepare,
			wantErr:        errPrepare,
			wantLeaseCalls: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			connector, err := NewConnector(ConnectorConfig{State: testStore(t)})
			if err != nil {
				t.Fatalf("NewConnector() error = %v", err)
			}
			if tt.qualcomm410 {
				connector.setQualcomm410State("modem-1", qualcomm410State{selected: true})
			}

			prepareCalls := 0
			leaseCalls := 0
			connectCalls := 0
			lease := &qualcomm410LeaseProbe{}
			connector.qualcomm410.openLease = func(context.Context) (qualcomm410DataFormatLease, error) {
				leaseCalls++
				if connectCalls != 0 {
					t.Fatal("Qualcomm 410 lease opened after bearer connection started")
				}
				if tt.leaseErr != nil {
					return nil, tt.leaseErr
				}
				return lease, nil
			}
			modem := fakeInternetModem{
				modemID:      "modem-1",
				prepareErr:   tt.prepareErr,
				prepareCalls: &prepareCalls,
				connectErr:   errConnect,
				connectCalls: &connectCalls,
			}
			_, err = connector.connect(t.Context(), modem, Preferences{APN: "internet", IPType: "ipv4"}, true)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("connect() error = %v, want %v", err, tt.wantErr)
			}
			if prepareCalls != tt.wantPrepareCalls {
				t.Fatalf("prepareBearerDataFormat() calls = %d, want %d", prepareCalls, tt.wantPrepareCalls)
			}
			if leaseCalls != tt.wantLeaseCalls {
				t.Fatalf("Qualcomm 410 lease open calls = %d, want %d", leaseCalls, tt.wantLeaseCalls)
			}
			if got := connectCalls > 0; got != tt.wantConnect {
				t.Fatalf("connectBearer() called = %t, want %t", got, tt.wantConnect)
			}
			if tt.qualcomm410 {
				state := connector.qualcomm410StateFor("modem-1")
				if got := state.lease == lease; got != tt.wantLeaseHeld {
					t.Fatalf("Qualcomm 410 lease held after connect() = %t, want %t", got, tt.wantLeaseHeld)
				}
			}
		})
	}
}

func TestConnectorRecoverSkipsSavedAirplaneMode(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := testStore(t)
	networkPreferences, err := networkprefs.New(store)
	if err != nil {
		t.Fatalf("networkprefs.New() error = %v", err)
	}
	if err := networkPreferences.SaveAirplaneMode(ctx, "modem-1", true); err != nil {
		t.Fatalf("SaveAirplaneMode() error = %v", err)
	}
	connector, err := NewConnector(ConnectorConfig{
		State:              store,
		NetworkPreferences: networkPreferences,
	})
	if err != nil {
		t.Fatalf("NewConnector() error = %v", err)
	}

	modem := &mmodem.Modem{EquipmentIdentifier: "modem-1"}
	if err := connector.Recover(ctx, []*mmodem.Modem{modem}); err != nil {
		t.Fatalf("Recover() error = %v", err)
	}
}

type fakeInternetModem struct {
	modemID      string
	operatorID   string
	gid1Value    string
	spnValue     string
	iccidValue   string
	imsiValue    string
	bearerList   []*mmodem.Bearer
	bearerReads  *int
	prepareErr   error
	prepareCalls *int
	connectErr   error
	connectCalls *int
}

func (m fakeInternetModem) id() string {
	return m.modemID
}

func (m fakeInternetModem) generation() uint64 { return 1 }

func (m fakeInternetModem) operatorIdentifier() string {
	return m.operatorID
}

func (m fakeInternetModem) gid1() string {
	return m.gid1Value
}

func (m fakeInternetModem) spn() string {
	return m.spnValue
}

func (m fakeInternetModem) profileID() string {
	return m.iccidValue
}

func (m fakeInternetModem) iccid() string {
	return m.iccidValue
}

func (m fakeInternetModem) imsi() string {
	return m.imsiValue
}

func (m fakeInternetModem) prepareBearerDataFormat(context.Context) error {
	if m.prepareCalls != nil {
		(*m.prepareCalls)++
	}
	return m.prepareErr
}

func (m fakeInternetModem) bearer(context.Context, uint64) (*mmodem.Bearer, error) {
	return nil, errors.New("bearer lookup unused")
}

func (m fakeInternetModem) bearers(context.Context) ([]*mmodem.Bearer, error) {
	if m.bearerReads != nil {
		(*m.bearerReads)++
	}
	return m.bearerList, nil
}

func (m fakeInternetModem) connectBearer(context.Context, mmodem.BearerProperties) (*mmodem.Bearer, error) {
	if m.connectCalls != nil {
		(*m.connectCalls)++
	}
	if m.connectErr != nil {
		return nil, m.connectErr
	}
	return nil, errors.New("connect bearer unused")
}

func (m fakeInternetModem) disconnectBearer(context.Context, uint64) error {
	return errors.New("disconnect bearer unused")
}

func (m fakeInternetModem) deleteBearer(context.Context, uint64) error {
	return errors.New("delete bearer unused")
}
