package internet

import (
	"context"
	"errors"
	"net/netip"
	"slices"
	"sync"
	"testing"
	"time"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/netlink"
)

type qualcomm410LinkProbe struct {
	closeCalls int
	closeErr   error
}

func (l *qualcomm410LinkProbe) Close() error {
	l.closeCalls++
	return l.closeErr
}

func TestSetQualcomm410EnabledHoldsWDAWithoutOpeningPDN(t *testing.T) {
	previousOpen := openInternetQualcomm410Link
	previousValidate := validateInternetQualcomm410Layout
	previousCurrent := currentQualcomm410Bearer
	previousCleanup := cleanupInternetQualcomm410StaleState
	t.Cleanup(func() {
		openInternetQualcomm410Link = previousOpen
		validateInternetQualcomm410Layout = previousValidate
		currentQualcomm410Bearer = previousCurrent
		cleanupInternetQualcomm410StaleState = previousCleanup
	})

	var openCalls int
	link := &qualcomm410LinkProbe{}
	validateInternetQualcomm410Layout = func(*mmodem.Modem) error { return nil }
	openInternetQualcomm410Link = func(_ context.Context, cfg mmodem.BAMDMUXLinkConfig) (qualcomm410Link, error) {
		openCalls++
		if cfg.ControlPort != mmodem.Qualcomm410InternetQMI || cfg.InterfaceName != mmodem.Qualcomm410InternetInterface {
			t.Fatalf("OpenBAMDMUXLink config = %+v", cfg)
		}
		return link, nil
	}
	currentQualcomm410Bearer = func(context.Context, internetModem) (bearerState, error) {
		return bearerState{}, nil
	}
	cleanupInternetQualcomm410StaleState = func(context.Context, *Connector, string) error { return nil }

	connector := &Connector{
		operations:        make(map[string]*sync.Mutex),
		qualcomm410States: make(map[string]qualcomm410State),
	}
	modem := &mmodem.Modem{EquipmentIdentifier: "modem-1"}
	for range 2 {
		if err := connector.SetQualcomm410Enabled(context.Background(), modem, true); err != nil {
			t.Fatalf("SetQualcomm410Enabled(true) error = %v", err)
		}
	}
	if openCalls != 1 {
		t.Fatalf("WDA holder open calls = %d, want 1", openCalls)
	}
	if !connector.qualcomm410EnabledFor(modem.EquipmentIdentifier) {
		t.Fatal("Qualcomm 410 mode is disabled")
	}
	if link.closeCalls != 0 {
		t.Fatalf("WDA holder close calls = %d, want 0", link.closeCalls)
	}

	if err := connector.SetQualcomm410Enabled(context.Background(), modem, false); err != nil {
		t.Fatalf("SetQualcomm410Enabled(false) error = %v", err)
	}
	if link.closeCalls != 1 {
		t.Fatalf("WDA holder close calls = %d, want 1", link.closeCalls)
	}
	if connector.qualcomm410EnabledFor(modem.EquipmentIdentifier) {
		t.Fatal("Qualcomm 410 mode remains enabled")
	}
}

func TestSetQualcomm410EnabledValidatesModemBeforeChangingState(t *testing.T) {
	previousOpen := openInternetQualcomm410Link
	previousValidate := validateInternetQualcomm410Layout
	previousCurrent := currentQualcomm410Bearer
	t.Cleanup(func() {
		openInternetQualcomm410Link = previousOpen
		validateInternetQualcomm410Layout = previousValidate
		currentQualcomm410Bearer = previousCurrent
	})

	wantErr := errors.New("MM primary is not DATA5")
	modem := &mmodem.Modem{EquipmentIdentifier: "modem-1"}
	validateInternetQualcomm410Layout = func(got *mmodem.Modem) error {
		if got != modem {
			t.Fatalf("validated modem = %p, want %p", got, modem)
		}
		return wantErr
	}
	openInternetQualcomm410Link = func(context.Context, mmodem.BAMDMUXLinkConfig) (qualcomm410Link, error) {
		t.Fatal("opened WDA holder after layout validation failed")
		return nil, nil
	}
	currentQualcomm410Bearer = func(context.Context, internetModem) (bearerState, error) {
		t.Fatal("read bearer after layout validation failed")
		return bearerState{}, nil
	}

	connector := &Connector{operations: make(map[string]*sync.Mutex)}
	err := connector.SetQualcomm410Enabled(context.Background(), modem, true)
	if !errors.Is(err, wantErr) {
		t.Fatalf("SetQualcomm410Enabled(true) error = %v, want %v", err, wantErr)
	}
	if len(connector.qualcomm410States) != 0 {
		t.Fatalf("Qualcomm 410 states = %+v, want none", connector.qualcomm410States)
	}
}

