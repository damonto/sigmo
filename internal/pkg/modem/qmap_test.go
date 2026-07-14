package modem

import (
	"errors"
	"net"
	"testing"

	"github.com/damonto/wwan-go/qcom"
)

func TestQMAPStopError(t *testing.T) {
	errStop := errors.New("stop rejected")
	tests := []struct {
		name    string
		err     error
		wantErr error
	}{
		{name: "already stopped", err: qcom.QMIErrorNoEffect},
		{name: "other error", err: errStop, wantErr: errStop},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := qmapStopError(tt.err); !errors.Is(err, tt.wantErr) {
				t.Fatalf("qmapStopError() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestNonQMAPLinkLayerForRawIP(t *testing.T) {
	tests := []struct {
		name  string
		rawIP string
		want  qcom.WDALinkLayerProtocol
	}{
		{name: "Ethernet framing", rawIP: "N\n", want: qcom.WDALinkLayerEthernet},
		{name: "raw IP framing", rawIP: "Y\n", want: qcom.WDALinkLayerRawIP},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := nonQMAPLinkLayerForRawIP(tt.rawIP); got != tt.want {
				t.Fatalf("nonQMAPLinkLayerForRawIP() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestNonQMAPLinkLayerForState(t *testing.T) {
	tests := []struct {
		name       string
		rawIP      string
		rawIPKnown bool
		flags      net.Flags
		want       qcom.WDALinkLayerProtocol
	}{
		{name: "sysfs raw IP", rawIP: "Y\n", rawIPKnown: true, want: qcom.WDALinkLayerRawIP},
		{name: "sysfs Ethernet", rawIP: "N\n", rawIPKnown: true, flags: net.FlagPointToPoint, want: qcom.WDALinkLayerEthernet},
		{name: "point-to-point fallback", flags: net.FlagPointToPoint | net.FlagUp, want: qcom.WDALinkLayerRawIP},
		{name: "Ethernet fallback", flags: net.FlagBroadcast, want: qcom.WDALinkLayerEthernet},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := nonQMAPLinkLayerForState(tt.rawIP, tt.rawIPKnown, tt.flags); got != tt.want {
				t.Fatalf("nonQMAPLinkLayerForState() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestSelectQMAPProfileIndex(t *testing.T) {
	profile := func(index uint8, apn string, pdp qcom.WDSPDPType) qcom.WDSProfileSettings {
		return qcom.WDSProfileSettings{
			ID:  qcom.WDSProfileID{Type: qcom.WDSProfileType3GPP, Index: index},
			APN: apn, APNKnown: true, PDPType: pdp, PDPKnown: true,
		}
	}
	profiles := []qcom.WDSProfileSettings{
		profile(3, "other", qcom.WDSPDPTypeIPv4),
		profile(4, "ereseller", qcom.WDSPDPTypeIPv4v6),
		profile(5, "ereseller", qcom.WDSPDPTypeIPv4),
		profile(6, "ereseller", qcom.WDSPDPTypeIPv6),
	}
	tests := []struct {
		name       string
		apn        string
		preference qcom.WDSIPPreference
		profiles   []qcom.WDSProfileSettings
		want       uint8
		wantErr    bool
		wantIs     error
	}{
		{name: "exact IPv4", apn: "ereseller", preference: qcom.WDSIPPreferenceIPv4, profiles: profiles, want: 5},
		{name: "exact IPv6", apn: "EReseller", preference: qcom.WDSIPPreferenceIPv6, profiles: profiles, want: 6},
		{name: "dual stack fallback", apn: "ereseller", preference: qcom.WDSIPPreferenceIPv6, profiles: profiles[:2], want: 4},
		{name: "profile not found", apn: "missing", preference: qcom.WDSIPPreferenceIPv4, profiles: profiles, wantErr: true, wantIs: qcom.ErrWDSProfileNotFound},
		{name: "APN required", preference: qcom.WDSIPPreferenceIPv4, profiles: profiles, wantErr: true},
		{name: "IP preference required", apn: "ereseller", profiles: profiles, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := selectQMAPProfileIndex(tt.apn, tt.preference, tt.profiles)
			if tt.wantErr {
				if err == nil {
					t.Fatal("selectQMAPProfileIndex() error = nil, want non-nil")
				}
				if tt.wantIs != nil && !errors.Is(err, tt.wantIs) {
					t.Fatalf("selectQMAPProfileIndex() error = %v, want %v", err, tt.wantIs)
				}
				return
			}
			if err != nil {
				t.Fatalf("selectQMAPProfileIndex() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("selectQMAPProfileIndex() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestLegacyQMAPDataPort(t *testing.T) {
	tests := []struct {
		name    string
		muxID   uint8
		want    qcom.WDSSIOPort
		wantErr bool
	}{
		{name: "mux 1", muxID: 1, want: qcom.WDSSIOPortA2MuxRMNET0},
		{name: "IMS mux 2", muxID: 2, want: qcom.WDSSIOPortA2MuxRMNET1},
		{name: "mux 8", muxID: 8, want: qcom.WDSSIOPortA2MuxRMNET7},
		{name: "zero", wantErr: true},
		{name: "outside range", muxID: 9, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := legacyQMAPDataPort(tt.muxID)
			if tt.wantErr {
				if err == nil {
					t.Fatal("legacyQMAPDataPort() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("legacyQMAPDataPort() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("legacyQMAPDataPort() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestMatchQMAPMuxInterface(t *testing.T) {
	tests := []struct {
		name       string
		muxID      uint8
		ids        []uint8
		interfaces []qmapMuxInterface
		want       string
		wantErr    bool
	}{
		{
			name:  "dense mux IDs",
			muxID: 2,
			ids:   []uint8{1, 2, 3},
			interfaces: []qmapMuxInterface{
				{name: "qmimux0", index: 10},
				{name: "qmimux1", index: 11},
				{name: "qmimux2", index: 12},
			},
			want: "qmimux1",
		},
		{
			name:  "sparse mux IDs use creation order",
			muxID: 3,
			ids:   []uint8{1, 3},
			interfaces: []qmapMuxInterface{
				{name: "qmimux0", index: 10},
				{name: "qmimux1", index: 11},
			},
			want: "qmimux1",
		},
		{
			name:  "missing mux",
			muxID: 2,
			ids:   []uint8{1, 3},
			interfaces: []qmapMuxInterface{
				{name: "qmimux0", index: 10},
				{name: "qmimux1", index: 11},
			},
			wantErr: true,
		},
		{
			name:       "mismatched counts",
			muxID:      1,
			ids:        []uint8{1, 3},
			interfaces: []qmapMuxInterface{{name: "qmimux0", index: 10}},
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := matchQMAPMuxInterface(tt.muxID, tt.ids, tt.interfaces)
			if tt.wantErr {
				if err == nil {
					t.Fatal("matchQMAPMuxInterface() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("matchQMAPMuxInterface() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("matchQMAPMuxInterface() = %q, want %q", got, tt.want)
			}
		})
	}
}
