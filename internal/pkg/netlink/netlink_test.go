package netlink

import (
	"encoding/binary"
	"errors"
	"net"
	"net/netip"
	"os"
	"slices"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func TestDisableIPv6Autoconfiguration(t *testing.T) {
	errWrite := errors.New("write rejected")
	tests := []struct {
		name          string
		interfaceName string
		failPath      string
		wantPaths     []string
		wantErr       error
	}{
		{
			name:          "disables SLAAC and router advertisements",
			interfaceName: "qmimux0",
			wantPaths:     []string{"/proc-test/qmimux0/autoconf", "/proc-test/qmimux0/accept_ra"},
		},
		{
			name:          "stops after write rejection",
			interfaceName: "qmimux0",
			failPath:      "/proc-test/qmimux0/autoconf",
			wantPaths:     []string{"/proc-test/qmimux0/autoconf"},
			wantErr:       errWrite,
		},
		{name: "rejects empty interface", wantErr: syscall.EINVAL},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var paths []string
			writeFile := func(path string, data []byte, mode os.FileMode) error {
				paths = append(paths, path)
				if string(data) != "0" || mode != 0o644 {
					t.Fatalf("write = %q/%#o, want 0/0644", data, mode)
				}
				if path == tt.failPath {
					return errWrite
				}
				return nil
			}

			err := disableIPv6Autoconfiguration("/proc-test", tt.interfaceName, writeFile)
			if tt.wantErr == syscall.EINVAL {
				if err == nil {
					t.Fatal("disableIPv6Autoconfiguration() error = nil, want error")
				}
			} else if !errors.Is(err, tt.wantErr) {
				t.Fatalf("disableIPv6Autoconfiguration() error = %v, want %v", err, tt.wantErr)
			}
			if !slices.Equal(paths, tt.wantPaths) {
				t.Fatalf("write paths = %v, want %v", paths, tt.wantPaths)
			}
		})
	}
}

func TestParseDefaultRoute(t *testing.T) {
	t.Parallel()

	loopback, err := net.InterfaceByName("lo")
	if err != nil {
		t.Skip("loopback interface is unavailable")
	}

	oif := make([]byte, 4)
	binary.NativeEndian.PutUint32(oif, uint32(loopback.Index))
	metric := make([]byte, 4)
	binary.NativeEndian.PutUint32(metric, 10)
	gateway := netip.MustParseAddr("10.0.0.1").As4()
	source := netip.MustParseAddr("10.0.0.2").As4()

	msg := make([]byte, unix.SizeofRtMsg)
	msg[0] = FamilyIPv4
	msg[4] = unix.RT_TABLE_MAIN
	msg[5] = unix.RTPROT_STATIC
	msg[7] = unix.RTN_UNICAST
	msg = appendAttr(msg, unix.RTA_OIF, oif)
	msg = appendAttr(msg, unix.RTA_PRIORITY, metric)
	msg = appendAttr(msg, unix.RTA_GATEWAY, gateway[:])
	msg = appendAttr(msg, unix.RTA_PREFSRC, source[:])

	routes := parseDefaultRoutes(msg)
	if len(routes) != 1 {
		t.Fatalf("parseDefaultRoutes() len = %d, want 1", len(routes))
	}
	got := routes[0]
	if got.Interface != loopback.Name {
		t.Fatalf("Interface = %q, want %q", got.Interface, loopback.Name)
	}
	if got.Family != FamilyIPv4 {
		t.Fatalf("Family = %d, want %d", got.Family, FamilyIPv4)
	}
	if got.Protocol != unix.RTPROT_STATIC {
		t.Fatalf("Protocol = %d, want %d", got.Protocol, unix.RTPROT_STATIC)
	}
	if got.Gateway != netip.MustParseAddr("10.0.0.1") {
		t.Fatalf("Gateway = %s, want 10.0.0.1", got.Gateway)
	}
	if got.Source != netip.MustParseAddr("10.0.0.2") {
		t.Fatalf("Source = %s, want 10.0.0.2", got.Source)
	}
	if got.Metric != 10 {
		t.Fatalf("Metric = %d, want 10", got.Metric)
	}
}

func TestRouteProtocol(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		route DefaultRoute
		want  int
	}{
		{name: "default", route: DefaultRoute{}, want: unix.RTPROT_STATIC},
		{name: "preserved", route: DefaultRoute{Protocol: unix.RTPROT_DHCP}, want: unix.RTPROT_DHCP},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := routeProtocol(tt.route); got != tt.want {
				t.Fatalf("routeProtocol() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestHostRouteMessage(t *testing.T) {
	tests := []struct {
		name        string
		destination netip.Addr
		wantFamily  byte
		wantBits    byte
	}{
		{name: "IPv4", destination: netip.MustParseAddr("198.51.100.10"), wantFamily: FamilyIPv4, wantBits: 32},
		{name: "IPv6", destination: netip.MustParseAddr("2001:db8::10"), wantFamily: FamilyIPv6, wantBits: 128},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := hostRouteMessage(7, tt.destination)
			if err != nil {
				t.Fatalf("hostRouteMessage() error = %v", err)
			}
			if msg[0] != tt.wantFamily || msg[1] != tt.wantBits || msg[6] != unix.RT_SCOPE_LINK {
				t.Fatalf("route header = family %d bits %d scope %d", msg[0], msg[1], msg[6])
			}
			attrs := parseAttrs(msg[unix.SizeofRtMsg:])
			if got := attrUint32(attrs[unix.RTA_OIF]); got != 7 {
				t.Fatalf("route interface = %d, want 7", got)
			}
			if got := attrAddr(int(tt.wantFamily), attrs[unix.RTA_DST]); got != tt.destination {
				t.Fatalf("route destination = %s, want %s", got, tt.destination)
			}
		})
	}
}