func TestEnableQualcomm410DisconnectsBearerBeforeOpeningHolder(t *testing.T) {
	previousOpen := openInternetQualcomm410Link
	previousValidate := validateInternetQualcomm410Layout
	previousCurrent := currentQualcomm410Bearer
	previousDisconnect := disconnectInternetQualcomm410Bearer
	previousReconnect := reconnectInternetQualcomm410Bearer
	t.Cleanup(func() {
		openInternetQualcomm410Link = previousOpen
		validateInternetQualcomm410Layout = previousValidate
		currentQualcomm410Bearer = previousCurrent
		disconnectInternetQualcomm410Bearer = previousDisconnect
		reconnectInternetQualcomm410Bearer = previousReconnect
	})

	prefs := Preferences{APN: "3gnet", IPType: "ipv4v6"}
	currentQualcomm410Bearer = func(context.Context, internetModem) (bearerState, error) {
		return bearerState{connected: true}, nil
	}
	validateInternetQualcomm410Layout = func(*mmodem.Modem) error { return nil }
	var calls []string
	disconnectInternetQualcomm410Bearer = func(context.Context, *Connector, internetModem) error {
		calls = append(calls, "disconnect")
		return nil
	}
	link := &qualcomm410LinkProbe{}
	openInternetQualcomm410Link = func(context.Context, mmodem.BAMDMUXLinkConfig) (qualcomm410Link, error) {
		calls = append(calls, "open-holder")
		return link, nil
	}
	reconnectInternetQualcomm410Bearer = func(_ context.Context, _ *Connector, _ internetModem, got Preferences) error {
		calls = append(calls, "connect")
		if got != prefs {
			t.Fatalf("reconnect preferences = %+v, want %+v", got, prefs)
		}
		return nil
	}

	connector := &Connector{
		connections: map[string]trackedConnection{
			"modem-1": {prefs: prefs},
		},
		operations:        make(map[string]*sync.Mutex),
		qualcomm410States: make(map[string]qualcomm410State),
	}
	if err := connector.SetQualcomm410Enabled(context.Background(), &mmodem.Modem{EquipmentIdentifier: "modem-1"}, true); err != nil {
		t.Fatalf("SetQualcomm410Enabled(true) error = %v", err)
	}
	wantCalls := []string{"disconnect", "open-holder", "connect"}
	if !slices.Equal(calls, wantCalls) {
		t.Fatalf("calls = %v, want %v", calls, wantCalls)
	}
}

func TestDisableQualcomm410RetriesPendingBearerReconnect(t *testing.T) {
	previousReconnect := reconnectInternetQualcomm410Bearer
	t.Cleanup(func() { reconnectInternetQualcomm410Bearer = previousReconnect })

	wantErr := errors.New("restore normal bearer")
	calls := 0
	reconnectInternetQualcomm410Bearer = func(_ context.Context, _ *Connector, _ internetModem, prefs Preferences) error {
		calls++
		if prefs.APN != "3gnet" || prefs.IPType != "ipv4v6" {
			t.Fatalf("reconnect preferences = %+v", prefs)
		}
		if calls == 1 {
			return wantErr
		}
		return nil
	}

	connector := &Connector{
		operations: make(map[string]*sync.Mutex),
		qualcomm410States: map[string]qualcomm410State{
			"modem-1": {
				reconnectPending:     true,
				reconnectPreferences: Preferences{APN: "3gnet", IPType: "ipv4v6"},
			},
		},
	}
	modem := &mmodem.Modem{EquipmentIdentifier: "modem-1"}
	if err := connector.SetQualcomm410Enabled(context.Background(), modem, false); !errors.Is(err, wantErr) {
		t.Fatalf("first SetQualcomm410Enabled(false) error = %v, want %v", err, wantErr)
	}
	if state := connector.qualcomm410StateFor("modem-1"); !state.reconnectPending {
		t.Fatalf("state after failed reconnect = %+v, want pending", state)
	}
	if err := connector.SetQualcomm410Enabled(context.Background(), modem, false); err != nil {
		t.Fatalf("second SetQualcomm410Enabled(false) error = %v", err)
	}
	if state := connector.qualcomm410StateFor("modem-1"); state.reconnectPending {
		t.Fatalf("state after successful reconnect = %+v, want cleared", state)
	}
	if calls != 2 {
		t.Fatalf("reconnect calls = %d, want 2", calls)
	}
}

