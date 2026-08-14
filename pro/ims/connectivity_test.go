//go:build ims

package ims

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/storage"
	wwanmodem "github.com/damonto/wwan-go/modem"
)

func TestResolveWiFiCallingSettingsUsesReplacementResource(t *testing.T) {
	modem := &mmodem.Modem{EquipmentIdentifier: "modem-1"}
	got, err := ResolveWiFiCallingSettings(modem, WiFiCallingSettings{
		Underlay: UnderlaySettings{Mode: UnderlayModeSystem},
	})
	if err != nil {
		t.Fatalf("ResolveWiFiCallingSettings() error = %v", err)
	}
	want := WiFiCallingSettings{Underlay: UnderlaySettings{Mode: UnderlayModeSystem}}
	if got != want {
		t.Fatalf("ResolveWiFiCallingSettings() = %+v, want %+v", got, want)
	}
}

func TestConnectivityLocksChangesPerModem(t *testing.T) {
	connectivity := &Connectivity{}
	unlockFirst := connectivity.lockModem("modem-1")

	sameModemAcquired := make(chan struct{})
	go func() {
		unlock := connectivity.lockModem("modem-1")
		close(sameModemAcquired)
		unlock()
	}()

	select {
	case <-sameModemAcquired:
		t.Fatal("second change acquired the same modem lock")
	case <-time.After(20 * time.Millisecond):
	}

	unlockOther := connectivity.lockModem("modem-2")
	unlockOther()
	unlockFirst()

	select {
	case <-sameModemAcquired:
	case <-time.After(time.Second):
		t.Fatal("second change did not acquire the released modem lock")
	}
}

func TestAirplaneModeVoLTELifecyclePreservesSetting(t *testing.T) {
	tests := []struct {
		name        string
		enabled     bool
		wantSession bool
	}{
		{name: "restores enabled VoLTE", enabled: true, wantSession: true},
		{name: "keeps disabled VoLTE stopped"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "sigmo.db"))
			if err != nil {
				t.Fatalf("storage.Open() error = %v", err)
			}
			t.Cleanup(func() {
				if err := store.Close(); err != nil {
					t.Errorf("storage.Close() error = %v", err)
				}
			})
			settingsStore := newVoLTESettingsStore(store)
			settings := VoLTESettings{Enabled: tt.enabled, DataPath: DataPathQMAP}
			if err := settingsStore.Put(ctx, "modem-1", settings); err != nil {
				t.Fatalf("Put() error = %v", err)
			}
			done := make(chan struct{})
			close(done)
			coordinator := &coordinator{
				access:           AccessVoLTE,
				volteSettings:    settingsStore,
				sessions:         map[string]*sessionState{"modem-1": {done: done}},
				voiceSubscribers: make(map[uint64]VoiceEventFunc),
			}
			connectivity := &Connectivity{volte: coordinator}
			modem := qmiTestModem("modem-1")
			modem.SIM = &mmodem.SIM{Identifier: "profile-1"}

			if err := connectivity.ChangeAirplaneMode(ctx, modem, true, func() (bool, error) {
				coordinator.mu.Lock()
				session := coordinator.sessions[modem.EquipmentIdentifier]
				suspended := coordinator.airplaneSuspended[modem.EquipmentIdentifier]
				coordinator.mu.Unlock()
				if session != nil || !suspended {
					t.Fatalf("VoLTE state during Airplane enable = session:%v suspended:%t", session != nil, suspended)
				}
				coordinator.start(t.Context(), modem, "profile-1")
				coordinator.mu.Lock()
				started := coordinator.sessions[modem.EquipmentIdentifier] != nil
				coordinator.mu.Unlock()
				if started {
					t.Fatal("VoLTE restarted while Airplane transition was suspended")
				}
				return true, nil
			}); err != nil {
				t.Fatalf("ChangeAirplaneMode(enable) error = %v", err)
			}
			got, err := settingsStore.Get(ctx, modem.EquipmentIdentifier)
			if err != nil {
				t.Fatalf("Get() error = %v", err)
			}
			if got != settings {
				t.Fatalf("settings after SuspendVoLTE() = %+v, want %+v", got, settings)
			}

			if err := connectivity.ChangeAirplaneMode(ctx, modem, false, func() (bool, error) {
				coordinator.mu.Lock()
				session := coordinator.sessions[modem.EquipmentIdentifier]
				suspended := coordinator.airplaneSuspended[modem.EquipmentIdentifier]
				coordinator.mu.Unlock()
				if session != nil || !suspended {
					t.Fatalf("VoLTE state before Airplane disable = session:%v suspended:%t", session != nil, suspended)
				}
				return true, nil
			}); err != nil {
				t.Fatalf("ChangeAirplaneMode(disable) error = %v", err)
			}
			coordinator.mu.Lock()
			hasSession := coordinator.sessions[modem.EquipmentIdentifier] != nil
			suspended := coordinator.airplaneSuspended[modem.EquipmentIdentifier]
			coordinator.mu.Unlock()
			if suspended {
				t.Fatal("VoLTE remained suspended after Airplane disable")
			}
			if hasSession != tt.wantSession {
				t.Fatalf("session after Airplane disable = %t, want %t", hasSession, tt.wantSession)
			}
			coordinator.stop(t.Context(), modem.EquipmentIdentifier)
		})
	}
}

