package modem

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/godbus/dbus/v5"

	"github.com/damonto/sigmo/internal/pkg/storage"
)

func TestNewNetworkPreferencesRequiresStorage(t *testing.T) {
	t.Parallel()

	_, err := NewNetworkPreferences(nil)
	if err == nil {
		t.Fatal("NewNetworkPreferences() error = nil, want storage error")
	}
	if !errors.Is(err, errNetworkPreferencesStorageRequired) {
		t.Fatalf("NewNetworkPreferences() error = %v, want storage error", err)
	}
}

func TestNetworkPreferencesStoreForModem(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		modemID   string
		save      func(context.Context, *NetworkPreferences) error
		assertion func(t *testing.T, got savedNetworkPreferences, ok bool)
	}{
		{
			name:    "save mode",
			modemID: "modem-1",
			save: func(ctx context.Context, prefs *NetworkPreferences) error {
				return prefs.SaveMode(ctx, "modem-1", ModemModePair{Allowed: ModemMode4G, Preferred: ModemModeNone})
			},
			assertion: func(t *testing.T, got savedNetworkPreferences, ok bool) {
				t.Helper()
				if !ok {
					t.Fatal("loadForModem() ok = false, want true")
				}
				if got.Mode == nil {
					t.Fatal("saved mode = nil, want value")
				}
				want := networkPreferenceMode{Allowed: ModemMode4G, Preferred: ModemModeNone}
				if *got.Mode != want {
					t.Fatalf("saved mode = %#v, want %#v", got.Mode, want)
				}
			},
		},
		{
			name:    "save bands",
			modemID: "modem-2",
			save: func(ctx context.Context, prefs *NetworkPreferences) error {
				return prefs.SaveBands(ctx, "modem-2", []ModemBand{71, 378})
			},
			assertion: func(t *testing.T, got savedNetworkPreferences, ok bool) {
				t.Helper()
				if !ok {
					t.Fatal("loadForModem() ok = false, want true")
				}
				want := []ModemBand{71, 378}
				if !slices.Equal(got.Bands, want) {
					t.Fatalf("saved bands = %#v, want %#v", got.Bands, want)
				}
			},
		},
		{
			name:    "save airplane mode",
			modemID: "modem-airplane",
			save: func(ctx context.Context, prefs *NetworkPreferences) error {
				return prefs.SaveAirplaneMode(ctx, "modem-airplane", true)
			},
			assertion: func(t *testing.T, got savedNetworkPreferences, ok bool) {
				t.Helper()
				if !ok {
					t.Fatal("loadForModem() ok = false, want true")
				}
				if got.AirplaneMode == nil {
					t.Fatal("saved airplane mode = nil, want value")
				}
				if !*got.AirplaneMode {
					t.Fatal("saved airplane mode = false, want true")
				}
			},
		},
		{
			name:    "overwrite modem keeps other field",
			modemID: "modem-3",
			save: func(ctx context.Context, prefs *NetworkPreferences) error {
				if err := prefs.SaveMode(ctx, "modem-3", ModemModePair{Allowed: ModemMode4G, Preferred: ModemModeNone}); err != nil {
					return err
				}
				return prefs.SaveBands(ctx, "modem-3", []ModemBand{ModemBandAny})
			},
			assertion: func(t *testing.T, got savedNetworkPreferences, ok bool) {
				t.Helper()
				if !ok {
					t.Fatal("loadForModem() ok = false, want true")
				}
				if got.Mode == nil {
					t.Fatal("saved mode = nil, want value")
				}
				if !slices.Equal(got.Bands, []ModemBand{ModemBandAny}) {
					t.Fatalf("saved bands = %#v, want any", got.Bands)
				}
			},
		},
		{
			name:    "missing modem is empty",
			modemID: "missing",
			save:    func(context.Context, *NetworkPreferences) error { return nil },
			assertion: func(t *testing.T, _ savedNetworkPreferences, ok bool) {
				t.Helper()
				if ok {
					t.Fatal("loadForModem() ok = true, want false")
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			db := openNetworkPreferencesTestStore(t)
			prefs, err := NewNetworkPreferences(db)
			if err != nil {
				t.Fatalf("NewNetworkPreferences() error = %v", err)
			}
			ctx := context.Background()
			if err := tt.save(ctx, prefs); err != nil {
				t.Fatalf("save() error = %v", err)
			}
			got, ok, err := prefs.loadForModem(ctx, tt.modemID)
			if err != nil {
				t.Fatalf("loadForModem() error = %v", err)
			}
			tt.assertion(t, got, ok)
		})
	}
}

