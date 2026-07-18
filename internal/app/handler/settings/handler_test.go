package settings

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/forwarder"
	"github.com/damonto/sigmo/internal/pkg/internet"
	appsettings "github.com/damonto/sigmo/internal/pkg/settings"
	"github.com/damonto/sigmo/internal/pkg/storage"
	appvalidator "github.com/damonto/sigmo/internal/pkg/validator"
)

func TestSettingsSchema(t *testing.T) {
	t.Parallel()

	schema := settingsSchema()
	tests := []struct {
		name      string
		channel   string
		wantField string
		wantKind  string
	}{
		{name: "telegram bot token is password", channel: "telegram", wantField: "botToken", wantKind: controlPassword},
		{name: "email smtp username is text", channel: "email", wantField: "smtpUsername", wantKind: controlText},
		{name: "email tls policy is select", channel: "email", wantField: "tlsPolicy", wantKind: controlSelect},
		{name: "email ssl is switch", channel: "email", wantField: "ssl", wantKind: controlSwitch},
		{name: "http headers are key value", channel: "http", wantField: "headers", wantKind: controlKeyValue},
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
			if !strings.HasPrefix(field.Label, "settings.schema.") {
				t.Fatalf("Label = %q, want translation key", field.Label)
			}
			if field.Description != "" && !strings.HasPrefix(field.Description, "settings.schema.") {
				t.Fatalf("Description = %q, want translation key", field.Description)
			}
			if tt.wantField == "tlsPolicy" && len(field.Options) != 3 {
				t.Fatalf("tlsPolicy options = %d, want 3", len(field.Options))
			}
		})
	}
}

func TestResponseJSONUsesCamelCase(t *testing.T) {
	t.Parallel()

	settings := appsettings.Default()
	settings.Proxy = &appsettings.Proxy{
		ListenAddress: "127.0.0.1",
		HTTPPort:      8080,
		SOCKS5Port:    1080,
	}
	settings.Channels = map[string]appsettings.Channel{
		"telegram": {
			BotToken:     "token",
			Recipients:   appsettings.Recipients{"123456"},
			SMTPPassword: "hidden",
		},
	}

	data, err := json.Marshal(responseFromSettings(*settings))
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
		"restartRequiredFields",
		"path",
	} {
		if strings.Contains(body, unwanted) {
			t.Fatalf("response JSON contains unexpected key %q: %s", unwanted, body)
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
		name       string
		request    UpdateRequest
		wantStatus int
		wantModem  bool
	}{
		{
			name: "rejects auth provider without enabled channel",
			request: UpdateRequest{
				Auth: AuthValues{AuthProviders: []string{"telegram"}},
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
				Auth: AuthValues{AuthProviders: []string{"telegram"}},
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
			name: "ignores hidden channel fields before validation",
			request: UpdateRequest{
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
				},
			},
			wantStatus: http.StatusOK,
			wantModem:  true,
		},
		{
			name: "saves editable settings and preserves modems",
			request: UpdateRequest{
				Auth: AuthValues{
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
			wantStatus: http.StatusOK,
			wantModem:  true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store, h := newTestHandler(t)
			body, err := json.Marshal(tt.request)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			rec := putSettings(t, h, body)
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
			if len(resp.Values.Auth.AuthProviders) > 0 && !slices.IsSorted(resp.Values.Auth.AuthProviders) {
				t.Fatalf("AuthProviders = %#v, want sorted", resp.Values.Auth.AuthProviders)
			}
		})
	}
}

func TestUpdatePersistsSettingsWhenProxyReloadFails(t *testing.T) {
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

	settings := appsettings.Default()
	settings.Proxy = &appsettings.Proxy{
		ListenAddress: "127.0.0.1",
		HTTPPort:      18080,
		SOCKS5Port:    11080,
		Password:      "old",
	}
	store := appsettings.NewMemoryStore(settings)
	relay, err := forwarder.New(store, nil, testStorage(t), nil)
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
		Proxy: proxy,
		State: testStorage(t),
	})
	if err != nil {
		t.Fatalf("internet.NewConnector() error = %v", err)
	}
	h := New(store, internetConnector, relay)

	reqBody := UpdateRequest{
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

	rec := putSettings(t, h, body)
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
	status := proxy.Status("wwan0")
	if !status.Enabled {
		t.Fatal("proxy disabled after failed reload")
	}
	if status.Password != "old" {
		t.Fatalf("proxy password = %q, want old", status.Password)
	}
}

func newTestHandler(t *testing.T) (*appsettings.Store, *Handler) {
	t.Helper()

	settings := appsettings.Default()
	settings.Modems = map[string]appsettings.Modem{
		"modem-1": {
			Alias: "Office",
			MSS:   128,
		},
	}
	store := appsettings.NewMemoryStore(settings)
	relay, err := forwarder.New(store, nil, testStorage(t), nil)
	if err != nil {
		t.Fatalf("forwarder.New() error = %v", err)
	}
	internetConnector, err := internet.NewConnector(internet.ConnectorConfig{
		Proxy: internet.NewProxy(internet.ProxyConfig{}),
		State: testStorage(t),
	})
	if err != nil {
		t.Fatalf("internet.NewConnector() error = %v", err)
	}
	return store, New(store, internetConnector, relay)
}

func putSettings(t *testing.T, h *Handler, body []byte) *httptest.ResponseRecorder {
	t.Helper()

	e := echo.New()
	e.Validator = appvalidator.New()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/settings", strings.NewReader(string(body)))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	if err := h.Update(c); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	return rec
}

func testStorage(t *testing.T) *storage.Store {
	t.Helper()
	store, err := storage.Open(t.Context(), filepath.Join(t.TempDir(), "sigmo.db"))
	if err != nil {
		t.Fatalf("storage.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})
	return store
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
