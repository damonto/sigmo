package esim

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	sgp22 "github.com/damonto/euicc-go/v2"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/reminder"
	"github.com/damonto/sigmo/internal/pkg/settings"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

func TestFindProfileName(t *testing.T) {
	profiles := &ProfilesResponse{SEs: []ProfileGroupResponse{
		{ID: "se0", Profiles: []ProfileResponse{{ICCID: "8985", Name: "Travel"}}},
		{ID: "se1", Profiles: []ProfileResponse{{ICCID: "8986", Name: "Work"}}},
	}}
	tests := []struct {
		name     string
		profiles *ProfilesResponse
		seID     string
		iccid    string
		wantName string
		wantOK   bool
	}{
		{name: "match", profiles: profiles, seID: " se0 ", iccid: "8985", wantName: "Travel", wantOK: true},
		{name: "wrong SE", profiles: profiles, seID: "se1", iccid: "8985"},
		{name: "wrong ICCID", profiles: profiles, seID: "se0", iccid: "0000"},
		{name: "nil response"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := findProfileName(tt.profiles, tt.seID, tt.iccid)
			if got != tt.wantName || ok != tt.wantOK {
				t.Fatalf("findProfileName() = (%q, %v), want (%q, %v)", got, ok, tt.wantName, tt.wantOK)
			}
		})
	}
}

func TestDeleteProfileReminderCleanup(t *testing.T) {
	iccid, err := sgp22.NewICCID("8985200012345678901")
	if err != nil {
		t.Fatalf("NewICCID() error = %v", err)
	}
	tests := []struct {
		name           string
		profilePresent bool
		storeReminder  bool
		reminderModem  string
		reminderSE     string
		wantErr        error
		wantReminder   bool
	}{
		{
			name:           "successful profile delete clears reminder",
			profilePresent: true,
			storeReminder:  true,
			reminderModem:  "modem-1",
			reminderSE:     "se0",
		},
		{
			name:          "retry after profile deletion clears matching reminder",
			storeReminder: true,
			reminderModem: "modem-1",
			reminderSE:    "se0",
		},
		{
			name: "retry after completed cleanup is idempotent",
		},
		{
			name:          "absent source does not delete reminder moved elsewhere",
			storeReminder: true,
			reminderModem: "modem-2",
			reminderSE:    "se1",
			wantErr:       errProfileNotFound,
			wantReminder:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
			if err != nil {
				t.Fatalf("storage.Open() error = %v", err)
			}
			t.Cleanup(func() {
				if err := store.Close(); err != nil {
					t.Errorf("Store.Close() error = %v", err)
				}
			})
			reminders, err := reminder.New(store, settings.NewMemoryStore(settings.Default()), nil)
			if err != nil {
				t.Fatalf("reminder.New() error = %v", err)
			}
			if tt.storeReminder {
				if err := reminders.Save(context.Background(), storage.Reminder{
					ProfileType: reminder.ProfileTypeESIM.String(),
					ProfileID:   iccid.String(),
					ModemID:     tt.reminderModem,
					SEID:        tt.reminderSE,
					ProfileName: "Travel",
					NextAt:      time.Now().Add(time.Hour),
					Content:     "Renew",
				}); err != nil {
					t.Fatalf("Save() error = %v", err)
				}
			}

			profiles := []*sgp22.ProfileInfo(nil)
			if tt.profilePresent {
				profiles = []*sgp22.ProfileInfo{{ICCID: iccid, ProfileState: sgp22.ProfileDisabled}}
			}
			h := &Handler{
				lifecycle: &lifecycle{
					settings: settings.Default(),
					newClient: func(*mmodem.Modem, *settings.Settings, string) (lifecycleClient, error) {
						return &fakeLifecycleClient{profiles: profiles}, nil
					},
				},
				reminders: reminders,
			}
			modem := &mmodem.Modem{EquipmentIdentifier: "modem-1"}
			err = h.DeleteProfile(context.Background(), modem, "se0", iccid)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("DeleteProfile() error = %v, want %v", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("DeleteProfile() error = %v", err)
			}
			_, exists, err := reminders.Get(context.Background(), reminder.ProfileTypeESIM, iccid.String())
			if err != nil {
				t.Fatalf("Get() error = %v", err)
			}
			if exists != tt.wantReminder {
				t.Fatalf("reminder exists = %v, want %v", exists, tt.wantReminder)
			}
		})
	}
}
