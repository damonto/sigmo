//go:build ims

package ims

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	imsgo "github.com/damonto/ims-go"
	pinternet "github.com/damonto/sigmo/internal/pkg/internet"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/storage"
	wwanmodem "github.com/damonto/wwan-go/modem"
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
			modems:     []*mmodem.Modem{nil, qmiTestModem("modem-1")},
			device:     &fakeManagedVoLTEDevice{},
			wantCalls:  []string{"test-mode"},
			wantOpened: true,
			wantClosed: true,
		},
		{
			name:              "restores QMAP after VoLTE cleanup error",
			access:            AccessVoLTE,
			modems:            []*mmodem.Modem{qmiTestModem("modem-1")},
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
			modems:            []*mmodem.Modem{qmiTestModem("modem-1")},
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
			opened := false
			managedVoLTE := managedVoLTEOps{openDevice: func(*mmodem.Modem) (managedVoLTEDevice, error) {
				opened = true
				return tt.device, nil
			}}

			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			coordinator := &coordinator{access: tt.access, managedVoLTE: managedVoLTE}
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
		enabled       bool
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
			enabled:   true,
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
			name: "waiting for uplink",
			session: &sessionState{
				phase:     sessionPhaseWaitingForUplink,
				profileID: "profile-1",
			},
			profileID: "profile-1",
			wantState: StateWaitingForUplink,
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
			name:    "profile mismatch uses settings state",
			enabled: true,
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
			got := statusFromSession(tt.enabled, tt.session, tt.profileID, now)
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
		name     string
		settings *VoLTESettings
		want     VoLTESettings
	}{
		{name: "empty store defaults to QMAP", want: VoLTESettings{DataPath: DataPathQMAP}},
		{
			name:     "enabled QMAP",
			settings: &VoLTESettings{Enabled: true, DataPath: DataPathQMAP},
			want:     VoLTESettings{Enabled: true, DataPath: DataPathQMAP},
		},
		{
			name:     "disabled legacy BAM-DMUX",
			settings: &VoLTESettings{DataPath: DataPathLegacyBAMDMUX},
			want:     VoLTESettings{DataPath: DataPathLegacyBAMDMUX},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "sigmo.db"))
			if err != nil {
				t.Fatalf("Open() error = %v", err)
			}
			t.Cleanup(func() {
				if err := store.Close(); err != nil {
					t.Errorf("Close() error = %v", err)
				}
			})
			settings := newVoLTESettingsStore(store)
			if tt.settings != nil {
				if err := settings.Put(ctx, "modem-1", *tt.settings); err != nil {
					t.Fatalf("Put() error = %v", err)
				}
			}
			got, err := settings.Get(ctx, "modem-1")
			if err != nil {
				t.Fatalf("Get() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("Get() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestVoLTESettingsUseDeviceDataPath(t *testing.T) {
	tests := []struct {
		name       string
		portType   wwanmodem.PortType
		storedPath DataPath
		wantPath   DataPath
	}{
		{
			name:       "MBIM reports MBIM",
			portType:   wwanmodem.PortMBIM,
			storedPath: DataPathLegacyBAMDMUX,
			wantPath:   DataPathMBIM,
		},
		{
			name:       "QMI keeps selected legacy BAM-DMUX",
			portType:   wwanmodem.PortQMI,
			storedPath: DataPathLegacyBAMDMUX,
			wantPath:   DataPathLegacyBAMDMUX,
		},
		{
			name:       "QMI defaults a previous MBIM path to QMAP",
			portType:   wwanmodem.PortQMI,
			storedPath: DataPathMBIM,
			wantPath:   DataPathQMAP,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "sigmo.db"))
			if err != nil {
				t.Fatalf("Open() error = %v", err)
			}
			t.Cleanup(func() {
				if err := store.Close(); err != nil {
					t.Errorf("Close() error = %v", err)
				}
			})
			settings := newVoLTESettingsStore(store)
			if err := settings.Put(ctx, "modem-1", VoLTESettings{
				DataPath: tt.storedPath,
			}); err != nil {
				t.Fatalf("Put() error = %v", err)
			}
			coordinator := &coordinator{access: AccessVoLTE, volteSettings: settings}
			modem := &mmodem.Modem{
				EquipmentIdentifier: "modem-1",
				Ports: []mmodem.ModemPort{{
					Device:   "cdc-wdm0",
					PortType: tt.portType,
				}},
			}

			got, err := coordinator.VoLTESettings(ctx, modem)
			if err != nil {
				t.Fatalf("VoLTESettings() error = %v", err)
			}
			if got.DataPath != tt.wantPath {
				t.Fatalf("DataPath = %q, want %q", got.DataPath, tt.wantPath)
			}
		})
	}
}

