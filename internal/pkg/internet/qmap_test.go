package internet

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"slices"
	"strings"
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

	got := connector.qmapMigrationPreferences(t.Context(), fakeInternetModem{modemID: "modem-1"}, nil)
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
		{name: "dual stack starts both families", input: "ipv4v6", want: []qcom.WDSIPPreference{qcom.WDSIPPreferenceIPv4, qcom.WDSIPPreferenceIPv6}},
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

func TestConnectQMAPLockedAllowsPartialDualStack(t *testing.T) {
	errIPv4 := errors.New("IPv4 unavailable")
	errIPv6 := errors.New("IPv6 unavailable")
	tests := []struct {
		name         string
		ipType       string
		failures     map[qcom.WDSIPPreference]error
		wantIPType   string
		wantErrs     []error
		wantOpened   []qcom.WDSIPPreference
		wantRemoved  []uint8
		emptyInfo    map[qcom.WDSIPPreference]bool
		wrongFamily  map[qcom.WDSIPPreference]bool
		nilSession   map[qcom.WDSIPPreference]bool
		wantErr      bool
		wantSessions int
	}{
		{
			name:         "combines dual stack on mux 1",
			ipType:       "ipv4v6",
			wantIPType:   "ipv4v6",
			wantOpened:   []qcom.WDSIPPreference{qcom.WDSIPPreferenceIPv4, qcom.WDSIPPreferenceIPv6},
			wantSessions: 2,
		},
		{
			name:         "keeps IPv4 when IPv6 is unavailable",
			ipType:       "ipv4v6",
			failures:     map[qcom.WDSIPPreference]error{qcom.WDSIPPreferenceIPv6: errIPv6},
			wantIPType:   "ipv4",
			wantOpened:   []qcom.WDSIPPreference{qcom.WDSIPPreferenceIPv4, qcom.WDSIPPreferenceIPv6},
			wantSessions: 1,
		},
		{
			name:         "keeps IPv6 when IPv4 is unavailable",
			ipType:       "ipv4v6",
			failures:     map[qcom.WDSIPPreference]error{qcom.WDSIPPreferenceIPv4: errIPv4},
			wantIPType:   "ipv6",
			wantOpened:   []qcom.WDSIPPreference{qcom.WDSIPPreferenceIPv4, qcom.WDSIPPreferenceIPv6},
			wantSessions: 1,
		},
		{
			name:        "returns all errors when no family connects",
			ipType:      "ipv4v6",
			failures:    map[qcom.WDSIPPreference]error{qcom.WDSIPPreferenceIPv4: errIPv4, qcom.WDSIPPreferenceIPv6: errIPv6},
			wantErrs:    []error{errIPv4, errIPv6},
			wantOpened:  []qcom.WDSIPPreference{qcom.WDSIPPreferenceIPv4, qcom.WDSIPPreferenceIPv6},
			wantRemoved: []uint8{internetQMAPMuxID},
		},
		{
			name:        "single stack failure remains fatal",
			ipType:      "ipv4",
			failures:    map[qcom.WDSIPPreference]error{qcom.WDSIPPreferenceIPv4: errIPv4},
			wantErrs:    []error{errIPv4},
			wantOpened:  []qcom.WDSIPPreference{qcom.WDSIPPreferenceIPv4},
			wantRemoved: []uint8{internetQMAPMuxID},
		},
		{
			name:        "single stack without network configuration is fatal",
			ipType:      "ipv4",
			emptyInfo:   map[qcom.WDSIPPreference]bool{qcom.WDSIPPreferenceIPv4: true},
			wantOpened:  []qcom.WDSIPPreference{qcom.WDSIPPreferenceIPv4},
			wantRemoved: []uint8{internetQMAPMuxID},
			wantErr:     true,
		},
		{
			name:        "single stack nil session is fatal",
			ipType:      "ipv4",
			nilSession:  map[qcom.WDSIPPreference]bool{qcom.WDSIPPreferenceIPv4: true},
			wantOpened:  []qcom.WDSIPPreference{qcom.WDSIPPreferenceIPv4},
			wantRemoved: []uint8{internetQMAPMuxID},
			wantErr:     true,
		},
		{
			name:         "keeps configured family when the other runtime is empty",
			ipType:       "ipv4v6",
			emptyInfo:    map[qcom.WDSIPPreference]bool{qcom.WDSIPPreferenceIPv6: true},
			wantIPType:   "ipv4",
			wantOpened:   []qcom.WDSIPPreference{qcom.WDSIPPreferenceIPv4, qcom.WDSIPPreferenceIPv6},
			wantSessions: 1,
		},
		{
			name:         "keeps IPv4 when IPv6 runtime has the wrong family",
			ipType:       "ipv4v6",
			wrongFamily:  map[qcom.WDSIPPreference]bool{qcom.WDSIPPreferenceIPv6: true},
			wantIPType:   "ipv4",
			wantOpened:   []qcom.WDSIPPreference{qcom.WDSIPPreferenceIPv4, qcom.WDSIPPreferenceIPv6},
			wantSessions: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var opened []qcom.WDSIPPreference
			var removed []uint8
			configureCalls := 0
			connector := &Connector{
				connections:     make(map[string]trackedConnection),
				preferences:     make(map[string]Preferences),
				qmapConnections: make(map[string]*qmapConnection),
			}
			connector.qmap.openSessions = func(_ context.Context, _ *mmodem.Modem, configs []modemlink.QMAPConfig) ([]modemlink.QMAPSessionResult, error) {
				results := make([]modemlink.QMAPSessionResult, len(configs))
				for i, cfg := range configs {
					opened = append(opened, cfg.IPPreference)
					if cfg.MuxID != internetQMAPMuxID {
						t.Fatalf("OpenQMAPSessions() mux ID = %d, want %d", cfg.MuxID, internetQMAPMuxID)
					}
					if err := tt.failures[cfg.IPPreference]; err != nil {
						results[i].Err = err
						continue
					}
					if tt.nilSession[cfg.IPPreference] {
						continue
					}
					info := qcom.PDNInfo{
						LocalIPv4:      net.IPv4(10, 0, 0, 2),
						IPv4SubnetMask: net.IPv4(255, 255, 255, 252),
						IPv4Gateway:    net.IPv4(10, 0, 0, 1),
					}
					if cfg.IPPreference == qcom.WDSIPPreferenceIPv6 {
						info = qcom.PDNInfo{
							LocalIPv6:        net.ParseIP("2001:db8::2"),
							IPv6Gateway:      net.ParseIP("2001:db8::1"),
							IPv6PrefixLength: 64,
						}
					}
					if tt.emptyInfo[cfg.IPPreference] {
						info = qcom.PDNInfo{}
					}
					if tt.wrongFamily[cfg.IPPreference] {
						info = qcom.PDNInfo{
							LocalIPv4:      net.IPv4(10, 0, 0, 3),
							IPv4SubnetMask: net.IPv4(255, 255, 255, 252),
							IPv4Gateway:    net.IPv4(10, 0, 0, 1),
						}
					}
					results[i].Session = &modemlink.QMAPSession{InterfaceName: "qmimux0", Info: info}
				}
				return results, nil
			}
			connector.qmap.configureNetwork = func(_ context.Context, _ connectionStateStore, _ string, _ Preferences, config qmapLinkConfig, _ defaultRouteOps) (trackedConnection, error) {
				configureCalls++
				var addresses []netip.Prefix
				for _, network := range config.networks {
					addresses = append(addresses, network.prefix)
				}
				return trackedConnection{interfaceName: config.interfaceName, addresses: addresses}, nil
			}
			connector.qmap.removeMuxes = func(_ *mmodem.Modem, muxIDs ...uint8) error {
				removed = append(removed, muxIDs...)
				return nil
			}

			connection, err := connector.connectQMAPLocked(t.Context(), &mmodem.Modem{EquipmentIdentifier: "modem-1"}, Preferences{IPType: tt.ipType})
			for _, wantErr := range tt.wantErrs {
				if !errors.Is(err, wantErr) {
					t.Fatalf("connectQMAPLocked() error = %v, want %v", err, wantErr)
				}
			}
			if tt.wantErr && err == nil {
				t.Fatal("connectQMAPLocked() error = nil, want error")
			}
			if !tt.wantErr && len(tt.wantErrs) == 0 && err != nil {
				t.Fatalf("connectQMAPLocked() error = %v", err)
			}
			if connection != nil && connection.IPType != tt.wantIPType {
				t.Fatalf("connectQMAPLocked() IPType = %q, want %q", connection.IPType, tt.wantIPType)
			}
			slices.Sort(opened)
			wantOpened := slices.Clone(tt.wantOpened)
			slices.Sort(wantOpened)
			if !slices.Equal(opened, wantOpened) {
				t.Fatalf("opened IP preferences = %v, want %v", opened, tt.wantOpened)
			}
			if !slices.Equal(removed, tt.wantRemoved) {
				t.Fatalf("removed muxes = %v, want %v", removed, tt.wantRemoved)
			}
			wantConfigureCalls := 1
			if tt.wantErr || len(tt.wantErrs) > 0 {
				wantConfigureCalls = 0
			}
			if configureCalls != wantConfigureCalls {
				t.Fatalf("configure QMAP calls = %d, want %d", configureCalls, wantConfigureCalls)
			}
			if tt.wantSessions > 0 {
				stored := connector.qmapConnections["modem-1"]
				gotSessions := 0
				if stored != nil {
					gotSessions = len(stored.sessions)
				}
				if gotSessions != tt.wantSessions {
					t.Fatalf("stored QMAP sessions = %d, want %d", gotSessions, tt.wantSessions)
				}
			}
		})
	}
}

