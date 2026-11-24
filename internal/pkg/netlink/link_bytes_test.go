package netlink

import (
	"encoding/binary"
	"testing"

	"golang.org/x/sys/unix"
)

func TestParseLinkBytes(t *testing.T) {
	header := make([]byte, unix.SizeofIfInfomsg)
	binary.NativeEndian.PutUint32(header[4:8], 17)
	stats := make([]byte, 32)
	binary.NativeEndian.PutUint64(stats[16:24], 8_000_000_000)
	binary.NativeEndian.PutUint64(stats[24:32], 420)
	want := LinkBytes{Index: 17, RXBytes: 8_000_000_000, TXBytes: 420}
	if got, err := parseLinkBytes(appendAttr(header, unix.IFLA_STATS64, stats)); err != nil || got != want {
		t.Fatalf("parseLinkBytes() = %+v, %v; want %+v", got, err, want)
	}
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{name: "truncated header", data: header[:8]},
		{name: "missing stats64", data: header},
		{name: "truncated stats64", data: appendAttr(header, unix.IFLA_STATS64, stats[:24])},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseLinkBytes(tc.data); err == nil {
				t.Fatal("parseLinkBytes accepted an incomplete sample")
			}
		})
	}
}