func TestDisableQualcomm410KeepsReconnectPendingAfterNormalBearerFailure(t *testing.T) {
	previousCurrent := currentQualcomm410Bearer
	previousDisconnect := disconnectInternetQualcomm410Bearer
	previousReconnect := reconnectInternetQualcomm410Bearer
	t.Cleanup(func() {
		currentQualcomm410Bearer = previousCurrent
		disconnectInternetQualcomm410Bearer = previousDisconnect
		reconnectInternetQualcomm410Bearer = previousReconnect
	})

	prefs := Preferences{APN: "3gnet", IPType: "ipv4v6"}
	currentQualcomm410Bearer = func(context.Context, internetModem) (bearerState, error) {
		return bearerState{connected: true}, nil
	}
	disconnectCalls := 0
	disconnectInternetQualcomm410Bearer = func(context.Context, *Connector, internetModem) error {
		disconnectCalls++
		return nil
	}
	wantErr := errors.New("restore normal bearer")
	reconnectCalls := 0
	reconnectInternetQualcomm410Bearer = func(_ context.Context, _ *Connector, _ internetModem, got Preferences) error {
		reconnectCalls++
		if got != prefs {
			t.Fatalf("reconnect preferences = %+v, want %+v", got, prefs)
		}
		if reconnectCalls == 1 {
			return wantErr
		}
		return nil
	}

	link := &qualcomm410LinkProbe{}
	connector := &Connector{
		connections: map[string]trackedConnection{
			"modem-1": {prefs: prefs},
		},
		operations: make(map[string]*sync.Mutex),
		qualcomm410States: map[string]qualcomm410State{
			"modem-1": {enabled: true, link: link},
		},
	}
	modem := &mmodem.Modem{EquipmentIdentifier: "modem-1"}
	if err := connector.SetQualcomm410Enabled(context.Background(), modem, false); !errors.Is(err, wantErr) {
		t.Fatalf("first SetQualcomm410Enabled(false) error = %v, want %v", err, wantErr)
	}
	state := connector.qualcomm410StateFor("modem-1")
	if state.enabled || state.link != nil || !state.reconnectPending || state.reconnectPreferences != prefs {
		t.Fatalf("state after failed normal restore = %+v", state)
	}
	if link.closeCalls != 1 || disconnectCalls != 1 {
		t.Fatalf("holder close/disconnect calls = %d/%d, want 1/1", link.closeCalls, disconnectCalls)
	}
	if err := connector.SetQualcomm410Enabled(context.Background(), modem, false); err != nil {
		t.Fatalf("second SetQualcomm410Enabled(false) error = %v", err)
	}
	if state := connector.qualcomm410StateFor("modem-1"); state.reconnectPending {
		t.Fatalf("state after successful retry = %+v, want cleared", state)
	}
	if reconnectCalls != 2 {
		t.Fatalf("reconnect calls = %d, want 2", reconnectCalls)
	}
}

func TestDisableQualcomm410CleansDisconnectedStaleNetworkBeforeClosingHolder(t *testing.T) {
	previousCurrent := currentQualcomm410Bearer
	previousCleanup := cleanupInternetQualcomm410StaleState
	t.Cleanup(func() {
		currentQualcomm410Bearer = previousCurrent
		cleanupInternetQualcomm410StaleState = previousCleanup
	})

	currentQualcomm410Bearer = func(context.Context, internetModem) (bearerState, error) {
		return bearerState{}, nil
	}
	wantErr := errors.New("clean stale network")
	cleanupCalls := 0
	cleanupInternetQualcomm410StaleState = func(_ context.Context, _ *Connector, modemID string) error {
		cleanupCalls++
		if modemID != "modem-1" {
			t.Fatalf("cleanup modem ID = %q", modemID)
		}
		if cleanupCalls == 1 {
			return wantErr
		}
		return nil
	}

	link := &qualcomm410LinkProbe{}
	connector := &Connector{
		operations: make(map[string]*sync.Mutex),
		qualcomm410States: map[string]qualcomm410State{
			"modem-1": {enabled: true, link: link},
		},
	}
	modem := &mmodem.Modem{EquipmentIdentifier: "modem-1"}
	if err := connector.SetQualcomm410Enabled(context.Background(), modem, false); !errors.Is(err, wantErr) {
		t.Fatalf("first SetQualcomm410Enabled(false) error = %v, want %v", err, wantErr)
	}
	if link.closeCalls != 0 || !connector.qualcomm410EnabledFor("modem-1") {
		t.Fatalf("state after cleanup failure = close calls %d, enabled %v", link.closeCalls, connector.qualcomm410EnabledFor("modem-1"))
	}
	if err := connector.SetQualcomm410Enabled(context.Background(), modem, false); err != nil {
		t.Fatalf("second SetQualcomm410Enabled(false) error = %v", err)
	}
	if cleanupCalls != 2 || link.closeCalls != 1 {
		t.Fatalf("cleanup/close calls = %d/%d, want 2/1", cleanupCalls, link.closeCalls)
	}
}

