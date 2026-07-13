//go:build ims

package ims

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"

	imsgo "github.com/damonto/ims-go"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

func TestReleaseManagedVoLTEOnShutdown(t *testing.T) {
	errTestMode := errors.New("read test mode")
	errQMAP := errors.New("restore QMAP")
	tests := []struct {
		name              string
		access            Access
		modems            []*mmodem.Modem
		device            *fakeManagedVoLTEDevice
		internet          *fakeInternetRestorer
		wantCalls         []string
		wantInternetCalls []string
		wantErrs          []error
		wantOpened        bool
		wantClosed        bool
	}{
		{
			name:   "ignores Wi-Fi Calling shutdown",
			access: AccessWiFiCalling,
			modems: []*mmodem.Modem{{EquipmentIdentifier: "modem-1"}},
			device: &fakeManagedVoLTEDevice{},
		},
		{
			name:       "restores managed VoLTE with uncanceled context",
			access:     AccessVoLTE,
			modems:     []*mmodem.Modem{nil, {EquipmentIdentifier: "modem-1"}},
			device:     &fakeManagedVoLTEDevice{},
			wantCalls:  []string{"test-mode"},
			wantOpened: true,
			wantClosed: true,
		},
		{
			name:              "restores QMAP after VoLTE cleanup error",
			access:            AccessVoLTE,
			modems:            []*mmodem.Modem{{EquipmentIdentifier: "modem-1"}},
			device:            &fakeManagedVoLTEDevice{testModeErr: errTestMode},
			internet:          &fakeInternetRestorer{},
			wantCalls:         []string{"test-mode"},
			wantInternetCalls: []string{"qmap:false"},
			wantErrs:          []error{errTestMode},
			wantOpened:        true,
			wantClosed:        true,
		},
		{
			name:              "joins VoLTE and QMAP cleanup errors",
			access:            AccessVoLTE,
			modems:            []*mmodem.Modem{{EquipmentIdentifier: "modem-1"}},
			device:            &fakeManagedVoLTEDevice{testModeErr: errTestMode},
			internet:          &fakeInternetRestorer{qmapErr: errQMAP},
			wantCalls:         []string{"test-mode"},
			wantInternetCalls: []string{"qmap:false"},
			wantErrs:          []error{errTestMode, errQMAP},
			wantOpened:        true,
			wantClosed:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			previousOpen := openManagedVoLTEDevice
			opened := false
			openManagedVoLTEDevice = func(*mmodem.Modem) (managedVoLTEDevice, error) {
				opened = true
				return tt.device, nil
			}
			t.Cleanup(func() {
				openManagedVoLTEDevice = previousOpen
			})

			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			coordinator := &coordinator{access: tt.access}
			if tt.internet != nil {
				coordinator.internet = tt.internet
			}
			err := coordinator.releaseManagedVoLTEOnShutdown(ctx, tt.modems)
			for _, wantErr := range tt.wantErrs {
				if !errors.Is(err, wantErr) {
					t.Fatalf("releaseManagedVoLTEOnShutdown() error = %v, want %v", err, wantErr)
				}
			}
			if len(tt.wantErrs) == 0 && err != nil {
				t.Fatalf("releaseManagedVoLTEOnShutdown() error = %v", err)
			}
			if opened != tt.wantOpened {
				t.Fatalf("openManagedVoLTEDevice called = %v, want %v", opened, tt.wantOpened)
			}
			if !slices.Equal(tt.device.calls, tt.wantCalls) {
				t.Fatalf("device calls = %v, want %v", tt.device.calls, tt.wantCalls)
			}
			if tt.device.closed != tt.wantClosed {
				t.Fatalf("device closed = %v, want %v", tt.device.closed, tt.wantClosed)
			}
			var internetCalls []string
			if tt.internet != nil {
				internetCalls = tt.internet.calls
			}
			if !slices.Equal(internetCalls, tt.wantInternetCalls) {
				t.Fatalf("Internet calls = %v, want %v", internetCalls, tt.wantInternetCalls)
			}
			if tt.device.testModeCtxErr != nil {
				t.Fatalf("IMSSTestMode() context error = %v, want nil", tt.device.testModeCtxErr)
			}
		})
	}
}

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

func TestVoLTESettingsStore(t *testing.T) {
	tests := []struct {
		name        string
		enabled     bool
		wantEnabled bool
	}{
		{name: "enabled", enabled: true, wantEnabled: true},
		{name: "disabled"},
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
			settings := NewVoLTESettingsStore(store)
			if err := settings.Put(ctx, "modem-1", tt.enabled); err != nil {
				t.Fatalf("Put() error = %v", err)
			}
			got, err := settings.Get(ctx, "modem-1")
			if err != nil {
				t.Fatalf("Get() error = %v", err)
			}
			if got.Enabled != tt.wantEnabled || got.Preferred {
				t.Fatalf("Get() = %+v, want enabled %v without preference", got, tt.wantEnabled)
			}
		})
	}
}
