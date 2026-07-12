package internet

import (
	"net"
	"slices"
	"testing"

	"github.com/damonto/wwan-go/qcom"
)

func TestQMAPIPPreferences(t *testing.T) {
	tests := []struct {
		name, input string
		want        []qcom.WDSIPPreference
		wantErr     bool
	}{
		{name: "dual stack lets network negotiate", input: "ipv4v6", want: []qcom.WDSIPPreference{qcom.WDSIPPreferenceUnspecified}},
		{name: "ipv4", input: "ipv4", want: []qcom.WDSIPPreference{qcom.WDSIPPreferenceIPv4}},
		{name: "ipv6", input: "ipv6", want: []qcom.WDSIPPreference{qcom.WDSIPPreferenceIPv6}},
		{name: "invalid", input: "ppp", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := qmapIPPreferences(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("qmapIPPreferences() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("qmapIPPreferences() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestQMAPNetworks(t *testing.T) {
	tests := []struct {
		name string
		info qcom.PDNInfo
		want int
	}{
		{name: "ipv4", info: qcom.PDNInfo{LocalIPv4: net.IPv4(10, 0, 0, 2), IPv4SubnetMask: net.IPv4(255, 255, 255, 252), IPv4Gateway: net.IPv4(10, 0, 0, 1)}, want: 1},
		{name: "dual stack", info: qcom.PDNInfo{LocalIPv4: net.IPv4(10, 0, 0, 2), IPv4SubnetMask: net.IPv4(255, 255, 255, 252), IPv4Gateway: net.IPv4(10, 0, 0, 1), LocalIPv6: net.ParseIP("2001:db8::2"), IPv6Gateway: net.ParseIP("2001:db8::1"), IPv6PrefixLength: 64}, want: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := qmapNetworks(tt.info); len(got) != tt.want {
				t.Fatalf("qmapNetworks() len = %d, want %d", len(got), tt.want)
			}
		})
	}
}
