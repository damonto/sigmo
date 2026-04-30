package netlink

import (
	"encoding/binary"
	"net"
	"net/netip"
	"testing"

	"golang.org/x/sys/unix"
)

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

	msg := make([]byte, unix.SizeofRtMsg)
	msg[0] = FamilyIPv4
	msg[4] = unix.RT_TABLE_MAIN
	msg[5] = unix.RTPROT_STATIC
	msg[7] = unix.RTN_UNICAST
	msg = appendAttr(msg, unix.RTA_OIF, oif)
	msg = appendAttr(msg, unix.RTA_PRIORITY, metric)
	msg = appendAttr(msg, unix.RTA_GATEWAY, gateway[:])

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
