package config

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/forwarder"
	pkgconfig "github.com/damonto/sigmo/internal/pkg/config"
	"github.com/damonto/sigmo/internal/pkg/internet"
	appvalidator "github.com/damonto/sigmo/internal/pkg/validator"
)

func TestConfigSchema(t *testing.T) {
	t.Parallel()

	schema := configSchema()
	tests := []struct {
		name      string
		channel   string
		wantField string
		wantKind  string
	}{
		{
			name:      "telegram bot token is password",
			channel:   "telegram",
			wantField: "botToken",
			wantKind:  controlPassword,
		},
		{
			name:      "email tls policy is select",
			channel:   "email",
			wantField: "tlsPolicy",
			wantKind:  controlSelect,
		},
		{
			name:      "email ssl is switch",
			channel:   "email",
			wantField: "ssl",
			wantKind:  controlSwitch,
		},
		{
			name:      "http headers are key value",
			channel:   "http",
			wantField: "headers",
			wantKind:  controlKeyValue,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			channel, ok := channelSchema(schema, tt.channel)
			if !ok {
				t.Fatalf("channel %q not found", tt.channel)
			}
			field, ok := fieldSchema(channel.Fields, tt.wantField)
			if !ok {
				t.Fatalf("field %q not found", tt.wantField)
			}
			if field.Control != tt.wantKind {
				t.Fatalf("Control = %q, want %q", field.Control, tt.wantKind)
			}
			if !strings.HasPrefix(field.Label, "config.schema.") {
				t.Fatalf("Label = %q, want translation key", field.Label)
			}
			if field.Description != "" && !strings.HasPrefix(field.Description, "config.schema.") {
				t.Fatalf("Description = %q, want translation key", field.Description)
			}
			if tt.wantField == "tlsPolicy" && len(field.Options) != 3 {
				t.Fatalf("tlsPolicy options = %d, want 3", len(field.Options))
			}
			if tt.wantField == "tlsPolicy" && !strings.HasPrefix(field.Options[0].Label, "config.schema.") {
				t.Fatalf("tlsPolicy first option Label = %q, want translation key", field.Options[0].Label)
			}
		})
	}
}

