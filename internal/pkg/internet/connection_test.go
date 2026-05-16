package internet

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/godbus/dbus/v5"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/netlink"
)

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
		operatorID: "23415",
	}

	stateDir := t.TempDir()
	connector, err := NewConnector(ConnectorConfig{
		AlwaysOnPath: filepath.Join(stateDir, "internet-always-on.json"),
		ProxyPath:    filepath.Join(stateDir, "internet-proxies.json"),
		RoutePath:    filepath.Join(stateDir, "internet-routes.json"),
	})
	if err != nil {
		t.Fatalf("NewConnector() error = %v", err)
	}

	got, err := connector.current(context.Background(), modem)
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
	if got.APNUsername != "wap" || got.APNPassword != "*wap" || got.APNAuth != "pap" {
		t.Fatalf("Current() credentials = %q/%q/%q, want wap/*wap/pap", got.APNUsername, got.APNPassword, got.APNAuth)
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

func TestDNSNetwork(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		network string
		want    string
	}{
		{name: "udp", network: "udp", want: "udp4"},
		{name: "udp6", network: "udp6", want: "udp4"},
		{name: "tcp", network: "tcp", want: "tcp4"},
		{name: "tcp6", network: "tcp6", want: "tcp4"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := dnsNetwork(tt.network); got != tt.want {
				t.Fatalf("dnsNetwork() = %q, want %q", got, tt.want)
			}
		})
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
	}{
		{name: "new default takes over previous default route state"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "internet-routes.json")
			if err := saveRouteStateForModem(path, "old-modem", "wws27u1i4", []netlink.DefaultRoute{oldDefaultRoute}, []defaultRouteChange{oldGatewayChange}); err != nil {
				t.Fatalf("save old route state: %v", err)
			}
			if err := saveRouteStateForModem(path, "new-modem", "wws27u2i4", []netlink.DefaultRoute{newDefaultRoute}, []defaultRouteChange{oldRouteChange}); err != nil {
				t.Fatalf("save new route state: %v", err)
			}
			alwaysOnPath := filepath.Join(t.TempDir(), "internet-always-on.json")
			if err := saveAlwaysOnStateForModem(alwaysOnPath, "old-modem", Preferences{DefaultRoute: true, AlwaysOn: true}); err != nil {
				t.Fatalf("save old always-on state: %v", err)
			}
			c := &Connector{
				connections: map[string]trackedConnection{
					"old-modem": {
						interfaceName: "wws27u1i4",
						prefs:         Preferences{DefaultRoute: true, AlwaysOn: true},
						routeMetric:   defaultRouteMetric,
						routes:        []netlink.DefaultRoute{oldDefaultRoute},
						routeChanges:  []defaultRouteChange{oldGatewayChange},
					},
				},
				preferences: map[string]Preferences{
					"old-modem": {DefaultRoute: true, AlwaysOn: true},
				},
				alwaysOnPath: alwaysOnPath,
			}
			tracked := trackedConnection{
				interfaceName: "wws27u2i4",
				prefs:         Preferences{DefaultRoute: true},
				routeMetric:   defaultRouteMetric,
				routes:        []netlink.DefaultRoute{newDefaultRoute},
				routeChanges:  []defaultRouteChange{oldRouteChange},
			}

			if err := c.syncDefaultRouteTakeover(path, "new-modem", &tracked); err != nil {
				t.Fatalf("syncDefaultRouteTakeover() error = %v", err)
			}

			oldTracked := c.connections["old-modem"]
			if oldTracked.prefs.DefaultRoute {
				t.Fatal("old tracked DefaultRoute = true, want false")
			}
			if oldTracked.routeMetric != demotedOldRoute.Metric {
				t.Fatalf("old tracked routeMetric = %d, want %d", oldTracked.routeMetric, demotedOldRoute.Metric)
			}
			if !slices.Equal(oldTracked.routes, []netlink.DefaultRoute{demotedOldRoute}) {
				t.Fatalf("old tracked routes = %#v, want %#v", oldTracked.routes, []netlink.DefaultRoute{demotedOldRoute})
			}
			if !slices.Equal(oldTracked.routeChanges, []defaultRouteChange{oldGatewayChange}) {
				t.Fatalf("old tracked routeChanges = %#v, want %#v", oldTracked.routeChanges, []defaultRouteChange{oldGatewayChange})
			}
			if c.preferences["old-modem"].DefaultRoute {
				t.Fatal("old preference DefaultRoute = true, want false")
			}
			oldAlwaysOn, ok, err := loadAlwaysOnStateForModem(alwaysOnPath, "old-modem")
			if err != nil || !ok || oldAlwaysOn.DefaultRoute {
				t.Fatalf("load old always-on after takeover = %#v, ok = %t, err = %v; want non-default", oldAlwaysOn, ok, err)
			}
			if !slices.Equal(tracked.routeChanges, []defaultRouteChange{oldRouteChange}) {
				t.Fatalf("new tracked routeChanges = %#v, want %#v", tracked.routeChanges, []defaultRouteChange{oldRouteChange})
			}
			gotOldChanges, ok, err := loadRouteStateForModem(path, "old-modem", "wws27u1i4")
			if err != nil || !ok || !slices.Equal(gotOldChanges, []defaultRouteChange{oldGatewayChange}) {
				t.Fatalf("loadRouteStateForModem(old) = %#v, ok = %t, err = %v; want %#v, true, nil", gotOldChanges, ok, err, []defaultRouteChange{oldGatewayChange})
			}
			gotChanges, ok, err := loadRouteStateForModem(path, "new-modem", "wws27u2i4")
			if err != nil || !ok || !slices.Equal(gotChanges, []defaultRouteChange{oldRouteChange}) {
				t.Fatalf("loadRouteStateForModem(new) = %#v, ok = %t, err = %v; want %#v, true, nil", gotChanges, ok, err, []defaultRouteChange{oldRouteChange})
			}

			if err := c.syncDefaultRouteRestore(tracked.routeChanges); err != nil {
				t.Fatalf("syncDefaultRouteRestore() error = %v", err)
			}
			oldTracked = c.connections["old-modem"]
			if !oldTracked.prefs.DefaultRoute {
				t.Fatal("old tracked DefaultRoute after restore = false, want true")
			}
			if oldTracked.routeMetric != oldDefaultRoute.Metric {
				t.Fatalf("old tracked routeMetric after restore = %d, want %d", oldTracked.routeMetric, oldDefaultRoute.Metric)
			}
			if !slices.Equal(oldTracked.routes, []netlink.DefaultRoute{oldDefaultRoute}) {
				t.Fatalf("old tracked routes after restore = %#v, want %#v", oldTracked.routes, []netlink.DefaultRoute{oldDefaultRoute})
			}
			oldAlwaysOn, ok, err = loadAlwaysOnStateForModem(alwaysOnPath, "old-modem")
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
	}{
		{name: "active default inherits removed demoted route state"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "internet-routes.json")
			if err := saveRouteStateForModem(path, "new-modem", "wws27u2i4", []netlink.DefaultRoute{newDefaultRoute}, []defaultRouteChange{oldRouteChange}); err != nil {
				t.Fatalf("save new route state: %v", err)
			}
			removed := trackedConnection{
				interfaceName: "wws27u1i4",
				prefs:         Preferences{AlwaysOn: true},
				routeMetric:   demotedOldRoute.Metric,
				routes:        []netlink.DefaultRoute{demotedOldRoute},
				routeChanges:  []defaultRouteChange{oldGatewayChange},
			}
			c := &Connector{
				connections: map[string]trackedConnection{
					"old-modem": removed,
					"new-modem": {
						interfaceName: "wws27u2i4",
						prefs:         Preferences{DefaultRoute: true},
						routeMetric:   defaultRouteMetric,
						routes:        []netlink.DefaultRoute{newDefaultRoute},
						routeChanges:  []defaultRouteChange{oldRouteChange},
					},
				},
				preferences: map[string]Preferences{
					"old-modem": {AlwaysOn: true},
					"new-modem": {DefaultRoute: true},
				},
			}

			if err := c.syncDefaultRouteRemoval(path, removed); err != nil {
				t.Fatalf("syncDefaultRouteRemoval() error = %v", err)
			}

			newTracked := c.connections["new-modem"]
			if !slices.Equal(newTracked.routeChanges, []defaultRouteChange{oldGatewayChange}) {
				t.Fatalf("new tracked routeChanges = %#v, want %#v", newTracked.routeChanges, []defaultRouteChange{oldGatewayChange})
			}
			gotChanges, ok, err := loadRouteStateForModem(path, "new-modem", "wws27u2i4")
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

			path := filepath.Join(t.TempDir(), "internet-routes.json")
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

			if _, err := takeoverDefaultRoutesWithState(path, "modem-1", "wws27u1i4", preferred, ops); err == nil {
				t.Fatal("takeoverDefaultRoutesWithState() error = nil, want error")
			}
			_, ok, err := loadRouteState(path, "wws27u1i4")
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

			path := filepath.Join(t.TempDir(), "internet-routes.json")
			t.Cleanup(func() {
				_ = os.RemoveAll(path)
			})
			ops := defaultRouteOps{
				defaultRoutes: func() ([]netlink.DefaultRoute, error) {
					return []netlink.DefaultRoute{original}, nil
				},
				deleteDefaultRoute: func(route netlink.DefaultRoute) error {
					if err := os.Remove(path); err != nil {
						return errors.Join(errDeleteOriginal, fmt.Errorf("prepare cleanup failure: %w", err))
					}
					if err := os.Mkdir(path, 0o700); err != nil {
						return errors.Join(errDeleteOriginal, fmt.Errorf("prepare cleanup failure: %w", err))
					}
					return errDeleteOriginal
				},
			}

			_, err := takeoverDefaultRoutesWithState(path, "modem-1", "wws27u1i4", preferred, ops)
			if err == nil {
				t.Fatal("takeoverDefaultRoutesWithState() error = nil, want error")
			}
			if !errors.Is(err, errDeleteOriginal) {
				t.Fatalf("takeoverDefaultRoutesWithState() error = %v, want %v", err, errDeleteOriginal)
			}
			if !strings.Contains(err.Error(), "delete default route state") {
				t.Fatalf("takeoverDefaultRoutesWithState() error = %v, want state cleanup context", err)
			}
		})
	}
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

			path := filepath.Join(t.TempDir(), "internet-routes.json")
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

			gotChanges, err := takeoverDefaultRoutesWithState(path, "modem-1", "wws27u1i4", preferred, ops)
			if err == nil {
				t.Fatal("takeoverDefaultRoutesWithState() error = nil, want error")
			}
			if !slices.Equal(gotChanges, wantChanges) {
				t.Fatalf("takeoverDefaultRoutesWithState() changes = %#v, want %#v", gotChanges, wantChanges)
			}

			cleanupErr := cleanupDefaultRouteChanges(path, "wws27u1i4", gotChanges, ops)
			if (cleanupErr != nil) != tt.wantCleanupError {
				t.Fatalf("cleanupDefaultRouteChanges() error = %v, want error %t", cleanupErr, tt.wantCleanupError)
			}
			_, ok, err := loadRouteState(path, "wws27u1i4")
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
		{
			name:        "use interface fallback for ownerless state",
			target:      routeStateRestoreTarget{modemID: "modem-1", interfaceNames: []string{"wws27u1i4"}},
			current:     []netlink.DefaultRoute{preferred[0], replacement},
			wantDeleted: []netlink.DefaultRoute{preferred[0], replacement},
			wantAdded:   []netlink.DefaultRoute{original},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "internet-routes.json")
			if err := saveRouteStateForModem(path, tt.modemID, "wws27u1i4", preferred, changes); err != nil {
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

			if err := restoreStaleDefaultRouteStatesWithState(path, tt.target, ops); err != nil {
				t.Fatalf("restoreStaleDefaultRouteStatesWithState() error = %v", err)
			}
			if !slices.Equal(deleted, tt.wantDeleted) {
				t.Fatalf("deleted routes = %#v, want %#v", deleted, tt.wantDeleted)
			}
			if !slices.Equal(added, tt.wantAdded) {
				t.Fatalf("added routes = %#v, want %#v", added, tt.wantAdded)
			}
			_, ok, err := loadRouteState(path, "wws27u1i4")
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

			path := filepath.Join(t.TempDir(), "internet-routes.json")
			if err := saveRouteStateForModem(path, "modem-1", "wws0", firstPreferred, firstChanges); err != nil {
				t.Fatalf("saveRouteStateForModem(wws0) error = %v", err)
			}
			if err := saveRouteStateForModem(path, "modem-1", "wws1", secondPreferred, secondChanges); err != nil {
				t.Fatalf("saveRouteStateForModem(wws1) error = %v", err)
			}
			if err := saveRouteStateForModem(path, "modem-2", "wws2", otherPreferred, otherChanges); err != nil {
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

			if err := restoreStaleDefaultRouteStatesWithState(path, routeStateRestoreTarget{modemID: "modem-1"}, ops); err != nil {
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
			if _, ok, err := loadRouteState(path, "wws0"); err != nil || ok {
				t.Fatalf("loadRouteState(wws0) ok = %t, err = %v; want false, nil", ok, err)
			}
			if _, ok, err := loadRouteState(path, "wws1"); err != nil || ok {
				t.Fatalf("loadRouteState(wws1) ok = %t, err = %v; want false, nil", ok, err)
			}
			if got, ok, err := loadRouteState(path, "wws2"); err != nil || !ok || !slices.Equal(got, otherChanges) {
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

			path := filepath.Join(t.TempDir(), "internet-routes.json")
			if err := saveOwnerlessRouteState(path, "wws0", firstPreferred, firstChanges); err != nil {
				t.Fatalf("saveRouteState(wws0) error = %v", err)
			}
			if err := saveOwnerlessRouteState(path, "wws1", secondPreferred, secondChanges); err != nil {
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

			if err := restoreStaleDefaultRouteStatesWithState(path, routeStateRestoreTarget{interfaceNames: []string{"wws0"}}, ops); err != nil {
				t.Fatalf("restoreStaleDefaultRouteStatesWithState() error = %v", err)
			}
			if want := []netlink.DefaultRoute{firstPreferred[0], firstReplacement}; !slices.Equal(deleted, want) {
				t.Fatalf("deleted routes = %#v, want %#v", deleted, want)
			}
			if want := []netlink.DefaultRoute{firstOriginal}; !slices.Equal(added, want) {
				t.Fatalf("added routes = %#v, want %#v", added, want)
			}
			if _, ok, err := loadRouteState(path, "wws0"); err != nil || ok {
				t.Fatalf("loadRouteState(wws0) ok = %t, err = %v; want false, nil", ok, err)
			}
			if got, ok, err := loadRouteState(path, "wws1"); err != nil || !ok || !slices.Equal(got, secondChanges) {
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

			path := filepath.Join(t.TempDir(), "internet-routes.json")
			if err := saveOwnerlessRouteState(path, "wws27u1i4", preferred, changes); err != nil {
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

			if err := restoreStaleDefaultRouteStatesWithState(path, routeStateRestoreTarget{interfaceNames: []string{"wws27u1i4"}}, ops); err != nil {
				t.Fatalf("restoreStaleDefaultRouteStatesWithState() error = %v", err)
			}
			if want := []netlink.DefaultRoute{preferred[0], replacement}; !slices.Equal(deleted, want) {
				t.Fatalf("deleted routes = %#v, want %#v", deleted, want)
			}
			if want := []netlink.DefaultRoute{original}; !slices.Equal(added, want) {
				t.Fatalf("added routes = %#v, want %#v", added, want)
			}
			_, ok, err := loadRouteState(path, "wws27u1i4")
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

			stateDir := t.TempDir()
			connector, err := NewConnector(ConnectorConfig{
				AlwaysOnPath: filepath.Join(stateDir, "internet-always-on.json"),
				ProxyPath:    filepath.Join(stateDir, "internet-proxies.json"),
				RoutePath:    filepath.Join(stateDir, "internet-routes.json"),
			})
			if err != nil {
				t.Fatalf("NewConnector() error = %v", err)
			}
			if err := tt.run(context.Background(), connector); !errors.Is(err, ErrModemRequired) {
				t.Fatalf("%s error = %v, want %v", tt.name, err, ErrModemRequired)
			}
		})
	}
}

type fakeInternetModem struct {
	modemID    string
	operatorID string
	bearerList []*mmodem.Bearer
}

func (m fakeInternetModem) id() string {
	return m.modemID
}

func (m fakeInternetModem) operatorIdentifier() string {
	return m.operatorID
}

func (m fakeInternetModem) bearer(context.Context, dbus.ObjectPath) (*mmodem.Bearer, error) {
	return nil, errors.New("bearer lookup unused")
}

func (m fakeInternetModem) bearers(context.Context) ([]*mmodem.Bearer, error) {
	return m.bearerList, nil
}

func (m fakeInternetModem) connectBearer(context.Context, mmodem.BearerProperties) (*mmodem.Bearer, error) {
	return nil, errors.New("connect bearer unused")
}

func (m fakeInternetModem) disconnectBearer(context.Context, dbus.ObjectPath) error {
	return errors.New("disconnect bearer unused")
}

func (m fakeInternetModem) deleteBearer(context.Context, dbus.ObjectPath) error {
	return errors.New("delete bearer unused")
}

func (m fakeInternetModem) restart(context.Context, bool) error {
	return errors.New("restart unused")
}