func TestRestoreLegacyInternetSurvivesCoordinatorRestart(t *testing.T) {
	tests := []struct {
		name string
	}{
		{name: "persisted suspension"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "sigmo.db"))
			if err != nil {
				t.Fatalf("Open() error = %v", err)
			}
			t.Cleanup(func() {
				if err := store.Close(); err != nil {
					t.Errorf("Close() error = %v", err)
				}
			})
			prefs := pinternet.Preferences{APN: "internet", IPType: "ipv4", DefaultRoute: true}
			settings := newVoLTESettingsStore(store)
			if err := settings.PutSuspendedInternet(ctx, "modem-1", prefs); err != nil {
				t.Fatalf("PutSuspendedInternet() error = %v", err)
			}
			internet := &fakeInternetRestorer{}
			coordinator := &coordinator{internet: internet, volteSettings: newVoLTESettingsStore(store)}

			if err := coordinator.restoreLegacyInternet(ctx, &mmodem.Modem{EquipmentIdentifier: "modem-1"}); err != nil {
				t.Fatalf("restoreLegacyInternet() error = %v", err)
			}
			if !slices.Equal(internet.calls, []string{"connect"}) {
				t.Fatalf("Internet calls = %v, want [connect]", internet.calls)
			}
			if internet.prefs != prefs {
				t.Fatalf("Internet preferences = %+v, want %+v", internet.prefs, prefs)
			}
			if _, ok, err := settings.SuspendedInternet(ctx, "modem-1"); err != nil || ok {
				t.Fatalf("SuspendedInternet() = ok %v, error %v; want deleted", ok, err)
			}
		})
	}
}

func TestDisableVoLTEPersistsStateAfterManagedCleanupError(t *testing.T) {
	errTestMode := errors.New("read test mode")
	tests := []struct {
		name string
	}{
		{name: "legacy BAM-DMUX cleanup"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "sigmo.db"))
			if err != nil {
				t.Fatalf("Open() error = %v", err)
			}
			t.Cleanup(func() {
				if err := store.Close(); err != nil {
					t.Errorf("Close() error = %v", err)
				}
			})
			settings := newVoLTESettingsStore(store)
			current := VoLTESettings{Enabled: true, DataPath: DataPathLegacyBAMDMUX}
			if err := settings.Put(ctx, "modem-1", current); err != nil {
				t.Fatalf("Put() error = %v", err)
			}
			prefs := pinternet.Preferences{APN: "internet", IPType: "ipv4"}
			if err := settings.PutSuspendedInternet(ctx, "modem-1", prefs); err != nil {
				t.Fatalf("PutSuspendedInternet() error = %v", err)
			}
			managedVoLTE := managedVoLTEOps{openDevice: func(*mmodem.Modem) (managedVoLTEDevice, error) {
				return &fakeManagedVoLTEDevice{testModeErr: errTestMode}, nil
			}}
			internet := &fakeInternetRestorer{}
			coordinator := &coordinator{
				access:           AccessVoLTE,
				internet:         internet,
				volteSettings:    settings,
				managedVoLTE:     managedVoLTE,
				sessions:         make(map[string]*sessionState),
				voiceSubscribers: make(map[uint64]VoiceEventFunc),
			}

			err = coordinator.UpdateVoLTESettings(ctx, qmiTestModem("modem-1"), VoLTESettings{
				DataPath: DataPathLegacyBAMDMUX,
			})
			if !errors.Is(err, errTestMode) {
				t.Fatalf("UpdateVoLTESettings() error = %v, want %v", err, errTestMode)
			}
			got, getErr := settings.Get(ctx, "modem-1")
			if getErr != nil {
				t.Fatalf("Get() error = %v", getErr)
			}
			if got.Enabled || got.DataPath != DataPathLegacyBAMDMUX {
				t.Fatalf("Get() = %+v, want disabled legacy BAM-DMUX", got)
			}
			if !slices.Equal(internet.calls, []string{"connect"}) {
				t.Fatalf("Internet calls = %v, want [connect]", internet.calls)
			}
		})
	}
}

