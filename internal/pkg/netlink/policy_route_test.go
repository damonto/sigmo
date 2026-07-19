package netlink

import (
	"encoding/binary"
	"net"
	"net/netip"
	"testing"

	"golang.org/x/sys/unix"
)

func TestDefaultRouteMessageTable(t *testing.T) {
	tests := []struct {
		name       string
		route      DefaultRoute
		wantHeader byte
		wantAttr   uint32
	}{
		{
			name:       "main table remains in header",
			route:      DefaultRoute{Family: FamilyIPv4, Source: netip.MustParseAddr("192.0.2.10")},
			wantHeader: unix.RT_TABLE_MAIN,
		},
		{
			name:     "extended policy table uses attribute",
			route:    DefaultRoute{Family: FamilyIPv6, Table: 20_003, Protocol: 200, Source: netip.MustParseAddr("2001:db8::10")},
			wantAttr: 20_003,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := defaultRouteMessage(7, tt.route)
			if err != nil {
				t.Fatalf("defaultRouteMessage() error = %v", err)
			}
			if msg[4] != tt.wantHeader {
				t.Fatalf("route header table = %d, want %d", msg[4], tt.wantHeader)
			}
			if got := msg[5]; got != byte(routeProtocol(tt.route)) {
				t.Fatalf("route protocol = %d, want %d", got, routeProtocol(tt.route))
			}
			attrs := parseAttrs(msg[unix.SizeofRtMsg:])
			if got := attrUint32(attrs[unix.RTA_TABLE]); got != tt.wantAttr {
				t.Fatalf("route table attribute = %d, want %d", got, tt.wantAttr)
			}
			if got := attrUint32(attrs[unix.RTA_OIF]); got != 7 {
				t.Fatalf("route output interface = %d, want 7", got)
			}
		})
	}
}

func TestParseDefaultRouteInTable(t *testing.T) {
	loopback, err := net.InterfaceByName("lo")
	if err != nil {
		t.Skip("loopback interface is unavailable")
	}
	oif := make([]byte, 4)
	binary.NativeEndian.PutUint32(oif, uint32(loopback.Index))
	table := make([]byte, 4)
	binary.NativeEndian.PutUint32(table, 20_007)
	msg := make([]byte, unix.SizeofRtMsg)
	msg[0] = FamilyIPv4
	msg[5] = unix.RTPROT_STATIC
	msg[7] = unix.RTN_UNICAST
	msg = appendAttr(msg, unix.RTA_TABLE, table)
	msg = appendAttr(msg, unix.RTA_OIF, oif)

	routes := parseDefaultRoutesInTable(msg, 20_007)
	if len(routes) != 1 {
		t.Fatalf("parseDefaultRoutesInTable() len = %d, want 1", len(routes))
	}
	if routes[0].Table != 20_007 || routes[0].Interface != loopback.Name {
		t.Fatalf("parsed route = %+v, want table 20007 via %s", routes[0], loopback.Name)
	}
	if got := parseDefaultRoutes(msg); len(got) != 0 {
		t.Fatalf("parseDefaultRoutes() = %+v, want policy route excluded", got)
	}
}

func TestParseRouteEntry(t *testing.T) {
	tests := []struct {
		name    string
		family  int
		table   uint32
		dstBits byte
		typeID  byte
		want    RouteEntry
		wantOK  bool
	}{
		{
			name:    "extended table non-default route",
			family:  FamilyIPv4,
			table:   20_000,
			dstBits: 24,
			typeID:  unix.RTN_UNICAST,
			want:    RouteEntry{Family: FamilyIPv4, Table: 20_000, Protocol: 200},
			wantOK:  true,
		},
		{
			name:   "unicast default route",
			family: FamilyIPv6,
			table:  200,
			typeID: unix.RTN_UNICAST,
			want:   RouteEntry{Family: FamilyIPv6, Table: 200, Protocol: 200, Default: true},
			wantOK: true,
		},
		{name: "truncated message"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.wantOK {
				if _, ok := parseRouteEntry(nil); ok {
					t.Fatal("parseRouteEntry() ok = true, want false")
				}
				return
			}
			msg := make([]byte, unix.SizeofRtMsg)
			msg[0] = byte(tt.family)
			msg[1] = tt.dstBits
			msg[5] = 200
			msg[7] = tt.typeID
			if tt.table <= 255 {
				msg[4] = byte(tt.table)
			} else {
				table := make([]byte, 4)
				binary.NativeEndian.PutUint32(table, tt.table)
				msg = appendAttr(msg, unix.RTA_TABLE, table)
			}

			got, ok := parseRouteEntry(msg)
			if !ok || got != tt.want {
				t.Fatalf("parseRouteEntry() = %+v, %v; want %+v, true", got, ok, tt.want)
			}
		})
	}
}
