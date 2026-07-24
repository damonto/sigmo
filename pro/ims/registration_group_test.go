//go:build ims

package ims

import "testing"

func TestRegistrationGroups(t *testing.T) {
	groups := &RegistrationGroups{}
	first := groups.Group("modem-1", "profile-1")
	tests := []struct {
		name      string
		modemID   string
		profileID string
		wantSame  bool
	}{
		{
			name:      "same modem and profile",
			modemID:   "modem-1",
			profileID: "profile-1",
			wantSame:  true,
		},
		{
			name:      "different modem",
			modemID:   "modem-2",
			profileID: "profile-1",
		},
		{
			name:      "different profile",
			modemID:   "modem-1",
			profileID: "profile-2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := groups.Group(tt.modemID, tt.profileID)
			if same := got == first; same != tt.wantSame {
				t.Fatalf("Group() same = %t, want %t", same, tt.wantSame)
			}
		})
	}
}