func TestStartIfDisabledRestoresNormalMode(t *testing.T) {
	ctx := t.Context()
	store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "sigmo.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	settings := newVoLTESettingsStore(store)
	if err := settings.Put(ctx, "modem-1", VoLTESettings{DataPath: DataPathQualcomm410}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	internet := &fakeInternetRestorer{}
	coordinator := &coordinator{
		access:        AccessVoLTE,
		internet:      internet,
		volteSettings: settings,
	}

	coordinator.startIfEnabled(ctx, qmiTestModem("modem-1"))
	if !slices.Equal(internet.calls, []string{"qualcomm410:false"}) {
		t.Fatalf("Internet calls = %v, want [qualcomm410:false]", internet.calls)
	}
}

type airplaneModePreferencesStub struct {
	enabled bool
	ok      bool
	err     error
}

func (s airplaneModePreferencesStub) SavedAirplaneMode(context.Context, string) (bool, bool, error) {
	return s.enabled, s.ok, s.err
}

func TestStartIfEnabledSkipsVoLTEInAirplaneMode(t *testing.T) {
	tests := []struct {
		name        string
		preferences airplaneModePreferences
		power       wwanmodem.PowerState
	}{
		{
			name:        "saved airplane mode wins before radio restore",
			preferences: airplaneModePreferencesStub{enabled: true, ok: true},
			power:       wwanmodem.PowerStateOn,
		},
		{
			name:  "observed low power blocks VoLTE",
			power: wwanmodem.PowerStateLow,
		},
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
			settings := newVoLTESettingsStore(store)
			if err := settings.Put(ctx, "modem-1", VoLTESettings{Enabled: true, DataPath: DataPathQMAP}); err != nil {
				t.Fatalf("Put() error = %v", err)
			}
			coordinator := &coordinator{
				access:             AccessVoLTE,
				networkPreferences: tt.preferences,
				volteSettings:      settings,
				sessions:           make(map[string]*sessionState),
			}
			modem := qmiTestModem("modem-1")
			modem.Status.Power = tt.power
			modem.SIM = &mmodem.SIM{Identifier: "profile-1"}

			coordinator.startIfEnabled(ctx, modem)
			coordinator.mu.Lock()
			session := coordinator.sessions[modem.EquipmentIdentifier]
			coordinator.mu.Unlock()
			if session != nil {
				coordinator.stop(t.Context(), modem.EquipmentIdentifier)
				t.Fatal("startIfEnabled() started VoLTE in airplane mode")
			}
		})
	}
}

func TestRemovedModemInvalidatesInternetGeneration(t *testing.T) {
	internet := &fakeInternetRestorer{}
	coordinator := &coordinator{access: AccessVoLTE, internet: internet}
	coordinator.processModemEvent(t.Context(), mmodem.ModemEvent{
		Type:       mmodem.ModemEventRemoved,
		Modem:      &mmodem.Modem{EquipmentIdentifier: "modem-1"},
		Path:       "/devices/modem-1",
		Generation: 1,
	})
	if !slices.Equal(internet.calls, []string{"internet:invalidate"}) {
		t.Fatalf("Internet calls = %v, want [internet:invalidate]", internet.calls)
	}
}

