//go:build ims

package call

import (
	"slices"
	"testing"
)

func TestParseIPv4DefaultRouteInterfaceNames(t *testing.T) {
	tests := []struct {
		name string
		data string
		want []string
	}{
		{
			name: "single default route",
			data: "Iface\tDestination\tGateway \tFlags\tRefCnt\tUse\tMetric\tMask\t\tMTU\tWindow\tIRTT\n" +
				"ens0\t00000000\t0101010A\t0003\t0\t0\t100\t00000000\t0\t0\t0\n" +
				"docker0\t00FEA8C0\t00000000\t0001\t0\t0\t0\t00FFFFFF\t0\t0\t0\n",
			want: []string{"ens0"},
		},
		{
			name: "lowest metric wins",
			data: "Iface\tDestination\tGateway \tFlags\tRefCnt\tUse\tMetric\tMask\t\tMTU\tWindow\tIRTT\n" +
				"wwan0\t00000000\t0101010A\t0003\t0\t0\t200\t00000000\t0\t0\t0\n" +
				"ens0\t00000000\t0101010A\t0003\t0\t0\t100\t00000000\t0\t0\t0\n",
			want: []string{"ens0"},
		},
		{
			name: "same metric keeps all defaults",
			data: "Iface\tDestination\tGateway \tFlags\tRefCnt\tUse\tMetric\tMask\t\tMTU\tWindow\tIRTT\n" +
				"wlan0\t00000000\t0101010A\t0003\t0\t0\t100\t00000000\t0\t0\t0\n" +
				"ens0\t00000000\t0101010A\t0003\t0\t0\t100\t00000000\t0\t0\t0\n",
			want: []string{"ens0", "wlan0"},
		},
		{
			name: "ignores down route",
			data: "Iface\tDestination\tGateway \tFlags\tRefCnt\tUse\tMetric\tMask\t\tMTU\tWindow\tIRTT\n" +
				"tun0\t00000000\t0101010A\t0000\t0\t0\t10\t00000000\t0\t0\t0\n" +
				"ens0\t00000000\t0101010A\t0003\t0\t0\t100\t00000000\t0\t0\t0\n",
			want: []string{"ens0"},
		},
		{
			name: "no default route",
			data: "Iface\tDestination\tGateway \tFlags\tRefCnt\tUse\tMetric\tMask\t\tMTU\tWindow\tIRTT\n" +
				"docker0\t00FEA8C0\t00000000\t0001\t0\t0\t0\t00FFFFFF\t0\t0\t0\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseIPv4DefaultRouteInterfaceNames(tt.data)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("parseIPv4DefaultRouteInterfaceNames() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseIPv6DefaultRouteInterfaceNames(t *testing.T) {
	tests := []struct {
		name string
		data string
		want []string
	}{
		{
			name: "single default route",
			data: "00000000000000000000000000000000 00 00000000000000000000000000000000 00 fe800000000000000000000000000001 00000400 00000000 00000000 00000001 ens0\n" +
				"fe800000000000000000000000000000 40 00000000000000000000000000000000 00 00000000000000000000000000000000 00000100 00000000 00000000 00000001 docker0\n",
			want: []string{"ens0"},
		},
		{
			name: "lowest metric wins",
			data: "00000000000000000000000000000000 00 00000000000000000000000000000000 00 fe800000000000000000000000000001 00000400 00000000 00000000 00000001 wwan0\n" +
				"00000000000000000000000000000000 00 00000000000000000000000000000000 00 fe800000000000000000000000000002 00000100 00000000 00000000 00000001 ens0\n",
			want: []string{"ens0"},
		},
		{
			name: "same metric keeps all defaults",
			data: "00000000000000000000000000000000 00 00000000000000000000000000000000 00 fe800000000000000000000000000001 00000100 00000000 00000000 00000001 wlan0\n" +
				"00000000000000000000000000000000 00 00000000000000000000000000000000 00 fe800000000000000000000000000002 00000100 00000000 00000000 00000001 ens0\n",
			want: []string{"ens0", "wlan0"},
		},
		{
			name: "ignores down route",
			data: "00000000000000000000000000000000 00 00000000000000000000000000000000 00 fe800000000000000000000000000001 00000010 00000000 00000000 00000000 tun0\n" +
				"00000000000000000000000000000000 00 00000000000000000000000000000000 00 fe800000000000000000000000000002 00000100 00000000 00000000 00000001 ens0\n",
			want: []string{"ens0"},
		},
		{
			name: "no default route",
			data: "fe800000000000000000000000000000 40 00000000000000000000000000000000 00 00000000000000000000000000000000 00000100 00000000 00000000 00000001 docker0\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseIPv6DefaultRouteInterfaceNames(tt.data)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("parseIPv6DefaultRouteInterfaceNames() = %v, want %v", got, tt.want)
			}
		})
	}
}