func TestRestoreAirplaneModePreference(t *testing.T) {
	tests := []struct {
		name            string
		saveAirplane    bool
		deviceAirplane  bool
		wantAirplane    bool
		wantSetCalls    int
		wantModeRestore bool
		wantBandRestore bool
	}{
		{
			name:         "enable skips modes and bands",
			saveAirplane: true,
			wantAirplane: true,
			wantSetCalls: 1,
		},
		{
			name:            "disable restores modes and bands",
			deviceAirplane:  true,
			wantSetCalls:    1,
			wantModeRestore: true,
			wantBandRestore: true,
		},
		{
			name:            "already disabled still restores modes and bands",
			wantModeRestore: true,
			wantBandRestore: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := openNetworkPreferencesTestStore(t)
			prefs, err := NewNetworkPreferences(db)
			if err != nil {
				t.Fatalf("NewNetworkPreferences() error = %v", err)
			}
			ctx := context.Background()
			if err := prefs.SaveAirplaneMode(ctx, "modem-1", tt.saveAirplane); err != nil {
				t.Fatalf("SaveAirplaneMode() error = %v", err)
			}
			if err := prefs.SaveMode(ctx, "modem-1", ModemModePair{Allowed: ModemMode4G, Preferred: ModemModeNone}); err != nil {
				t.Fatalf("SaveMode() error = %v", err)
			}
			if err := prefs.SaveBands(ctx, "modem-1", []ModemBand{71}); err != nil {
				t.Fatalf("SaveBands() error = %v", err)
			}

			device := &fakeDeviceControl{airplane: tt.deviceAirplane}
			prefs.openDevice = fakeDeviceOpener(t, device, nil)
			object := &fakeBusObject{
				path: "/org/freedesktop/ModemManager1/Modem/1",
				properties: map[string]dbus.Variant{
					ModemInterface + ".SupportedModes": dbus.MakeVariant([]dbusModePair{{Allowed: uint32(ModemMode4G), Preferred: uint32(ModemModeNone)}}),
					ModemInterface + ".CurrentModes":   dbus.MakeVariant(dbusModePair{Allowed: uint32(ModemMode5G), Preferred: uint32(ModemModeNone)}),
					ModemInterface + ".SupportedBands": dbus.MakeVariant([]uint32{uint32(ModemBandAny), 71}),
					ModemInterface + ".CurrentBands":   dbus.MakeVariant([]uint32{uint32(ModemBandAny)}),
				},
			}
			modem := &Modem{
				dbusObject:          object,
				objectPath:          object.path,
				EquipmentIdentifier: "modem-1",
				PrimaryPort:         "/dev/cdc-wdm0",
				Ports: []ModemPort{
					{PortType: ModemPortTypeQmi, Device: "/dev/cdc-wdm0"},
				},
			}

			retry, err := prefs.restoreOnce(ctx, modem)
			if retry {
				t.Fatal("restoreOnce() retry = true, want false")
			}
			if err != nil {
				t.Fatalf("restoreOnce() error = %v", err)
			}
			if got := countCalls(device.calls, "set-airplane-mode"); got != tt.wantSetCalls {
				t.Fatalf("set airplane mode calls = %d, want %d", got, tt.wantSetCalls)
			}
			if tt.wantSetCalls > 0 && device.airplane != tt.wantAirplane {
				t.Fatalf("airplane mode = %v, want %v", device.airplane, tt.wantAirplane)
			}
			assertCallPresence(t, object.calls, ModemInterface+".SetCurrentModes", tt.wantModeRestore)
			assertCallPresence(t, object.calls, ModemInterface+".SetCurrentBands", tt.wantBandRestore)
		})
	}
}