func TestInvalidateQualcomm410ReopensHolderAfterModemReload(t *testing.T) {
	previousOpen := openInternetQualcomm410Link
	previousValidate := validateInternetQualcomm410Layout
	previousCurrent := currentQualcomm410Bearer
	previousCleanup := cleanupInternetQualcomm410StaleState
	t.Cleanup(func() {
		openInternetQualcomm410Link = previousOpen
		validateInternetQualcomm410Layout = previousValidate
		currentQualcomm410Bearer = previousCurrent
		cleanupInternetQualcomm410StaleState = previousCleanup
	})

	oldLink := &qualcomm410LinkProbe{}
	newLink := &qualcomm410LinkProbe{}
	validateInternetQualcomm410Layout = func(*mmodem.Modem) error { return nil }
	openCalls := 0
	openInternetQualcomm410Link = func(context.Context, mmodem.BAMDMUXLinkConfig) (qualcomm410Link, error) {
		openCalls++
		return newLink, nil
	}
	currentQualcomm410Bearer = func(context.Context, internetModem) (bearerState, error) {
		return bearerState{}, nil
	}
	cleanupInternetQualcomm410StaleState = func(context.Context, *Connector, string) error { return nil }

	connector := &Connector{
		operations: make(map[string]*sync.Mutex),
		qualcomm410States: map[string]qualcomm410State{
			"modem-1": {enabled: true, link: oldLink},
		},
	}
	if err := connector.InvalidateQualcomm410("modem-1"); err != nil {
		t.Fatalf("InvalidateQualcomm410() error = %v", err)
	}
	if oldLink.closeCalls != 1 {
		t.Fatalf("old holder close calls = %d, want 1", oldLink.closeCalls)
	}
	state := connector.qualcomm410StateFor("modem-1")
	if state.link != nil || !state.reloadPending {
		t.Fatalf("state after invalidation = %+v", state)
	}
	if err := connector.SetQualcomm410Enabled(context.Background(), &mmodem.Modem{EquipmentIdentifier: "modem-1"}, true); err != nil {
		t.Fatalf("SetQualcomm410Enabled(true) error = %v", err)
	}
	state = connector.qualcomm410StateFor("modem-1")
	if openCalls != 1 || state.link != newLink || state.reloadPending {
		t.Fatalf("reloaded state = %+v, open calls %d", state, openCalls)
	}
}

func TestSetQualcomm410EnabledDoesNotPublishFailedHolder(t *testing.T) {
	previousOpen := openInternetQualcomm410Link
	previousValidate := validateInternetQualcomm410Layout
	previousCurrent := currentQualcomm410Bearer
	t.Cleanup(func() {
		openInternetQualcomm410Link = previousOpen
		validateInternetQualcomm410Layout = previousValidate
		currentQualcomm410Bearer = previousCurrent
	})

	wantErr := errors.New("allocate WDA client")
	validateInternetQualcomm410Layout = func(*mmodem.Modem) error { return nil }
	openInternetQualcomm410Link = func(context.Context, mmodem.BAMDMUXLinkConfig) (qualcomm410Link, error) {
		return nil, wantErr
	}
	currentQualcomm410Bearer = func(context.Context, internetModem) (bearerState, error) {
		return bearerState{}, nil
	}
	connector := &Connector{
		operations:        make(map[string]*sync.Mutex),
		qualcomm410States: make(map[string]qualcomm410State),
	}
	modem := &mmodem.Modem{EquipmentIdentifier: "modem-1"}
	if err := connector.SetQualcomm410Enabled(context.Background(), modem, true); !errors.Is(err, wantErr) {
		t.Fatalf("SetQualcomm410Enabled() error = %v, want %v", err, wantErr)
	}
	if connector.qualcomm410EnabledFor(modem.EquipmentIdentifier) {
		t.Fatal("Qualcomm 410 mode enabled after WDA holder failure")
	}
}

