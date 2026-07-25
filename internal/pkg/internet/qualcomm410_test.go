package internet

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"sync"
	"testing"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/netlink"
	"github.com/damonto/wwan-go/qcom"
)

type qualcomm410PDNLinkProbe struct {
	calls []mmodem.BAMDMUXPDNConfig
	infos map[qcom.WDSIPPreference]qcom.PDNInfo
	errs  map[qcom.WDSIPPreference]error
}

func (p *qualcomm410PDNLinkProbe) OpenPDN(_ context.Context, cfg mmodem.BAMDMUXPDNConfig) (qcom.PDNInfo, error) {
	p.calls = append(p.calls, cfg)
	return p.infos[cfg.IPPreference], p.errs[cfg.IPPreference]
}

func TestQualcomm410IPPreferences(t *testing.T) {
	tests := []struct {
		name    string
		ipType  string
		want    []qcom.WDSIPPreference
		wantErr bool
	}{
		{name: "default", want: []qcom.WDSIPPreference{qcom.WDSIPPreferenceIPv4, qcom.WDSIPPreferenceIPv6}},
		{name: "dual stack", ipType: "IPv4V6", want: []qcom.WDSIPPreference{qcom.WDSIPPreferenceIPv4, qcom.WDSIPPreferenceIPv6}},
		{name: "IPv4", ipType: "ipv4", want: []qcom.WDSIPPreference{qcom.WDSIPPreferenceIPv4}},
		{name: "IPv6", ipType: "ipv6", want: []qcom.WDSIPPreference{qcom.WDSIPPreferenceIPv6}},
		{name: "unsupported", ipType: "ipv5", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := qualcomm410IPPreferences(tt.ipType)
			if (err != nil) != tt.wantErr {
				t.Fatalf("qualcomm410IPPreferences() error = %v, wantErr %t", err, tt.wantErr)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("qualcomm410IPPreferences() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestQualcomm410Networks(t *testing.T) {
	infos := []qcom.PDNInfo{
		{
			LocalIPv4:        net.ParseIP("10.0.0.2"),
			IPv4Gateway:      net.ParseIP("10.0.0.1"),
			LocalIPv6:        net.ParseIP("2001:db8::2"),
			IPv6Gateway:      net.ParseIP("2001:db8::1"),
			IPv6PrefixLength: 64,
			DNS:              []net.IP{nil, {1, 2, 3}, net.IPv4zero, net.IPv6zero, net.ParseIP("1.1.1.1"), net.ParseIP("2606:4700:4700::1111")},
			MTU:              1428,
		},
		{
			LocalIPv4:   net.ParseIP("10.0.0.2"),
			IPv4Gateway: net.ParseIP("10.0.0.1"),
			DNS:         []net.IP{net.ParseIP("1.1.1.1"), net.ParseIP("2606:4700:4700::1111")},
			MTU:         1400,
		},
	}

	networks, dns, mtu, err := qualcomm410Networks(infos)
	if err != nil {
		t.Fatalf("qualcomm410Networks() error = %v", err)
	}
	if mtu != 1400 {
		t.Fatalf("MTU = %d, want 1400", mtu)
	}
	if len(networks) != 2 {
		t.Fatalf("networks = %d, want 2", len(networks))
	}
	wantV4 := netip.MustParsePrefix("10.0.0.2/32")
	wantV6 := netip.MustParsePrefix("2001:db8::2/128")
	if networks[0].prefix != wantV4 || networks[0].peer != netip.MustParseAddr("10.0.0.1") || networks[0].family != 2 {
		t.Fatalf("IPv4 network = %+v", networks[0])
	}
	if networks[1].prefix != wantV6 || networks[1].peer != netip.MustParseAddr("2001:db8::1") || networks[1].family != 10 {
		t.Fatalf("IPv6 network = %+v", networks[1])
	}
	if !slices.Equal(dns, []string{"1.1.1.1", "2606:4700:4700::1111"}) {
		t.Fatalf("DNS = %v", dns)
	}
}

func TestQualcomm410NetworksIgnoresZeroAndInvalidLegMTU(t *testing.T) {
	tests := []struct {
		name  string
		infos []qcom.PDNInfo
		want  uint32
	}{
		{
			name: "zero MTU does not erase known MTU",
			infos: []qcom.PDNInfo{
				{LocalIPv4: net.ParseIP("10.0.0.2"), IPv4Gateway: net.ParseIP("10.0.0.1"), MTU: 1428},
				{LocalIPv6: net.ParseIP("2001:db8::2"), IPv6Gateway: net.ParseIP("2001:db8::1")},
			},
			want: 1428,
		},
		{
			name: "invalid leg does not constrain MTU",
			infos: []qcom.PDNInfo{
				{MTU: 1200},
				{LocalIPv4: net.ParseIP("10.0.0.2"), IPv4Gateway: net.ParseIP("10.0.0.1"), MTU: 1428},
			},
			want: 1428,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, got, err := qualcomm410Networks(tt.infos)
			if err != nil {
				t.Fatalf("qualcomm410Networks() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("MTU = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestQualcomm410NetworksFiltersDNSByAvailableFamily(t *testing.T) {
	tests := []struct {
		name string
		info qcom.PDNInfo
		want []string
	}{
		{
			name: "IPv4 leg",
			info: qcom.PDNInfo{
				LocalIPv4:   net.ParseIP("10.0.0.2"),
				IPv4Gateway: net.ParseIP("10.0.0.1"),
				DNS:         []net.IP{net.ParseIP("1.1.1.1"), net.ParseIP("2606:4700:4700::1111")},
			},
			want: []string{"1.1.1.1"},
		},
		{
			name: "IPv6 leg",
			info: qcom.PDNInfo{
				LocalIPv6:   net.ParseIP("2001:db8::2"),
				IPv6Gateway: net.ParseIP("2001:db8::1"),
				DNS:         []net.IP{net.ParseIP("1.1.1.1"), net.ParseIP("2606:4700:4700::1111")},
			},
			want: []string{"2606:4700:4700::1111"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, got, _, err := qualcomm410Networks([]qcom.PDNInfo{tt.info})
			if err != nil {
				t.Fatalf("qualcomm410Networks() error = %v", err)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("DNS = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestQualcomm410NetworksRejectsEmptyInfo(t *testing.T) {
	if _, _, _, err := qualcomm410Networks(nil); err == nil {
		t.Fatal("qualcomm410Networks() error = nil, want error")
	}
}

func TestQualcomm410NetworksRejectsMismatchedAddressFamilies(t *testing.T) {
	tests := []struct {
		name string
		info qcom.PDNInfo
	}{
		{
			name: "IPv4 field contains IPv6",
			info: qcom.PDNInfo{LocalIPv4: net.ParseIP("2001:db8::2"), IPv4Gateway: net.ParseIP("2001:db8::1")},
		},
		{
			name: "IPv6 field contains IPv4",
			info: qcom.PDNInfo{LocalIPv6: net.ParseIP("10.0.0.2"), IPv6Gateway: net.ParseIP("10.0.0.1")},
		},
		{
			name: "IPv4 field contains unspecified address",
			info: qcom.PDNInfo{LocalIPv4: net.IPv4zero, IPv4Gateway: net.ParseIP("10.0.0.1")},
		},
		{
			name: "IPv6 gateway is unspecified",
			info: qcom.PDNInfo{LocalIPv6: net.ParseIP("2001:db8::2"), IPv6Gateway: net.IPv6zero},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, _, err := qualcomm410Networks([]qcom.PDNInfo{tt.info}); !errors.Is(err, ErrUnsupportedIPMethod) {
				t.Fatalf("qualcomm410Networks() error = %v, want %v", err, ErrUnsupportedIPMethod)
			}
		})
	}
}

func TestConfigureQualcomm410NetworkRestoresInterfaceStateOnFailure(t *testing.T) {
	setMTUCalls := []uint32{}
	setIPv6Calls := []netlink.IPv6Autoconfiguration{}
	flushCalls := []string{}
	setMTUErr := errors.New("set MTU rejected")
	ops := systemQualcomm410NetworkOps
	ops.interfaceByName = func(string) (*net.Interface, error) {
		return &net.Interface{MTU: 1500}, nil
	}
	ops.readIPv6Autoconfiguration = func(string) (netlink.IPv6Autoconfiguration, error) {
		return netlink.IPv6Autoconfiguration{Autoconf: 1, AcceptRA: 2}, nil
	}
	ops.setIPv6Autoconfiguration = func(_ string, state netlink.IPv6Autoconfiguration) error {
		setIPv6Calls = append(setIPv6Calls, state)
		return nil
	}
	ops.disableIPv6Autoconfiguration = func(string) error { return nil }
	ops.flushDefaultRoutes = func(name string) error {
		flushCalls = append(flushCalls, "routes:"+name)
		return nil
	}
	ops.flushGlobalAddresses = func(name string) error {
		flushCalls = append(flushCalls, "addresses:"+name)
		return nil
	}
	ops.setUp = func(string) error { return nil }
	ops.defaultRoutes = func() ([]netlink.DefaultRoute, error) { return nil, nil }
	ops.setMTU = func(_ string, mtu uint32) error {
		setMTUCalls = append(setMTUCalls, mtu)
		if len(setMTUCalls) == 1 {
			return setMTUErr
		}
		return nil
	}
	ops.addPointToPointAddress = func(string, netip.Addr, netip.Addr) error { return nil }
	ops.addDefaultRoute = func(netlink.DefaultRoute) error { return nil }

	_, _, _, err := configureQualcomm410NetworkWithOps(
		context.Background(),
		nil,
		"modem-1",
		Preferences{APN: "internet"},
		"wwan0",
		[]qcom.PDNInfo{{
			LocalIPv4:   net.ParseIP("10.0.0.2"),
			IPv4Gateway: net.ParseIP("10.0.0.1"),
			MTU:         1400,
		}},
		ops,
	)
	if !errors.Is(err, setMTUErr) {
		t.Fatalf("configureQualcomm410NetworkWithOps() error = %v, want %v", err, setMTUErr)
	}
	if !slices.Equal(setMTUCalls, []uint32{1400, 1500}) {
		t.Fatalf("SetMTU calls = %v, want [1400 1500]", setMTUCalls)
	}
	if !slices.Equal(setIPv6Calls, []netlink.IPv6Autoconfiguration{{Autoconf: 1, AcceptRA: 2}}) {
		t.Fatalf("IPv6 restore calls = %v, want original state", setIPv6Calls)
	}
	if want := []string{"routes:wwan0", "addresses:wwan0", "routes:wwan0", "addresses:wwan0"}; !slices.Equal(flushCalls, want) {
		t.Fatalf("flush calls = %v, want initial and rollback cleanup %v", flushCalls, want)
	}
}

func TestConfigureQualcomm410NetworkDoesNotResetInterfaceAfterIPv6DisableFailure(t *testing.T) {
	disableErr := errors.New("disable IPv6 rejected")
	var flushCalls []string
	var restored []netlink.IPv6Autoconfiguration
	ops := systemQualcomm410NetworkOps
	ops.interfaceByName = func(string) (*net.Interface, error) {
		return &net.Interface{MTU: 1500}, nil
	}
	ops.readIPv6Autoconfiguration = func(string) (netlink.IPv6Autoconfiguration, error) {
		return netlink.IPv6Autoconfiguration{Autoconf: 1, AcceptRA: 2}, nil
	}
	ops.disableIPv6Autoconfiguration = func(string) error { return disableErr }
	ops.setIPv6Autoconfiguration = func(_ string, state netlink.IPv6Autoconfiguration) error {
		restored = append(restored, state)
		return nil
	}
	ops.flushDefaultRoutes = func(name string) error {
		flushCalls = append(flushCalls, "routes:"+name)
		return nil
	}
	ops.flushGlobalAddresses = func(name string) error {
		flushCalls = append(flushCalls, "addresses:"+name)
		return nil
	}

	_, _, _, err := configureQualcomm410NetworkWithOps(
		context.Background(),
		nil,
		"modem-1",
		Preferences{APN: "internet", DefaultRoute: true},
		"wwan0",
		[]qcom.PDNInfo{{LocalIPv4: net.ParseIP("10.0.0.2"), IPv4Gateway: net.ParseIP("10.0.0.1")}},
		ops,
	)
	if !errors.Is(err, disableErr) {
		t.Fatalf("configureQualcomm410NetworkWithOps() error = %v, want %v", err, disableErr)
	}
	if len(flushCalls) != 0 {
		t.Fatalf("flush calls = %v, want none before interface reset starts", flushCalls)
	}
	want := []netlink.IPv6Autoconfiguration{{Autoconf: 1, AcceptRA: 2}}
	if !slices.Equal(restored, want) {
		t.Fatalf("IPv6 restore calls = %v, want %v", restored, want)
	}
}

func TestConfigureQualcomm410NetworkRollsBackAfterAddressFailure(t *testing.T) {
	addressErr := errors.New("add address rejected")
	var flushCalls []string
	var mtuCalls []uint32
	var restored []netlink.IPv6Autoconfiguration
	ops := systemQualcomm410NetworkOps
	ops.interfaceByName = func(string) (*net.Interface, error) {
		return &net.Interface{MTU: 1500}, nil
	}
	ops.readIPv6Autoconfiguration = func(string) (netlink.IPv6Autoconfiguration, error) {
		return netlink.IPv6Autoconfiguration{Autoconf: 1, AcceptRA: 2}, nil
	}
	ops.disableIPv6Autoconfiguration = func(string) error { return nil }
	ops.setIPv6Autoconfiguration = func(_ string, state netlink.IPv6Autoconfiguration) error {
		restored = append(restored, state)
		return nil
	}
	ops.flushDefaultRoutes = func(name string) error {
		flushCalls = append(flushCalls, "routes:"+name)
		return nil
	}
	ops.flushGlobalAddresses = func(name string) error {
		flushCalls = append(flushCalls, "addresses:"+name)
		return nil
	}
	ops.setUp = func(string) error { return nil }
	ops.setMTU = func(_ string, mtu uint32) error {
		mtuCalls = append(mtuCalls, mtu)
		return nil
	}
	ops.addPointToPointAddress = func(string, netip.Addr, netip.Addr) error {
		return addressErr
	}

	_, _, _, err := configureQualcomm410NetworkWithOps(
		context.Background(),
		nil,
		"modem-1",
		Preferences{APN: "internet", DefaultRoute: true},
		"wwan0",
		[]qcom.PDNInfo{{
			LocalIPv4:   net.ParseIP("10.0.0.2"),
			IPv4Gateway: net.ParseIP("10.0.0.1"),
			MTU:         1400,
		}},
		ops,
	)
	if !errors.Is(err, addressErr) {
		t.Fatalf("configureQualcomm410NetworkWithOps() error = %v, want %v", err, addressErr)
	}
	wantFlushes := []string{"routes:wwan0", "addresses:wwan0", "routes:wwan0", "addresses:wwan0"}
	if !slices.Equal(flushCalls, wantFlushes) {
		t.Fatalf("flush calls = %v, want %v", flushCalls, wantFlushes)
	}
	if !slices.Equal(mtuCalls, []uint32{1400, 1500}) {
		t.Fatalf("MTU calls = %v, want [1400 1500]", mtuCalls)
	}
	wantIPv6 := []netlink.IPv6Autoconfiguration{{Autoconf: 1, AcceptRA: 2}}
	if !slices.Equal(restored, wantIPv6) {
		t.Fatalf("IPv6 restore calls = %v, want %v", restored, wantIPv6)
	}
}

func TestRestoreQualcomm410InterfaceStateAttemptsAllSettings(t *testing.T) {
	var calls []string
	mtuErr := errors.New("restore MTU rejected")
	state := qualcomm410InterfaceState{
		originalIPv6: netlink.IPv6Autoconfiguration{Autoconf: 1, AcceptRA: 2},
		originalMTU:  1500,
		restoreIPv6:  true,
		restoreMTU:   true,
	}
	ops := systemQualcomm410NetworkOps
	ops.setMTU = func(name string, mtu uint32) error {
		calls = append(calls, "mtu:"+name+":"+fmt.Sprint(mtu))
		return mtuErr
	}
	ops.setIPv6Autoconfiguration = func(name string, cfg netlink.IPv6Autoconfiguration) error {
		calls = append(calls, fmt.Sprintf("ipv6:%s:%d:%d", name, cfg.Autoconf, cfg.AcceptRA))
		return nil
	}

	err := restoreQualcomm410InterfaceState("wwan0", state, ops)
	if !errors.Is(err, mtuErr) {
		t.Fatalf("restoreQualcomm410InterfaceState() error = %v, want %v", err, mtuErr)
	}
	want := []string{"mtu:wwan0:1500", "ipv6:wwan0:1:2"}
	if !slices.Equal(calls, want) {
		t.Fatalf("restore calls = %v, want %v", calls, want)
	}
}

func TestOpenQualcomm410PDNsSharesLinkAndDefersProfileSelection(t *testing.T) {
	ipv6Err := errors.New("IPv6 unavailable")
	link := &qualcomm410PDNLinkProbe{
		infos: map[qcom.WDSIPPreference]qcom.PDNInfo{
			qcom.WDSIPPreferenceIPv4: {LocalIPv4: net.ParseIP("10.0.0.2")},
		},
		errs: map[qcom.WDSIPPreference]error{
			qcom.WDSIPPreferenceIPv6: ipv6Err,
		},
	}
	infos, err := openQualcomm410PDNs(context.Background(), link, "3gnet", []qcom.WDSIPPreference{
		qcom.WDSIPPreferenceIPv4,
		qcom.WDSIPPreferenceIPv6,
	})
	if !errors.Is(err, ipv6Err) {
		t.Fatalf("openQualcomm410PDNs() error = %v, want %v", err, ipv6Err)
	}
	if len(infos) != 1 || !infos[0].LocalIPv4.Equal(net.ParseIP("10.0.0.2")) {
		t.Fatalf("openQualcomm410PDNs() infos = %+v, want IPv4 leg", infos)
	}
	if len(link.calls) != 2 {
		t.Fatalf("OpenPDN calls = %d, want 2 on one shared link", len(link.calls))
	}
	for i, want := range []qcom.WDSIPPreference{qcom.WDSIPPreferenceIPv4, qcom.WDSIPPreferenceIPv6} {
		if got := link.calls[i]; got.APN != "3gnet" || got.ProfileIndex != 0 || got.IPPreference != want {
			t.Fatalf("OpenPDN call %d = %+v", i, got)
		}
	}
}

func TestQualcomm410ConstantsMatchKitLayout(t *testing.T) {
	if mmodem.Qualcomm410InternetQMI != "/dev/qmi_rmnet0" || mmodem.Qualcomm410IMSQMI != "/dev/qmi_rmnet1" {
		t.Fatal("Qualcomm 410 QMI paths do not match the stable kit layout")
	}
	if mmodem.Qualcomm410InternetInterface != "wwan0" || mmodem.Qualcomm410IMSInterface != "wwan1" {
		t.Fatal("Qualcomm 410 interfaces do not match the stable kit layout")
	}
}

func TestHasQMIControlPortAcceptsSecondaryQMI(t *testing.T) {
	tests := []struct {
		name  string
		modem *mmodem.Modem
		want  bool
	}{
		{
			name: "primary QMI",
			modem: &mmodem.Modem{
				PrimaryPort: "/dev/cdc-wdm0",
				Ports:       []mmodem.ModemPort{{Device: "/dev/cdc-wdm0", PortType: mmodem.ModemPortTypeQmi}},
			},
			want: true,
		},
		{
			name: "secondary QMI",
			modem: &mmodem.Modem{
				PrimaryPort: "/dev/cdc-wdm0",
				Ports: []mmodem.ModemPort{
					{Device: "/dev/cdc-wdm0", PortType: mmodem.ModemPortTypeMbim},
					{Device: "/dev/qmi_rmnet0", PortType: mmodem.ModemPortTypeQmi},
				},
			},
			want: true,
		},
		{
			name:  "no QMI",
			modem: &mmodem.Modem{Ports: []mmodem.ModemPort{{Device: "/dev/cdc-wdm0", PortType: mmodem.ModemPortTypeMbim}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasQMIControlPort(tt.modem); got != tt.want {
				t.Fatalf("hasQMIControlPort() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDisableQualcomm410WithoutOwnedStateDoesNotTouchInterface(t *testing.T) {
	previousCleanup := cleanupInternetQualcomm410State
	previousCurrent := currentQualcomm410Bearer
	t.Cleanup(func() {
		cleanupInternetQualcomm410State = previousCleanup
		currentQualcomm410Bearer = previousCurrent
	})

	currentQualcomm410Bearer = func(context.Context, internetModem) (bearerState, error) {
		return bearerState{connected: true}, nil
	}
	cleanupCalled := false
	cleanupInternetQualcomm410State = func(_ context.Context, _ *Connector, _ string) error {
		cleanupCalled = true
		return nil
	}
	connector := &Connector{
		operations:        make(map[string]*sync.Mutex),
		qualcomm410States: make(map[string]qualcomm410State),
	}
	modem := &mmodem.Modem{EquipmentIdentifier: "modem-1"}
	if err := connector.SetQualcomm410Enabled(context.Background(), modem, false); err != nil {
		t.Fatalf("SetQualcomm410Enabled() error = %v", err)
	}
	if cleanupCalled {
		t.Fatal("SetQualcomm410Enabled() cleaned wwan0 without owned Qualcomm 410 state")
	}
}

func TestDisableQualcomm410WithoutOwnedStateCleansStaleInterfaceWhenBearerIsDisconnected(t *testing.T) {
	previousCleanup := cleanupInternetQualcomm410State
	previousCurrent := currentQualcomm410Bearer
	t.Cleanup(func() {
		cleanupInternetQualcomm410State = previousCleanup
		currentQualcomm410Bearer = previousCurrent
	})

	currentQualcomm410Bearer = func(context.Context, internetModem) (bearerState, error) {
		return bearerState{}, nil
	}
	cleanupCalls := 0
	cleanupInternetQualcomm410State = func(context.Context, *Connector, string) error {
		cleanupCalls++
		return nil
	}
	connector := &Connector{
		operations:        make(map[string]*sync.Mutex),
		qualcomm410States: make(map[string]qualcomm410State),
	}
	modem := &mmodem.Modem{EquipmentIdentifier: "modem-1"}
	if err := connector.SetQualcomm410Enabled(context.Background(), modem, false); err != nil {
		t.Fatalf("SetQualcomm410Enabled() error = %v", err)
	}
	if cleanupCalls != 1 {
		t.Fatalf("cleanup calls = %d, want 1", cleanupCalls)
	}
}

func TestRestoreAlwaysOnKeepsActiveQualcomm410Connection(t *testing.T) {
	connector, err := NewConnector(ConnectorConfig{State: testStore(t)})
	if err != nil {
		t.Fatalf("NewConnector() error = %v", err)
	}
	const modemID = "modem-1"
	const profileID = "8901000000000000000"
	prefs := Preferences{APN: "3gnet", AlwaysOn: true}
	if err := connector.syncAlwaysOnState(context.Background(), profileID, prefs); err != nil {
		t.Fatalf("syncAlwaysOnState() error = %v", err)
	}
	connection := &qualcomm410Connection{prefs: prefs}
	connector.setQualcomm410ConnectionAndPreference(modemID, connection, prefs)

	modem := &mmodem.Modem{EquipmentIdentifier: modemID, Sim: &mmodem.SIM{Identifier: profileID}}
	if err := connector.restoreAlwaysOn(context.Background(), modem, prefs); err != nil {
		t.Fatalf("restoreAlwaysOn() error = %v", err)
	}
	if got := connector.qualcomm410ConnectionFor(modemID); got != connection {
		t.Fatalf("Qualcomm 410 connection = %p, want %p", got, connection)
	}
}

func TestDisconnectQualcomm410ClearsAlwaysOnState(t *testing.T) {
	connector, err := NewConnector(ConnectorConfig{State: testStore(t)})
	if err != nil {
		t.Fatalf("NewConnector() error = %v", err)
	}
	modem := &mmodem.Modem{
		EquipmentIdentifier: "modem-1",
		Sim:                 &mmodem.SIM{Identifier: "8901000000000000000"},
	}
	prefs := Preferences{APN: "3gnet", AlwaysOn: true}
	if err := connector.syncAlwaysOnState(context.Background(), modem.Sim.Identifier, prefs); err != nil {
		t.Fatalf("syncAlwaysOnState() error = %v", err)
	}
	connector.setQualcomm410ConnectionAndPreference(modem.EquipmentIdentifier, &qualcomm410Connection{prefs: prefs}, prefs)

	if err := connector.Disconnect(context.Background(), modem); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
	if _, ok, err := connector.loadAlwaysOnStateForProfile(context.Background(), modem.Sim.Identifier); err != nil {
		t.Fatalf("loadAlwaysOnStateForProfile() error = %v", err)
	} else if ok {
		t.Fatal("loadAlwaysOnStateForProfile() ok = true, want false")
	}
	if got := connector.preference(modem.EquipmentIdentifier); got.AlwaysOn {
		t.Fatalf("preference AlwaysOn = true, want false: %+v", got)
	}
}

type qualcomm410ConnectionStateProbe struct {
	connectionStateStore
	deleteProxyErrors []error
	interfaces        []string
}

func (s *qualcomm410ConnectionStateProbe) deleteProxyState(_ context.Context, interfaceName string) error {
	s.interfaces = append(s.interfaces, interfaceName)
	if len(s.deleteProxyErrors) == 0 {
		return nil
	}
	err := s.deleteProxyErrors[0]
	s.deleteProxyErrors = s.deleteProxyErrors[1:]
	return err
}

func TestDisconnectQualcomm410RetainsStateForRetry(t *testing.T) {
	t.Run("network cleanup failure keeps tracked state", func(t *testing.T) {
		connector, err := NewConnector(ConnectorConfig{State: testStore(t)})
		if err != nil {
			t.Fatalf("NewConnector() error = %v", err)
		}
		modem := &mmodem.Modem{EquipmentIdentifier: "modem-1"}
		connection := &qualcomm410Connection{
			tracked: trackedConnection{interfaceName: "wwan0"},
			prefs:   Preferences{APN: "3gnet"},
		}
		connector.setQualcomm410ConnectionAndPreference(modem.EquipmentIdentifier, connection, connection.prefs)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		if err := connector.disconnectQualcomm410Locked(ctx, modem); !errors.Is(err, context.Canceled) {
			t.Fatalf("disconnectQualcomm410Locked() error = %v, want context canceled", err)
		}
		if got := connector.qualcomm410Connection(modem); got != connection {
			t.Fatalf("Qualcomm 410 connection = %p, want retained %p", got, connection)
		}
		if connection.tracked.interfaceName != "wwan0" || connection.prefs.APN != "3gnet" {
			t.Fatalf("retained connection = %+v", connection)
		}
		if err := connector.disconnectQualcomm410Locked(context.Background(), modem); err != nil {
			t.Fatalf("retry disconnectQualcomm410Locked() error = %v", err)
		}
		if connector.qualcomm410Connection(modem) != nil {
			t.Fatal("retry kept Qualcomm 410 connection")
		}
	})

	t.Run("proxy cleanup failure keeps original interface", func(t *testing.T) {
		connector, err := NewConnector(ConnectorConfig{State: testStore(t)})
		if err != nil {
			t.Fatalf("NewConnector() error = %v", err)
		}
		proxyErr := errors.New("delete proxy state")
		state := &qualcomm410ConnectionStateProbe{
			connectionStateStore: connector.persistence,
			deleteProxyErrors:    []error{proxyErr, nil},
		}
		connector.persistence = state
		modem := &mmodem.Modem{EquipmentIdentifier: "modem-1"}
		connection := &qualcomm410Connection{
			tracked:            trackedConnection{interfaceName: "wwan0"},
			proxyInterfaceName: "wwan0",
			prefs:              Preferences{APN: "3gnet"},
		}
		connector.setQualcomm410ConnectionAndPreference(modem.EquipmentIdentifier, connection, connection.prefs)

		if err := connector.disconnectQualcomm410Locked(context.Background(), modem); !errors.Is(err, proxyErr) {
			t.Fatalf("disconnectQualcomm410Locked() error = %v, want %v", err, proxyErr)
		}
		if connector.qualcomm410Connection(modem) != connection {
			t.Fatal("proxy cleanup failure removed Qualcomm 410 connection")
		}
		if connection.proxyInterfaceName != "wwan0" || connection.prefs.APN != "3gnet" {
			t.Fatalf("retained connection = %+v", connection)
		}
		if err := connector.disconnectQualcomm410Locked(context.Background(), modem); err != nil {
			t.Fatalf("retry disconnectQualcomm410Locked() error = %v", err)
		}
		if !slices.Equal(state.interfaces, []string{"wwan0", "wwan0"}) {
			t.Fatalf("proxy cleanup interfaces = %v, want [wwan0 wwan0]", state.interfaces)
		}
	})
}

func TestCurrentSerializesQualcomm410StateAccess(t *testing.T) {
	connector, err := NewConnector(ConnectorConfig{State: testStore(t)})
	if err != nil {
		t.Fatalf("NewConnector() error = %v", err)
	}
	const modemID = "modem-1"
	modem := &mmodem.Modem{EquipmentIdentifier: modemID}
	connection := &qualcomm410Connection{
		tracked: trackedConnection{interfaceName: mmodem.Qualcomm410InternetInterface},
		prefs:   Preferences{APN: "3gnet", IPType: "ipv4"},
	}
	connector.setQualcomm410ConnectionAndPreference(modemID, connection, connection.prefs)

	const iterations = 200
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for range iterations {
			unlock := connector.lockModem(modemID)
			state := connector.qualcomm410StateFor(modemID)
			state.connection.dns = []string{"1.1.1.1"}
			unlock()
		}
	}()
	go func() {
		defer wg.Done()
		for range iterations {
			if _, err := connector.Current(context.Background(), modem); err != nil {
				t.Errorf("Current() error = %v", err)
				return
			}
		}
	}()
	wg.Wait()
}

func TestDisableQualcomm410RetainsEnabledStateWhenCleanupFails(t *testing.T) {
	previousCleanup := cleanupInternetQualcomm410State
	t.Cleanup(func() { cleanupInternetQualcomm410State = previousCleanup })

	cleanupErr := errors.New("stale cleanup")
	cleanupCalls := 0
	cleanupInternetQualcomm410State = func(context.Context, *Connector, string) error {
		cleanupCalls++
		if cleanupCalls == 1 {
			return cleanupErr
		}
		return nil
	}
	connector, err := NewConnector(ConnectorConfig{State: testStore(t)})
	if err != nil {
		t.Fatalf("NewConnector() error = %v", err)
	}
	const modemID = "modem-1"
	connector.setQualcomm410Enabled(modemID, true)
	modem := &mmodem.Modem{EquipmentIdentifier: modemID}

	if err := connector.SetQualcomm410Enabled(context.Background(), modem, false); !errors.Is(err, cleanupErr) {
		t.Fatalf("SetQualcomm410Enabled() error = %v, want %v", err, cleanupErr)
	}
	if !connector.qualcomm410EnabledFor(modemID) {
		t.Fatal("Qualcomm 410 was disabled after failed stale cleanup")
	}
	if err := connector.SetQualcomm410Enabled(context.Background(), modem, false); err != nil {
		t.Fatalf("retry SetQualcomm410Enabled() error = %v", err)
	}
	if connector.qualcomm410EnabledFor(modemID) {
		t.Fatal("Qualcomm 410 remained enabled after successful cleanup")
	}
}

func TestDisableQualcomm410RetriesPendingNormalRestoreAfterRollbackFailure(t *testing.T) {
	previousRestore := restoreQualcomm410NormalBearer
	previousOpen := openInternetQualcomm410Link
	previousCleanup := cleanupInternetQualcomm410State
	t.Cleanup(func() {
		restoreQualcomm410NormalBearer = previousRestore
		openInternetQualcomm410Link = previousOpen
		cleanupInternetQualcomm410State = previousCleanup
	})

	normalErr := errors.New("restore normal bearer")
	rollbackErr := errors.New("rollback Qualcomm 410")
	restoreCalls := 0
	restoreQualcomm410NormalBearer = func(context.Context, *Connector, internetModem, Preferences) error {
		restoreCalls++
		if restoreCalls == 1 {
			return normalErr
		}
		return nil
	}
	openInternetQualcomm410Link = func(context.Context, mmodem.BAMDMUXLinkConfig) (*mmodem.BAMDMUXLink, error) {
		return nil, rollbackErr
	}
	cleanupInternetQualcomm410State = func(context.Context, *Connector, string) error { return nil }

	connector, err := NewConnector(ConnectorConfig{State: testStore(t)})
	if err != nil {
		t.Fatalf("NewConnector() error = %v", err)
	}
	const modemID = "modem-1"
	prefs := Preferences{APN: "3gnet", IPType: "ipv4v6", DefaultRoute: true}
	connector.setQualcomm410ConnectionAndPreference(modemID, &qualcomm410Connection{prefs: prefs}, prefs)
	modem := &mmodem.Modem{EquipmentIdentifier: modemID}

	err = connector.SetQualcomm410Enabled(context.Background(), modem, false)
	if !errors.Is(err, normalErr) || !errors.Is(err, rollbackErr) {
		t.Fatalf("first SetQualcomm410Enabled() error = %v, want restore and rollback errors", err)
	}
	state := connector.qualcomm410StateFor(modemID)
	if !state.restorePending || state.restorePreferences != prefs || state.connection != nil || state.enabled {
		t.Fatalf("state after failed restore = %+v, want pending normal bearer", state)
	}

	if err := connector.SetQualcomm410Enabled(context.Background(), modem, false); err != nil {
		t.Fatalf("retry SetQualcomm410Enabled() error = %v", err)
	}
	if state := connector.qualcomm410StateFor(modemID); state.active() {
		t.Fatalf("state after successful retry = %+v, want cleared", state)
	}
	if restoreCalls != 2 {
		t.Fatalf("normal bearer restore calls = %d, want 2", restoreCalls)
	}
}

func TestDisconnectQualcomm410CancelsPendingRestore(t *testing.T) {
	t.Run("fallback connection keeps selected 410 path", func(t *testing.T) {
		connector, err := NewConnector(ConnectorConfig{State: testStore(t)})
		if err != nil {
			t.Fatalf("NewConnector() error = %v", err)
		}
		const modemID = "modem-1"
		prefs := Preferences{APN: "3gnet", IPType: "ipv4v6"}
		connector.setQualcomm410ConnectionAndPreference(modemID, &qualcomm410Connection{prefs: prefs}, prefs)
		connector.setQualcomm410RestorePending(modemID, prefs)

		if err := connector.Disconnect(context.Background(), &mmodem.Modem{EquipmentIdentifier: modemID}); err != nil {
			t.Fatalf("Disconnect() error = %v", err)
		}
		state := connector.qualcomm410StateFor(modemID)
		if !state.enabled || state.connection != nil || state.restorePending {
			t.Fatalf("state after Disconnect() = %+v, want selected 410 path without a connection", state)
		}
	})

	t.Run("pending normal bearer is cleared without touching connected bearer", func(t *testing.T) {
		previousCleanup := cleanupInternetQualcomm410State
		previousCurrent := currentQualcomm410Bearer
		t.Cleanup(func() {
			cleanupInternetQualcomm410State = previousCleanup
			currentQualcomm410Bearer = previousCurrent
		})

		currentQualcomm410Bearer = func(context.Context, internetModem) (bearerState, error) {
			return bearerState{connected: true}, nil
		}
		cleanupCalled := false
		cleanupInternetQualcomm410State = func(context.Context, *Connector, string) error {
			cleanupCalled = true
			return nil
		}
		connector, err := NewConnector(ConnectorConfig{State: testStore(t)})
		if err != nil {
			t.Fatalf("NewConnector() error = %v", err)
		}
		const modemID = "modem-1"
		connector.setQualcomm410RestorePending(modemID, Preferences{APN: "3gnet"})

		if err := connector.Disconnect(context.Background(), &mmodem.Modem{EquipmentIdentifier: modemID}); err != nil {
			t.Fatalf("Disconnect() error = %v", err)
		}
		if state := connector.qualcomm410StateFor(modemID); state.active() {
			t.Fatalf("state after Disconnect() = %+v, want cleared", state)
		}
		if cleanupCalled {
			t.Fatal("Disconnect() cleaned a connected normal bearer")
		}
	})
}

func TestRestoreQualcomm410RetainsEnabledStateWhenCleanupFails(t *testing.T) {
	previousCleanup := cleanupInternetQualcomm410State
	t.Cleanup(func() { cleanupInternetQualcomm410State = previousCleanup })

	cleanupErr := errors.New("stale cleanup")
	cleanupCalls := 0
	cleanupInternetQualcomm410State = func(context.Context, *Connector, string) error {
		cleanupCalls++
		if cleanupCalls == 1 {
			return cleanupErr
		}
		return nil
	}
	connector, err := NewConnector(ConnectorConfig{State: testStore(t)})
	if err != nil {
		t.Fatalf("NewConnector() error = %v", err)
	}
	const modemID = "modem-1"
	connector.setQualcomm410Enabled(modemID, true)
	modem := &mmodem.Modem{EquipmentIdentifier: modemID}

	if err := connector.Restore(context.Background(), modem); !errors.Is(err, cleanupErr) {
		t.Fatalf("Restore() error = %v, want %v", err, cleanupErr)
	}
	if !connector.qualcomm410EnabledFor(modemID) {
		t.Fatal("Restore() cleared Qualcomm 410 state before cleanup succeeded")
	}
	if err := connector.Restore(context.Background(), modem); err != nil {
		t.Fatalf("retry Restore() error = %v", err)
	}
	if connector.qualcomm410EnabledFor(modemID) {
		t.Fatal("retry Restore() kept Qualcomm 410 enabled")
	}
}

func TestDisconnectQualcomm410ClearsConnectionAlwaysOnBeforeRetry(t *testing.T) {
	connector, err := NewConnector(ConnectorConfig{State: testStore(t)})
	if err != nil {
		t.Fatalf("NewConnector() error = %v", err)
	}
	const modemID = "modem-1"
	const profileID = "8901000000000000000"
	modem := &mmodem.Modem{
		EquipmentIdentifier: modemID,
		Sim:                 &mmodem.SIM{Identifier: profileID},
	}
	prefs := Preferences{APN: "3gnet", AlwaysOn: true}
	if err := connector.syncAlwaysOnState(context.Background(), profileID, prefs); err != nil {
		t.Fatalf("syncAlwaysOnState() error = %v", err)
	}
	proxyErr := errors.New("delete proxy state")
	state := &qualcomm410ConnectionStateProbe{
		connectionStateStore: connector.persistence,
		deleteProxyErrors:    []error{proxyErr, nil},
	}
	connector.persistence = state
	connection := &qualcomm410Connection{
		proxyInterfaceName: "wwan0",
		prefs:              prefs,
	}
	connector.setQualcomm410ConnectionAndPreference(modemID, connection, prefs)

	if err := connector.Disconnect(context.Background(), modem); !errors.Is(err, proxyErr) {
		t.Fatalf("Disconnect() error = %v, want %v", err, proxyErr)
	}
	retained := connector.qualcomm410ConnectionFor(modemID)
	if retained == nil || retained.prefs.AlwaysOn {
		t.Fatalf("retained connection = %+v, want AlwaysOn=false", retained)
	}
	if got := connector.preference(modemID); got.AlwaysOn {
		t.Fatalf("in-memory preference = %+v, want AlwaysOn=false", got)
	}
	if _, ok, err := connector.loadAlwaysOnStateForProfile(context.Background(), profileID); err != nil {
		t.Fatalf("loadAlwaysOnStateForProfile() error = %v", err)
	} else if ok {
		t.Fatal("persistent AlwaysOn state remained after manual disconnect")
	}

	if err := connector.Disconnect(context.Background(), modem); err != nil {
		t.Fatalf("retry Disconnect() error = %v", err)
	}
}

func TestRestoreQualcomm410DisconnectsWithoutCreatingBearer(t *testing.T) {
	previousCleanup := cleanupInternetQualcomm410State
	t.Cleanup(func() {
		cleanupInternetQualcomm410State = previousCleanup
	})
	cleanupInternetQualcomm410State = func(context.Context, *Connector, string) error { return nil }

	connector, err := NewConnector(ConnectorConfig{State: testStore(t)})
	if err != nil {
		t.Fatalf("NewConnector() error = %v", err)
	}
	modem := &mmodem.Modem{EquipmentIdentifier: "modem-1"}
	connector.setQualcomm410Enabled(modem.EquipmentIdentifier, true)
	connector.setQualcomm410ConnectionAndPreference(modem.EquipmentIdentifier, &qualcomm410Connection{}, Preferences{})

	if err := connector.Restore(context.Background(), modem); err != nil {
		t.Fatalf("Restore() error = %v", err)
	}
	if connector.qualcomm410Connection(modem) != nil {
		t.Fatal("Restore() kept Qualcomm 410 connection")
	}
	if connector.qualcomm410EnabledFor(modem.EquipmentIdentifier) {
		t.Fatal("Restore() kept Qualcomm 410 enabled")
	}
}