func TestRestoreModePreference(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		supported   []dbusModePair
		current     dbusModePair
		want        ModemModePair
		wantRetry   bool
		wantErr     string
		wantSetCall bool
	}{
		{
			name:      "skip current",
			supported: []dbusModePair{{Allowed: uint32(ModemMode4G), Preferred: uint32(ModemModeNone)}},
			current:   dbusModePair{Allowed: uint32(ModemMode4G), Preferred: uint32(ModemModeNone)},
			want:      ModemModePair{Allowed: ModemMode4G, Preferred: ModemModeNone},
		},
		{
			name:        "set supported",
			supported:   []dbusModePair{{Allowed: uint32(ModemMode4G), Preferred: uint32(ModemModeNone)}},
			current:     dbusModePair{Allowed: uint32(ModemMode5G), Preferred: uint32(ModemModeNone)},
			want:        ModemModePair{Allowed: ModemMode4G, Preferred: ModemModeNone},
			wantSetCall: true,
		},
		{
			name:      "skip unsupported",
			supported: []dbusModePair{{Allowed: uint32(ModemMode4G), Preferred: uint32(ModemModeNone)}},
			current:   dbusModePair{Allowed: uint32(ModemMode4G), Preferred: uint32(ModemModeNone)},
			want:      ModemModePair{Allowed: ModemMode5G, Preferred: ModemModeNone},
			wantErr:   "unsupported",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			object := &fakeBusObject{
				path: "/org/freedesktop/ModemManager1/Modem/1",
				properties: map[string]dbus.Variant{
					ModemInterface + ".SupportedModes": dbus.MakeVariant(tt.supported),
					ModemInterface + ".CurrentModes":   dbus.MakeVariant(tt.current),
				},
			}
			modem := &Modem{dbusObject: object, objectPath: object.path, EquipmentIdentifier: "modem-1"}

			retry, err := restoreModePreference(context.Background(), modem, tt.want)
			assertRestoreResult(t, retry, err, tt.wantRetry, tt.wantErr)
			assertCallPresence(t, object.calls, ModemInterface+".SetCurrentModes", tt.wantSetCall)
		})
	}
}

func TestRestoreBandPreference(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		supported   []uint32
		current     []uint32
		want        []ModemBand
		wantRetry   bool
		wantErr     string
		wantSetCall bool
	}{
		{
			name:      "skip current",
			supported: []uint32{uint32(ModemBandAny), 71, 378},
			current:   []uint32{71, 378},
			want:      []ModemBand{71, 378},
		},
		{
			name:      "skip current with different order",
			supported: []uint32{uint32(ModemBandAny), 71, 378},
			current:   []uint32{71, 378},
			want:      []ModemBand{378, 71},
		},
		{
			name:        "set supported",
			supported:   []uint32{uint32(ModemBandAny), 71, 378},
			current:     []uint32{71},
			want:        []ModemBand{71, 378},
			wantSetCall: true,
		},
		{
			name:      "skip empty",
			supported: []uint32{uint32(ModemBandAny), 71},
			current:   []uint32{71},
			wantErr:   "empty",
		},
		{
			name:      "skip any with other bands",
			supported: []uint32{uint32(ModemBandAny), 71},
			current:   []uint32{71},
			want:      []ModemBand{ModemBandAny, 71},
			wantErr:   "any",
		},
		{
			name:      "skip duplicate",
			supported: []uint32{uint32(ModemBandAny), 71},
			current:   []uint32{71},
			want:      []ModemBand{71, 71},
			wantErr:   "duplicates",
		},
		{
			name:      "skip unsupported",
			supported: []uint32{uint32(ModemBandAny), 71},
			current:   []uint32{71},
			want:      []ModemBand{378},
			wantErr:   "unsupported",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			object := &fakeBusObject{
				path: "/org/freedesktop/ModemManager1/Modem/1",
				properties: map[string]dbus.Variant{
					ModemInterface + ".SupportedBands": dbus.MakeVariant(tt.supported),
					ModemInterface + ".CurrentBands":   dbus.MakeVariant(tt.current),
				},
			}
			modem := &Modem{dbusObject: object, objectPath: object.path, EquipmentIdentifier: "modem-1"}

			retry, err := restoreBandPreference(context.Background(), modem, tt.want)
			assertRestoreResult(t, retry, err, tt.wantRetry, tt.wantErr)
			assertCallPresence(t, object.calls, ModemInterface+".SetCurrentBands", tt.wantSetCall)
		})
	}
}

func openNetworkPreferencesTestStore(t *testing.T) *storage.Store {
	t.Helper()

	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "sigmo.db"))
	if err != nil {
		t.Fatalf("storage.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})
	return db
}

func assertRestoreResult(t *testing.T, retry bool, err error, wantRetry bool, wantErr string) {
	t.Helper()

	if retry != wantRetry {
		t.Fatalf("retry = %v, want %v", retry, wantRetry)
	}
	if wantErr == "" {
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		return
	}
	if err == nil {
		t.Fatalf("error = nil, want it to contain %q", wantErr)
	}
	if !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("error = %v, want it to contain %q", err, wantErr)
	}
}

func assertCallPresence(t *testing.T, calls []string, method string, want bool) {
	t.Helper()

	if got := slices.Contains(calls, method); got != want {
		t.Fatalf("call %q present = %v, want %v; calls = %#v", method, got, want, calls)
	}
}