func TestOpenQMAPFamilySessionsPassesOrderedDualStackBatch(t *testing.T) {
	connector := &Connector{}
	connector.qmap.openSessions = func(_ context.Context, _ *mmodem.Modem, configs []modemlink.QMAPConfig) ([]modemlink.QMAPSessionResult, error) {
		if len(configs) != 2 {
			t.Fatalf("OpenQMAPSessions() configs = %d, want 2", len(configs))
		}
		if configs[0].IPPreference != qcom.WDSIPPreferenceIPv4 || configs[1].IPPreference != qcom.WDSIPPreferenceIPv6 {
			t.Fatalf("OpenQMAPSessions() preferences = [%v %v], want [IPv4 IPv6]", configs[0].IPPreference, configs[1].IPPreference)
		}
		for _, cfg := range configs {
			if cfg.APN != "cmnet" || cfg.MuxID != internetQMAPMuxID {
				t.Fatalf("OpenQMAPSessions() config = %+v", cfg)
			}
		}
		return []modemlink.QMAPSessionResult{
			{Session: &modemlink.QMAPSession{}},
			{Session: &modemlink.QMAPSession{}},
		}, nil
	}

	results, err := connector.openQMAPFamilySessions(t.Context(), &mmodem.Modem{}, "cmnet", []qcom.WDSIPPreference{
		qcom.WDSIPPreferenceIPv4,
		qcom.WDSIPPreferenceIPv6,
	})
	if err != nil {
		t.Fatalf("openQMAPFamilySessions() error = %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("openQMAPFamilySessions() results = %d, want 2", len(results))
	}
}

func TestOpenQMAPFamilySessionsRejectsInvalidResults(t *testing.T) {
	wantErr := errors.New("open family")
	tests := []struct {
		name      string
		opened    []modemlink.QMAPSessionResult
		wantBatch bool
		wantIs    error
	}{
		{
			name:      "result count mismatch",
			opened:    []modemlink.QMAPSessionResult{{Session: &modemlink.QMAPSession{}}},
			wantBatch: true,
		},
		{
			name:   "session and error",
			opened: []modemlink.QMAPSessionResult{{Session: &modemlink.QMAPSession{}, Err: wantErr}},
			wantIs: wantErr,
		},
		{
			name:   "neither session nor error",
			opened: []modemlink.QMAPSessionResult{{}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			connector := &Connector{}
			connector.qmap.openSessions = func(context.Context, *mmodem.Modem, []modemlink.QMAPConfig) ([]modemlink.QMAPSessionResult, error) {
				return tt.opened, nil
			}
			preferences := []qcom.WDSIPPreference{qcom.WDSIPPreferenceIPv4}
			if tt.wantBatch {
				preferences = append(preferences, qcom.WDSIPPreferenceIPv6)
			}

			results, err := connector.openQMAPFamilySessions(t.Context(), &mmodem.Modem{}, "internet", preferences)
			if tt.wantBatch {
				if err == nil || !strings.Contains(err.Error(), "returned 1 results, want 2") {
					t.Fatalf("openQMAPFamilySessions() error = %v, want result count error", err)
				}
				if results != nil {
					t.Fatalf("openQMAPFamilySessions() results = %+v, want nil", results)
				}
				return
			}
			if err != nil {
				t.Fatalf("openQMAPFamilySessions() error = %v", err)
			}
			if len(results) != 1 || results[0].session != nil || results[0].err == nil {
				t.Fatalf("openQMAPFamilySessions() results = %+v, want one family error", results)
			}
			if tt.wantIs != nil && !errors.Is(results[0].err, tt.wantIs) {
				t.Fatalf("openQMAPFamilySessions() family error = %v, want %v", results[0].err, tt.wantIs)
			}
		})
	}
}

func TestConfigureQMAPNetworkRejectsEmptyNetwork(t *testing.T) {
	tracked, err := configureQMAPNetwork(t.Context(), nil, "modem-1", Preferences{}, qmapLinkConfig{
		interfaceName: "qmimux0",
	}, defaultRouteOps{})
	if err == nil || !strings.Contains(err.Error(), "network configuration is empty") {
		t.Fatalf("configureQMAPNetwork() error = %v, want empty network error", err)
	}
	if tracked.interfaceName != "" || len(tracked.addresses) != 0 || len(tracked.routes) != 0 {
		t.Fatalf("configureQMAPNetwork() tracked = %+v, want untouched network state", tracked)
	}
}

func TestQMAPProxyLifecycle(t *testing.T) {
	const (
		modemID       = "modem-1"
		interfaceName = "qmimux0"
	)
	wantDNS := []string{"10.51.190.5", "10.51.190.6"}
	var removed []uint8

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
	connector.qmap = qmapOps{
		openSessions: func(_ context.Context, _ *mmodem.Modem, _ []modemlink.QMAPConfig) ([]modemlink.QMAPSessionResult, error) {
			return []modemlink.QMAPSessionResult{{Session: &modemlink.QMAPSession{
				InterfaceName: "qmimux0",
				Info: qcom.PDNInfo{
					LocalIPv4:      net.IPv4(10, 0, 0, 2),
					IPv4SubnetMask: net.IPv4(255, 255, 255, 252),
					IPv4Gateway:    net.IPv4(10, 0, 0, 1),
				},
			}}}, nil
		},
		configureNetwork: func(_ context.Context, _ connectionStateStore, _ string, prefs Preferences, _ qmapLinkConfig, _ defaultRouteOps) (trackedConnection, error) {
			return trackedConnection{interfaceName: interfaceName, prefs: prefs, dns: slices.Clone(wantDNS)}, nil
		},
		removeMuxes: func(_ *mmodem.Modem, muxIDs ...uint8) error {
			removed = append(removed, muxIDs...)
			return nil
		},
	}
	ctx := t.Context()
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
	if !slices.Equal(removed, []uint8{internetQMAPMuxID}) {
		t.Fatalf("removed muxes after disconnect = %v, want %v", removed, []uint8{internetQMAPMuxID})
	}
}

func TestConnectQMAPLockedRollsBackWhenProxyRegistrationFails(t *testing.T) {
	var removed []uint8
	connector, err := NewConnector(ConnectorConfig{State: testStore(t)})
	if err != nil {
		t.Fatalf("NewConnector() error = %v", err)
	}
	connector.qmap = qmapOps{
		openSessions: func(_ context.Context, _ *mmodem.Modem, _ []modemlink.QMAPConfig) ([]modemlink.QMAPSessionResult, error) {
			return []modemlink.QMAPSessionResult{{Session: &modemlink.QMAPSession{
				InterfaceName: "qmimux0",
				Info: qcom.PDNInfo{
					LocalIPv4:      net.IPv4(10, 0, 0, 2),
					IPv4SubnetMask: net.IPv4(255, 255, 255, 252),
					IPv4Gateway:    net.IPv4(10, 0, 0, 1),
				},
			}}}, nil
		},
		configureNetwork: func(_ context.Context, _ connectionStateStore, _ string, prefs Preferences, config qmapLinkConfig, _ defaultRouteOps) (trackedConnection, error) {
			return trackedConnection{interfaceName: config.interfaceName, prefs: prefs}, nil
		},
		removeMuxes: func(_ *mmodem.Modem, muxIDs ...uint8) error {
			removed = append(removed, muxIDs...)
			return nil
		},
	}
	modem := &mmodem.Modem{EquipmentIdentifier: "modem-1"}
	connection, err := connector.connectQMAPLocked(t.Context(), modem, Preferences{IPType: "ipv4", ProxyEnabled: true})
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
	if err := state.saveRouteStateForModem(t.Context(), "modem-1", "qmimux0", []netlink.DefaultRoute{preferred}, []defaultRouteChange{{
		Original: original, Replacement: replacement,
	}}); err != nil {
		t.Fatalf("saveRouteStateForModem() error = %v", err)
	}

	var added, deleted []netlink.DefaultRoute
	routeOps := defaultRouteOps{
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
	connector := &Connector{persistence: state, routes: routeOps}
	connector.qmap.removeMuxes = func(_ *mmodem.Modem, muxIDs ...uint8) error {
		removed = append(removed, muxIDs...)
		return nil
	}
	if err := connector.cleanupStaleQMAPInternet(t.Context(), &mmodem.Modem{EquipmentIdentifier: "modem-1"}); err != nil {
		t.Fatalf("cleanupStaleQMAPInternet() error = %v", err)
	}
	if !slices.Equal(added, []netlink.DefaultRoute{original}) {
		t.Fatalf("added routes = %+v, want %+v", added, []netlink.DefaultRoute{original})
	}
	if !slices.Equal(deleted, []netlink.DefaultRoute{preferred, replacement}) {
		t.Fatalf("deleted routes = %+v, want %+v", deleted, []netlink.DefaultRoute{preferred, replacement})
	}
	if !slices.Equal(removed, []uint8{internetQMAPMuxID}) {
		t.Fatalf("removed muxes = %v, want %v", removed, []uint8{internetQMAPMuxID})
	}
}

func TestQMAPNetworks(t *testing.T) {
	tests := []struct {
		name    string
		info    qcom.PDNInfo
		want    int
		wantErr bool
	}{
		{name: "ipv4", info: qcom.PDNInfo{LocalIPv4: net.IPv4(10, 0, 0, 2), IPv4SubnetMask: net.IPv4(255, 255, 255, 252), IPv4Gateway: net.IPv4(10, 0, 0, 1)}, want: 1},
		{name: "dual stack", info: qcom.PDNInfo{LocalIPv4: net.IPv4(10, 0, 0, 2), IPv4SubnetMask: net.IPv4(255, 255, 255, 252), IPv4Gateway: net.IPv4(10, 0, 0, 1), LocalIPv6: net.ParseIP("2001:db8::2"), IPv6Gateway: net.ParseIP("2001:db8::1"), IPv6PrefixLength: 64}, want: 2},
		{name: "missing IPv4 mask", info: qcom.PDNInfo{LocalIPv4: net.IPv4(10, 0, 0, 2), IPv4Gateway: net.IPv4(10, 0, 0, 1)}, wantErr: true},
		{name: "non-contiguous IPv4 mask", info: qcom.PDNInfo{LocalIPv4: net.IPv4(10, 0, 0, 2), IPv4SubnetMask: net.IPv4(255, 0, 255, 0), IPv4Gateway: net.IPv4(10, 0, 0, 1)}, wantErr: true},
		{name: "zero-length IPv4 prefix", info: qcom.PDNInfo{LocalIPv4: net.IPv4(10, 0, 0, 2), IPv4SubnetMask: net.IPv4zero, IPv4Gateway: net.IPv4(10, 0, 0, 1)}, wantErr: true},
		{name: "missing IPv4 gateway", info: qcom.PDNInfo{LocalIPv4: net.IPv4(10, 0, 0, 2), IPv4SubnetMask: net.IPv4(255, 255, 255, 252)}, wantErr: true},
		{name: "unspecified IPv4 local address", info: qcom.PDNInfo{LocalIPv4: net.IPv4zero, IPv4SubnetMask: net.IPv4(255, 255, 255, 252), IPv4Gateway: net.IPv4(10, 0, 0, 1)}, wantErr: true},
		{name: "unspecified IPv4 gateway", info: qcom.PDNInfo{LocalIPv4: net.IPv4(10, 0, 0, 2), IPv4SubnetMask: net.IPv4(255, 255, 255, 252), IPv4Gateway: net.IPv4zero}, wantErr: true},
		{name: "IPv4 gateway has wrong family", info: qcom.PDNInfo{LocalIPv4: net.IPv4(10, 0, 0, 2), IPv4SubnetMask: net.IPv4(255, 255, 255, 252), IPv4Gateway: net.ParseIP("2001:db8::1")}, wantErr: true},
		{name: "missing IPv6 gateway", info: qcom.PDNInfo{LocalIPv6: net.ParseIP("2001:db8::2"), IPv6PrefixLength: 64}, wantErr: true},
		{name: "unspecified IPv6 local address", info: qcom.PDNInfo{LocalIPv6: net.IPv6unspecified, IPv6Gateway: net.ParseIP("2001:db8::1"), IPv6PrefixLength: 64}, wantErr: true},
		{name: "unspecified IPv6 gateway", info: qcom.PDNInfo{LocalIPv6: net.ParseIP("2001:db8::2"), IPv6Gateway: net.IPv6unspecified, IPv6PrefixLength: 64}, wantErr: true},
		{name: "IPv6 gateway has wrong family", info: qcom.PDNInfo{LocalIPv6: net.ParseIP("2001:db8::2"), IPv6Gateway: net.IPv4(10, 0, 0, 1), IPv6PrefixLength: 64}, wantErr: true},
		{name: "zero-length IPv6 prefix", info: qcom.PDNInfo{LocalIPv6: net.ParseIP("2001:db8::2"), IPv6Gateway: net.ParseIP("2001:db8::1")}, wantErr: true},
		{name: "IPv6 prefix exceeds address size", info: qcom.PDNInfo{LocalIPv6: net.ParseIP("2001:db8::2"), IPv6Gateway: net.ParseIP("2001:db8::1"), IPv6PrefixLength: 129}, wantErr: true},
		{name: "IPv4-mapped local IPv6", info: qcom.PDNInfo{LocalIPv6: net.ParseIP("::ffff:10.0.0.2"), IPv6Gateway: net.ParseIP("2001:db8::1"), IPv6PrefixLength: 64}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := qmapNetworks(tt.info)
			if (err != nil) != tt.wantErr {
				t.Fatalf("qmapNetworks() error = %v, wantErr %t", err, tt.wantErr)
			}
			if len(got) != tt.want {
				t.Fatalf("qmapNetworks() len = %d, want %d", len(got), tt.want)
			}
		})
	}
}

func TestCombineQMAPSessions(t *testing.T) {
	tests := []struct {
		name     string
		sessions []*modemlink.QMAPSession
		wantErr  bool
	}{
		{
			name: "dual stack on one mux",
			sessions: []*modemlink.QMAPSession{
				{
					InterfaceName: "qmimux0",
					Info: qcom.PDNInfo{
						LocalIPv4:       net.IPv4(10, 0, 0, 2),
						IPv4SubnetMask:  net.IPv4(255, 255, 255, 252),
						IPv4Gateway:     net.IPv4(10, 0, 0, 1),
						DNS:             []net.IP{net.IPv4(1, 1, 1, 1)},
						MTU:             1500,
						PacketDataReady: true,
					},
				},
				{
					InterfaceName: "qmimux0",
					Info: qcom.PDNInfo{
						LocalIPv6:        net.ParseIP("2001:db8::2"),
						IPv6Gateway:      net.ParseIP("2001:db8::1"),
						IPv6PrefixLength: 64,
						DNS: []net.IP{
							net.IPv4zero,
							net.IPv4(1, 1, 1, 1),
							net.IPv6unspecified,
							net.ParseIP("2001:4860:4860::8888"),
						},
						MTU: 1420,
					},
				},
			},
		},
		{
			name: "different mux interfaces",
			sessions: []*modemlink.QMAPSession{
				{InterfaceName: "qmimux0"},
				{InterfaceName: "qmimux2"},
			},
			wantErr: true,
		},
		{name: "empty session interface", sessions: []*modemlink.QMAPSession{{InterfaceName: "  "}}, wantErr: true},
		{name: "empty network configuration", sessions: []*modemlink.QMAPSession{{InterfaceName: "qmimux0"}}, wantErr: true},
		{name: "no sessions", wantErr: true},
		{name: "nil session", sessions: []*modemlink.QMAPSession{nil}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := combineQMAPSessions(tt.sessions)
			if (err != nil) != tt.wantErr {
				t.Fatalf("combineQMAPSessions() error = %v, wantErr %t", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got.interfaceName != "qmimux0" {
				t.Fatalf("combineQMAPSessions() interface = %q, want qmimux0", got.interfaceName)
			}
			wantNetworks := []qmapNetwork{
				{prefix: netip.MustParsePrefix("10.0.0.2/30"), gateway: netip.MustParseAddr("10.0.0.1")},
				{prefix: netip.MustParsePrefix("2001:db8::2/64"), gateway: netip.MustParseAddr("2001:db8::1")},
			}
			if !slices.Equal(got.networks, wantNetworks) {
				t.Fatalf("combineQMAPSessions() networks = %v, want %v", got.networks, wantNetworks)
			}
			if got.mtu != 1420 {
				t.Fatalf("combineQMAPSessions() MTU = %d, want 1420", got.mtu)
			}
			wantDNS := []string{"1.1.1.1", "2001:4860:4860::8888"}
			if !slices.Equal(got.dns, wantDNS) {
				t.Fatalf("combineQMAPSessions() DNS = %v, want %v", got.dns, wantDNS)
			}
		})
	}
}