func TestPointToPointAddressMessage(t *testing.T) {
	tests := []struct {
		name       string
		local      netip.Addr
		peer       netip.Addr
		wantFamily byte
		wantBits   byte
		wantErr    bool
	}{
		{
			name:       "IPv4 peer",
			local:      netip.MustParseAddr("10.0.0.2"),
			peer:       netip.MustParseAddr("10.0.0.1"),
			wantFamily: FamilyIPv4,
			wantBits:   32,
		},
		{
			name:       "IPv6 peer",
			local:      netip.MustParseAddr("2001:db8::2"),
			peer:       netip.MustParseAddr("2001:db8::1"),
			wantFamily: FamilyIPv6,
			wantBits:   128,
		},
		{
			name:    "rejects mixed families",
			local:   netip.MustParseAddr("10.0.0.2"),
			peer:    netip.MustParseAddr("2001:db8::1"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := pointToPointAddressMessage(7, tt.local, tt.peer)
			if tt.wantErr {
				if err == nil {
					t.Fatal("pointToPointAddressMessage() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("pointToPointAddressMessage() error = %v", err)
			}
			if msg[0] != tt.wantFamily || msg[1] != tt.wantBits {
				t.Fatalf("address header = family %d bits %d, want %d/%d", msg[0], msg[1], tt.wantFamily, tt.wantBits)
			}
			attrs := parseAttrs(msg[unix.SizeofIfAddrmsg:])
			if got := attrAddr(int(tt.wantFamily), attrs[unix.IFA_LOCAL]); got != tt.local {
				t.Fatalf("local address = %s, want %s", got, tt.local)
			}
			if got := attrAddr(int(tt.wantFamily), attrs[unix.IFA_ADDRESS]); got != tt.peer {
				t.Fatalf("peer address = %s, want %s", got, tt.peer)
			}
		})
	}
}

func TestVLANLinkMessage(t *testing.T) {
	tests := []struct {
		name          string
		parentIndex   int
		interfaceName string
		id            uint16
	}{
		{name: "MBIM session", parentIndex: 7, interfaceName: "mbim7s1", id: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := vlanLinkMessage(tt.parentIndex, tt.interfaceName, tt.id)
			attrs := parseAttrs(msg[unix.SizeofIfInfomsg:])
			if got := attrUint32(attrs[unix.IFLA_LINK]); got != uint32(tt.parentIndex) {
				t.Fatalf("parent index = %d, want %d", got, tt.parentIndex)
			}
			if got := string(attrs[unix.IFLA_IFNAME]); got != tt.interfaceName+"\x00" {
				t.Fatalf("interface name = %q, want %q", got, tt.interfaceName+"\x00")
			}
			linkInfo := parseAttrs(attrs[unix.IFLA_LINKINFO|unix.NLA_F_NESTED])
			if got := string(linkInfo[unix.IFLA_INFO_KIND]); got != "vlan\x00" {
				t.Fatalf("link kind = %q, want vlan", got)
			}
			infoData := parseAttrs(linkInfo[unix.IFLA_INFO_DATA|unix.NLA_F_NESTED])
			gotID := binary.NativeEndian.Uint16(infoData[unix.IFLA_VLAN_ID])
			if gotID != tt.id {
				t.Fatalf("VLAN ID = %d, want %d", gotID, tt.id)
			}
		})
	}
}

func TestParseDefaultRouteRejectsNonDefault(t *testing.T) {
	t.Parallel()

	msg := make([]byte, unix.SizeofRtMsg)
	msg[0] = FamilyIPv4
	msg[1] = 24
	msg[4] = unix.RT_TABLE_MAIN
	msg[7] = unix.RTN_UNICAST

	if routes := parseDefaultRoutes(msg); len(routes) != 0 {
		t.Fatalf("parseDefaultRoutes() = %#v, want empty", routes)
	}
}

func TestParseDefaultRouteMultipath(t *testing.T) {
	t.Parallel()

	loopback, err := net.InterfaceByName("lo")
	if err != nil {
		t.Skip("loopback interface is unavailable")
	}

	gateway := netip.MustParseAddr("10.10.10.201").As4()
	nexthopAttrs := appendAttr(nil, unix.RTA_GATEWAY, gateway[:])
	nexthop := make([]byte, unix.SizeofRtNexthop)
	binary.NativeEndian.PutUint16(nexthop[:2], uint16(len(nexthop)+len(nexthopAttrs)))
	binary.NativeEndian.PutUint32(nexthop[4:8], uint32(loopback.Index))
	nexthop = append(nexthop, nexthopAttrs...)

	metric := make([]byte, 4)
	binary.NativeEndian.PutUint32(metric, 10)
	msg := make([]byte, unix.SizeofRtMsg)
	msg[0] = FamilyIPv4
	msg[4] = unix.RT_TABLE_MAIN
	msg[5] = unix.RTPROT_STATIC
	msg[7] = unix.RTN_UNICAST
	msg = appendAttr(msg, unix.RTA_PRIORITY, metric)
	msg = appendAttr(msg, unix.RTA_MULTIPATH, nexthop)

	got := parseDefaultRoutes(msg)
	want := []DefaultRoute{
		{
			Interface: loopback.Name,
			Family:    FamilyIPv4,
			Protocol:  unix.RTPROT_STATIC,
			Gateway:   netip.MustParseAddr("10.10.10.201"),
			Metric:    10,
		},
	}
	if len(got) != len(want) {
		t.Fatalf("parseDefaultRoutes() = %#v, want %#v", got, want)
	}
	for i := range got {
		if !sameDefaultRoute(got[i], want[i]) {
			t.Fatalf("parseDefaultRoutes()[%d] = %#v, want %#v", i, got[i], want[i])
		}
	}
}