func TestDisconnectInternetKeepsQualcomm410Holder(t *testing.T) {
	connector, err := NewConnector(ConnectorConfig{State: testStore(t)})
	if err != nil {
		t.Fatalf("NewConnector() error = %v", err)
	}
	link := &qualcomm410LinkProbe{}
	connector.setQualcomm410State("modem-1", qualcomm410State{enabled: true, link: link})

	if err := connector.disconnect(context.Background(), fakeInternetModem{modemID: "modem-1"}, true); err != nil {
		t.Fatalf("disconnect() error = %v", err)
	}
	state := connector.qualcomm410StateFor("modem-1")
	if !state.enabled || state.link != link {
		t.Fatalf("Qualcomm 410 state = %+v, want retained holder", state)
	}
	if link.closeCalls != 0 {
		t.Fatalf("WDA holder close calls = %d, want 0", link.closeCalls)
	}
}

func TestQualcomm410BearerNetworkUsesPeerAddresses(t *testing.T) {
	ip4 := mmodem.BearerIPConfig{
		Method: mmodem.BearerIPMethodStatic, Address: "10.109.162.26", Prefix: 30,
		Gateway: "10.109.162.25", MTU: 1500,
	}
	ip6 := mmodem.BearerIPConfig{
		Method: mmodem.BearerIPMethodStatic, Address: "2408:843e:9c00:6884::2", Prefix: 64,
		Gateway: "2408:843e:9c00:6884::1", MTU: 1428,
	}

	addresses, peers, routes, mtu, err := qualcomm410BearerNetwork("wwan0", 50, ip4, ip6)
	if err != nil {
		t.Fatalf("qualcomm410BearerNetwork() error = %v", err)
	}
	wantAddresses := []netip.Prefix{
		netip.MustParsePrefix("10.109.162.26/32"),
		netip.MustParsePrefix("2408:843e:9c00:6884::2/128"),
	}
	if !slices.Equal(addresses, wantAddresses) {
		t.Fatalf("addresses = %v, want %v", addresses, wantAddresses)
	}
	if got := peers[wantAddresses[0]]; got != netip.MustParseAddr("10.109.162.25") {
		t.Fatalf("IPv4 peer = %v", got)
	}
	if got := peers[wantAddresses[1]]; got != netip.MustParseAddr("2408:843e:9c00:6884::1") {
		t.Fatalf("IPv6 peer = %v", got)
	}
	if got := routes[0].Gateway; got != netip.MustParseAddr("10.109.162.25") {
		t.Fatalf("IPv4 route gateway = %v", got)
	}
	if got := routes[1].Gateway; got.IsValid() {
		t.Fatalf("IPv6 route gateway = %v, want no gateway", got)
	}
	if mtu != 1428 {
		t.Fatalf("MTU = %d, want 1428", mtu)
	}
}

func TestQualcomm410BearerNetworkRejectsMissingPeer(t *testing.T) {
	ip4 := mmodem.BearerIPConfig{
		Method:  mmodem.BearerIPMethodStatic,
		Address: "10.109.162.26",
	}
	if _, _, _, _, err := qualcomm410BearerNetwork("wwan0", 50, ip4, mmodem.BearerIPConfig{}); err == nil {
		t.Fatal("qualcomm410BearerNetwork() error = nil, want missing peer error")
	}
}

type qualcomm410NetworkProbe struct {
	originalIPv6     netlink.IPv6Autoconfiguration
	restoredIPv6     []netlink.IPv6Autoconfiguration
	addedAddresses   []addressPair
	deletedAddresses []addressPair
	addedRoutes      []netlink.DefaultRoute
	deletedRoutes    []netlink.DefaultRoute
	readIPv6Err      error
	disableIPv6Err   error
	setIPv6Err       error
	addAddressErr    error
	addAddressCalls  int
	addAddressErrAt  int
}

