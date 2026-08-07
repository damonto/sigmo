package internet

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"slices"
	"testing"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	modemlink "github.com/damonto/sigmo/internal/pkg/modem/link"
	"github.com/damonto/sigmo/internal/pkg/netlink"
	"github.com/damonto/wwan-go/qcom"
)

func TestQMAPMigrationPreferencesKeepTrackedFrontendSettings(t *testing.T) {
	want := Preferences{APN: "ereseller", IPType: "ipv4v6", ProxyEnabled: true}
	connector := &Connector{
		connections: map[string]trackedConnection{
			"modem-1": {prefs: want},
		},
	}

	got := connector.qmapMigrationPreferences(context.Background(), fakeInternetModem{modemID: "modem-1"}, nil)
	if got != want {
		t.Fatalf("qmapMigrationPreferences() = %+v, want %+v", got, want)
	}
}

func TestQMAPIPPreferences(t *testing.T) {
	tests := []struct {
		name, input string
		want        []qcom.WDSIPPreference
		wantErr     bool
	}{
		{name: "dual stack starts both family legs", input: "ipv4v6", want: []qcom.WDSIPPreference{qcom.WDSIPPreferenceIPv4, qcom.WDSIPPreferenceIPv6}},
		{name: "ipv4", input: "ipv4", want: []qcom.WDSIPPreference{qcom.WDSIPPreferenceIPv4}},
		{name: "ipv6", input: "ipv6", want: []qcom.WDSIPPreference{qcom.WDSIPPreferenceIPv6}},
		{name: "invalid", input: "ppp", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := qmapIPPreferences(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("qmapIPPreferences() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("qmapIPPreferences() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestQMAPDataLegs(t *testing.T) {
	tests := []struct {
		name    string
		ipType  string
		wantMux []uint8
	}{
		{name: "IPv4 uses mux 1", ipType: "ipv4", wantMux: []uint8{1}},
		{name: "IPv6 uses mux 1", ipType: "ipv6", wantMux: []uint8{1}},
		{name: "dual stack reserves mux 2 for IMS", ipType: "ipv4v6", wantMux: []uint8{1, 3}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			legs, err := qmapDataLegs(tt.ipType)
			if err != nil {
				t.Fatalf("qmapDataLegs() error = %v", err)
			}
			if len(legs) != len(tt.wantMux) {
				t.Fatalf("qmapDataLegs() len = %d, want %d", len(legs), len(tt.wantMux))
			}
			for i, leg := range legs {
				if leg.muxID != tt.wantMux[i] {
					t.Fatalf("qmapDataLegs()[%d].muxID = %d, want %d", i, leg.muxID, tt.wantMux[i])
				}
			}
		})
	}
}

func TestConnectQMAPLockedAllowsPartialDualStack(t *testing.T) {
	errIPv4 := errors.New("IPv4 unavailable")
	errIPv6 := errors.New("IPv6 unavailable")
	tests := []struct {
		name        string
		ipType      string
		failures    map[uint8]error
		wantIPType  string
		wantErrs    []error
		wantOpened  []uint8
		wantRemoved []uint8
	}{
		{
			name:        "keeps IPv4 when IPv6 is unavailable",
			ipType:      "ipv4v6",
			failures:    map[uint8]error{ipv6QMAPMuxID: errIPv6},
			wantIPType:  "ipv4",
			wantOpened:  []uint8{internetQMAPMuxID, ipv6QMAPMuxID},
			wantRemoved: []uint8{ipv6QMAPMuxID},
		},
		{
			name:        "keeps IPv6 when IPv4 is unavailable",
			ipType:      "ipv4v6",
			failures:    map[uint8]error{internetQMAPMuxID: errIPv4},
			wantIPType:  "ipv6",
			wantOpened:  []uint8{internetQMAPMuxID, ipv6QMAPMuxID},
			wantRemoved: []uint8{internetQMAPMuxID},
		},
		{
			name:        "returns all errors when no leg connects",
			ipType:      "ipv4v6",
			failures:    map[uint8]error{internetQMAPMuxID: errIPv4, ipv6QMAPMuxID: errIPv6},
			wantErrs:    []error{errIPv4, errIPv6},
			wantOpened:  []uint8{internetQMAPMuxID, ipv6QMAPMuxID},
			wantRemoved: []uint8{internetQMAPMuxID, ipv6QMAPMuxID},
		},
		{
			name:        "single stack failure remains fatal",
			ipType:      "ipv4",
			failures:    map[uint8]error{internetQMAPMuxID: errIPv4},
			wantErrs:    []error{errIPv4},
			wantOpened:  []uint8{internetQMAPMuxID},
			wantRemoved: []uint8{internetQMAPMuxID},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			previousOpen := openInternetQMAPSession
			previousConfigure := configureInternetQMAPNetwork
			previousRemove := removeInternetQMAPMuxes
			t.Cleanup(func() {
				openInternetQMAPSession = previousOpen
				configureInternetQMAPNetwork = previousConfigure
				removeInternetQMAPMuxes = previousRemove
			})

			var opened, removed []uint8
			openInternetQMAPSession = func(_ context.Context, _ *mmodem.Modem, cfg modemlink.QMAPConfig) (*modemlink.QMAPSession, error) {
				opened = append(opened, cfg.MuxID)
				if err := tt.failures[cfg.MuxID]; err != nil {
					return nil, err
				}
				return &modemlink.QMAPSession{InterfaceName: qmapTestInterface(cfg.MuxID)}, nil
			}
			configureInternetQMAPNetwork = func(_ context.Context, _ connectionStateStore, _ string, _ Preferences, session *modemlink.QMAPSession) (trackedConnection, []string, error) {
				prefix := netip.MustParsePrefix("2001:db8::2/64")
				if session.InterfaceName == qmapTestInterface(internetQMAPMuxID) {
					prefix = netip.MustParsePrefix("10.0.0.2/30")
				}
				return trackedConnection{interfaceName: session.InterfaceName, addresses: []netip.Prefix{prefix}}, nil, nil
			}
			removeInternetQMAPMuxes = func(_ *mmodem.Modem, muxIDs ...uint8) error {
				removed = append(removed, muxIDs...)
				return nil
			}

			connector := &Connector{
				connections:     make(map[string]trackedConnection),
				preferences:     make(map[string]Preferences),
				qmapConnections: make(map[string]*qmapConnection),
			}
			connection, err := connector.connectQMAPLocked(context.Background(), &mmodem.Modem{EquipmentIdentifier: "modem-1"}, Preferences{IPType: tt.ipType})
			for _, wantErr := range tt.wantErrs {
				if !errors.Is(err, wantErr) {
					t.Fatalf("connectQMAPLocked() error = %v, want %v", err, wantErr)
				}
			}
			if len(tt.wantErrs) == 0 && err != nil {
				t.Fatalf("connectQMAPLocked() error = %v", err)
			}
			if connection != nil && connection.IPType != tt.wantIPType {
				t.Fatalf("connectQMAPLocked() IPType = %q, want %q", connection.IPType, tt.wantIPType)
			}
			if !slices.Equal(opened, tt.wantOpened) {
				t.Fatalf("opened muxes = %v, want %v", opened, tt.wantOpened)
			}
			if !slices.Equal(removed, tt.wantRemoved) {
				t.Fatalf("removed muxes = %v, want %v", removed, tt.wantRemoved)
			}
		})
	}
}

func TestQMAPProxyLifecycle(t *testing.T) {
	previousOpen := openInternetQMAPSession
	previousConfigure := configureInternetQMAPNetwork
	previousRemove := removeInternetQMAPMuxes
	t.Cleanup(func() {
		openInternetQMAPSession = previousOpen
		configureInternetQMAPNetwork = previousConfigure
		removeInternetQMAPMuxes = previousRemove
	})

	const (
		modemID       = "modem-1"
		interfaceName = "qmimux0"
	)
	wantDNS := []string{"10.51.190.5", "10.51.190.6"}
	openInternetQMAPSession = func(_ context.Context, _ *mmodem.Modem, cfg modemlink.QMAPConfig) (*modemlink.QMAPSession, error) {
		return &modemlink.QMAPSession{InterfaceName: qmapTestInterface(cfg.MuxID)}, nil
	}
	configureInternetQMAPNetwork = func(_ context.Context, _ connectionStateStore, _ string, prefs Preferences, _ *modemlink.QMAPSession) (trackedConnection, []string, error) {
		return trackedConnection{interfaceName: interfaceName, prefs: prefs}, slices.Clone(wantDNS), nil
	}
	removeInternetQMAPMuxes = func(_ *mmodem.Modem, _ ...uint8) error { return nil }

	proxy := newProxyWithDial(ProxyConfig{
		ListenAddress: "127.0.0.1",
		Password:      "secret",
	}, func(context.Context, string, []string, string, string) (net.Conn, error) {
		return nil, errors.New("dial should not be called")
	})
	connector, err := NewConnector(ConnectorConfig{Proxy: proxy, State: testStore(t)})
	if err != nil {
		t.Fatalf("NewConnector() error = %v", err)
	}
	ctx := context.Background()
	modem := &mmodem.Modem{EquipmentIdentifier: modemID}
	connection, err := connector.connectQMAPLocked(ctx, modem, Preferences{IPType: "ipv4", ProxyEnabled: true})
	if err != nil {
		t.Fatalf("connectQMAPLocked() error = %v", err)
	}
	if !connection.Proxy.Enabled || connection.Proxy.Username != modemID {
		t.Fatalf("connectQMAPLocked() proxy = %+v, want active proxy for %s", connection.Proxy, modemID)
	}
	binding, ok := proxy.bindingForUser(modemID)
	if !ok {
		t.Fatal("QMAP proxy binding was not registered")
	}
	if binding.InterfaceName != interfaceName || !slices.Equal(binding.DNS, wantDNS) {
		t.Fatalf("QMAP proxy binding = %+v, want interface %s and DNS %v", binding, interfaceName, wantDNS)
	}
	enabled, found, err := connector.persistence.loadProxyStateForModem(ctx, modemID, interfaceName)
	if err != nil {
		t.Fatalf("loadProxyStateForModem() error = %v", err)
	}
	if !found || !enabled {
		t.Fatalf("loadProxyStateForModem() = %t, found %t; want true, true", enabled, found)
	}

	if err := connector.disconnectQMAPLocked(ctx, modem); err != nil {
		t.Fatalf("disconnectQMAPLocked() error = %v", err)
	}
	if status := proxy.Status(modemID); status.Enabled {
		t.Fatalf("proxy status after disconnect = %+v, want inactive", status)
	}
	_, found, err = connector.persistence.loadProxyStateForModem(ctx, modemID, interfaceName)
	if err != nil {
		t.Fatalf("loadProxyStateForModem() after disconnect error = %v", err)
	}
	if found {
		t.Fatal("proxy state still exists after QMAP disconnect")
	}
}

func TestConnectQMAPLockedRollsBackWhenProxyRegistrationFails(t *testing.T) {
	previousOpen := openInternetQMAPSession
	previousConfigure := configureInternetQMAPNetwork
	previousRemove := removeInternetQMAPMuxes
	t.Cleanup(func() {
		openInternetQMAPSession = previousOpen
		configureInternetQMAPNetwork = previousConfigure
		removeInternetQMAPMuxes = previousRemove
	})

	var removed []uint8
	openInternetQMAPSession = func(_ context.Context, _ *mmodem.Modem, cfg modemlink.QMAPConfig) (*modemlink.QMAPSession, error) {
		return &modemlink.QMAPSession{InterfaceName: qmapTestInterface(cfg.MuxID)}, nil
	}
	configureInternetQMAPNetwork = func(_ context.Context, _ connectionStateStore, _ string, prefs Preferences, session *modemlink.QMAPSession) (trackedConnection, []string, error) {
		return trackedConnection{interfaceName: session.InterfaceName, prefs: prefs}, nil, nil
	}
	removeInternetQMAPMuxes = func(_ *mmodem.Modem, muxIDs ...uint8) error {
		removed = append(removed, muxIDs...)
		return nil
	}

	connector, err := NewConnector(ConnectorConfig{State: testStore(t)})
	if err != nil {
		t.Fatalf("NewConnector() error = %v", err)
	}
	modem := &mmodem.Modem{EquipmentIdentifier: "modem-1"}
	connection, err := connector.connectQMAPLocked(context.Background(), modem, Preferences{IPType: "ipv4", ProxyEnabled: true})
	if !errors.Is(err, ErrProxyNotConfigured) {
		t.Fatalf("connectQMAPLocked() error = %v, want %v", err, ErrProxyNotConfigured)
	}
	if connection != nil {
		t.Fatalf("connectQMAPLocked() connection = %+v, want nil", connection)
	}
	if connector.qmapConnections[modem.EquipmentIdentifier] != nil {
		t.Fatal("failed QMAP connection was published")
	}
	if !slices.Equal(removed, []uint8{internetQMAPMuxID}) {
		t.Fatalf("removed muxes = %v, want %v", removed, []uint8{internetQMAPMuxID})
	}
}

func TestCleanupStaleQMAPInternetRestoresRoutesAndRemovesInternetMuxes(t *testing.T) {
	original := netlink.DefaultRoute{Interface: "ens18", Family: netlink.FamilyIPv4, Gateway: netip.MustParseAddr("10.0.0.1"), Metric: 10}
	replacement := original
	replacement.Metric = 21
	preferred := netlink.DefaultRoute{
		Interface: "qmimux0", Family: netlink.FamilyIPv4,
		Gateway: netip.MustParseAddr("10.61.158.137"), Source: netip.MustParseAddr("10.61.158.138"), Metric: 10,
	}
	state := testDBConnectionState(t)
	if err := state.saveRouteStateForModem(context.Background(), "modem-1", "qmimux0", []netlink.DefaultRoute{preferred}, []defaultRouteChange{{
		Original: original, Replacement: replacement,
	}}); err != nil {
		t.Fatalf("saveRouteStateForModem() error = %v", err)
	}

	previousOps := netlinkDefaultRouteOps
	previousRemove := removeInternetQMAPMuxes
	t.Cleanup(func() {
		netlinkDefaultRouteOps = previousOps
		removeInternetQMAPMuxes = previousRemove
	})
	var added, deleted []netlink.DefaultRoute
	netlinkDefaultRouteOps = defaultRouteOps{
		defaultRoutes: func() ([]netlink.DefaultRoute, error) {
			return []netlink.DefaultRoute{preferred, replacement}, nil
		},
		addDefaultRoute: func(route netlink.DefaultRoute) error {
			added = append(added, route)
			return nil
		},
		deleteDefaultRoute: func(route netlink.DefaultRoute) error {
			deleted = append(deleted, route)
			return nil
		},
	}
	var removed []uint8
	removeInternetQMAPMuxes = func(_ *mmodem.Modem, muxIDs ...uint8) error {
		removed = append(removed, muxIDs...)
		return nil
	}

	connector := &Connector{persistence: state}
	if err := connector.cleanupStaleQMAPInternet(context.Background(), &mmodem.Modem{EquipmentIdentifier: "modem-1"}); err != nil {
		t.Fatalf("cleanupStaleQMAPInternet() error = %v", err)
	}
	if !slices.Equal(added, []netlink.DefaultRoute{original}) {
		t.Fatalf("added routes = %+v, want %+v", added, []netlink.DefaultRoute{original})
	}
	if !slices.Equal(deleted, []netlink.DefaultRoute{preferred, replacement}) {
		t.Fatalf("deleted routes = %+v, want %+v", deleted, []netlink.DefaultRoute{preferred, replacement})
	}
	if !slices.Equal(removed, []uint8{internetQMAPMuxID, ipv6QMAPMuxID}) {
		t.Fatalf("removed muxes = %v, want %v", removed, []uint8{internetQMAPMuxID, ipv6QMAPMuxID})
	}
}

func qmapTestInterface(muxID uint8) string {
	if muxID == internetQMAPMuxID {
		return "qmimux0"
	}
	return "qmimux2"
}

func TestQMAPNetworks(t *testing.T) {
	tests := []struct {
		name string
		info qcom.PDNInfo
		want int
	}{
		{name: "ipv4", info: qcom.PDNInfo{LocalIPv4: net.IPv4(10, 0, 0, 2), IPv4SubnetMask: net.IPv4(255, 255, 255, 252), IPv4Gateway: net.IPv4(10, 0, 0, 1)}, want: 1},
		{name: "dual stack", info: qcom.PDNInfo{LocalIPv4: net.IPv4(10, 0, 0, 2), IPv4SubnetMask: net.IPv4(255, 255, 255, 252), IPv4Gateway: net.IPv4(10, 0, 0, 1), LocalIPv6: net.ParseIP("2001:db8::2"), IPv6Gateway: net.ParseIP("2001:db8::1"), IPv6PrefixLength: 64}, want: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := qmapNetworks(tt.info); len(got) != tt.want {
				t.Fatalf("qmapNetworks() len = %d, want %d", len(got), tt.want)
			}
		})
	}
}