func TestSIMProfileChangeRebuildsEnabledVoLTESession(t *testing.T) {
	ctx := t.Context()
	store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "sigmo.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	settings := newVoLTESettingsStore(store)
	if err := settings.Put(ctx, "modem-1", VoLTESettings{Enabled: true, DataPath: DataPathQMAP}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	done := make(chan struct{})
	close(done)
	const path = "/devices/modem-1"
	old := &sessionState{
		id:         1,
		done:       done,
		deviceKey:  path,
		generation: 1,
		profileID:  "old-profile",
	}
	internet := &fakeInternetRestorer{}
	registrationGroups := &RegistrationGroups{}
	oldRegistrationGroup := registrationGroups.Group("modem-1", "old-profile")
	modem := qmiTestModem("modem-1")
	modem.SIM = &mmodem.SIM{Identifier: "new-profile"}
	coordinator := &coordinator{
		access:             AccessVoLTE,
		internet:           internet,
		volteSettings:      settings,
		registrationGroups: registrationGroups,
		registry: modemFinderFunc(func(context.Context, string) (*mmodem.Modem, error) {
			return modem, nil
		}),
		sessions:         map[string]*sessionState{"modem-1": old},
		voiceSubscribers: make(map[uint64]VoiceEventFunc),
	}

	coordinator.processModemEvent(ctx, mmodem.ModemEvent{
		Type:                  mmodem.ModemEventSIMChanged,
		Modem:                 modem,
		Path:                  path,
		Generation:            1,
		PreviousSIMIdentifier: "old-profile",
		SIMIdentifier:         "new-profile",
	})

	coordinator.mu.Lock()
	current := coordinator.sessions[modem.EquipmentIdentifier]
	coordinator.mu.Unlock()
	if current == nil || current == old {
		t.Fatal("SIM profile change did not replace the old VoLTE session")
	}
	if current.profileID != "new-profile" {
		t.Fatalf("new session profile ID = %q, want %q", current.profileID, "new-profile")
	}
	if registrationGroups.Group("modem-1", "old-profile") == oldRegistrationGroup {
		t.Fatal("SIM profile change preserved the old registration group")
	}
	coordinator.stop(t.Context(), modem.EquipmentIdentifier)
	if !slices.Contains(internet.calls, "internet:invalidate") {
		t.Fatalf("Internet calls = %v, want generation invalidation", internet.calls)
	}
}

func TestStaleSIMProfileChangeKeepsReplacementSession(t *testing.T) {
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
	settings := newWiFiCallingSettingsStore(store)
	if err := settings.Put(ctx, "old-profile", WiFiCallingSettings{
		Enabled:  true,
		Underlay: UnderlaySettings{Mode: UnderlayModeSystem},
	}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	const path = "/devices/modem-1"
	replacement := &sessionState{
		id:         2,
		deviceKey:  path,
		generation: 2,
		profileID:  "new-profile",
	}
	registrationGroups := &RegistrationGroups{}
	replacementRegistrationGroup := registrationGroups.Group("modem-1", "new-profile")
	coordinator := &coordinator{
		access:              AccessWiFiCalling,
		wifiCallingSettings: settings,
		registrationGroups:  registrationGroups,
		sessions:            map[string]*sessionState{"modem-1": replacement},
		voiceSubscribers:    make(map[uint64]VoiceEventFunc),
	}
	stale := &mmodem.Modem{
		EquipmentIdentifier: "modem-1",
		SIM:                 &mmodem.SIM{Identifier: "old-profile"},
	}

	coordinator.processModemEvent(ctx, mmodem.ModemEvent{
		Type:       mmodem.ModemEventSIMChanged,
		Modem:      stale,
		Path:       path,
		Generation: 1,
	})

	coordinator.mu.Lock()
	current := coordinator.sessions[stale.EquipmentIdentifier]
	coordinator.mu.Unlock()
	if current != replacement {
		coordinator.stop(t.Context(), stale.EquipmentIdentifier)
		t.Fatalf("session = %+v, want replacement generation", current)
	}
	if registrationGroups.Group("modem-1", "new-profile") != replacementRegistrationGroup {
		t.Fatal("stale SIM profile change replaced the current registration group")
	}
}

type modemFinderFunc func(context.Context, string) (*mmodem.Modem, error)

func (f modemFinderFunc) Find(ctx context.Context, id string) (*mmodem.Modem, error) {
	return f(ctx, id)
}

func TestStaleSIMProfileChangeAfterRemovalWaitsForAdded(t *testing.T) {
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
	settings := newWiFiCallingSettingsStore(store)
	for _, profileID := range []string{"old-profile", "new-profile"} {
		if err := settings.Put(ctx, profileID, WiFiCallingSettings{
			Enabled:  true,
			Underlay: UnderlaySettings{Mode: UnderlayModeSystem},
		}); err != nil {
			t.Fatalf("Put(%q) error = %v", profileID, err)
		}
	}

	const (
		modemID = "modem-1"
		path    = "/devices/modem-1"
	)
	stale := &mmodem.Modem{
		EquipmentIdentifier: modemID,
		SIM:                 &mmodem.SIM{Identifier: "old-profile"},
	}
	replacement := &mmodem.Modem{
		EquipmentIdentifier: modemID,
		SIM:                 &mmodem.SIM{Identifier: "new-profile"},
	}
	oldDone := make(chan struct{})
	close(oldDone)
	var current *mmodem.Modem
	coordinator := &coordinator{
		access:              AccessWiFiCalling,
		wifiCallingSettings: settings,
		registry: modemFinderFunc(func(_ context.Context, id string) (*mmodem.Modem, error) {
			if id != modemID {
				t.Fatalf("Find() id = %q, want %q", id, modemID)
			}
			if current == nil {
				return nil, mmodem.ErrNotFound
			}
			return current, nil
		}),
		sessions: map[string]*sessionState{
			modemID: {
				id:         1,
				modem:      stale,
				done:       oldDone,
				deviceKey:  path,
				generation: 1,
				profileID:  "old-profile",
			},
		},
		voiceSubscribers: make(map[uint64]VoiceEventFunc),
	}

	coordinator.processModemEvent(ctx, mmodem.ModemEvent{
		Type:       mmodem.ModemEventRemoved,
		Modem:      stale,
		Path:       path,
		Generation: 1,
	})
	coordinator.processModemEvent(ctx, mmodem.ModemEvent{
		Type:       mmodem.ModemEventSIMChanged,
		Modem:      stale,
		Path:       path,
		Generation: 1,
	})

	coordinator.mu.Lock()
	staleSession := coordinator.sessions[modemID]
	coordinator.mu.Unlock()
	if staleSession != nil {
		coordinator.stop(t.Context(), modemID)
		t.Fatalf("session modem = %p, want no stale session before Added", staleSession.modem)
	}

	current = replacement
	coordinator.processModemEvent(ctx, mmodem.ModemEvent{
		Type:       mmodem.ModemEventAdded,
		Modem:      replacement,
		Path:       path,
		Generation: 2,
	})

	coordinator.mu.Lock()
	session := coordinator.sessions[modemID]
	coordinator.mu.Unlock()
	if session == nil {
		t.Fatal("Added did not start the replacement session")
	}
	if session.modem != replacement {
		coordinator.stop(t.Context(), modemID)
		t.Fatalf("session modem = %p, want replacement %p", session.modem, replacement)
	}
	coordinator.stop(t.Context(), modemID)
}

type keyedBlockingInvalidationInternet struct {
	fakeInternetRestorer

	mu                sync.Mutex
	invalidations     map[string]int
	firstEntered      chan struct{}
	secondSameEntered chan struct{}
	otherEntered      chan struct{}
	releaseFirst      chan struct{}
}

func (r *keyedBlockingInvalidationInternet) InvalidateModem(_ context.Context, modem *mmodem.Modem) error {
	r.mu.Lock()
	r.invalidations[modem.EquipmentIdentifier]++
	call := r.invalidations[modem.EquipmentIdentifier]
	r.mu.Unlock()

	switch {
	case modem.EquipmentIdentifier == "modem-1" && call == 1:
		close(r.firstEntered)
		<-r.releaseFirst
	case modem.EquipmentIdentifier == "modem-1" && call == 2:
		close(r.secondSameEntered)
	case modem.EquipmentIdentifier == "modem-2":
		close(r.otherEntered)
	}
	return nil
}

func TestProcessModemEventsSerializesPerModem(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		internet := &keyedBlockingInvalidationInternet{
			invalidations:     make(map[string]int),
			firstEntered:      make(chan struct{}),
			secondSameEntered: make(chan struct{}),
			otherEntered:      make(chan struct{}),
			releaseFirst:      make(chan struct{}),
		}
		releaseFirst := sync.OnceFunc(func() { close(internet.releaseFirst) })
		defer releaseFirst()

		coordinator := &coordinator{
			access:              AccessWiFiCalling,
			internet:            internet,
			wifiCallingSettings: newWiFiCallingSettingsStore(nil),
			sessions:            make(map[string]*sessionState),
		}
		modem := &mmodem.Modem{EquipmentIdentifier: "modem-1"}
		firstDone := make(chan struct{})
		go func() {
			defer close(firstDone)
			coordinator.processModemEvent(t.Context(), mmodem.ModemEvent{
				Type:       mmodem.ModemEventSIMChanged,
				Modem:      modem,
				Path:       "/devices/modem-1",
				Generation: 1,
			})
		}()

		select {
		case <-internet.firstEntered:
		case <-time.After(time.Second):
			t.Fatal("first modem event did not enter invalidation")
		}

		sameDone := make(chan struct{})
		go func() {
			defer close(sameDone)
			coordinator.processModemEvent(t.Context(), mmodem.ModemEvent{
				Type:       mmodem.ModemEventRemoved,
				Modem:      modem,
				Path:       "/devices/modem-1",
				Generation: 1,
			})
		}()

		otherDone := make(chan struct{})
		go func() {
			defer close(otherDone)
			coordinator.processModemEvent(t.Context(), mmodem.ModemEvent{
				Type:       mmodem.ModemEventSIMChanged,
				Modem:      &mmodem.Modem{EquipmentIdentifier: "modem-2"},
				Path:       "/devices/modem-2",
				Generation: 1,
			})
		}()

		synctest.Wait()
		select {
		case <-internet.otherEntered:
		default:
			t.Fatal("unrelated modem event did not enter invalidation")
		}
		select {
		case <-otherDone:
		default:
			t.Fatal("unrelated modem event did not finish")
		}
		select {
		case <-internet.secondSameEntered:
			t.Fatal("events for the same modem overlapped")
		default:
		}
		select {
		case <-sameDone:
			t.Fatal("second event for the same modem finished before the first")
		default:
		}

		releaseFirst()
		synctest.Wait()
		select {
		case <-firstDone:
		default:
			t.Fatal("first modem event did not finish")
		}
		select {
		case <-internet.secondSameEntered:
		default:
			t.Fatal("second event for the same modem did not run after the first")
		}
		select {
		case <-sameDone:
		default:
			t.Fatal("second event for the same modem did not finish")
		}
	})
}

