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
	msg[7] = unix.RTN_UNICAST
	msg = appendAttr(msg, unix.RTA_OIF, oif)
	msg = appendAttr(msg, unix.RTA_PRIORITY, metric)
	msg = appendAttr(msg, unix.RTA_GATEWAY, gateway[:])

	got, ok := parseDefaultRoute(msg)
	if !ok {
		t.Fatal("parseDefaultRoute() ok = false, want true")
	}
	if got.Interface != loopback.Name {
		t.Fatalf("Interface = %q, want %q", got.Interface, loopback.Name)
	}
	if got.Family != FamilyIPv4 {
		t.Fatalf("Family = %d, want %d", got.Family, FamilyIPv4)
	}
	if got.Gateway != netip.MustParseAddr("10.0.0.1") {
		t.Fatalf("Gateway = %s, want 10.0.0.1", got.Gateway)
	}
	if got.Metric != 10 {
		t.Fatalf("Metric = %d, want 10", got.Metric)
	}
}

func TestParseDefaultRouteRejectsNonDefault(t *testing.T) {
	t.Parallel()

	msg := make([]byte, unix.SizeofRtMsg)
	msg[0] = FamilyIPv4
	msg[1] = 24
	msg[4] = unix.RT_TABLE_MAIN
	msg[7] = unix.RTN_UNICAST

	if route, ok := parseDefaultRoute(msg); ok {
		t.Fatalf("parseDefaultRoute() = %#v, true; want false", route)
	}
}
