package internet

import (
	"errors"
	"testing"
	"time"

	"github.com/damonto/sigmo/internal/pkg/netlink"
)

func TestConnectionTrafficCountsBytesAfterBaseline(t *testing.T) {
	meter := connectionTraffic{
		id: "connection-1", started: time.Now(),
		last: netlink.LinkBytes{Index: 1, RXBytes: 1000, TXBytes: 300},
	}
	for _, tc := range []struct {
		name    string
		current netlink.LinkBytes
		rx, tx  uint64
	}{
		{name: "first sample", current: netlink.LinkBytes{Index: 1, RXBytes: 1300, TXBytes: 375}, rx: 300, tx: 75},
		{name: "receive counter reset", current: netlink.LinkBytes{Index: 1, RXBytes: 120, TXBytes: 450}, rx: 420, tx: 150},
		{name: "count after reset", current: netlink.LinkBytes{Index: 1, RXBytes: 160, TXBytes: 480}, rx: 460, tx: 180},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := meter.sample(tc.current)
			if got == nil || got.SessionID != meter.id || got.RXBytes != tc.rx || got.TXBytes != tc.tx {
				t.Fatalf("sample() = %+v; want %s, RX %d, TX %d", got, meter.id, tc.rx, tc.tx)
			}
		})
	}
	if got := meter.sample(netlink.LinkBytes{Index: 2, RXBytes: 4000}); got != nil {
		t.Fatalf("sample() after interface replacement = %+v; want unavailable", got)
	}
}

func TestSampleTrafficDistinguishesMissingLinkFromUnavailableCounters(t *testing.T) {
	for _, tc := range []struct {
		name      string
		link      netlink.LinkBytes
		readErr   error
		connected bool
	}{
		{name: "missing link", readErr: netlink.ErrInterfaceNotFound},
		{name: "reused interface name", link: netlink.LinkBytes{Index: 2}},
		{name: "unavailable counters", readErr: errors.New("read failed"), connected: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			connector := Connector{readLinkBytes: func(string) (netlink.LinkBytes, error) {
				return tc.link, tc.readErr
			}}
			tracked := trackedConnection{
				interfaceName: "wwan0",
				traffic:       &connectionTraffic{last: netlink.LinkBytes{Index: 1}},
			}
			status := connector.sampleTraffic(tracked)
			if status.Connected != tc.connected || status.Sample != nil {
				t.Fatalf("sampleTraffic() = %+v, want connected=%t and no sample", status, tc.connected)
			}
		})
	}
}
