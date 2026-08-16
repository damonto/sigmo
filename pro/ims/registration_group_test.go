//go:build ims

package ims

import (
	"testing"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

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

func TestRegistrationGroupsRotateForSIMChange(t *testing.T) {
	groups := &RegistrationGroups{}
	oldProfile := groups.Group("modem-1", "profile-old")
	inactiveProfile := groups.Group("modem-1", "profile-inactive")
	otherModem := groups.Group("modem-2", "profile-old")
	event := mmodem.ModemEvent{
		Type:                  mmodem.ModemEventSIMChanged,
		Modem:                 &mmodem.Modem{EquipmentIdentifier: "modem-1"},
		Path:                  "/devices/modem-1",
		Generation:            3,
		PreviousSIMSlot:       1,
		SIMSlot:               1,
		PreviousSIMIdentifier: "profile-old",
		SIMIdentifier:         "profile-new",
	}

	groups.RotateForSIMChange(event)
	newProfile := groups.Group("modem-1", "profile-old")
	if newProfile == oldProfile {
		t.Fatal("RotateForSIMChange() preserved the old profile group")
	}
	if groups.Group("modem-1", "profile-inactive") == inactiveProfile {
		t.Fatal("RotateForSIMChange() preserved another profile for the changed modem")
	}
	if groups.Group("modem-2", "profile-old") != otherModem {
		t.Fatal("RotateForSIMChange() replaced an unrelated modem group")
	}

	groups.RotateForSIMChange(event)
	if groups.Group("modem-1", "profile-old") != newProfile {
		t.Fatal("duplicate SIM change replaced the new lifecycle group")
	}

	event.PreviousSIMIdentifier = "profile-new"
	event.SIMIdentifier = "profile-old"
	groups.RotateForSIMChange(event)
	if groups.Group("modem-1", "profile-old") == newProfile {
		t.Fatal("next SIM change preserved the previous lifecycle group")
	}
}
