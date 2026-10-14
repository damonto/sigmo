package esim

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	sgp22 "github.com/damonto/euicc-go/v2"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/settings"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

type deletionProbe struct {
	fakeLifecycleClient
	deletions int
}

func (p *deletionProbe) Delete(iccid sgp22.ICCID) error {
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
		{name: "already absent", wantState: "deleted", wantErr: errProfileNotFound},
		{name: "active profile", present: true, active: true, wantErr: errActiveProfileCannotDelete},
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
			if tc.present {
				info := &sgp22.ProfileInfo{ICCID: iccid, ProfileState: sgp22.ProfileDisabled}
				if tc.active {
					info.ProfileState = sgp22.ProfileEnabled
				}
				probe.profiles = []*sgp22.ProfileInfo{info}
			}
			h := &Handler{profile: &profile{history: store}, lifecycle: &lifecycle{
				settings: settings.Default(),
				newClient: func(ctx context.Context, _ *mmodem.Modem, _ *settings.Settings, _ string) (lifecycleClient, error) {
					lines, err := store.SIMLines(ctx)
					if err != nil || len(lines) != 1 || lines[0].DeletionState != "pending" {
						t.Fatalf("hardware accessed before durable intent: %+v, %v", lines, err)
					}
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
			if err := h.DeleteProfile(t.Context(), modem, "se0", iccid); !errors.Is(err, errProfileNotFound) {
				t.Fatal(err)
			}
			if probe.deletions != 1 {
				t.Fatalf("retry repeated deletion: %d", probe.deletions)
			}
		})
	}
}