type addressPair struct {
	local netip.Addr
	peer  netip.Addr
}

func (p *qualcomm410NetworkProbe) ops() qualcomm410NetworkOps {
	return qualcomm410NetworkOps{
		readIPv6Autoconfiguration: func(string) (netlink.IPv6Autoconfiguration, error) {
			return p.originalIPv6, p.readIPv6Err
		},
		setIPv6Autoconfiguration: func(_ string, cfg netlink.IPv6Autoconfiguration) error {
			p.restoredIPv6 = append(p.restoredIPv6, cfg)
			return p.setIPv6Err
		},
		disableIPv6Autoconfiguration: func(string) error { return p.disableIPv6Err },
		flushDefaultRoutes:           func(string) error { return nil },
		flushGlobalAddresses:         func(string) error { return nil },
		setUp:                        func(string) error { return nil },
		setMTU:                       func(string, uint32) error { return nil },
		defaultRoutes:                func() ([]netlink.DefaultRoute, error) { return nil, nil },
		addPointToPointAddress: func(_ string, local, peer netip.Addr) error {
			p.addAddressCalls++
			if p.addAddressErrAt > 0 && p.addAddressCalls == p.addAddressErrAt {
				if p.addAddressErr != nil {
					return p.addAddressErr
				}
				return errors.New("add peer address")
			}
			p.addedAddresses = append(p.addedAddresses, addressPair{local: local, peer: peer})
			return nil
		},
		deletePointToPointAddress: func(_ string, local, peer netip.Addr) error {
			p.deletedAddresses = append(p.deletedAddresses, addressPair{local: local, peer: peer})
			return nil
		},
		addDefaultRoute: func(route netlink.DefaultRoute) error {
			p.addedRoutes = append(p.addedRoutes, route)
			return nil
		},
		deleteDefaultRoute: func(route netlink.DefaultRoute) error {
			p.deletedRoutes = append(p.deletedRoutes, route)
			return nil
		},
	}
}

func TestConfigureQualcomm410BearerAppliesPeerAddresses(t *testing.T) {
	originalIPv6 := netlink.IPv6Autoconfiguration{Autoconf: 1, AcceptRA: 2}
	probe := &qualcomm410NetworkProbe{originalIPv6: originalIPv6}
	state := dbConnectionState{store: testStore(t)}
	tracked, err := configureQualcomm410BearerWithOps(
		context.Background(),
		state,
		"modem-1",
		"wwan0",
		mmodem.BearerIPConfig{Method: mmodem.BearerIPMethodStatic, Address: "10.0.0.2", Gateway: "10.0.0.1", MTU: 1500},
		mmodem.BearerIPConfig{Method: mmodem.BearerIPMethodStatic, Address: "2001:db8::2", Gateway: "2001:db8::1", MTU: 1428},
		Preferences{},
		probe.ops(),
	)
	if err != nil {
		t.Fatalf("configureQualcomm410BearerWithOps() error = %v", err)
	}
	if len(probe.addedAddresses) != 2 || len(probe.addedRoutes) != 2 {
		t.Fatalf("added addresses/routes = %d/%d, want 2/2", len(probe.addedAddresses), len(probe.addedRoutes))
	}
	if len(tracked.peers) != 2 {
		t.Fatalf("tracked peers = %v", tracked.peers)
	}
	if probe.addedRoutes[1].Gateway.IsValid() {
		t.Fatalf("IPv6 route gateway = %v, want none", probe.addedRoutes[1].Gateway)
	}
	if err := cleanupQualcomm410Applied(context.Background(), state, tracked, probe.ops()); err != nil {
		t.Fatalf("cleanupQualcomm410Applied() error = %v", err)
	}
	if !slices.Equal(probe.restoredIPv6, []netlink.IPv6Autoconfiguration{originalIPv6}) {
		t.Fatalf("restored IPv6 state = %v, want %v", probe.restoredIPv6, originalIPv6)
	}
}

