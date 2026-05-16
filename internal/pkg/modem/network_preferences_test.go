package modem

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestNetworkPreferencesPathFromEnv(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		env     map[string]string
		home    string
		homeErr error
		want    string
		wantErr string
	}{
		{
			name: "xdg state home",
			env:  map[string]string{"XDG_STATE_HOME": "/var/lib/sigmo-state"},
			home: "/home/sigmo",
			want: "/var/lib/sigmo-state/sigmo/network-preferences.json",
		},
		{
			name: "home default",
			env:  map[string]string{},
			home: "/home/sigmo",
			want: "/home/sigmo/.local/state/sigmo/network-preferences.json",
		},
		{
			name:    "relative xdg state home",
			env:     map[string]string{"XDG_STATE_HOME": "state"},
			home:    "/home/sigmo",
			wantErr: "XDG_STATE_HOME",
		},
		{
			name:    "home error",
			env:     map[string]string{},
			homeErr: errors.New("home missing"),
			wantErr: "resolve user home dir",
		},
		{
			name:    "empty home",
			env:     map[string]string{},
			wantErr: "user home dir is empty",
		},
		{
			name:    "relative home",
			env:     map[string]string{},
			home:    "home/sigmo",
			wantErr: "user home dir",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			lookupEnv := func(key string) (string, bool) {
				value, ok := tt.env[key]
				return value, ok
			}
			userHomeDir := func() (string, error) {
				return tt.home, tt.homeErr
			}

			got, err := networkPreferencesPathFromEnv(lookupEnv, userHomeDir)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatal("networkPreferencesPathFromEnv() error = nil, want error")
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("networkPreferencesPathFromEnv() error = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("networkPreferencesPathFromEnv() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("networkPreferencesPathFromEnv() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNetworkPreferencesStateForModem(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		run  func(t *testing.T, path string)
	}{
		{
			name: "save mode",
			run: func(t *testing.T, path string) {
				t.Helper()

				prefs := NewNetworkPreferencesWithPath(path)
				want := ModemModePair{Allowed: ModemMode4G, Preferred: ModemModeNone}
				if err := prefs.SaveMode("modem-1", want); err != nil {
					t.Fatalf("SaveMode() error = %v", err)
				}

				got, ok, err := prefs.loadForModem("modem-1")
				if err != nil {
					t.Fatalf("loadForModem() error = %v", err)
				}
				if !ok {
					t.Fatal("loadForModem() ok = false, want true")
				}
				if got.Mode == nil {
					t.Fatal("saved mode = nil, want value")
				}
				if got.Mode.Allowed != want.Allowed || got.Mode.Preferred != want.Preferred {
					t.Fatalf("saved mode = %#v, want %#v", got.Mode, want)
				}
				assertNetworkPreferencePermissions(t, path)
			},
		},
		{
			name: "save bands",
			run: func(t *testing.T, path string) {
				t.Helper()

				prefs := NewNetworkPreferencesWithPath(path)
				want := []ModemBand{71, 378}
				if err := prefs.SaveBands("modem-1", want); err != nil {
					t.Fatalf("SaveBands() error = %v", err)
				}

				got, ok, err := prefs.loadForModem("modem-1")
				if err != nil {
					t.Fatalf("loadForModem() error = %v", err)
				}
				if !ok {
					t.Fatal("loadForModem() ok = false, want true")
				}
				if !slices.Equal(got.Bands, want) {
					t.Fatalf("saved bands = %#v, want %#v", got.Bands, want)
				}
				assertNetworkPreferencePermissions(t, path)
			},
		},
		{
			name: "overwrite modem keeps other field",
			run: func(t *testing.T, path string) {
				t.Helper()

				prefs := NewNetworkPreferencesWithPath(path)
				if err := prefs.SaveMode("modem-1", ModemModePair{Allowed: ModemMode4G, Preferred: ModemModeNone}); err != nil {
					t.Fatalf("SaveMode() error = %v", err)
				}
				wantBands := []ModemBand{ModemBandAny}
				if err := prefs.SaveBands("modem-1", wantBands); err != nil {
					t.Fatalf("SaveBands() error = %v", err)
				}

				got, ok, err := prefs.loadForModem("modem-1")
				if err != nil {
					t.Fatalf("loadForModem() error = %v", err)
				}
				if !ok {
					t.Fatal("loadForModem() ok = false, want true")
				}
				if got.Mode == nil {
					t.Fatal("saved mode = nil, want value")
				}
				if !slices.Equal(got.Bands, wantBands) {
					t.Fatalf("saved bands = %#v, want %#v", got.Bands, wantBands)
				}
			},
		},
		{
			name: "missing file is empty",
			run: func(t *testing.T, path string) {
				t.Helper()

				prefs := NewNetworkPreferencesWithPath(path)
				_, ok, err := prefs.loadForModem("modem-1")
				if err != nil {
					t.Fatalf("loadForModem() error = %v", err)
				}
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
			tt.run(t, filepath.Join(t.TempDir(), "network-preferences.json"))
		})
	}
}

func TestNetworkPreferencesStateReadErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "corrupt json",
			content: "{",
			want:    "decode network preferences state",
		},
		{
			name:    "unsupported version",
			content: `{"version":99,"modems":{}}`,
			want:    "unsupported",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "network-preferences.json")
			if err := os.WriteFile(path, []byte(tt.content), 0o600); err != nil {
				t.Fatalf("os.WriteFile() error = %v", err)
			}
			_, err := readNetworkPreferencesState(path)
			if err == nil {
				t.Fatal("readNetworkPreferencesState() error = nil, want error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("readNetworkPreferencesState() error = %v, want it to contain %q", err, tt.want)
			}
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

			retry, err := restoreModePreference(modem, tt.want)
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

			retry, err := restoreBandPreference(modem, tt.want)
			assertRestoreResult(t, retry, err, tt.wantRetry, tt.wantErr)
			assertCallPresence(t, object.calls, ModemInterface+".SetCurrentBands", tt.wantSetCall)
		})
	}
}

func assertNetworkPreferencePermissions(t *testing.T, path string) {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("os.Stat() error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("state file mode = %#o, want %#o", got, os.FileMode(0o600))
	}

	info, err = os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("os.Stat(dir) error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("state directory mode = %#o, want %#o", got, os.FileMode(0o700))
	}
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