func TestAirplaneModeVoLTERollbackUsesAppliedState(t *testing.T) {
	errChange := errors.New("change airplane mode")
	tests := []struct {
		name          string
		applied       bool
		wantSession   bool
		wantSuspended bool
	}{
		{name: "radio rejection resumes VoLTE", wantSession: true},
		{name: "persistence rejection keeps VoLTE suspended", applied: true, wantSuspended: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "sigmo.db"))
			if err != nil {
				t.Fatalf("storage.Open() error = %v", err)
			}
			t.Cleanup(func() {
				if err := store.Close(); err != nil {
					t.Errorf("storage.Close() error = %v", err)
				}
			})
			settingsStore := newVoLTESettingsStore(store)
			if err := settingsStore.Put(ctx, "modem-1", VoLTESettings{Enabled: true, DataPath: DataPathQMAP}); err != nil {
				t.Fatalf("Put() error = %v", err)
			}
			done := make(chan struct{})
			close(done)
			coordinator := &coordinator{
				access:        AccessVoLTE,
				volteSettings: settingsStore,
				sessions:      map[string]*sessionState{"modem-1": {done: done}},
			}
			connectivity := &Connectivity{volte: coordinator}
			modem := qmiTestModem("modem-1")
			modem.SIM = &mmodem.SIM{Identifier: "profile-1"}

			err = connectivity.ChangeAirplaneMode(ctx, modem, true, func() (bool, error) {
				return tt.applied, errChange
			})
			if !errors.Is(err, errChange) {
				t.Fatalf("ChangeAirplaneMode() error = %v, want %v", err, errChange)
			}
			coordinator.mu.Lock()
			hasSession := coordinator.sessions[modem.EquipmentIdentifier] != nil
			suspended := coordinator.airplaneSuspended[modem.EquipmentIdentifier]
			coordinator.mu.Unlock()
			if hasSession != tt.wantSession {
				t.Fatalf("session = %t, want %t", hasSession, tt.wantSession)
			}
			if suspended != tt.wantSuspended {
				t.Fatalf("suspended = %t, want %t", suspended, tt.wantSuspended)
			}
			coordinator.stop(t.Context(), modem.EquipmentIdentifier)
		})
	}
}

func TestAirplaneModeVoLTERestoreUsesReloadedQMIModem(t *testing.T) {
	ctx := t.Context()
	store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "sigmo.db"))
	if err != nil {
		t.Fatalf("storage.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("storage.Close() error = %v", err)
		}
	})
	settingsStore := newVoLTESettingsStore(store)
	if err := settingsStore.Put(ctx, "modem-1", VoLTESettings{Enabled: true, DataPath: DataPathQMAP}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	coordinator := &coordinator{
		access:            AccessVoLTE,
		volteSettings:     settingsStore,
		sessions:          make(map[string]*sessionState),
		airplaneSuspended: map[string]bool{"modem-1": true},
	}
	old := qmiTestModem("modem-1")
	old.PrimaryPort = "cdc-wdm0"
	old.Status.Power = wwanmodem.PowerStateLow
	replacement := qmiTestModem("modem-1")
	replacement.PrimaryPort = "cdc-wdm0"
	replacement.Status.Power = wwanmodem.PowerStateOn
	replacement.SIM = &mmodem.SIM{Identifier: "profile-1"}
	changed := false
	connectivity := &Connectivity{
		volte: coordinator,
		reloadModem: func(_ context.Context, got *mmodem.Modem) (*mmodem.Modem, error) {
			if got != old {
				t.Fatalf("Reload modem = %p, want old modem %p", got, old)
			}
			if !changed {
				t.Fatal("QMI modem reloaded before radio change")
			}
			return replacement, nil
		},
	}

	err = connectivity.ChangeAirplaneMode(ctx, old, false, func() (bool, error) {
		changed = true
		old.Status.Power = wwanmodem.PowerStateOn
		return true, nil
	})
	if err != nil {
		t.Fatalf("ChangeAirplaneMode() error = %v", err)
	}
	coordinator.mu.Lock()
	session := coordinator.sessions[old.EquipmentIdentifier]
	suspended := coordinator.airplaneSuspended[old.EquipmentIdentifier]
	coordinator.mu.Unlock()
	if suspended {
		t.Fatal("VoLTE remained suspended after QMI reload")
	}
	if session == nil {
		t.Fatal("VoLTE session was not restored")
	}
	if session.modem != replacement {
		t.Fatalf("VoLTE session modem = %p, want replacement %p", session.modem, replacement)
	}
	coordinator.stop(t.Context(), old.EquipmentIdentifier)
}