func TestDisableVoLTERestoresNormalInternetMode(t *testing.T) {
	ctx := t.Context()
	store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "sigmo.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	settings := newVoLTESettingsStore(store)
	if err := settings.Put(ctx, "modem-1", VoLTESettings{Enabled: true, DataPath: DataPathQualcomm410}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	device := &fakeManagedVoLTEDevice{}
	managedVoLTE := managedVoLTEOps{openDevice: func(*mmodem.Modem) (managedVoLTEDevice, error) {
		return device, nil
	}}

	internet := &fakeInternetRestorer{}
	coordinator := &coordinator{
		access:           AccessVoLTE,
		internet:         internet,
		volteSettings:    settings,
		managedVoLTE:     managedVoLTE,
		sessions:         make(map[string]*sessionState),
		voiceSubscribers: make(map[uint64]VoiceEventFunc),
	}
	if err := coordinator.UpdateVoLTESettings(ctx, qmiTestModem("modem-1"), VoLTESettings{DataPath: DataPathQualcomm410}); err != nil {
		t.Fatalf("UpdateVoLTESettings() error = %v", err)
	}
	if !slices.Equal(internet.calls, []string{"qualcomm410:false"}) {
		t.Fatalf("Internet calls = %v, want [qualcomm410:false]", internet.calls)
	}
	if !slices.Equal(device.calls, []string{"test-mode"}) {
		t.Fatalf("device calls = %v, want [test-mode]", device.calls)
	}
	got, err := settings.Get(ctx, "modem-1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Enabled || got.DataPath != DataPathQualcomm410 {
		t.Fatalf("Get() = %+v, want disabled Qualcomm 410", got)
	}
}

func TestDisableVoLTESwitchesQualcomm410InternetModeWithDataPath(t *testing.T) {
	tests := []struct {
		name        string
		currentPath DataPath
		nextPath    DataPath
		wantCalls   []string
	}{
		{
			name:        "leave Qualcomm 410",
			currentPath: DataPathQualcomm410,
			nextPath:    DataPathQMAP,
			wantCalls:   []string{"qualcomm410:false"},
		},
		{
			name:        "change preference without enabling Qualcomm 410",
			currentPath: DataPathQMAP,
			nextPath:    DataPathQualcomm410,
			wantCalls:   []string{"qmap:false"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "sigmo.db"))
			if err != nil {
				t.Fatalf("Open() error = %v", err)
			}
			t.Cleanup(func() {
				if err := store.Close(); err != nil {
					t.Errorf("Close() error = %v", err)
				}
			})
			settings := newVoLTESettingsStore(store)
			if err := settings.Put(ctx, "modem-1", VoLTESettings{Enabled: true, DataPath: tt.currentPath}); err != nil {
				t.Fatalf("Put() error = %v", err)
			}
			managedVoLTE := managedVoLTEOps{openDevice: func(*mmodem.Modem) (managedVoLTEDevice, error) {
				return &fakeManagedVoLTEDevice{}, nil
			}}

			internet := &fakeInternetRestorer{}
			coordinator := &coordinator{
				access:           AccessVoLTE,
				internet:         internet,
				volteSettings:    settings,
				managedVoLTE:     managedVoLTE,
				sessions:         make(map[string]*sessionState),
				voiceSubscribers: make(map[uint64]VoiceEventFunc),
			}
			if err := coordinator.UpdateVoLTESettings(ctx, qmiTestModem("modem-1"), VoLTESettings{DataPath: tt.nextPath}); err != nil {
				t.Fatalf("UpdateVoLTESettings() error = %v", err)
			}
			if !slices.Equal(internet.calls, tt.wantCalls) {
				t.Fatalf("Internet calls = %v, want %v", internet.calls, tt.wantCalls)
			}
		})
	}
}

