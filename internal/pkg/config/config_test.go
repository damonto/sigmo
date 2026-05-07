package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func TestLoad(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		content        string
		wantErr        string
		wantOTPDefault bool
	}{
		{
			name: "uses zero value when otp_required is omitted",
			content: `
[app]
environment = "development"
listen_address = "127.0.0.1:9527"

[channels.telegram]
bot_token = "token"
recipients = ["123456"]
`,
			wantOTPDefault: false,
		},
		{
			name: "fails on unknown subject field",
			content: `
[app]
environment = "development"
listen_address = "127.0.0.1:9527"

[channels.bark]
endpoint = "https://api.day.app"
recipients = ["device-key"]
subject = "deprecated"
`,
			wantErr: "subject",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(path, []byte(strings.TrimSpace(tt.content)), 0o644); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}

			got, err := Load(path)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("Load() error = nil, want %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), "decode config") {
					t.Fatalf("Load() error = %v, want wrapped decode error", err)
				}
				var strictErr *toml.StrictMissingError
				if !errors.As(err, &strictErr) {
					t.Fatalf("Load() error = %v, want StrictMissingError", err)
				}
				if len(strictErr.Errors) != 1 {
					t.Fatalf("StrictMissingError count = %d, want 1", len(strictErr.Errors))
				}
				if got := strings.Join(strictErr.Errors[0].Key(), "."); got != "channels.bark.subject" {
					t.Fatalf("StrictMissingError key = %q, want %q", got, "channels.bark.subject")
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if got.App.OTPRequired != tt.wantOTPDefault {
				t.Fatalf("OTPRequired = %v, want %v", got.App.OTPRequired, tt.wantOTPDefault)
			}
			if channel, ok := got.Channels["telegram"]; ok && !channel.IsEnabled() {
				t.Fatal("legacy telegram channel is disabled, want enabled")
			}
		})
	}
}

func TestLoadOrCreate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setup   func(*testing.T) string
		wantErr bool
	}{
		{
			name: "creates missing default file",
			setup: func(t *testing.T) string {
				t.Helper()
				return filepath.Join(t.TempDir(), "sigmo", "config.toml")
			},
		},
		{
			name: "loads existing file",
			setup: func(t *testing.T) string {
				t.Helper()
				path := filepath.Join(t.TempDir(), "config.toml")
				content := `
[app]
listen_address = "127.0.0.1:9527"
`
				if err := os.WriteFile(path, []byte(strings.TrimSpace(content)), 0o644); err != nil {
					t.Fatalf("WriteFile() error = %v", err)
				}
				return path
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := tt.setup(t)
			got, err := LoadOrCreate(path)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("LoadOrCreate() error = nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadOrCreate() error = %v", err)
			}
			if got.Path != path {
				t.Fatalf("Path = %q, want %q", got.Path, path)
			}
			if got.App.Environment != "production" {
				t.Fatalf("Environment = %q, want production", got.App.Environment)
			}
			if got.App.ListenAddress == "" {
				t.Fatal("ListenAddress is empty")
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("Stat() error = %v", err)
			}
		})
	}
}

func TestDefaultPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	got, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath() error = %v", err)
	}
	want := filepath.Join(dir, "sigmo", "config.toml")
	if got != want {
		t.Fatalf("DefaultPath() = %q, want %q", got, want)
	}
}

