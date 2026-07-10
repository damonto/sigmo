//go:build ims

package ims

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	imsgo "github.com/damonto/ims-go"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

func TestStatusFromSession(t *testing.T) {
	now := time.Date(2026, 5, 29, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name          string
		settings      Settings
		session       *sessionState
		profileID     string
		wantState     string
		wantConnected bool
		wantDuration  int64
	}{
		{
			name:      "idle when disabled without session",
			profileID: "profile-1",
			wantState: StateIdle,
		},
		{
			name:      "disconnected when enabled without session",
			settings:  Settings{Enabled: true},
			profileID: "profile-1",
			wantState: StateDisconnected,
		},
		{
			name: "connecting session",
			session: &sessionState{
				phase:     sessionPhaseConnecting,
				profileID: "profile-1",
			},
			profileID: "profile-1",
			wantState: StateConnecting,
		},
		{
			name: "websheet required session",
			session: &sessionState{
				phase:     sessionPhaseWebsheetRequired,
				profileID: "profile-1",
			},
			profileID: "profile-1",
			wantState: StateWebsheetRequired,
		},
		{
			name: "connected session",
			session: &sessionState{
				phase:       sessionPhaseConnected,
				client:      &imsgo.Client{},
				profileID:   "profile-1",
				connectedAt: now.Add(-2 * time.Minute),
			},
			profileID:     "profile-1",
			wantState:     StateConnected,
			wantConnected: true,
			wantDuration:  120,
		},
		{
			name: "profile mismatch uses settings state",
			settings: Settings{
				Enabled: true,
			},
			session: &sessionState{
				phase:     sessionPhaseConnected,
				client:    &imsgo.Client{},
				profileID: "profile-2",
			},
			profileID: "profile-1",
			wantState: StateDisconnected,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := statusFromSession(tt.settings, tt.session, tt.profileID, now)
			if got.State != tt.wantState {
				t.Fatalf("State = %q, want %q", got.State, tt.wantState)
			}
			if got.Connected != tt.wantConnected {
				t.Fatalf("Connected = %v, want %v", got.Connected, tt.wantConnected)
			}
			if got.DurationSeconds != tt.wantDuration {
				t.Fatalf("DurationSeconds = %d, want %d", got.DurationSeconds, tt.wantDuration)
			}
		})
	}
}

func TestApplyVoLTEPreferenceUsesSavedState(t *testing.T) {
	tests := []struct {
		name  string
		event mmodem.VoLTEPreferenceEvent
	}{
		{
			name:  "stale enabled event follows saved disabled state",
			event: mmodem.VoLTEPreferenceEvent{ModemID: "modem-1", Enabled: true},
		},
		{
			name:  "disabled event follows saved disabled state",
			event: mmodem.VoLTEPreferenceEvent{ModemID: "modem-1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "sigmo.db"))
			if err != nil {
				t.Fatalf("Open() error = %v", err)
			}
			t.Cleanup(func() {
				if err := store.Close(); err != nil {
					t.Errorf("Close() error = %v", err)
				}
			})
			preferences, err := mmodem.NewNetworkPreferences(store)
			if err != nil {
				t.Fatalf("NewNetworkPreferences() error = %v", err)
			}
			if err := preferences.SaveVoLTE(ctx, tt.event.ModemID, true); err != nil {
				t.Fatalf("SaveVoLTE(true) error = %v", err)
			}
			if err := preferences.SaveVoLTE(ctx, tt.event.ModemID, false); err != nil {
				t.Fatalf("SaveVoLTE(false) error = %v", err)
			}

			c := New(Config{
				Store:              store,
				Access:             AccessVoLTE,
				NetworkPreferences: preferences,
			}).(*coordinator)
			c.sessions[tt.event.ModemID] = &sessionState{}

			c.applyVoLTEPreference(ctx, nil, tt.event)

			c.mu.Lock()
			_, exists := c.sessions[tt.event.ModemID]
			c.mu.Unlock()
			if exists {
				t.Fatal("session still exists after saved VoLTE preference was disabled")
			}
		})
	}
}
