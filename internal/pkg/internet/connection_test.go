package internet

import (
	"errors"
	"net/netip"
	"reflect"
	"testing"

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
			if !reflect.DeepEqual(gotAddrs, tt.wantAddrs) {
				t.Fatalf("addressesAndRoutes() addresses = %#v, want %#v", gotAddrs, tt.wantAddrs)
			}
			if !reflect.DeepEqual(gotRoutes, tt.wantRoutes) {
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
			if !reflect.DeepEqual(gotRoutes, tt.wantRoutes) {
				t.Fatalf("addressesAndRoutesWithMetric() routes = %#v, want %#v", gotRoutes, tt.wantRoutes)
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
			if !reflect.DeepEqual(gotIPv4, tt.wantIPv4) {
				t.Fatalf("connectionAddressStrings() ipv4 = %#v, want %#v", gotIPv4, tt.wantIPv4)
			}
			if !reflect.DeepEqual(gotIPv6, tt.wantIPv6) {
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