func TestResponseJSONUsesCamelCase(t *testing.T) {
	t.Parallel()

	cfg := pkgconfig.Default()
	cfg.Proxy = &pkgconfig.Proxy{
		ListenAddress: "127.0.0.1",
		HTTPPort:      8080,
		SOCKS5Port:    1080,
	}
	cfg.Channels = map[string]pkgconfig.Channel{
		"telegram": {
			BotToken:     "token",
			Recipients:   pkgconfig.Recipients{"123456"},
			SMTPPassword: "hidden",
		},
	}

	data, err := json.Marshal(responseFromConfig(*cfg, []string{"app.listenAddress"}))
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	body := string(data)
	for _, want := range []string{
		`"listenAddress"`,
		`"authProviders"`,
		`"otpRequired"`,
		`"httpPort"`,
		`"socks5Port"`,
		`"botToken"`,
		`"tlsPolicy"`,
		`"restartRequiredFields"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("response JSON missing %s: %s", want, body)
		}
	}
	for _, unwanted := range []string{
		"listen_address",
		"auth_providers",
		"otp_required",
		"http_port",
		"socks5_port",
		"bot_token",
		"tls_policy",
	} {
		if strings.Contains(body, unwanted) {
			t.Fatalf("response JSON contains snake_case key %q: %s", unwanted, body)
		}
	}
	var resp Response
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	telegram := resp.Values.Channels["telegram"]
	if telegram.SMTPPassword != "" {
		t.Fatalf("telegram SMTPPassword = %q, want empty hidden field", telegram.SMTPPassword)
	}
}

func TestUpdate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		request     UpdateRequest
		wantStatus  int
		wantModem   bool
		wantRestart string
	}{
		{
			name: "rejects auth provider without enabled channel",
			request: UpdateRequest{
				App: AppValues{
					Environment:   "production",
					ListenAddress: "0.0.0.0:9527",
					AuthProviders: []string{"telegram"},
				},
				Proxy: ProxyValues{
					ListenAddress: "127.0.0.1",
					HTTPPort:      8080,
					SOCKS5Port:    1080,
				},
			},
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name: "rejects auth provider with disabled channel",
			request: UpdateRequest{
				App: AppValues{
					Environment:   "production",
					ListenAddress: "0.0.0.0:9527",
					AuthProviders: []string{"telegram"},
				},
				Proxy: ProxyValues{
					ListenAddress: "127.0.0.1",
					HTTPPort:      8080,
					SOCKS5Port:    1080,
				},
				Channels: map[string]ChannelValues{
					"telegram": {
						Enabled:    new(false),
						BotToken:   "token",
						Recipients: []string{"123456"},
					},
				},
			},
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name: "rejects zero proxy http port",
			request: UpdateRequest{
				App: AppValues{
					Environment:   "production",
					ListenAddress: "0.0.0.0:9527",
				},
				Proxy: ProxyValues{
					ListenAddress: "127.0.0.1",
					HTTPPort:      0,
					SOCKS5Port:    1080,
				},
			},
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name: "rejects channel without enabled flag",
			request: UpdateRequest{
				App: AppValues{
					Environment:   "production",
					ListenAddress: "0.0.0.0:9527",
				},
				Proxy: ProxyValues{
					ListenAddress: "127.0.0.1",
					HTTPPort:      8080,
					SOCKS5Port:    1080,
				},
				Channels: map[string]ChannelValues{
					"telegram": {
						BotToken:   "token",
						Recipients: []string{"123456"},
					},
				},
			},
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name: "rejects empty auth provider before canonicalization",
			request: UpdateRequest{
				App: AppValues{
					Environment:   "production",
					ListenAddress: "0.0.0.0:9527",
					OTPRequired:   true,
					AuthProviders: []string{"telegram", "  "},
				},
				Proxy: ProxyValues{
					ListenAddress: "127.0.0.1",
					HTTPPort:      8080,
					SOCKS5Port:    1080,
				},
				Channels: map[string]ChannelValues{
					"telegram": {
						Enabled:    new(true),
						BotToken:   "token",
						Recipients: []string{"123456"},
					},
				},
			},
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name: "rejects disabled channel outside schema bounds",
			request: UpdateRequest{
				App: AppValues{
					Environment:   "production",
					ListenAddress: "0.0.0.0:9527",
				},
				Proxy: ProxyValues{
					ListenAddress: "127.0.0.1",
					HTTPPort:      8080,
					SOCKS5Port:    1080,
				},
				Channels: map[string]ChannelValues{
					"gotify": {
						Enabled:  new(false),
						Priority: -1,
					},
				},
			},
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name: "rejects non-http channel endpoint",
			request: UpdateRequest{
				App: AppValues{
					Environment:   "production",
					ListenAddress: "0.0.0.0:9527",
				},
				Proxy: ProxyValues{
					ListenAddress: "127.0.0.1",
					HTTPPort:      8080,
					SOCKS5Port:    1080,
				},
				Channels: map[string]ChannelValues{
					"http": {
						Enabled:  new(false),
						Endpoint: "ftp://example.com/webhook",
					},
				},
			},
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name: "ignores hidden channel fields before validation",
			request: UpdateRequest{
				App: AppValues{
					Environment:   "development",
					ListenAddress: "127.0.0.1:9999",
				},
				Proxy: ProxyValues{
					ListenAddress: "127.0.0.1",
					HTTPPort:      18080,
					SOCKS5Port:    11080,
				},
				Channels: map[string]ChannelValues{
					"telegram": {
						Enabled:   new(false),
						BotToken:  "draft-token",
						SMTPPort:  70000,
						TLSPolicy: "invalid",
					},
					"email": {
						Enabled:  new(false),
						Endpoint: "not-url",
						Priority: -1,
					},
				},
			},
			wantStatus:  http.StatusOK,
			wantModem:   true,
			wantRestart: "app.listenAddress",
		},
		{
			name: "saves editable config and preserves modems",
			request: UpdateRequest{
				App: AppValues{
					Environment:   "development",
					ListenAddress: "127.0.0.1:9999",
					OTPRequired:   true,
					AuthProviders: []string{"telegram"},
				},
				Proxy: ProxyValues{
					ListenAddress: "127.0.0.1",
					HTTPPort:      18080,
					SOCKS5Port:    11080,
					Password:      "secret",
				},
				Channels: map[string]ChannelValues{
					"telegram": {
						Enabled:      new(true),
						BotToken:     "token",
						Recipients:   []string{"123456"},
						SMTPPassword: "hidden",
					},
				},
			},
			wantStatus:  http.StatusOK,
			wantModem:   true,
			wantRestart: "app.listenAddress",
		},
		{
			name: "saves disabled channel without sender validation",
			request: UpdateRequest{
				App: AppValues{
					Environment:   "development",
					ListenAddress: "127.0.0.1:9999",
				},
				Proxy: ProxyValues{
					ListenAddress: "127.0.0.1",
					HTTPPort:      18080,
					SOCKS5Port:    11080,
					Password:      "secret",
				},
				Channels: map[string]ChannelValues{
					"telegram": {
						Enabled:  new(false),
						BotToken: "draft-token",
					},
				},
			},
			wantStatus:  http.StatusOK,
			wantModem:   true,
			wantRestart: "app.listenAddress",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := pkgconfig.Default()
			cfg.Path = filepath.Join(t.TempDir(), "config.toml")
			cfg.Modems = map[string]pkgconfig.Modem{
				"modem-1": {
					Alias:      "Office",
					Compatible: true,
					MSS:        128,
				},
			}
			if err := cfg.Save(); err != nil {
				t.Fatalf("Save() error = %v", err)
			}
			store := pkgconfig.NewStore(cfg)
			relay, err := forwarder.New(store, nil)
			if err != nil {
				t.Fatalf("forwarder.New() error = %v", err)
			}
			internetConnector, err := internet.NewConnector(internet.ConnectorConfig{
				Proxy:        internet.NewProxy(internet.ProxyConfig{}),
				AlwaysOnPath: filepath.Join(t.TempDir(), "internet-always-on.json"),
			})
			if err != nil {
				t.Fatalf("internet.NewConnector() error = %v", err)
			}
			h := New(store, internetConnector, relay)

			body, err := json.Marshal(tt.request)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			e := echo.New()
			e.Validator = appvalidator.New()
			req := httptest.NewRequest(http.MethodPut, "/api/v1/config", strings.NewReader(string(body)))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			if err := h.Update(c); err != nil {
				t.Fatalf("Update() error = %v", err)
			}
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if tt.wantStatus != http.StatusOK {
				return
			}

			snapshot := store.Snapshot()
			if _, ok := snapshot.Modems["modem-1"]; ok != tt.wantModem {
				t.Fatalf("modem preserved = %v, want %v", ok, tt.wantModem)
			}
			if telegram := snapshot.Channels["telegram"]; telegram.SMTPPassword != "" {
				t.Fatalf("telegram SMTPPassword = %q, want hidden field dropped", telegram.SMTPPassword)
			}
			var resp Response
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}
			if !slices.Contains(resp.RestartRequiredFields, tt.wantRestart) {
				t.Fatalf("RestartRequiredFields = %#v, want %q", resp.RestartRequiredFields, tt.wantRestart)
			}
		})
	}
}

func TestUpdatePersistsConfigWhenProxyReloadFails(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0") //nolint:noctx
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	t.Cleanup(func() {
		if err := occupied.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})
	occupiedPort := occupied.Addr().(*net.TCPAddr).Port

	cfg := pkgconfig.Default()
	cfg.Path = filepath.Join(t.TempDir(), "config.toml")
	cfg.Proxy = &pkgconfig.Proxy{
		ListenAddress: "127.0.0.1",
		HTTPPort:      18080,
		SOCKS5Port:    11080,
		Password:      "old",
	}
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	store := pkgconfig.NewStore(cfg)
	relay, err := forwarder.New(store, nil)
	if err != nil {
		t.Fatalf("forwarder.New() error = %v", err)
	}
	proxy := internet.NewProxy(internet.ProxyConfig{
		ListenAddress: "127.0.0.1",
		HTTPPort:      0,
		SOCKS5Port:    0,
		Password:      "old",
	})
	if _, err := proxy.Register(internet.ProxyBinding{Username: "wwan0", InterfaceName: "wwan0"}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	t.Cleanup(func() {
		if err := proxy.Unregister("wwan0"); err != nil {
			t.Fatalf("Unregister() error = %v", err)
		}
	})
	internetConnector, err := internet.NewConnector(internet.ConnectorConfig{
		Proxy:        proxy,
		AlwaysOnPath: filepath.Join(t.TempDir(), "internet-always-on.json"),
	})
	if err != nil {
		t.Fatalf("internet.NewConnector() error = %v", err)
	}
	h := New(store, internetConnector, relay)

	reqBody := UpdateRequest{
		App: AppValues{
			Environment:   "production",
			ListenAddress: "0.0.0.0:9527",
		},
		Proxy: ProxyValues{
			ListenAddress: "127.0.0.1",
			HTTPPort:      occupiedPort,
			SOCKS5Port:    occupiedPort,
			Password:      "new",
		},
		Channels: map[string]ChannelValues{},
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	e := echo.New()
	e.Validator = appvalidator.New()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/config", strings.NewReader(string(body)))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := h.Update(c); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "internal server error") {
		t.Fatalf("body = %s, want generic internal error", rec.Body.String())
	}
	snapshot := store.Snapshot()
	if got := snapshot.ProxySettings().HTTPPort; got != occupiedPort {
		t.Fatalf("saved proxy HTTPPort = %d, want %d", got, occupiedPort)
	}
	data, err := os.ReadFile(cfg.Path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(data), fmt.Sprintf("http_port = %d", occupiedPort)) {
		t.Fatalf("saved config missing updated proxy port:\n%s", string(data))
	}
	status := proxy.Status("wwan0")
	if !status.Enabled {
		t.Fatal("proxy disabled after failed reload")
	}
	if status.Password != "old" {
		t.Fatalf("proxy password = %q, want old", status.Password)
	}
}

func channelSchema(schema Schema, key string) (ChannelSchema, bool) {
	for _, channel := range schema.Channels {
		if channel.Key == key {
			return channel, true
		}
	}
	return ChannelSchema{}, false
}

func fieldSchema(fields []Field, key string) (Field, bool) {
	for _, field := range fields {
		if field.Key == key {
			return field, true
		}
	}
	return Field{}, false
}