func TestRestoreQualcomm410DataPathReleasesInternetMode(t *testing.T) {
	internet := &fakeInternetRestorer{}
	coordinator := &coordinator{internet: internet}
	if err := coordinator.restoreVoLTEDataPath(t.Context(), qmiTestModem("modem-1"), DataPathQualcomm410); err != nil {
		t.Fatalf("restoreVoLTEDataPath() error = %v", err)
	}
	if !slices.Equal(internet.calls, []string{"qualcomm410:false"}) {
		t.Fatalf("Internet calls = %v, want [qualcomm410:false]", internet.calls)
	}
}

type failingVoLTESettingsStore struct {
	*voLTESettingsStore
	putErr error
}

func (s *failingVoLTESettingsStore) Put(context.Context, string, VoLTESettings) error {
	return s.putErr
}

func TestDisabledVoLTEDataPathRollsBackAfterPersistenceFailure(t *testing.T) {
	persistErr := errors.New("persist settings")
	tests := []struct {
		name        string
		currentPath DataPath
		nextPath    DataPath
		wantCalls   []string
	}{
		{
			name:        "leave Qualcomm 410",
			currentPath: DataPathQualcomm410,
			nextPath:    DataPathQMAP,
			wantCalls:   []string{"qualcomm410:false"},
		},
		{
			name:        "change preference without enabling Qualcomm 410",
			currentPath: DataPathQMAP,
			nextPath:    DataPathQualcomm410,
			wantCalls:   []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "sigmo.db"))
			if err != nil {
				t.Fatalf("Open() error = %v", err)
			}
			t.Cleanup(func() {
				if err := store.Close(); err != nil {
					t.Errorf("Close() error = %v", err)
				}
			})
			settings := newVoLTESettingsStore(store)
			current := VoLTESettings{DataPath: tt.currentPath}
			if err := settings.Put(ctx, "modem-1", current); err != nil {
				t.Fatalf("Put() error = %v", err)
			}
			internet := &fakeInternetRestorer{}
			coordinator := &coordinator{
				access:        AccessVoLTE,
				internet:      internet,
				volteSettings: &failingVoLTESettingsStore{voLTESettingsStore: settings, putErr: persistErr},
			}

			err = coordinator.UpdateVoLTESettings(ctx, qmiTestModem("modem-1"), VoLTESettings{DataPath: tt.nextPath})
			if !errors.Is(err, persistErr) {
				t.Fatalf("UpdateVoLTESettings() error = %v, want %v", err, persistErr)
			}
			if !slices.Equal(internet.calls, tt.wantCalls) {
				t.Fatalf("Internet calls = %v, want %v", internet.calls, tt.wantCalls)
			}
			got, getErr := settings.Get(ctx, "modem-1")
			if getErr != nil {
				t.Fatalf("Get() error = %v", getErr)
			}
			if got != current {
				t.Fatalf("Get() = %+v, want %+v", got, current)
			}
		})
	}
}

