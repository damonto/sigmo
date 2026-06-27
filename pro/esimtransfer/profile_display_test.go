//go:build esim_transfer

package esimtransfer

import (
	"testing"

	sgp22 "github.com/damonto/euicc-go/v2"
)

func TestProfilePreviewFromIncludesOwner(t *testing.T) {
	tests := []struct {
		name string
		info *sgp22.ProfileInfo
		want ProfileOwnerResponse
	}{
		{
			name: "owner mcc and mnc",
			info: &sgp22.ProfileInfo{
				ICCID:               mustTestICCID(t, "8901000000000000001"),
				ServiceProviderName: "Carrier",
				ProfileName:         "Travel Line",
				ProfileState:        sgp22.ProfileDisabled,
				ProfileOwner: sgp22.OperatorId{
					PLMN: []byte{0x02, 0xf8, 0x90},
					GID1: []byte{0x63, 0x32},
				},
			},
			want: ProfileOwnerResponse{MCC: "208", MNC: "09", GID1: "6332"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := profilePreviewFrom(tt.info)
			if got.ProfileOwner != tt.want {
				t.Fatalf("ProfileOwner = %+v, want %+v", got.ProfileOwner, tt.want)
			}
		})
	}
}

func TestESIMCandidateIncludesProfileDetails(t *testing.T) {
	tests := []struct {
		name string
		info *sgp22.ProfileInfo
		want ProfileResponse
	}{
		{
			name: "technical profile fields",
			info: &sgp22.ProfileInfo{
				ICCID:               mustTestICCID(t, "8901000000000000001"),
				ISDPAID:             sgp22.ISDPAID{0xa0, 0x00, 0x00, 0x05, 0x59},
				ProfileState:        sgp22.ProfileEnabled,
				ProfileNickname:     "Travel",
				ServiceProviderName: "Carrier",
				ProfileName:         "Travel Line",
				ProfileClass:        sgp22.ProfileClassOperational,
				ProfileOwner: sgp22.OperatorId{
					PLMN: []byte{0x02, 0xf8, 0x90},
					GID1: []byte{0x63, 0x32},
					GID2: []byte{0xab, 0xcd},
				},
			},
			want: ProfileResponse{
				Name:                "Travel",
				ServiceProviderName: "Carrier",
				ICCID:               "8901000000000000001",
				ISDPAID:             "A000000559",
				ProfileName:         "Travel Line",
				ProfileNickname:     "Travel",
				ProfileStateName:    "enabled",
				ProfileClass:        "operational",
				ProfileOwner: ProfileOwnerResponse{
					MCC:  "208",
					MNC:  "09",
					GID1: "6332",
					GID2: "ABCD",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := esimCandidate(tt.info).response
			if got.Name != tt.want.Name {
				t.Fatalf("Name = %q, want %q", got.Name, tt.want.Name)
			}
			if got.ServiceProviderName != tt.want.ServiceProviderName {
				t.Fatalf("ServiceProviderName = %q, want %q", got.ServiceProviderName, tt.want.ServiceProviderName)
			}
			if got.ICCID != tt.want.ICCID {
				t.Fatalf("ICCID = %q, want %q", got.ICCID, tt.want.ICCID)
			}
			if got.ISDPAID != tt.want.ISDPAID {
				t.Fatalf("ISDPAID = %q, want %q", got.ISDPAID, tt.want.ISDPAID)
			}
			if got.ProfileName != tt.want.ProfileName {
				t.Fatalf("ProfileName = %q, want %q", got.ProfileName, tt.want.ProfileName)
			}
			if got.ProfileNickname != tt.want.ProfileNickname {
				t.Fatalf("ProfileNickname = %q, want %q", got.ProfileNickname, tt.want.ProfileNickname)
			}
			if got.ProfileStateName != tt.want.ProfileStateName {
				t.Fatalf("ProfileStateName = %q, want %q", got.ProfileStateName, tt.want.ProfileStateName)
			}
			if got.ProfileClass != tt.want.ProfileClass {
				t.Fatalf("ProfileClass = %q, want %q", got.ProfileClass, tt.want.ProfileClass)
			}
			if got.ProfileOwner != tt.want.ProfileOwner {
				t.Fatalf("ProfileOwner = %+v, want %+v", got.ProfileOwner, tt.want.ProfileOwner)
			}
		})
	}
}

func mustTestICCID(t *testing.T, value string) sgp22.ICCID {
	t.Helper()

	iccid, err := sgp22.NewICCID(value)
	if err != nil {
		t.Fatalf("NewICCID() error = %v", err)
	}
	return iccid
}
