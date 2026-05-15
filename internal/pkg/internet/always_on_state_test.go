package internet

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

func TestAlwaysOnStatePathFromEnv(t *testing.T) {
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
			want: "/var/lib/sigmo-state/sigmo/internet-always-on.json",
		},
		{
			name: "home default",
			env:  map[string]string{},
			home: "/home/sigmo",
			want: "/home/sigmo/.local/state/sigmo/internet-always-on.json",
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

			got, err := alwaysOnStatePathFromEnv(lookupEnv, userHomeDir)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatal("alwaysOnStatePathFromEnv() error = nil, want error")
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("alwaysOnStatePathFromEnv() error = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("alwaysOnStatePathFromEnv() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("alwaysOnStatePathFromEnv() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAlwaysOnStateForModem(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		run  func(t *testing.T, path string)
	}{
		{
			name: "save and load modem",
			run: func(t *testing.T, path string) {
				t.Helper()
				dir := filepath.Dir(path)
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatalf("os.MkdirAll() error = %v", err)
				}

				want := Preferences{
					APN:          "internet",
					IPType:       "ipv4",
					APNUsername:  "user",
					APNPassword:  "secret",
					APNAuth:      "pap",
					DefaultRoute: true,
					ProxyEnabled: true,
					AlwaysOn:     true,
				}
				if err := saveAlwaysOnStateForModem(path, "modem-1", want); err != nil {
					t.Fatalf("saveAlwaysOnStateForModem() error = %v", err)
				}
				info, err := os.Stat(path)
				if err != nil {
					t.Fatalf("os.Stat() error = %v", err)
				}
				if got := info.Mode().Perm(); got != 0o600 {
					t.Fatalf("state file mode = %#o, want %#o", got, os.FileMode(0o600))
				}
				info, err = os.Stat(dir)
				if err != nil {
					t.Fatalf("os.Stat(dir) error = %v", err)
				}
				if got := info.Mode().Perm(); got != 0o700 {
					t.Fatalf("state directory mode = %#o, want %#o", got, os.FileMode(0o700))
				}

				got, ok, err := loadAlwaysOnStateForModem(path, "modem-1")
				if err != nil {
					t.Fatalf("loadAlwaysOnStateForModem() error = %v", err)
				}
				if !ok {
					t.Fatal("loadAlwaysOnStateForModem() ok = false, want true")
				}
				if got != want {
					t.Fatalf("loadAlwaysOnStateForModem() = %#v, want %#v", got, want)
				}
			},
		},
		{
			name: "overwrite modem",
			run: func(t *testing.T, path string) {
				t.Helper()

				first := Preferences{APN: "internet", DefaultRoute: true, ProxyEnabled: true, AlwaysOn: true}
				second := Preferences{APN: "ims", AlwaysOn: true}
				if err := saveAlwaysOnStateForModem(path, "modem-1", first); err != nil {
					t.Fatalf("saveAlwaysOnStateForModem() first error = %v", err)
				}
				if err := saveAlwaysOnStateForModem(path, "modem-1", second); err != nil {
					t.Fatalf("saveAlwaysOnStateForModem() second error = %v", err)
				}

				got, ok, err := loadAlwaysOnStateForModem(path, "modem-1")
				if err != nil {
					t.Fatalf("loadAlwaysOnStateForModem() error = %v", err)
				}
				if !ok {
					t.Fatal("loadAlwaysOnStateForModem() ok = false, want true")
				}
				if got != second {
					t.Fatalf("loadAlwaysOnStateForModem() = %#v, want %#v", got, second)
				}
			},
		},
		{
			name: "delete modem removes empty file",
			run: func(t *testing.T, path string) {
				t.Helper()

				prefs := Preferences{APN: "internet", AlwaysOn: true}
				if err := saveAlwaysOnStateForModem(path, "modem-1", prefs); err != nil {
					t.Fatalf("saveAlwaysOnStateForModem() error = %v", err)
				}
				if err := deleteAlwaysOnStateForModem(path, "modem-1"); err != nil {
					t.Fatalf("deleteAlwaysOnStateForModem() error = %v", err)
				}

				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("state file exists after delete, stat error = %v", err)
				}
				_, ok, err := loadAlwaysOnStateForModem(path, "modem-1")
				if err != nil {
					t.Fatalf("loadAlwaysOnStateForModem() error = %v", err)
				}
				if ok {
					t.Fatal("loadAlwaysOnStateForModem() ok = true, want false")
				}
			},
		},
		{
			name: "load all skips disabled entries",
			run: func(t *testing.T, path string) {
				t.Helper()

				store := alwaysOnStateFile{
					Version: alwaysOnStateVersion,
					Modems: map[string]alwaysOnStateEntry{
						"modem-1": {APN: "internet", AlwaysOn: true},
						"modem-2": {APN: "ims"},
					},
				}
				if err := writeAlwaysOnState(path, store); err != nil {
					t.Fatalf("writeAlwaysOnState() error = %v", err)
				}

				got, err := loadAlwaysOnStates(path)
				if err != nil {
					t.Fatalf("loadAlwaysOnStates() error = %v", err)
				}
				want := map[string]Preferences{
					"modem-1": {APN: "internet", AlwaysOn: true},
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("loadAlwaysOnStates() = %#v, want %#v", got, want)
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tt.run(t, filepath.Join(t.TempDir(), "internet-always-on.json"))
		})
	}
}

func TestAlwaysOnStateReadErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "corrupt json",
			content: "{",
			want:    "decode always on state",
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

			path := filepath.Join(t.TempDir(), "internet-always-on.json")
			if err := os.WriteFile(path, []byte(tt.content), 0o600); err != nil {
				t.Fatalf("os.WriteFile() error = %v", err)
			}

			_, err := loadAlwaysOnStates(path)
			if err == nil {
				t.Fatal("loadAlwaysOnStates() error = nil, want error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("loadAlwaysOnStates() error = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestConnectorAlwaysOnStatePolicy(t *testing.T) {
	t.Parallel()

	const modemID = "modem-1"

	tests := []struct {
		name      string
		run       func(t *testing.T, c *Connector, path string)
		wantState bool
		wantPrefs Preferences
	}{
		{
			name: "sync enabled writes state",
			run: func(t *testing.T, c *Connector, path string) {
				t.Helper()

				prefs := Preferences{APN: "internet", DefaultRoute: true, ProxyEnabled: true, AlwaysOn: true}
				if err := c.syncAlwaysOnState(modemID, prefs); err != nil {
					t.Fatalf("syncAlwaysOnState() error = %v", err)
				}
				c.preferences[modemID] = prefs
			},
			wantState: true,
			wantPrefs: Preferences{APN: "internet", DefaultRoute: true, ProxyEnabled: true, AlwaysOn: true},
		},
		{
			name: "manual clear deletes state and keeps disabled preferences",
			run: func(t *testing.T, c *Connector, path string) {
				t.Helper()

				prefs := Preferences{APN: "internet", DefaultRoute: true, ProxyEnabled: true, AlwaysOn: true}
				if err := saveAlwaysOnStateForModem(path, modemID, prefs); err != nil {
					t.Fatalf("saveAlwaysOnStateForModem() error = %v", err)
				}
				if err := c.clearAlwaysOnStateLocked(modemID); err != nil {
					t.Fatalf("clearAlwaysOnStateLocked() error = %v", err)
				}
			},
			wantPrefs: Preferences{APN: "internet", DefaultRoute: true, ProxyEnabled: true},
		},
		{
			name: "sync disabled deletes state",
			run: func(t *testing.T, c *Connector, path string) {
				t.Helper()

				prefs := Preferences{APN: "internet", AlwaysOn: true}
				if err := saveAlwaysOnStateForModem(path, modemID, prefs); err != nil {
					t.Fatalf("saveAlwaysOnStateForModem() error = %v", err)
				}
				if err := c.syncAlwaysOnState(modemID, Preferences{APN: "internet"}); err != nil {
					t.Fatalf("syncAlwaysOnState() error = %v", err)
				}
				c.preferences[modemID] = Preferences{APN: "internet"}
			},
			wantPrefs: Preferences{APN: "internet"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "internet-always-on.json")
			c := NewConnectorWithProxyStatePath(nil, path)
			tt.run(t, c, path)

			_, ok, err := loadAlwaysOnStateForModem(path, modemID)
			if err != nil {
				t.Fatalf("loadAlwaysOnStateForModem() error = %v", err)
			}
			if ok != tt.wantState {
				t.Fatalf("loadAlwaysOnStateForModem() ok = %t, want %t", ok, tt.wantState)
			}
			if got := c.preference(modemID); got != tt.wantPrefs {
				t.Fatalf("preference() = %#v, want %#v", got, tt.wantPrefs)
			}
		})
	}
}

func TestRestoreAlwaysOnSkipsStaleSnapshotAfterManualClear(t *testing.T) {
	t.Parallel()

	const modemID = "modem-1"

	path := filepath.Join(t.TempDir(), "internet-always-on.json")
	c := NewConnectorWithProxyStatePath(nil, path)
	prefs := Preferences{APN: "internet", DefaultRoute: true, AlwaysOn: true}
	if err := saveAlwaysOnStateForModem(path, modemID, prefs); err != nil {
		t.Fatalf("saveAlwaysOnStateForModem() error = %v", err)
	}
	states, err := loadAlwaysOnStates(path)
	if err != nil {
		t.Fatalf("loadAlwaysOnStates() error = %v", err)
	}
	stale := states[modemID]
	if err := c.clearAlwaysOnStateLocked(modemID); err != nil {
		t.Fatalf("clearAlwaysOnStateLocked() error = %v", err)
	}

	if err := c.restoreAlwaysOn(&mmodem.Modem{EquipmentIdentifier: modemID}, stale); err != nil {
		t.Fatalf("restoreAlwaysOn() error = %v", err)
	}
	_, ok, err := loadAlwaysOnStateForModem(path, modemID)
	if err != nil {
		t.Fatalf("loadAlwaysOnStateForModem() error = %v", err)
	}
	if ok {
		t.Fatal("loadAlwaysOnStateForModem() ok = true, want false")
	}
}
