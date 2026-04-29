package modem

import (
	"reflect"
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestBearerIPConfigFromVariants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  map[string]dbus.Variant
		want BearerIPConfig
	}{
		{
			name: "static ipv4 with dns dedupe",
			raw: map[string]dbus.Variant{
				"method":  dbus.MakeVariant(uint32(BearerIPMethodStatic)),
				"address": dbus.MakeVariant("10.10.0.2"),
				"prefix":  dbus.MakeVariant(uint32(30)),
				"gateway": dbus.MakeVariant("10.10.0.1"),
				"dns1":    dbus.MakeVariant("1.1.1.1"),
				"dns2":    dbus.MakeVariant("1.1.1.1"),
				"dns3":    dbus.MakeVariant("8.8.8.8"),
				"mtu":     dbus.MakeVariant(uint32(1500)),
			},
			want: BearerIPConfig{
				Method:  BearerIPMethodStatic,
				Address: "10.10.0.2",
				Prefix:  30,
				Gateway: "10.10.0.1",
				DNS:     []string{"1.1.1.1", "8.8.8.8"},
				MTU:     1500,
			},
		},
		{
			name: "dhcp has method only",
			raw: map[string]dbus.Variant{
				"method": dbus.MakeVariant(uint32(BearerIPMethodDHCP)),
			},
			want: BearerIPConfig{
				Method: BearerIPMethodDHCP,
			},
		},
		{
			name: "empty config",
			raw:  map[string]dbus.Variant{},
			want: BearerIPConfig{},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := bearerIPConfigFromVariants(tt.raw)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("bearerIPConfigFromVariants() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestBearerStatsFromVariants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  map[string]dbus.Variant
		want BearerStats
	}{
		{
			name: "ongoing connection stats",
			raw: map[string]dbus.Variant{
				"rx-bytes": dbus.MakeVariant(uint64(2048)),
				"tx-bytes": dbus.MakeVariant(uint64(1024)),
				"duration": dbus.MakeVariant(uint32(60)),
			},
			want: BearerStats{
				RXBytes:  2048,
				TXBytes:  1024,
				Duration: 60,
			},
		},
		{
			name: "missing stats",
			raw:  map[string]dbus.Variant{},
			want: BearerStats{},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := bearerStatsFromVariants(tt.raw)
			if got != tt.want {
				t.Fatalf("bearerStatsFromVariants() = %#v, want %#v", got, tt.want)
			}
		})
	}
}
