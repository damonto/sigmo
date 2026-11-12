package esim

import (
	"testing"

	sgp22 "github.com/damonto/euicc-go/v2"

	appesim "github.com/damonto/sigmo/internal/app/esim"
)

func TestProfilePreviewFromIncludesOwner(t *testing.T) {
	tests := []struct {
		name string
		info *sgp22.ProfileInfo
		want appesim.ProfileOwner
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
				},
			},
			want: appesim.ProfileOwner{MCC: "208", MNC: "09"},
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

func mustTestICCID(t *testing.T, value string) sgp22.ICCID {
	t.Helper()

	iccid, err := sgp22.NewICCID(value)
	if err != nil {
		t.Fatalf("NewICCID() error = %v", err)
	}
	return iccid
}
