//go:build ims

package ims

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/netlink"
	"github.com/damonto/wwan-go/qcom"
	usimcard "github.com/damonto/wwan-go/sim/card"
)

func TestATReaderPortsPreferPrimaryThenFallbackPorts(t *testing.T) {
	tests := []struct {
		name  string
		modem *mmodem.Modem
		want  []mmodem.ModemPort
	}{
		{
			name: "keeps AT fallback ports after Device primary",
			modem: &mmodem.Modem{
				PrimaryPort:    "/dev/cdc-wdm1",
				PrimarySimSlot: 1,
				Ports: []mmodem.ModemPort{
					{PortType: mmodem.ModemPortTypeQmi, Device: "/dev/cdc-wdm1"},
					{PortType: mmodem.ModemPortTypeAt, Device: "/dev/ttyUSB6"},
					{PortType: mmodem.ModemPortTypeAt, Device: "/dev/ttyUSB7"},
				},
			},
			want: []mmodem.ModemPort{
				{PortType: mmodem.ModemPortTypeAt, Device: "/dev/ttyUSB6"},
				{PortType: mmodem.ModemPortTypeAt, Device: "/dev/ttyUSB7"},
			},
		},
		{
			name: "unknown primary uses AT ports only",
			modem: &mmodem.Modem{
				PrimaryPort: "/dev/ttyGPS0",
				Ports: []mmodem.ModemPort{
					{PortType: mmodem.ModemPortTypeGps, Device: "/dev/ttyGPS0"},
					{PortType: mmodem.ModemPortTypeMbim, Device: "/dev/cdc-wdm0"},
					{PortType: mmodem.ModemPortTypeAt, Device: "/dev/ttyUSB2"},
				},
			},
			want: []mmodem.ModemPort{{PortType: mmodem.ModemPortTypeAt, Device: "/dev/ttyUSB2"}},
		},
		{
			name: "MBIM primary keeps AT fallback",
			modem: &mmodem.Modem{
				PrimaryPort: "/dev/cdc-wdm0",
				Ports: []mmodem.ModemPort{
					{PortType: mmodem.ModemPortTypeMbim, Device: "/dev/cdc-wdm0"},
					{PortType: mmodem.ModemPortTypeAt, Device: "/dev/ttyUSB2"},
				},
			},
			want: []mmodem.ModemPort{{PortType: mmodem.ModemPortTypeAt, Device: "/dev/ttyUSB2"}},
		},
		{
			name: "deduplicates primary port",
			modem: &mmodem.Modem{
				PrimaryPort: "/dev/ttyUSB2",
				Ports: []mmodem.ModemPort{
					{PortType: mmodem.ModemPortTypeAt, Device: "/dev/ttyUSB2"},
				},
			},
			want: []mmodem.ModemPort{{PortType: mmodem.ModemPortTypeAt, Device: "/dev/ttyUSB2"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := atReaderPorts(tt.modem)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("atReaderPorts() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestOpenWWANRejectsUnsupportedAccess(t *testing.T) {
	tests := []struct {
		name   string
		access Access
	}{
		{name: "empty access"},
		{name: "unknown access", access: "satellite"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := OpenWWAN(context.Background(), nil, WWANConfig{Access: tt.access})
			if err == nil {
				t.Fatal("OpenWWAN() error = nil, want error")
			}
		})
	}
}

func TestOpenWWANRequiresOneQMIDataPath(t *testing.T) {
	muxDataPort := &qcom.WDSMuxDataPort{MuxID: 2}
	tests := []struct {
		name string
		cfg  WWANConfig
	}{
		{
			name: "missing data path",
			cfg:  WWANConfig{Access: AccessVoLTE},
		},
		{
			name: "both data paths",
			cfg: WWANConfig{
				Access:            AccessVoLTE,
				MuxDataPort:       muxDataPort,
				LegacyMuxDataPort: qcom.WDSSIOPortA2MuxRMNET0,
			},
		},
	}
	modem := &mmodem.Modem{
		PrimarySimSlot: 1,
		Ports: []mmodem.ModemPort{{
			Device:   "/dev/cdc-wdm0",
			PortType: mmodem.ModemPortTypeQmi,
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := OpenWWAN(context.Background(), modem, tt.cfg)
			if err == nil {
				t.Fatal("OpenWWAN() error = nil, want error")
			}
		})
	}
}

func TestOpenWWANFallsBackAfterDeviceFailure(t *testing.T) {
	modem := &mmodem.Modem{
		PrimaryPort:    "/dev/cdc-wdm1",
		PrimarySimSlot: 2,
		Ports: []mmodem.ModemPort{
			{PortType: mmodem.ModemPortTypeQmi, Device: "/dev/cdc-wdm1"},
			{PortType: mmodem.ModemPortTypeAt, Device: "/dev/ttyUSB6"},
			{PortType: mmodem.ModemPortTypeAt, Device: "/dev/ttyUSB7"},
		},
	}
	var atAttempts []string
	var deviceCalled bool
	reader, err := openWiFiCallingWWANWith(
		context.Background(),
		modem,
		func(_ context.Context, got *mmodem.Modem) (usimcard.Reader, error) {
			deviceCalled = true
			if got != modem {
				t.Fatalf("OpenDevice modem = %p, want %p", got, modem)
			}
			return nil, errors.New("device unavailable")
		},
		func(_ context.Context, port mmodem.ModemPort) (usimcard.Reader, error) {
			atAttempts = append(atAttempts, port.Device)
			if port.Device != "/dev/ttyUSB7" {
				return nil, errors.New("AT reader unavailable")
			}
			return fakeUSIMReader{}, nil
		},
	)
	if err != nil {
		t.Fatalf("openWiFiCallingWWANWith() error = %v", err)
	}
	if reader == nil {
		t.Fatal("openWiFiCallingWWANWith() reader is nil")
	}
	if !deviceCalled {
		t.Fatal("device open called = false, want true")
	}
	want := []string{"/dev/ttyUSB6", "/dev/ttyUSB7"}
	if !slices.Equal(atAttempts, want) {
		t.Fatalf("AT attempts = %+v, want %+v", atAttempts, want)
	}
}

func TestOpenWWANReturnsJoinedTransportErrors(t *testing.T) {
	modem := &mmodem.Modem{
		PrimaryPort: "/dev/cdc-wdm1",
		Ports: []mmodem.ModemPort{
			{PortType: mmodem.ModemPortTypeQmi, Device: "/dev/cdc-wdm1"},
			{PortType: mmodem.ModemPortTypeAt, Device: "/dev/ttyUSB6"},
		},
	}
	_, err := openWiFiCallingWWANWith(
		context.Background(),
		modem,
		func(context.Context, *mmodem.Modem) (usimcard.Reader, error) {
			return nil, errors.New("device unavailable")
		},
		func(context.Context, mmodem.ModemPort) (usimcard.Reader, error) {
			return nil, errors.New("AT unavailable")
		},
	)
	if err == nil {
		t.Fatal("openWiFiCallingWWANWith() error = nil, want error")
	}
	for _, want := range []string{"open modem WWAN", "open AT WWAN on /dev/ttyUSB6"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not contain %q", err, want)
		}
	}
}

func TestVoLTEInterfaceName(t *testing.T) {
	tests := []struct {
		name    string
		modem   *mmodem.Modem
		want    string
		wantErr bool
	}{
		{
			name: "strips device directory from modem network port",
			modem: &mmodem.Modem{Ports: []mmodem.ModemPort{
				{PortType: mmodem.ModemPortTypeQmi, Device: "/dev/cdc-wdm2"},
				{PortType: mmodem.ModemPortTypeNet, Device: "/dev/wws27u4i4"},
			}},
			want: "wws27u4i4",
		},
		{
			name:  "keeps bare network interface name",
			modem: &mmodem.Modem{Ports: []mmodem.ModemPort{{PortType: mmodem.ModemPortTypeNet, Device: "wws27u4i4"}}},
			want:  "wws27u4i4",
		},
		{name: "missing network port", modem: &mmodem.Modem{}, wantErr: true},
		{name: "nil modem", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := voLTEInterfaceName(tt.modem)
			if tt.wantErr {
				if err == nil {
					t.Fatal("voLTEInterfaceName() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("voLTEInterfaceName() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("voLTEInterfaceName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestVoLTEControlPort(t *testing.T) {
	tests := []struct {
		name    string
		modem   *mmodem.Modem
		want    mmodem.ModemPort
		wantErr bool
	}{
		{
			name: "prefers QMI for IMS PDN access",
			modem: &mmodem.Modem{Ports: []mmodem.ModemPort{
				{PortType: mmodem.ModemPortTypeMbim, Device: "/dev/cdc-wdm0"},
				{PortType: mmodem.ModemPortTypeQmi, Device: "/dev/cdc-wdm1"},
			}},
			want: mmodem.ModemPort{PortType: mmodem.ModemPortTypeQmi, Device: "/dev/cdc-wdm1"},
		},
		{
			name:  "falls back to MBIM",
			modem: &mmodem.Modem{Ports: []mmodem.ModemPort{{PortType: mmodem.ModemPortTypeMbim, Device: "/dev/cdc-wdm0"}}},
			want:  mmodem.ModemPort{PortType: mmodem.ModemPortTypeMbim, Device: "/dev/cdc-wdm0"},
		},
		{name: "rejects missing control port", modem: &mmodem.Modem{}, wantErr: true},
		{name: "rejects nil modem", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := voLTEControlPort(tt.modem)
			if tt.wantErr {
				if err == nil {
					t.Fatal("voLTEControlPort() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("voLTEControlPort() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("voLTEControlPort() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestVoLTESIMSlot(t *testing.T) {
	tests := []struct {
		name    string
		modem   *mmodem.Modem
		want    uint8
		wantErr bool
	}{
		{name: "primary slot", modem: &mmodem.Modem{PrimarySimSlot: 2}, want: 2},
		{name: "unspecified slot defaults to first slot", modem: &mmodem.Modem{}, want: 1},
		{name: "slot out of range", modem: &mmodem.Modem{PrimarySimSlot: 6}, wantErr: true},
		{name: "nil modem", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := voLTESIMSlot(tt.modem)
			if tt.wantErr {
				if err == nil {
					t.Fatal("voLTESIMSlot() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("voLTESIMSlot() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("voLTESIMSlot() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestConfigureIMSPDNNetwork(t *testing.T) {
	resetIMSPolicyReservations(t)
	tests := []struct {
		name               string
		dedicatedInterface bool
		wantDisable        bool
	}{
		{name: "dedicated interface disables autoconfiguration", dedicatedInterface: true, wantDisable: true},
		{name: "shared interface preserves autoconfiguration"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls []string
			links := fakePDNLinks{
				disableIPv6Autoconfiguration: func(name string) error {
					calls = append(calls, "disable-ipv6-autoconf:"+name)
					return nil
				},
				setUp: func(name string) error {
					calls = append(calls, "up:"+name)
					return nil
				},
				addAddress: func(name string, prefix netip.Prefix) error {
					calls = append(calls, fmt.Sprintf("add-address:%s:%s", name, prefix))
					return nil
				},
				addPointToPointAddress: func(name string, local, peer netip.Addr) error {
					calls = append(calls, fmt.Sprintf("add-peer-address:%s:%s:%s", name, local, peer))
					return nil
				},
				deleteAddress: func(name string, prefix netip.Prefix) error {
					calls = append(calls, fmt.Sprintf("delete-address:%s:%s", name, prefix))
					return nil
				},
				deletePointToPointAddress: func(name string, local, peer netip.Addr) error {
					calls = append(calls, fmt.Sprintf("delete-peer-address:%s:%s:%s", name, local, peer))
					return nil
				},
				addDefaultRoute: func(route netlink.DefaultRoute) error {
					calls = append(calls, fmt.Sprintf("add-default:%s:%d:%s:%d", route.Interface, route.Family, route.Source, route.Table))
					return nil
				},
				deleteDefaultRoute: func(route netlink.DefaultRoute) error {
					calls = append(calls, fmt.Sprintf("delete-default:%s:%d:%s:%d", route.Interface, route.Family, route.Source, route.Table))
					return nil
				},
				flushDefaultRoutesInTable: func(table uint32) error {
					calls = append(calls, fmt.Sprintf("flush-table:%d", table))
					return nil
				},
				addPolicyRule: func(rule netlink.PolicyRule) error {
					calls = append(calls, fmt.Sprintf("add-rule:%s:%d:%d:%d", rule.OutputInterface, rule.Family, rule.Priority, rule.Table))
					return nil
				},
				deletePolicyRule: func(rule netlink.PolicyRule) error {
					calls = append(calls, fmt.Sprintf("delete-rule:%s:%d:%d:%d", rule.OutputInterface, rule.Family, rule.Priority, rule.Table))
					return nil
				},
			}
			info := imsPDNInfo{
				LocalIPv4:   net.ParseIP("10.0.0.2"),
				LocalIPv6:   net.ParseIP("2001:db8::2"),
				IPv4Gateway: net.ParseIP("10.0.0.1"),
				IPv6Gateway: net.ParseIP("2001:db8::1"),
				PCSCFIPs:    []net.IP{net.ParseIP("10.0.0.10"), net.ParseIP("2001:db8::10")},
			}

			network := &pdnNetwork{parent: "wwan0", mbim: tt.dedicatedInterface, links: links}
			state, err := network.configure(context.Background(), "wwan0", info)
			if err != nil {
				t.Fatalf("configure() error = %v", err)
			}
			if _, err := network.cleanup("wwan0", state); err != nil {
				t.Fatalf("cleanup() error = %v", err)
			}
			policyCalls := []string{
				"flush-table:20000",
				"add-default:wwan0:2:10.0.0.2:20000",
				"add-default:wwan0:10:2001:db8::2:20000",
				"add-rule:wwan0:2:10000:20000",
				"add-rule:wwan0:10:10000:20000",
				"delete-rule:wwan0:2:10000:20000",
				"delete-rule:wwan0:10:10000:20000",
				"delete-default:wwan0:2:10.0.0.2:20000",
				"delete-default:wwan0:10:2001:db8::2:20000",
				"flush-table:20000",
			}
			var want []string
			if tt.wantDisable {
				want = []string{
					"disable-ipv6-autoconf:wwan0",
					"up:wwan0",
					"add-address:wwan0:10.0.0.2/32",
					"add-address:wwan0:2001:db8::2/128",
				}
				want = append(want, policyCalls...)
				want = append(want,
					"delete-address:wwan0:10.0.0.2/32",
					"delete-address:wwan0:2001:db8::2/128",
				)
			} else {
				want = []string{
					"up:wwan0",
					"add-peer-address:wwan0:10.0.0.2:10.0.0.1",
					"add-peer-address:wwan0:2001:db8::2:2001:db8::1",
				}
				want = append(want, policyCalls...)
				want = append(want,
					"delete-peer-address:wwan0:10.0.0.2:10.0.0.1",
					"delete-peer-address:wwan0:2001:db8::2:2001:db8::1",
				)
			}
			if !slices.Equal(calls, want) {
				t.Fatalf("network calls = %v, want %v", calls, want)
			}
		})
	}
}

func TestManagedVoLTECardUsesMBIMSessionInterface(t *testing.T) {
	resetIMSPolicyReservations(t)
	tests := []struct {
		name      string
		sessionID uint32
		stale     bool
		wantErr   bool
	}{
		{name: "session one VLAN", sessionID: 1},
		{name: "stale session VLAN replaced", sessionID: 1, stale: true},
		{name: "session zero rejected", sessionID: 0, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			previousInterfaceByName := imsInterfaceByName
			imsInterfaceByName = func(name string) (*net.Interface, error) {
				return &net.Interface{Index: 7, Name: name}, nil
			}
			t.Cleanup(func() { imsInterfaceByName = previousInterfaceByName })
			interfaceName, err := mbimSessionInterfaceName("lo", tt.sessionID)
			if err != nil {
				t.Fatalf("mbimSessionInterfaceName() error = %v", err)
			}
			var calls []string
			addVLANCalls := 0
			links := fakePDNLinks{
				disableIPv6Autoconfiguration: func(name string) error {
					calls = append(calls, "disable-ipv6-autoconf:"+name)
					return nil
				},
				setUp: func(name string) error {
					calls = append(calls, "up:"+name)
					return nil
				},
				addAddress: func(name string, prefix netip.Prefix) error {
					calls = append(calls, fmt.Sprintf("add-address:%s:%s", name, prefix))
					return nil
				},
				deleteAddress: func(name string, prefix netip.Prefix) error {
					calls = append(calls, fmt.Sprintf("delete-address:%s:%s", name, prefix))
					return nil
				},
				addDefaultRoute: func(route netlink.DefaultRoute) error {
					calls = append(calls, fmt.Sprintf("add-default:%s:%d:%s:%d", route.Interface, route.Family, route.Source, route.Table))
					return nil
				},
				deleteDefaultRoute: func(route netlink.DefaultRoute) error {
					calls = append(calls, fmt.Sprintf("delete-default:%s:%d:%s:%d", route.Interface, route.Family, route.Source, route.Table))
					return nil
				},
				flushDefaultRoutesInTable: func(table uint32) error {
					calls = append(calls, fmt.Sprintf("flush-table:%d", table))
					return nil
				},
				addPolicyRule: func(rule netlink.PolicyRule) error {
					calls = append(calls, fmt.Sprintf("add-rule:%s:%d:%d:%d", rule.OutputInterface, rule.Family, rule.Priority, rule.Table))
					return nil
				},
				deletePolicyRule: func(rule netlink.PolicyRule) error {
					calls = append(calls, fmt.Sprintf("delete-rule:%s:%d:%d:%d", rule.OutputInterface, rule.Family, rule.Priority, rule.Table))
					return nil
				},
				addVLAN: func(parent, name string, id uint16) error {
					calls = append(calls, fmt.Sprintf("add-vlan:%s:%s:%d", parent, name, id))
					addVLANCalls++
					if tt.stale && addVLANCalls == 1 {
						return syscall.EEXIST
					}
					return nil
				},
				deleteLink: func(name string) error {
					calls = append(calls, "delete-link:"+name)
					return nil
				},
			}
			network := &pdnNetwork{parent: "lo", mbim: true, links: links}
			info := imsPDNInfo{
				SessionID: tt.sessionID,
				LocalIPv6: net.ParseIP("2001:db8::2"),
				PCSCFIPs:  []net.IP{net.ParseIP("2001:db8::10")},
			}

			gotInterfaceName, err := network.Replace(context.Background(), info)
			if tt.wantErr {
				if err == nil {
					t.Fatal("Replace() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Replace() error = %v", err)
			}
			if gotInterfaceName != interfaceName {
				t.Fatalf("Replace() interface = %q, want %q", gotInterfaceName, interfaceName)
			}
			if err := network.Close(); err != nil {
				t.Fatalf("Close() error = %v", err)
			}
			want := []string{
				"add-vlan:lo:" + interfaceName + ":1",
			}
			if tt.stale {
				want = append(want,
					"delete-link:"+interfaceName,
					"add-vlan:lo:"+interfaceName+":1",
				)
			}
			want = append(want,
				"disable-ipv6-autoconf:"+interfaceName,
				"up:"+interfaceName,
				"add-address:"+interfaceName+":2001:db8::2/128",
				"flush-table:20000",
				"add-default:"+interfaceName+":10:2001:db8::2:20000",
				"add-rule:"+interfaceName+":10:10000:20000",
				"delete-rule:"+interfaceName+":10:10000:20000",
				"delete-default:"+interfaceName+":10:2001:db8::2:20000",
				"flush-table:20000",
				"delete-address:"+interfaceName+":2001:db8::2/128",
				"delete-link:"+interfaceName,
			)
			if !slices.Equal(calls, want) {
				t.Fatalf("network calls = %v, want %v", calls, want)
			}
		})
	}
}

func TestPDNNetworkRollsBackConfigurationOnce(t *testing.T) {
	tests := []struct {
		name string
	}{
		{name: "address failure cleans configured address once"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addCalls := 0
			deleteCalls := 0
			network := &pdnNetwork{
				parent: "wwan0",
				links: fakePDNLinks{
					addPointToPointAddress: func(string, netip.Addr, netip.Addr) error {
						addCalls++
						if addCalls == 2 {
							return errors.New("add address rejected")
						}
						return nil
					},
					deletePointToPointAddress: func(string, netip.Addr, netip.Addr) error {
						deleteCalls++
						return nil
					},
				},
			}
			info := imsPDNInfo{
				LocalIPv4:   net.ParseIP("10.0.0.2"),
				LocalIPv6:   net.ParseIP("2001:db8::2"),
				IPv4Gateway: net.ParseIP("10.0.0.1"),
				IPv6Gateway: net.ParseIP("2001:db8::1"),
			}

			if _, err := network.Replace(context.Background(), info); err == nil {
				t.Fatal("Replace() error = nil, want error")
			}
			if deleteCalls != 1 {
				t.Fatalf("DeleteAddress calls = %d, want 1", deleteCalls)
			}
			if err := network.Close(); err != nil {
				t.Fatalf("Close() error = %v", err)
			}
			if deleteCalls != 1 {
				t.Fatalf("DeleteAddress calls after Close = %d, want 1", deleteCalls)
			}
		})
	}
}

func TestPDNNetworkRetainsFailedCleanupForRetry(t *testing.T) {
	tests := []struct {
		name string
	}{
		{name: "shared interface keeps routes and addresses until rule deletion succeeds"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deleteAddressCalls := 0
			deleteRouteCalls := 0
			deleteRuleCalls := 0
			route := netlink.DefaultRoute{Interface: "wwan0", Family: netlink.FamilyIPv4, Table: imsPolicyRouteTableBase, Protocol: imsPolicyProtocol, Source: netip.MustParseAddr("10.0.0.2")}
			rule := netlink.PolicyRule{Family: netlink.FamilyIPv4, Priority: imsPolicyRulePriorityBase, Table: imsPolicyRouteTableBase, OutputInterface: "wwan0", Protocol: imsPolicyProtocol}
			network := &pdnNetwork{
				parent:        "wwan0",
				interfaceName: "wwan0",
				state: pdnNetworkState{
					prefixes:    []netip.Prefix{netip.MustParsePrefix("10.0.0.2/32")},
					defaults:    []netlink.DefaultRoute{route},
					rules:       []netlink.PolicyRule{rule},
					policyTable: imsPolicyRouteTableBase,
				},
				links: fakePDNLinks{
					deletePolicyRule: func(netlink.PolicyRule) error {
						deleteRuleCalls++
						if deleteRuleCalls == 1 {
							return errors.New("delete rule rejected")
						}
						return nil
					},
					deleteDefaultRoute: func(netlink.DefaultRoute) error {
						deleteRouteCalls++
						return nil
					},
					deleteAddress: func(string, netip.Prefix) error {
						deleteAddressCalls++
						return nil
					},
				},
			}

			if err := network.Close(); err == nil {
				t.Fatal("first Close() error = nil, want error")
			}
			if network.interfaceName != "wwan0" || len(network.state.rules) != 1 || len(network.state.defaults) != 1 || len(network.state.prefixes) != 1 {
				t.Fatalf("state after failed Close = %q/%+v, want retained resources", network.interfaceName, network.state)
			}
			if deleteRouteCalls != 0 || deleteAddressCalls != 0 {
				t.Fatalf("cleanup after rule failure deleted routes/addresses = %d/%d", deleteRouteCalls, deleteAddressCalls)
			}
			if err := network.Close(); err != nil {
				t.Fatalf("second Close() error = %v", err)
			}
			if network.interfaceName != "" || len(network.state.prefixes) != 0 || len(network.state.defaults) != 0 || len(network.state.rules) != 0 {
				t.Fatalf("state after successful Close = %q/%+v, want empty", network.interfaceName, network.state)
			}
		})
	}
}

func TestPDNNetworkRetainsMBIMLinkUntilAddressCleanupSucceeds(t *testing.T) {
	deleteAddressCalls := 0
	deleteLinkCalls := 0
	network := &pdnNetwork{
		parent:        "wwan0",
		mbim:          true,
		interfaceName: "mbim7s1",
		state: pdnNetworkState{
			prefixes: []netip.Prefix{netip.MustParsePrefix("2001:db8::2/128")},
		},
		links: fakePDNLinks{
			deleteAddress: func(string, netip.Prefix) error {
				deleteAddressCalls++
				if deleteAddressCalls == 1 {
					return errors.New("delete address rejected")
				}
				return nil
			},
			deleteLink: func(string) error {
				deleteLinkCalls++
				return nil
			},
		},
	}

	if err := network.Close(); err == nil {
		t.Fatal("first Close() error = nil, want error")
	}
	if deleteLinkCalls != 0 || network.interfaceName != "mbim7s1" || len(network.state.prefixes) != 1 {
		t.Fatalf("state after failed Close = link deletes %d, interface %q, prefixes %v", deleteLinkCalls, network.interfaceName, network.state.prefixes)
	}
	if err := network.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if deleteLinkCalls != 1 || network.interfaceName != "" || len(network.state.prefixes) != 0 {
		t.Fatalf("state after successful Close = link deletes %d, interface %q, prefixes %v", deleteLinkCalls, network.interfaceName, network.state.prefixes)
	}
}

func TestConfigureIMSPDNNetworkUsesDistinctPolicyTables(t *testing.T) {
	resetIMSPolicyReservations(t)
	var (
		mu     sync.Mutex
		routes = make(map[string]netlink.DefaultRoute)
		rules  = make(map[string]netlink.PolicyRule)
	)
	links := fakePDNLinks{
		addDefaultRoute: func(route netlink.DefaultRoute) error {
			mu.Lock()
			routes[route.Interface] = route
			mu.Unlock()
			return nil
		},
		addPolicyRule: func(rule netlink.PolicyRule) error {
			mu.Lock()
			rules[rule.OutputInterface] = rule
			mu.Unlock()
			return nil
		},
	}
	networks := []*pdnNetwork{
		{parent: "qmimux0", links: links},
		{parent: "qmimux1", links: links},
	}
	infos := []imsPDNInfo{
		{LocalIPv4: net.ParseIP("10.0.0.2"), IPv4Gateway: net.ParseIP("10.0.0.1")},
		{LocalIPv4: net.ParseIP("10.0.1.2"), IPv4Gateway: net.ParseIP("10.0.1.1")},
	}

	errCh := make(chan error, len(networks))
	var wg sync.WaitGroup
	for i := range networks {
		wg.Go(func() {
			_, err := networks[i].Replace(context.Background(), infos[i])
			errCh <- err
		})
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("Replace() error = %v", err)
		}
	}

	firstRoute, secondRoute := routes["qmimux0"], routes["qmimux1"]
	firstRule, secondRule := rules["qmimux0"], rules["qmimux1"]
	if firstRoute.Table == 0 || secondRoute.Table == 0 || firstRoute.Table == secondRoute.Table {
		t.Fatalf("policy tables = %d/%d, want distinct non-zero tables", firstRoute.Table, secondRoute.Table)
	}
	if firstRule.Table != firstRoute.Table || secondRule.Table != secondRoute.Table || firstRule.Priority == secondRule.Priority {
		t.Fatalf("policy routes/rules = %+v/%+v and %+v/%+v", firstRoute, firstRule, secondRoute, secondRule)
	}
	for _, network := range networks {
		if err := network.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}
}

func TestReserveIMSPolicyTableSkipsForeignRoutes(t *testing.T) {
	resetIMSPolicyReservations(t)
	foreign := netlink.RouteEntry{
		Family:   netlink.FamilyIPv4,
		Table:    imsPolicyRouteTableBase,
		Protocol: 201,
		Default:  false,
	}
	links := fakePDNLinks{
		routeEntries: func() ([]netlink.RouteEntry, error) {
			return []netlink.RouteEntry{foreign}, nil
		},
	}

	table, priority, err := reserveIMSPolicyTable(links)
	if err != nil {
		t.Fatalf("reserveIMSPolicyTable() error = %v", err)
	}
	if table != imsPolicyRouteTableBase+1 || priority != imsPolicyRulePriorityBase+1 {
		t.Fatalf("policy slot = %d/%d, want %d/%d", table, priority, imsPolicyRouteTableBase+1, imsPolicyRulePriorityBase+1)
	}
	releaseIMSPolicyTable(table)
}

func TestCleanupStaleIMSPolicyRouting(t *testing.T) {
	resetIMSPolicyReservations(t)
	ownedIPv4 := netlink.PolicyRule{
		Family:          netlink.FamilyIPv4,
		Priority:        imsPolicyRulePriorityBase + 3,
		Table:           imsPolicyRouteTableBase + 3,
		OutputInterface: "qmimux0",
		Protocol:        imsPolicyProtocol,
	}
	ownedIPv6 := ownedIPv4
	ownedIPv6.Family = netlink.FamilyIPv6
	foreign := netlink.PolicyRule{
		Family:          netlink.FamilyIPv4,
		Priority:        ownedIPv4.Priority,
		Table:           ownedIPv4.Table,
		OutputInterface: "eth0",
		Protocol:        imsPolicyProtocol - 1,
	}
	orphanTable := imsPolicyRouteTableBase + 7
	routes := []netlink.RouteEntry{
		{Family: netlink.FamilyIPv4, Table: orphanTable, Protocol: imsPolicyProtocol, Default: true},
		{Family: netlink.FamilyIPv4, Table: imsPolicyRouteTableBase + 8, Protocol: imsPolicyProtocol - 1, Default: true},
	}
	rules := []netlink.PolicyRule{ownedIPv4, foreign, ownedIPv6}
	imsPolicyRoutes.Lock()
	imsPolicyRoutes.reserved[ownedIPv4.Table] = struct{}{}
	imsPolicyRoutes.Unlock()
	var (
		deleted []netlink.PolicyRule
		flushed []uint32
	)
	links := fakePDNLinks{
		policyRules: func() ([]netlink.PolicyRule, error) {
			return slices.Clone(rules), nil
		},
		routeEntries: func() ([]netlink.RouteEntry, error) {
			return slices.Clone(routes), nil
		},
		deletePolicyRule: func(rule netlink.PolicyRule) error {
			deleted = append(deleted, rule)
			return nil
		},
		flushDefaultRoutesInTable: func(table uint32) error {
			flushed = append(flushed, table)
			return nil
		},
	}

	if err := cleanupStaleIMSPolicyRouting(links); err != nil {
		t.Fatalf("cleanupStaleIMSPolicyRouting() error = %v", err)
	}
	if want := []netlink.PolicyRule{ownedIPv4, ownedIPv6}; !slices.Equal(deleted, want) {
		t.Fatalf("deleted rules = %+v, want %+v", deleted, want)
	}
	slices.Sort(flushed)
	wantFlushed := []uint32{ownedIPv4.Table, orphanTable}
	slices.Sort(wantFlushed)
	if !slices.Equal(flushed, wantFlushed) {
		t.Fatalf("flushed tables = %v, want %v", flushed, wantFlushed)
	}
	imsPolicyRoutes.Lock()
	_, reserved := imsPolicyRoutes.reserved[ownedIPv4.Table]
	imsPolicyRoutes.Unlock()
	if reserved {
		t.Fatalf("policy table %d remains reserved after stale cleanup", ownedIPv4.Table)
	}
}

func TestCleanupStaleIMSPolicyRoutingRetainsFailedTable(t *testing.T) {
	resetIMSPolicyReservations(t)
	rule := netlink.PolicyRule{
		Family:          netlink.FamilyIPv4,
		Priority:        imsPolicyRulePriorityBase,
		Table:           imsPolicyRouteTableBase,
		OutputInterface: "qmimux0",
		Protocol:        imsPolicyProtocol,
	}
	imsPolicyRoutes.Lock()
	imsPolicyRoutes.reserved[rule.Table] = struct{}{}
	imsPolicyRoutes.Unlock()
	errDelete := errors.New("delete policy rule")
	flushed := false
	links := fakePDNLinks{
		policyRules: func() ([]netlink.PolicyRule, error) {
			return []netlink.PolicyRule{rule}, nil
		},
		deletePolicyRule: func(netlink.PolicyRule) error {
			return errDelete
		},
		flushDefaultRoutesInTable: func(uint32) error {
			flushed = true
			return nil
		},
	}

	if err := cleanupStaleIMSPolicyRouting(links); !errors.Is(err, errDelete) {
		t.Fatalf("cleanupStaleIMSPolicyRouting() error = %v, want %v", err, errDelete)
	}
	imsPolicyRoutes.Lock()
	_, reserved := imsPolicyRoutes.reserved[rule.Table]
	imsPolicyRoutes.Unlock()
	if flushed || !reserved {
		t.Fatalf("failed table state = flushed %v, reserved %v; want false/true", flushed, reserved)
	}
}

func resetIMSPolicyReservations(t *testing.T) {
	t.Helper()
	imsPolicyRoutes.Lock()
	if len(imsPolicyRoutes.reserved) != 0 {
		imsPolicyRoutes.Unlock()
		t.Fatal("IMS policy table reservations leaked from another test")
	}
	imsPolicyRoutes.Unlock()
	t.Cleanup(func() {
		imsPolicyRoutes.Lock()
		clear(imsPolicyRoutes.reserved)
		imsPolicyRoutes.Unlock()
	})
}

func TestWaitForIMSInterface(t *testing.T) {
	tests := []struct {
		name      string
		errors    []error
		cancel    bool
		wantCalls int
		wantErr   error
	}{
		{name: "available immediately", errors: []error{nil}, wantCalls: 1},
		{
			name: "waits for interface enumeration",
			errors: []error{
				fmt.Errorf("disable IPv6 autoconf: %w", syscall.ENOENT),
				fmt.Errorf("read interface flags: %w", syscall.ENODEV),
				fmt.Errorf("read interface flags: %w", syscall.ENXIO),
				nil,
			},
			wantCalls: 4,
		},
		{name: "returns other errors", errors: []error{syscall.EPERM}, wantCalls: 1, wantErr: syscall.EPERM},
		{name: "context cancelled", errors: []error{syscall.ENODEV}, cancel: true, wantCalls: 1, wantErr: context.Canceled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			previousInterval := imsInterfacePollInterval
			imsInterfacePollInterval = time.Nanosecond
			if tt.cancel {
				imsInterfacePollInterval = time.Hour
			}
			t.Cleanup(func() {
				imsInterfacePollInterval = previousInterval
			})

			calls := 0
			setUp := func(string) error {
				index := min(calls, len(tt.errors)-1)
				calls++
				return tt.errors[index]
			}
			ctx := context.Background()
			if tt.cancel {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}

			err := waitForIMSInterface(ctx, "wwan0", setUp)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("waitForIMSInterface() error = %v, want %v", err, tt.wantErr)
			}
			if calls != tt.wantCalls {
				t.Fatalf("set up calls = %d, want %d", calls, tt.wantCalls)
			}
		})
	}
}

func TestIsIMSCallAlreadyPresent(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "internal call already present",
			err: &qcom.WDSStartNetworkError{
				Err:                     qcom.QMIErrorCallFailed,
				HasVerboseCallEndReason: true,
				VerboseCallEndReason: qcom.WDSVerboseCallEndReason{
					Type:   qcom.WDSVerboseCallEndReasonTypeInternal,
					Reason: qcom.WDSVerboseCallEndReasonInternalCallAlreadyPresent,
				},
			},
			want: true,
		},
		{
			name: "wrapped call already present",
			err: fmt.Errorf("opening IMS PDN: %w", &qcom.WDSStartNetworkError{
				HasVerboseCallEndReason: true,
				VerboseCallEndReason: qcom.WDSVerboseCallEndReason{
					Type:   qcom.WDSVerboseCallEndReasonTypeInternal,
					Reason: qcom.WDSVerboseCallEndReasonInternalCallAlreadyPresent,
				},
			}),
			want: true,
		},
		{
			name: "different internal reason",
			err: &qcom.WDSStartNetworkError{
				HasVerboseCallEndReason: true,
				VerboseCallEndReason: qcom.WDSVerboseCallEndReason{
					Type:   qcom.WDSVerboseCallEndReasonTypeInternal,
					Reason: qcom.WDSVerboseCallEndReasonInternalInterfaceInUse,
				},
			},
		},
		{name: "ordinary error", err: syscall.EIO},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isIMSCallAlreadyPresent(tt.err); got != tt.want {
				t.Fatalf("isIMSCallAlreadyPresent() = %v, want %v", got, tt.want)
			}
		})
	}
}

type fakePDNLinks struct {
	disableIPv6Autoconfiguration func(string) error
	setUp                        func(string) error
	addAddress                   func(string, netip.Prefix) error
	addPointToPointAddress       func(string, netip.Addr, netip.Addr) error
	deleteAddress                func(string, netip.Prefix) error
	deletePointToPointAddress    func(string, netip.Addr, netip.Addr) error
	addDefaultRoute              func(netlink.DefaultRoute) error
	deleteDefaultRoute           func(netlink.DefaultRoute) error
	routeEntries                 func() ([]netlink.RouteEntry, error)
	flushDefaultRoutesInTable    func(uint32) error
	policyRules                  func() ([]netlink.PolicyRule, error)
	addPolicyRule                func(netlink.PolicyRule) error
	deletePolicyRule             func(netlink.PolicyRule) error
	addVLAN                      func(string, string, uint16) error
	deleteLink                   func(string) error
}

func (f fakePDNLinks) DisableIPv6Autoconfiguration(name string) error {
	if f.disableIPv6Autoconfiguration == nil {
		return nil
	}
	return f.disableIPv6Autoconfiguration(name)
}

func (f fakePDNLinks) SetUp(name string) error {
	if f.setUp == nil {
		return nil
	}
	return f.setUp(name)
}

func (f fakePDNLinks) AddAddress(name string, prefix netip.Prefix) error {
	if f.addAddress == nil {
		return nil
	}
	return f.addAddress(name, prefix)
}

func (f fakePDNLinks) AddPointToPointAddress(name string, local, peer netip.Addr) error {
	if f.addPointToPointAddress == nil {
		return nil
	}
	return f.addPointToPointAddress(name, local, peer)
}

func (f fakePDNLinks) DeleteAddress(name string, prefix netip.Prefix) error {
	if f.deleteAddress == nil {
		return nil
	}
	return f.deleteAddress(name, prefix)
}

func (f fakePDNLinks) DeletePointToPointAddress(name string, local, peer netip.Addr) error {
	if f.deletePointToPointAddress == nil {
		return nil
	}
	return f.deletePointToPointAddress(name, local, peer)
}

func (f fakePDNLinks) AddDefaultRoute(route netlink.DefaultRoute) error {
	if f.addDefaultRoute == nil {
		return nil
	}
	return f.addDefaultRoute(route)
}

func (f fakePDNLinks) DeleteDefaultRoute(route netlink.DefaultRoute) error {
	if f.deleteDefaultRoute == nil {
		return nil
	}
	return f.deleteDefaultRoute(route)
}

func (f fakePDNLinks) RouteEntries() ([]netlink.RouteEntry, error) {
	if f.routeEntries == nil {
		return nil, nil
	}
	return f.routeEntries()
}

func (f fakePDNLinks) FlushDefaultRoutesInTable(table uint32) error {
	if f.flushDefaultRoutesInTable == nil {
		return nil
	}
	return f.flushDefaultRoutesInTable(table)
}

func (f fakePDNLinks) PolicyRules() ([]netlink.PolicyRule, error) {
	if f.policyRules == nil {
		return nil, nil
	}
	return f.policyRules()
}

func (f fakePDNLinks) AddPolicyRule(rule netlink.PolicyRule) error {
	if f.addPolicyRule == nil {
		return nil
	}
	return f.addPolicyRule(rule)
}

func (f fakePDNLinks) DeletePolicyRule(rule netlink.PolicyRule) error {
	if f.deletePolicyRule == nil {
		return nil
	}
	return f.deletePolicyRule(rule)
}

func (f fakePDNLinks) AddVLAN(parent, name string, id uint16) error {
	if f.addVLAN == nil {
		return nil
	}
	return f.addVLAN(parent, name, id)
}

func (f fakePDNLinks) DeleteLink(name string) error {
	if f.deleteLink == nil {
		return nil
	}
	return f.deleteLink(name)
}

type fakeUSIMReader struct{}

func (fakeUSIMReader) ListApplications(context.Context) ([]usimcard.Application, error) {
	return nil, nil
}

func (fakeUSIMReader) FileAttributes(context.Context, usimcard.FileRef) (usimcard.FileAttributes, error) {
	return usimcard.FileAttributes{}, nil
}

func (fakeUSIMReader) ReadTransparent(context.Context, usimcard.TransparentRead) ([]byte, error) {
	return nil, nil
}

func (fakeUSIMReader) ReadRecord(context.Context, usimcard.RecordRead) ([]byte, error) {
	return nil, nil
}

func (fakeUSIMReader) Authenticate3G(context.Context, usimcard.AuthenticateRequest) ([]byte, error) {
	return nil, nil
}

func (fakeUSIMReader) SMSPPDownload(context.Context, usimcard.SMSPPDownloadRequest) (usimcard.SMSPPDownloadResponse, error) {
	return usimcard.SMSPPDownloadResponse{}, nil
}

func (fakeUSIMReader) Close() error {
	return nil
}
