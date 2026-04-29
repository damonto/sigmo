package esim

import (
	"testing"

	sgp22 "github.com/damonto/euicc-go/v2"
)

func TestActiveProfile(t *testing.T) {
	t.Parallel()

	target, err := sgp22.NewICCID("8985200012345678901")
	if err != nil {
		t.Fatalf("NewICCID() error = %v", err)
	}
	other, err := sgp22.NewICCID("8985200099999999999")
	if err != nil {
		t.Fatalf("NewICCID() error = %v", err)
	}

	tests := []struct {
		name     string
		profiles []*sgp22.ProfileInfo
		want     bool
	}{
		{
			name: "target enabled",
			profiles: []*sgp22.ProfileInfo{
				{ICCID: target, ProfileState: sgp22.ProfileEnabled},
			},
			want: true,
		},
		{
			name: "target disabled",
			profiles: []*sgp22.ProfileInfo{
				{ICCID: target, ProfileState: sgp22.ProfileDisabled},
			},
			want: false,
		},
		{
			name: "other enabled",
			profiles: []*sgp22.ProfileInfo{
				{ICCID: other, ProfileState: sgp22.ProfileEnabled},
			},
			want: false,
		},
		{
			name: "nil profile",
			profiles: []*sgp22.ProfileInfo{
				nil,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := activeProfile(tt.profiles, target); got != tt.want {
				t.Fatalf("activeProfile() = %v, want %v", got, tt.want)
			}
		})
	}
}
