package esim

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"

	sgp22 "github.com/damonto/euicc-go/v2"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/settings"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

type deletionProbe struct {
	fakeLifecycleClient
	deletions    int
	beforeDelete func()
}

func (p *deletionProbe) Delete(iccid sgp22.ICCID) error {
	if p.beforeDelete != nil {
		p.beforeDelete()
	}
	p.deletions++
	return p.fakeLifecycleClient.Delete(iccid)
}

func TestDeletionHistoryPersistsIntentAndRecovery(t *testing.T) {
	iccid, err := sgp22.NewICCID("8985200012345678901")
	if err != nil {
		t.Fatal(err)
	}
	unknown := errors.New("device disconnected before response")
	for _, tc := range []struct {
		name               string
		present, active    bool
		deleteErr, wantErr error
		wantState          string
		wantDeletions      int
	}{
		{name: "confirmed", present: true, wantState: "deleted", wantDeletions: 1},
		{name: "already absent", wantState: "deleted"},
		{name: "active profile", present: true, active: true, wantErr: ErrActiveProfileCannotDelete},
		{name: "unknown outcome", present: true, deleteErr: unknown, wantErr: unknown, wantState: "pending", wantDeletions: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "history.db")
			store, err := storage.Open(t.Context(), path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := store.Close(); err != nil {
					t.Error(err)
				}
			})
			probe := &deletionProbe{fakeLifecycleClient: fakeLifecycleClient{deleteErr: tc.deleteErr}}
			if err := store.RecordSIMProfile(t.Context(), storage.SIMProfile{
				ProfileID: iccid.String(), ModemID: "modem", Kind: "esim", SEID: "se0",
			}); err != nil {
				t.Fatal(err)
			}
			probe.beforeDelete = func() {
				lines, err := store.SIMLines(t.Context())
				if err != nil || len(lines) != 1 || lines[0].DeletionState != "pending" {
					t.Fatalf("hardware mutated before durable intent: %+v, %v", lines, err)
				}
			}
			if tc.present {
				info := &sgp22.ProfileInfo{ICCID: iccid, ProfileState: sgp22.ProfileDisabled}
				if tc.active {
					info.ProfileState = sgp22.ProfileEnabled
				}
				probe.profiles = []*sgp22.ProfileInfo{info}
			}
			h := &Service{profile: &profile{history: store}, lifecycle: &lifecycle{
				settings: settings.Default(),
				newClient: func(context.Context, *mmodem.Modem, *settings.Settings, string) (lifecycleClient, error) {
					return probe, nil
				},
			}}
			modem := &mmodem.Modem{EquipmentIdentifier: "modem"}
			if err := h.DeleteProfile(t.Context(), modem, "se0", iccid); !errors.Is(err, tc.wantErr) {
				t.Fatalf("delete = %v, want %v", err, tc.wantErr)
			}
			if probe.deletions != tc.wantDeletions {
				t.Fatalf("hardware deletions = %d, want %d", probe.deletions, tc.wantDeletions)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			store, err = storage.Open(t.Context(), path)
			if err != nil {
				t.Fatal(err)
			}
			lines, err := store.SIMLines(t.Context())
			if err != nil || len(lines) != 1 || lines[0].DeletionState != tc.wantState {
				t.Fatalf("restored state = %+v, %v", lines, err)
			}
			if tc.deleteErr == nil {
				return
			}
			// The card confirms absence on retry: do not repeat the hardware mutation.
			h.profile.history = store
			probe.profiles = nil
			if err := h.DeleteProfile(t.Context(), modem, "se0", iccid); err != nil {
				t.Fatal(err)
			}
			if probe.deletions != 1 {
				t.Fatalf("retry repeated deletion: %d", probe.deletions)
			}
		})
	}
}

func TestDeletionRejectsUnverifiedTargetsWithoutChangingHistory(t *testing.T) {
	iccid := mustTestICCID(t, "8985200012345678901")
	for _, tt := range []struct {
		name, modemID, seID, state string
		active, assigned, unknown  bool
		prepareErr                 error
		wantErr                    error
	}{
		{name: "wrong modem", modemID: "other", seID: "se0", state: "pending"},
		{name: "wrong secure element", modemID: "modem", seID: "other", state: "pending"},
		{name: "active profile", modemID: "modem", seID: "se0", active: true, wantErr: ErrActiveProfileCannotDelete},
		{name: "lookup unavailable", modemID: "modem", seID: "se0", prepareErr: context.DeadlineExceeded, wantErr: context.DeadlineExceeded},
		{name: "unknown ICCID", unknown: true},
		{name: "completed history assignment", modemID: "modem", seID: "se0", assigned: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			store, err := storage.Open(t.Context(), filepath.Join(t.TempDir(), "history.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			if !tt.unknown {
				if err := store.RecordSIMProfile(t.Context(), storage.SIMProfile{
					ProfileID: iccid.String(), ModemID: tt.modemID, Kind: "esim", SEID: tt.seID,
				}); err != nil {
					t.Fatal(err)
				}
				if err := store.MarkProfileDeletion(t.Context(), iccid.String(), tt.state); err != nil {
					t.Fatal(err)
				}
			}
			if tt.assigned {
				if err := store.RecordSIMProfile(t.Context(), storage.SIMProfile{ProfileID: "target", ModemID: "modem"}); err != nil {
					t.Fatal(err)
				}
				if err := store.MarkProfileDeletion(t.Context(), iccid.String(), "deleted"); err != nil {
					t.Fatal(err)
				}
				if err := store.AssignHistory(t.Context(), storage.HistoryAssignment{SourceProfileID: iccid.String(), TargetProfileID: "target"}); err != nil {
					t.Fatal(err)
				}
			}
			before, err := store.SIMLines(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			probe := &deletionProbe{}
			if tt.active {
				probe.profiles = []*sgp22.ProfileInfo{{ICCID: iccid, ProfileState: sgp22.ProfileEnabled}}
			}
			service := &Service{profile: &profile{history: store}, lifecycle: &lifecycle{
				newClient: func(context.Context, *mmodem.Modem, *settings.Settings, string) (lifecycleClient, error) {
					if tt.prepareErr != nil {
						return nil, tt.prepareErr
					}
					return probe, nil
				},
			}}
			device := &mmodem.Modem{EquipmentIdentifier: "modem"}
			if err := service.DeleteProfile(t.Context(), device, "se0", iccid); !errors.Is(err, tt.wantErr) {
				t.Fatalf("DeleteProfile = %v, want %v", err, tt.wantErr)
			}
			after, err := store.SIMLines(t.Context())
			if err != nil || !slices.Equal(before, after) {
				t.Fatalf("history changed: before = %+v, after = %+v, error = %v", before, after, err)
			}
			if probe.deletions != 0 {
				t.Fatalf("unverified deletion reached hardware %d times", probe.deletions)
			}
			if tt.prepareErr == nil && !probe.released {
				t.Fatal("card lease was not released")
			}
		})
	}
}