func TestSaveUsesExclusiveTempFile(t *testing.T) {
	t.Parallel()

	cfg := Default()
	cfg.Path = filepath.Join(t.TempDir(), "config.toml")
	predictableTempPath := fmt.Sprintf("%s.%d.tmp", cfg.Path, os.Getpid())
	const sentinel = "do not touch"
	if err := os.WriteFile(predictableTempPath, []byte(sentinel), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	data, err := os.ReadFile(predictableTempPath)
	if err != nil {
		t.Fatalf("ReadFile() predictable temp error = %v", err)
	}
	if got := string(data); got != sentinel {
		t.Fatalf("predictable temp = %q, want %q", got, sentinel)
	}
}

func TestFindModem(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config Config
		id     string
		want   Modem
	}{
		{
			name: "default modem settings",
			id:   "missing",
			want: Modem{
				Compatible: false,
				MSS:        240,
			},
		},
		{
			name: "configured modem settings",
			config: Config{
				Modems: map[string]Modem{
					"123": {
						Alias:      "Office",
						Compatible: true,
						MSS:        128,
					},
				},
			},
			id: "123",
			want: Modem{
				Alias:      "Office",
				Compatible: true,
				MSS:        128,
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.config.FindModem(tt.id); got != tt.want {
				t.Fatalf("FindModem() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestProxySettings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config Config
		want   Proxy
	}{
		{
			name:   "defaults listen address when proxy is omitted",
			config: Config{},
			want: Proxy{
				ListenAddress: "127.0.0.1",
				HTTPPort:      8080,
				SOCKS5Port:    1080,
			},
		},
		{
			name: "keeps configured proxy",
			config: Config{
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
			config: Config{
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

			if got := tt.config.ProxySettings(); got != tt.want {
				t.Fatalf("ProxySettings() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestSave(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		config      Config
		wantContain []string
		wantAbsent  []string
	}{
		{
			name: "telegram omits unrelated channel keys",
			config: Config{
				App: App{
					Environment:   "development",
					ListenAddress: "127.0.0.1:9527",
				},
				Channels: map[string]Channel{
					"telegram": {
						BotToken:   "token",
						Recipients: Recipients{"123456"},
					},
				},
			},
			wantContain: []string{
				"[channels.telegram]",
				"enabled = true",
				`bot_token = 'token'`,
				`recipients = ['123456']`,
			},
			wantAbsent: []string{
				"smtp_host",
				"smtp_port",
				"smtp_username",
				"smtp_password",
				"from =",
				"tls_policy",
				"ssl =",
				"priority =",
				"endpoint =",
				"apn =",
				"internet_default_route =",
				"[proxy]",
				"[channels.telegram.headers]",
			},
		},
		{
			name: "default config omits empty optional tables",
			config: Config{
				App: App{
					Environment:   "production",
					ListenAddress: "0.0.0.0:9527",
				},
			},
			wantContain: []string{
				"[app]",
				`environment = 'production'`,
				`listen_address = '0.0.0.0:9527'`,
			},
			wantAbsent: []string{
				"[proxy]",
				"[channels]",
				"[modems]",
			},
		},
		{
			name: "disabled channel keeps settings",
			config: Config{
				App: App{
					Environment:   "production",
					ListenAddress: "0.0.0.0:9527",
				},
				Channels: map[string]Channel{
					"telegram": {
						Enabled:    new(false),
						BotToken:   "token",
						Recipients: Recipients{"123456"},
					},
				},
			},
			wantContain: []string{
				"[channels.telegram]",
				"enabled = false",
				`bot_token = 'token'`,
				`recipients = ['123456']`,
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := tt.config
			cfg.Path = filepath.Join(t.TempDir(), "config.toml")

			if err := cfg.Save(); err != nil {
				t.Fatalf("Save() error = %v", err)
			}

			data, err := os.ReadFile(cfg.Path)
			if err != nil {
				t.Fatalf("ReadFile() error = %v", err)
			}
			info, err := os.Stat(cfg.Path)
			if err != nil {
				t.Fatalf("Stat() error = %v", err)
			}
			if mode := info.Mode().Perm(); mode != 0o600 {
				t.Fatalf("config mode = %v, want 0600", mode)
			}
			got := string(data)

			for _, want := range tt.wantContain {
				if !strings.Contains(got, want) {
					t.Fatalf("saved config missing %q:\n%s", want, got)
				}
			}
			for _, unwanted := range tt.wantAbsent {
				if strings.Contains(got, unwanted) {
					t.Fatalf("saved config unexpectedly contains %q:\n%s", unwanted, got)
				}
			}
		})
	}
}
