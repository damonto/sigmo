package modem

import (
	"net/netip"
	"slices"
	"testing"

	wwanmodem "github.com/damonto/wwan-go/modem"
)

func TestBearerUpdateInfoRefreshesClonedCache(t *testing.T) {
	addresses := []netip.Prefix{netip.MustParsePrefix("10.0.0.2/30")}
	dns := []netip.Addr{netip.MustParseAddr("1.1.1.1")}
	bearer := &Bearer{}
	info := wwanmodem.BearerInfo{
		ID:        7,
		APN:       " internet ",
		Connected: true,
		Network: wwanmodem.NetworkConfig{
			Interface: "wwan0",
			Addresses: addresses,
			DNS:       dns,
		},
	}

	bearer.updateInfo(info)
	addresses[0] = netip.MustParsePrefix("192.0.2.2/24")
	dns[0] = netip.MustParseAddr("9.9.9.9")

	bearer.mu.RLock()
	gotInfo := cloneBearerInfo(bearer.infoValue)
	gotProperties := bearer.properties
	bearer.mu.RUnlock()
	if gotInfo.ID != 7 || !gotInfo.Connected || gotInfo.Network.Interface != "wwan0" {
		t.Fatalf("cached bearer info = %+v", gotInfo)
	}
	if !slices.Equal(gotInfo.Network.Addresses, []netip.Prefix{netip.MustParsePrefix("10.0.0.2/30")}) {
		t.Fatalf("cached addresses = %v, want original address", gotInfo.Network.Addresses)
	}
	if !slices.Equal(gotInfo.Network.DNS, []netip.Addr{netip.MustParseAddr("1.1.1.1")}) {
		t.Fatalf("cached DNS = %v, want original DNS", gotInfo.Network.DNS)
	}
	if gotProperties.APN != "internet" {
		t.Fatalf("cached APN = %q, want internet", gotProperties.APN)
	}
}

func TestBearerUpdateInfoPreservesExplicitAPN(t *testing.T) {
	bearer := &Bearer{properties: BearerProperties{APN: "configured"}}
	bearer.updateInfo(wwanmodem.BearerInfo{APN: "reported"})

	if got := bearer.properties.APN; got != "configured" {
		t.Fatalf("APN = %q, want configured", got)
	}
}

func TestBearerConsumeRejectsNilStream(t *testing.T) {
	if err := new(Bearer).consume(t.Context(), nil); err == nil {
		t.Fatal("consume() error = nil, want nil stream error")
	}
}
