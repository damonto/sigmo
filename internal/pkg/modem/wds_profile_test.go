package modem

import (
	"errors"
	"testing"

	"github.com/damonto/wwan-go/qcom"
)

func TestSelectWDSProfileIndex(t *testing.T) {
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
		{name: "exact IPv6", apn: " EReseller ", preference: qcom.WDSIPPreferenceIPv6, profiles: profiles, want: 6},
		{name: "dual stack fallback", apn: "ereseller", preference: qcom.WDSIPPreferenceIPv6, profiles: profiles[:2], want: 4},
		{name: "profile not found", apn: "missing", preference: qcom.WDSIPPreferenceIPv4, profiles: profiles, wantErr: true, wantIs: qcom.ErrWDSProfileNotFound},
		{name: "APN required", preference: qcom.WDSIPPreferenceIPv4, profiles: profiles, wantErr: true},
		{name: "IP preference required", apn: "ereseller", profiles: profiles, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := selectWDSProfileIndex(tt.apn, tt.preference, tt.profiles)
			if tt.wantErr {
				if err == nil {
					t.Fatal("selectWDSProfileIndex() error = nil, want non-nil")
				}
				if tt.wantIs != nil && !errors.Is(err, tt.wantIs) {
					t.Fatalf("selectWDSProfileIndex() error = %v, want %v", err, tt.wantIs)
				}
				return
			}
			if err != nil {
				t.Fatalf("selectWDSProfileIndex() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("selectWDSProfileIndex() = %d, want %d", got, tt.want)
			}
		})
	}
}
