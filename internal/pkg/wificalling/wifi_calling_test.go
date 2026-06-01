//go:build wifi_calling

package wificalling

import (
	"testing"
	"time"

	vowifi "github.com/damonto/vowifi-go"
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
				client:      &vowifi.Client{},
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
				client:    &vowifi.Client{},
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