func TestAirplaneModeVoLTEResumesDeferredReplacementAfterReloadError(t *testing.T) {
	coordinator := &coordinator{
		access:            AccessVoLTE,
		sessions:          make(map[string]*sessionState),
		airplaneSuspended: make(map[string]bool),
		deferredStarts:    make(map[string]deferredSessionStart),
	}
	old := qmiTestModem("modem-1")
	old.PrimaryPort = "cdc-wdm0"
	old.Status.Power = wwanmodem.PowerStateLow
	replacement := qmiTestModem("modem-1")
	replacement.PrimaryPort = "cdc-wdm0"
	replacement.SIM = &mmodem.SIM{Identifier: "profile-1"}
	connectivity := &Connectivity{
		volte: coordinator,
		reloadModem: func(context.Context, *mmodem.Modem) (*mmodem.Modem, error) {
			// Registry publishes the replacement before a timed-out Reload caller
			// observes its error. The start must be remembered until suspension ends.
			coordinator.start(t.Context(), replacement, "profile-1")
			return nil, context.DeadlineExceeded
		},
	}

	err := connectivity.ChangeAirplaneMode(t.Context(), old, false, func() (bool, error) {
		old.Status.Power = wwanmodem.PowerStateOn
		return true, nil
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ChangeAirplaneMode() error = %v, want %v", err, context.DeadlineExceeded)
	}

	coordinator.mu.Lock()
	session := coordinator.sessions[old.EquipmentIdentifier]
	suspended := coordinator.airplaneSuspended[old.EquipmentIdentifier]
	_, deferred := coordinator.deferredStarts[old.EquipmentIdentifier]
	coordinator.mu.Unlock()
	if suspended {
		t.Fatal("VoLTE remained suspended after reload error")
	}
	if deferred {
		t.Fatal("replacement start remained deferred after suspension ended")
	}
	if session == nil || session.modem != replacement {
		t.Fatalf("VoLTE session = %+v, want replacement modem %p", session, replacement)
	}
	coordinator.stop(t.Context(), old.EquipmentIdentifier)
}

func TestReplaceVoLTESettingsRejectsAirplaneMode(t *testing.T) {
	ctx := t.Context()
	store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "sigmo.db"))
	if err != nil {
		t.Fatalf("storage.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("storage.Close() error = %v", err)
		}
	})
	settingsStore := newVoLTESettingsStore(store)
	want := VoLTESettings{Enabled: true, DataPath: DataPathQMAP}
	if err := settingsStore.Put(ctx, "modem-1", want); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	opened := false
	coordinator := &coordinator{
		access:        AccessVoLTE,
		volteSettings: settingsStore,
		managedVoLTE: managedVoLTEOps{openDevice: func(*mmodem.Modem) (managedVoLTEDevice, error) {
			opened = true
			return &fakeManagedVoLTEDevice{testMode: true}, nil
		}},
	}
	connectivity := &Connectivity{volte: coordinator}
	modem := qmiTestModem("modem-1")
	modem.Status.Power = wwanmodem.PowerStateLow

	err = connectivity.ReplaceVoLTESettings(ctx, modem, VoLTESettings{DataPath: DataPathQMAP})
	if !errors.Is(err, ErrVoLTEAirplaneMode) {
		t.Fatalf("ReplaceVoLTESettings() error = %v, want %v", err, ErrVoLTEAirplaneMode)
	}
	if opened {
		t.Fatal("ReplaceVoLTESettings() touched IMS hardware in airplane mode")
	}
	got, err := settingsStore.Get(ctx, modem.EquipmentIdentifier)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got != want {
		t.Fatalf("settings = %+v, want %+v", got, want)
	}
}

func TestAirplaneModeChangeSerializesVoLTESettings(t *testing.T) {
	coordinator := &coordinator{access: AccessVoLTE, sessions: make(map[string]*sessionState)}
	connectivity := &Connectivity{volte: coordinator}
	modem := qmiTestModem("modem-1")
	modem.Status.Power = wwanmodem.PowerStateOn
	transitionEntered := make(chan struct{})
	releaseTransition := make(chan struct{})
	transitionDone := make(chan error, 1)
	go func() {
		transitionDone <- connectivity.ChangeAirplaneMode(t.Context(), modem, true, func() (bool, error) {
			close(transitionEntered)
			<-releaseTransition
			modem.Status.Power = wwanmodem.PowerStateLow
			return true, nil
		})
	}()

	select {
	case <-transitionEntered:
	case <-time.After(time.Second):
		t.Fatal("Airplane transition did not start")
	}
	settingsDone := make(chan error, 1)
	go func() {
		settingsDone <- connectivity.ReplaceVoLTESettings(t.Context(), modem, VoLTESettings{DataPath: DataPathQMAP})
	}()
	select {
	case err := <-settingsDone:
		t.Fatalf("VoLTE settings bypassed Airplane transition lock: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	close(releaseTransition)
	if err := <-transitionDone; err != nil {
		t.Fatalf("ChangeAirplaneMode() error = %v", err)
	}
	select {
	case err := <-settingsDone:
		if !errors.Is(err, ErrVoLTEAirplaneMode) {
			t.Fatalf("ReplaceVoLTESettings() error = %v, want %v", err, ErrVoLTEAirplaneMode)
		}
	case <-time.After(time.Second):
		t.Fatal("VoLTE settings did not resume after Airplane transition")
	}
}