func TestVoLTEDataPathSwitchRollsBackAfterNewPathFailure(t *testing.T) {
	errQMAP := errors.New("qmap rejected")
	tests := []struct {
		name string
	}{
		{name: "legacy BAM-DMUX to QMAP"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "sigmo.db"))
			if err != nil {
				t.Fatalf("Open() error = %v", err)
			}
			t.Cleanup(func() {
				if err := store.Close(); err != nil {
					t.Errorf("Close() error = %v", err)
				}
			})
			settings := newVoLTESettingsStore(store)
			current := VoLTESettings{Enabled: true, DataPath: DataPathLegacyBAMDMUX}
			if err := settings.Put(ctx, "modem-1", current); err != nil {
				t.Fatalf("Put() error = %v", err)
			}
			if err := settings.PutSuspendedInternet(ctx, "modem-1", pinternet.Preferences{APN: "internet"}); err != nil {
				t.Fatalf("PutSuspendedInternet() error = %v", err)
			}
			internet := &fakeInternetRestorer{qmapErrors: []error{errQMAP, nil, nil}}
			coordinator := &coordinator{
				access:           AccessVoLTE,
				internet:         internet,
				volteSettings:    settings,
				sessions:         make(map[string]*sessionState),
				voiceSubscribers: make(map[uint64]VoiceEventFunc),
			}
			modem := &mmodem.Modem{
				EquipmentIdentifier: "modem-1",
				SIM:                 &mmodem.SIM{Identifier: "profile-1"},
				Ports: []mmodem.ModemPort{{
					Device:   "cdc-wdm0",
					PortType: wwanmodem.PortQMI,
				}},
			}

			err = coordinator.UpdateVoLTESettings(ctx, modem, VoLTESettings{Enabled: true, DataPath: DataPathQMAP})
			if !errors.Is(err, errQMAP) {
				t.Fatalf("UpdateVoLTESettings() error = %v, want %v", err, errQMAP)
			}
			got, getErr := settings.Get(ctx, "modem-1")
			if getErr != nil {
				t.Fatalf("Get() error = %v", getErr)
			}
			if got != current {
				t.Fatalf("Get() = %+v, want %+v", got, current)
			}
			coordinator.mu.Lock()
			session := coordinator.sessions[modem.EquipmentIdentifier]
			coordinator.mu.Unlock()
			if session == nil {
				t.Fatal("old VoLTE session was not restarted")
			}
			coordinator.stop(t.Context(), modem.EquipmentIdentifier)
			wantCalls := []string{"connect", "qmap:true", "qmap:false", "qmap:false"}
			if !slices.Equal(internet.calls, wantCalls) {
				t.Fatalf("Internet calls = %v, want %v", internet.calls, wantCalls)
			}
		})
	}
}

func qmiTestModem(id string) *mmodem.Modem {
	return &mmodem.Modem{
		EquipmentIdentifier: id,
		Ports: []mmodem.ModemPort{{
			Device:   "cdc-wdm0",
			PortType: wwanmodem.PortQMI,
		}},
	}
}
