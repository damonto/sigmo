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

func TestDeletionEntryPointsPreserveHistoryAndCleanReminders(t *testing.T) {
	for _, entry := range []string{"typed HTTP and transfer action", "MCP action by modem ID"} {
		t.Run(entry, func(t *testing.T) {
			db, err := storage.Open(t.Context(), filepath.Join(t.TempDir(), "profiles.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := db.Close(); err != nil {
					t.Error(err)
				}
			})
			reminders, err := reminder.New(db, settings.NewMemoryStore(settings.Default()), nil)
			if err != nil {
				t.Fatal(err)
			}
			iccid := mustTestICCID(t, "8985200012345678901")
			device := &mmodem.Modem{EquipmentIdentifier: "modem-1"}
			if err := reminders.Save(t.Context(), storage.Reminder{
				ProfileType: "esim", ProfileID: iccid.String(), ModemID: device.EquipmentIdentifier,
				SEID: "se0", ProfileName: "Travel", Content: "Renew", NextAt: time.Now().Add(time.Hour),
			}); err != nil {
				t.Fatal(err)
			}
			probe := &deletionProbe{fakeLifecycleClient: fakeLifecycleClient{
				profiles: []*sgp22.ProfileInfo{{ICCID: iccid, ProfileState: sgp22.ProfileDisabled}},
			}}
			service := &Service{
				registry:  testModemFinder{device},
				profile:   &profile{history: db},
				reminders: reminders,
				lifecycle: &lifecycle{newClient: func(context.Context, *mmodem.Modem, *settings.Settings, string) (lifecycleClient, error) {
					return probe, nil
				}},
			}
			if entry == "MCP action by modem ID" {
				err = service.RemoveProfile(t.Context(), device.EquipmentIdentifier, "se0", iccid.String())
			} else {
				err = service.DeleteProfile(t.Context(), device, "se0", iccid)
			}
			if err != nil {
				t.Fatal(err)
			}
			lines, err := db.SIMLines(t.Context())
			if err != nil || len(lines) != 1 || lines[0].DeletionState != "deleted" {
				t.Fatalf("deletion history = %+v, %v", lines, err)
			}
			if _, exists, err := reminders.Get(t.Context(), reminder.ProfileTypeESIM, iccid.String()); err != nil || exists {
				t.Fatalf("reminder remains = %v, %v", exists, err)
			}
			if probe.deletions != 1 {
				t.Fatalf("device deletions = %d, want 1", probe.deletions)
			}
		})
	}
}

type testModemFinder struct{ device *mmodem.Modem }

func (f testModemFinder) Find(ctx context.Context, id string) (*mmodem.Modem, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.device == nil || f.device.EquipmentIdentifier != id {
		return nil, mmodem.ErrNotFound
	}
	return f.device, nil
}

func TestActivateProfileTreatsAlreadyActiveAsSuccess(t *testing.T) {
	device := &mmodem.Modem{EquipmentIdentifier: "modem-1"}
	iccid := mustTestICCID(t, "8985200012345678901")
	probe := &deletionProbe{fakeLifecycleClient: fakeLifecycleClient{
		profiles: []*sgp22.ProfileInfo{{ICCID: iccid, ProfileState: sgp22.ProfileEnabled}},
	}}
	service := &Service{
		registry: testModemFinder{device},
		lifecycle: &lifecycle{newClient: func(context.Context, *mmodem.Modem, *settings.Settings, string) (lifecycleClient, error) {
			return probe, nil
		}},
	}
	if err := service.ActivateProfile(t.Context(), device.EquipmentIdentifier, "se0", iccid.String()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := service.ActivateProfile(ctx, device.EquipmentIdentifier, "se0", iccid.String()); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled activation = %v", err)
	}
}
