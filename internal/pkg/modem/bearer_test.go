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

func TestBearerPropertiesFromVariants(t *testing.T) {
	t.Parallel()

	raw := map[string]dbus.Variant{
		"apn":          dbus.MakeVariant(" wap.vodafone.co.uk "),
		"ip-type":      dbus.MakeVariant(bearerIPFamilyIPv4V6),
		"user":         dbus.MakeVariant(" wap "),
		"password":     dbus.MakeVariant("*wap"),
		"allowed-auth": dbus.MakeVariant(bearerAllowedAuthPAP | bearerAllowedAuthCHAP),
	}

	got := bearerPropertiesFromVariants(raw)
	want := BearerProperties{
		APN:         "wap.vodafone.co.uk",
		IPType:      "ipv4v6",
		Username:    "wap",
		Password:    "*wap",
		AllowedAuth: "pap|chap",
	}
	if got != want {
		t.Fatalf("bearerPropertiesFromVariants() = %#v, want %#v", got, want)
	}
}

func TestBearerIPFamily(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		ipType  string
		want    uint32
		wantErr bool
	}{
		{name: "default", ipType: "", want: bearerIPFamilyIPv4V6},
		{name: "ipv4", ipType: "ipv4", want: bearerIPFamilyIPv4},
		{name: "ipv6", ipType: "ipv6", want: bearerIPFamilyIPv6},
		{name: "ipv4v6", ipType: " ipv4v6 ", want: bearerIPFamilyIPv4V6},
		{name: "unsupported", ipType: "ppp", wantErr: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := BearerIPFamily(tt.ipType)
			if (err != nil) != tt.wantErr {
				t.Fatalf("BearerIPFamily() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("BearerIPFamily() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestBearerAllowedAuth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		auth    string
		want    uint32
		wantErr bool
	}{
		{name: "empty", auth: "", want: 0},
		{name: "single", auth: "pap", want: bearerAllowedAuthPAP},
		{name: "multiple", auth: " pap|chap ", want: bearerAllowedAuthPAP | bearerAllowedAuthCHAP},
		{name: "comma separated", auth: "mschap,mschapv2", want: bearerAllowedAuthMSCHAP | bearerAllowedAuthMSCHAPV2},
		{name: "none combined", auth: "none|pap", wantErr: true},
		{name: "unsupported", auth: "token", wantErr: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := BearerAllowedAuth(tt.auth)
			if (err != nil) != tt.wantErr {
				t.Fatalf("BearerAllowedAuth() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("BearerAllowedAuth() = %d, want %d", got, tt.want)
			}
		})
	}
}
