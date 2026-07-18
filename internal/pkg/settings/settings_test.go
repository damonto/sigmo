package settings

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/damonto/sigmo/internal/pkg/storage"
)

func TestFindModem(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		settings Settings
		id       string
		want     Modem
	}{
		{
			name: "default modem settings",
			id:   "missing",
			want: Modem{
				MSS: 240,
			},
		},
		{
			name: "configured modem settings",
			settings: Settings{
				Modems: map[string]Modem{
					"123": {
						Alias: "Office",
						MSS:   128,
					},
				},
			},
			id: "123",
			want: Modem{
				Alias: "Office",
				MSS:   128,
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.settings.FindModem(tt.id); got != tt.want {
				t.Fatalf("FindModem() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestProxySettings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		settings Settings
		want     Proxy
	}{
		{
			name:     "defaults listen address when proxy is omitted",
			settings: Settings{},
			want: Proxy{
				ListenAddress: "127.0.0.1",
				HTTPPort:      8080,
				SOCKS5Port:    1080,
			},
		},
		{
			name: "keeps configured proxy",
			settings: Settings{
				Proxy: &Proxy{
					ListenAddress: "0.0.0.0",
					HTTPPort:      8080,
					SOCKS5Port:    1080,
					Password:      "secret",
				},
			},
			want: Proxy{
				ListenAddress: "0.0.0.0",
				HTTPPort:      8080,
				SOCKS5Port:    1080,
				Password:      "secret",
			},
		},
		{
			name: "defaults blank listener settings",
			settings: Settings{
				Proxy: &Proxy{
					Password: "secret",
				},
			},
			want: Proxy{
				ListenAddress: "127.0.0.1",
				HTTPPort:      8080,
				SOCKS5Port:    1080,
				Password:      "secret",
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.settings.ProxySettings(); got != tt.want {
				t.Fatalf("ProxySettings() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestStorePersistsSettings(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openTestStore(t)
	store, err := NewStore(ctx, db)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	enabled := true
	_, err = store.Update(ctx, func(current *Settings) error {
		current.Auth = Auth{
			OTPRequired:       true,
			AuthProviders:     []string{"telegram"},
			TokenValidityDays: 90,
		}
		current.Channels = map[string]Channel{
			"telegram": {
				Enabled:    &enabled,
				BotToken:   "token",
				Recipients: Recipients{"123456"},
			},
		}
		current.Proxy = &Proxy{
			ListenAddress: "0.0.0.0",
			HTTPPort:      18080,
			SOCKS5Port:    11080,
			Password:      "secret",
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if err := store.UpdateModem(ctx, "modem-1", Modem{Alias: "Office", MSS: 128}); err != nil {
		t.Fatalf("UpdateModem() error = %v", err)
	}

	reloaded, err := NewStore(ctx, db)
	if err != nil {
		t.Fatalf("NewStore() reload error = %v", err)
	}
	got := reloaded.Snapshot()
	if !got.Auth.OTPRequired {
		t.Fatal("OTPRequired = false, want true")
	}
	if got.Auth.AuthProviders[0] != "telegram" {
		t.Fatalf("AuthProviders = %#v, want telegram", got.Auth.AuthProviders)
	}
	if got.Auth.TokenValidityDays != 90 {
		t.Fatalf("TokenValidityDays = %d, want 90", got.Auth.TokenValidityDays)
	}
	if got.Channels["telegram"].BotToken != "token" {
		t.Fatalf("telegram bot token = %q, want token", got.Channels["telegram"].BotToken)
	}
	if got.ProxySettings().HTTPPort != 18080 {
		t.Fatalf("proxy http port = %d, want 18080", got.ProxySettings().HTTPPort)
	}
	if modem := got.FindModem("modem-1"); modem.Alias != "Office" || modem.MSS != 128 {
		t.Fatalf("modem settings = %#v, want saved settings", modem)
	}
}

func TestStoreDefaultsEmptyDatabase(t *testing.T) {
	t.Parallel()

	store, err := NewStore(context.Background(), openTestStore(t))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	got := store.Snapshot()
	if got.Auth.OTPRequired {
		t.Fatal("OTPRequired = true, want false")
	}
	if got.Auth.TokenValidityDays != DefaultTokenValidityDays {
		t.Fatalf("TokenValidityDays = %d, want %d", got.Auth.TokenValidityDays, DefaultTokenValidityDays)
	}
	if len(got.Channels) != 0 {
		t.Fatalf("Channels length = %d, want 0", len(got.Channels))
	}
	if got.ProxySettings() != DefaultProxy() {
		t.Fatalf("ProxySettings() = %#v, want %#v", got.ProxySettings(), DefaultProxy())
	}
}

func TestStoreDefaultsLegacyAuthSettings(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openTestStore(t)
	legacy := struct {
		AuthProviders []string `json:"authProviders"`
		OTPRequired   bool     `json:"otpRequired"`
	}{
		AuthProviders: []string{"telegram"},
		OTPRequired:   true,
	}
	if err := db.Put(ctx, globalScopeKey, authSettingsKey, legacy); err != nil {
		t.Fatalf("Put() legacy auth error = %v", err)
	}

	store, err := NewStore(ctx, db)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	if got := store.Snapshot().Auth.TokenValidityDays; got != DefaultTokenValidityDays {
		t.Fatalf("TokenValidityDays = %d, want %d", got, DefaultTokenValidityDays)
	}
}

func TestNewStoreRequiresDatabase(t *testing.T) {
	t.Parallel()

	_, err := NewStore(context.Background(), nil)
	if err == nil {
		t.Fatal("NewStore() error = nil, want storage error")
	}
	if !errors.Is(err, errStorageRequired) {
		t.Fatalf("NewStore() error = %v, want storage error", err)
	}
}

func openTestStore(t *testing.T) *storage.Store {
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