func TestConfigureQualcomm410BearerRestoresIPv6AfterFailure(t *testing.T) {
	originalIPv6 := netlink.IPv6Autoconfiguration{Autoconf: 1, AcceptRA: 2}
	setupErr := errors.New("configure network")
	tests := []struct {
		name             string
		probe            *qualcomm410NetworkProbe
		wantDeleted      []addressPair
		wantRestoreError bool
	}{
		{
			name:  "disable autoconfiguration",
			probe: &qualcomm410NetworkProbe{originalIPv6: originalIPv6, disableIPv6Err: setupErr},
		},
		{
			name: "second peer address",
			probe: &qualcomm410NetworkProbe{
				originalIPv6:    originalIPv6,
				addAddressErr:   setupErr,
				addAddressErrAt: 2,
			},
			wantDeleted: []addressPair{{local: netip.MustParseAddr("10.0.0.2"), peer: netip.MustParseAddr("10.0.0.1")}},
		},
		{
			name: "restore error is joined",
			probe: &qualcomm410NetworkProbe{
				originalIPv6:    originalIPv6,
				addAddressErr:   setupErr,
				addAddressErrAt: 2,
				setIPv6Err:      errors.New("restore IPv6"),
			},
			wantDeleted:      []addressPair{{local: netip.MustParseAddr("10.0.0.2"), peer: netip.MustParseAddr("10.0.0.1")}},
			wantRestoreError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := configureQualcomm410BearerWithOps(
				context.Background(),
				dbConnectionState{store: testStore(t)},
				"modem-1",
				"wwan0",
				mmodem.BearerIPConfig{Method: mmodem.BearerIPMethodStatic, Address: "10.0.0.2", Gateway: "10.0.0.1"},
				mmodem.BearerIPConfig{Method: mmodem.BearerIPMethodStatic, Address: "2001:db8::2", Gateway: "2001:db8::1"},
				Preferences{},
				tt.probe.ops(),
			)
			if !errors.Is(err, setupErr) {
				t.Fatalf("configureQualcomm410BearerWithOps() error = %v, want %v", err, setupErr)
			}
			if !slices.Equal(tt.probe.deletedAddresses, tt.wantDeleted) {
				t.Fatalf("deleted peer addresses = %v, want %v", tt.probe.deletedAddresses, tt.wantDeleted)
			}
			if !slices.Equal(tt.probe.restoredIPv6, []netlink.IPv6Autoconfiguration{originalIPv6}) {
				t.Fatalf("restored IPv6 state = %v, want %v", tt.probe.restoredIPv6, originalIPv6)
			}
			if tt.wantRestoreError && !errors.Is(err, tt.probe.setIPv6Err) {
				t.Fatalf("configureQualcomm410BearerWithOps() error = %v, want joined %v", err, tt.probe.setIPv6Err)
			}
		})
	}
}

func TestQualcomm410CleanupContextIgnoresCancellationAndHasDeadline(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	cancelParent()

	ctx, cancel := qualcomm410CleanupContext(parent)
	defer cancel()
	if err := ctx.Err(); err != nil {
		t.Fatalf("qualcomm410CleanupContext() error = %v", err)
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("qualcomm410CleanupContext() has no deadline")
	}
	remaining := time.Until(deadline)
	if remaining <= qualcomm410CleanupTimeout-time.Second || remaining > qualcomm410CleanupTimeout {
		t.Fatalf("qualcomm410CleanupContext() remaining = %v, want about %v", remaining, qualcomm410CleanupTimeout)
	}
}

func TestConfigureQualcomm410BearerRejectsUnexpectedInterface(t *testing.T) {
	_, err := configureQualcomm410BearerWithOps(
		context.Background(),
		dbConnectionState{store: testStore(t)},
		"modem-1",
		"wwan1",
		mmodem.BearerIPConfig{},
		mmodem.BearerIPConfig{},
		Preferences{},
		(&qualcomm410NetworkProbe{}).ops(),
	)
	if err == nil {
		t.Fatal("configureQualcomm410BearerWithOps() error = nil")
	}
}

func TestQualcomm410ConstantsMatchDualDataLayout(t *testing.T) {
	if mmodem.Qualcomm410InternetQMI != "/dev/qmi_rmnet0" || mmodem.Qualcomm410InternetInterface != "wwan0" {
		t.Fatalf("DATA5 layout = %s/%s", mmodem.Qualcomm410InternetQMI, mmodem.Qualcomm410InternetInterface)
	}
	if mmodem.Qualcomm410IMSQMI != "/dev/qmi_rmnet1" || mmodem.Qualcomm410IMSInterface != "wwan1" {
		t.Fatalf("DATA6 layout = %s/%s", mmodem.Qualcomm410IMSQMI, mmodem.Qualcomm410IMSInterface)
	}
}
