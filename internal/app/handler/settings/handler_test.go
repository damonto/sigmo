package settings

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
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

func TestUpdateAuth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		request       AuthValues
		configure     func(*appsettings.Settings)
		wantStatus    int
		wantProviders []string
	}{
		{
			name:       "rejects auth provider without enabled channel",
			request:    AuthValues{AuthProviders: []string{"telegram"}},
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:    "rejects auth provider with disabled channel",
			request: AuthValues{AuthProviders: []string{"telegram"}},
			configure: func(settings *appsettings.Settings) {
				settings.Channels["telegram"] = appsettings.Channel{Enabled: new(false)}
			},
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "rejects otp without auth providers",
			request:    AuthValues{OTPRequired: true},
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name: "saves only auth settings",
			request: AuthValues{
				OTPRequired:   true,
				AuthProviders: []string{" Telegram ", "telegram"},
			},
			configure: func(settings *appsettings.Settings) {
				settings.Channels["telegram"] = appsettings.Channel{Enabled: new(true)}
			},
			wantStatus:    http.StatusOK,
			wantProviders: []string{"telegram"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store, h := newTestHandler(t)
			if tt.configure != nil {
				if _, err := store.Update(t.Context(), func(current *appsettings.Settings) error {
					tt.configure(current)
					return nil
				}); err != nil {
					t.Fatalf("configure settings: %v", err)
				}
			}
			body, err := json.Marshal(tt.request)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			rec := putAuth(t, h, body)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if tt.wantStatus != http.StatusOK {
				return
			}

			snapshot := store.Snapshot()
			if !slices.Equal(snapshot.Auth.AuthProviders, tt.wantProviders) {
				t.Fatalf("AuthProviders = %#v, want %#v", snapshot.Auth.AuthProviders, tt.wantProviders)
			}
			if snapshot.ProxySettings() != appsettings.DefaultProxy() {
				t.Fatalf("proxy settings changed while saving auth")
			}
			if _, ok := snapshot.Modems["modem-1"]; !ok {
				t.Fatal("modem settings were not preserved")
			}
			if _, ok := snapshot.Channels["telegram"]; !ok {
				t.Fatal("notification channels were not preserved")
			}
		})
	}
}

func TestAuthTestValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		body       string
		configure  func(*appsettings.Settings)
		wantStatus int
	}{
		{
			name:       "rejects malformed request",
			body:       `{"authProviders":`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "rejects empty providers",
			body:       `{}`,
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "rejects missing provider",
			body:       `{"authProviders":["telegram"]}`,
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name: "rejects disabled provider",
			body: `{"authProviders":["telegram"]}`,
			configure: func(settings *appsettings.Settings) {
				settings.Channels["telegram"] = appsettings.Channel{Enabled: new(false)}
			},
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name: "rejects invalid provider configuration",
			body: `{"authProviders":["http"]}`,
			configure: func(settings *appsettings.Settings) {
				settings.Channels["http"] = appsettings.Channel{Enabled: new(true)}
			},
			wantStatus: http.StatusUnprocessableEntity,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store, h := newTestHandler(t)
			if tt.configure != nil {
				if _, err := store.Update(t.Context(), func(current *appsettings.Settings) error {
					tt.configure(current)
					return nil
				}); err != nil {
					t.Fatalf("configure settings: %v", err)
				}
			}
			before := store.Snapshot()
			rec := postAuthTest(t, h, []byte(tt.body))
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if after := store.Snapshot(); !reflect.DeepEqual(after, before) {
				t.Fatal("authentication test changed stored settings")
			}
		})
	}
}

func TestAuthTestDelivery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		providers     []string
		larkStatus    int
		wantStatus    int
		wantHTTPCalls int32
		wantLarkCalls int32
	}{
		{
			name:          "tests only the selected provider",
			providers:     []string{"http"},
			larkStatus:    http.StatusOK,
			wantStatus:    http.StatusCreated,
			wantHTTPCalls: 1,
		},
		{
			name:          "succeeds when all selected providers deliver",
			providers:     []string{"http", "lark"},
			larkStatus:    http.StatusOK,
			wantStatus:    http.StatusCreated,
			wantHTTPCalls: 1,
			wantLarkCalls: 1,
		},
		{
			name:          "fails when one selected provider rejects delivery",
			providers:     []string{"http", "lark"},
			larkStatus:    http.StatusBadGateway,
			wantStatus:    http.StatusUnprocessableEntity,
			wantHTTPCalls: 1,
			wantLarkCalls: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			type payload struct {
				Kind    string `json:"kind"`
				Payload struct {
					Code string `json:"code"`
				} `json:"payload"`
			}

			var httpCalls atomic.Int32
			codes := make(chan string, 1)
			httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				httpCalls.Add(1)
				var got payload
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Errorf("decode HTTP test payload: %v", err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if got.Kind != "otp" {
					t.Errorf("kind = %q, want otp", got.Kind)
				}
				codes <- got.Payload.Code
				w.WriteHeader(http.StatusOK)
			}))
			defer httpServer.Close()

			var larkCalls atomic.Int32
			larkServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				larkCalls.Add(1)
				w.WriteHeader(tt.larkStatus)
				if tt.larkStatus >= http.StatusBadRequest {
					_, _ = w.Write([]byte("sensitive upstream detail"))
				}
			}))
			defer larkServer.Close()

			store, h := newTestHandler(t)
			if _, err := store.Update(t.Context(), func(current *appsettings.Settings) error {
				current.Channels["http"] = appsettings.Channel{
					Enabled:  new(true),
					Endpoint: httpServer.URL,
				}
				current.Channels["lark"] = appsettings.Channel{
					Enabled:  new(true),
					Endpoint: larkServer.URL,
				}
				current.Channels["email"] = appsettings.Channel{Enabled: new(true)}
				return nil
			}); err != nil {
				t.Fatalf("configure settings: %v", err)
			}
			before := store.Snapshot()
			body, err := json.Marshal(AuthTestRequest{AuthProviders: tt.providers})
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}

			rec := postAuthTest(t, h, body)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if got := httpCalls.Load(); got != tt.wantHTTPCalls {
				t.Fatalf("HTTP calls = %d, want %d", got, tt.wantHTTPCalls)
			}
			if got := larkCalls.Load(); got != tt.wantLarkCalls {
				t.Fatalf("Lark calls = %d, want %d", got, tt.wantLarkCalls)
			}
			if tt.wantHTTPCalls > 0 {
				if got := <-codes; got != "000000" {
					t.Fatalf("test code = %q, want 000000", got)
				}
			}
			if strings.Contains(rec.Body.String(), "sensitive upstream detail") {
				t.Fatalf("response exposed upstream body: %s", rec.Body.String())
			}
			if after := store.Snapshot(); !reflect.DeepEqual(after, before) {
				t.Fatal("authentication test changed stored settings")
			}
		})
	}
}

func TestUpdateProxy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		request    ProxyValues
		wantStatus int
	}{
		{
			name: "rejects zero http port",
			request: ProxyValues{
				ListenAddress: "127.0.0.1",
				SOCKS5Port:    1080,
			},
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name: "saves only proxy settings",
			request: ProxyValues{
				ListenAddress: " 127.0.0.1 ",
				HTTPPort:      18080,
				SOCKS5Port:    11080,
				Password:      "secret",
			},
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store, h := newTestHandler(t)
			if _, err := store.Update(t.Context(), func(current *appsettings.Settings) error {
				current.Auth.AuthProviders = []string{"telegram"}
				current.Channels["telegram"] = appsettings.Channel{Enabled: new(true)}
				return nil
			}); err != nil {
				t.Fatalf("configure settings: %v", err)
			}
			body, err := json.Marshal(tt.request)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			rec := putProxy(t, h, body)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if tt.wantStatus != http.StatusOK {
				return
			}

			snapshot := store.Snapshot()
			if snapshot.ProxySettings().HTTPPort != tt.request.HTTPPort {
				t.Fatalf("HTTPPort = %d, want %d", snapshot.ProxySettings().HTTPPort, tt.request.HTTPPort)
			}
			if !slices.Equal(snapshot.Auth.AuthProviders, []string{"telegram"}) {
				t.Fatalf("AuthProviders = %#v, want preserved", snapshot.Auth.AuthProviders)
			}
			if _, ok := snapshot.Channels["telegram"]; !ok {
				t.Fatal("notification channels were not preserved")
			}
		})
	}
}

func TestUpdateNotificationChannel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		channel       string
		request       ChannelValues
		configure     func(*appsettings.Settings)
		wantStatus    int
		wantEnabled   bool
		wantProviders []string
		wantEmailHost string
	}{
		{
			name:    "saves one enabled channel",
			channel: "telegram",
			request: ChannelValues{
				Enabled:    new(true),
				BotToken:   "token",
				Recipients: []string{"123456"},
			},
			configure: func(settings *appsettings.Settings) {
				settings.Channels["email"] = appsettings.Channel{
					Enabled:  new(false),
					SMTPHost: "smtp.example.com",
				}
			},
			wantStatus:    http.StatusOK,
			wantEnabled:   true,
			wantEmailHost: "smtp.example.com",
		},
		{
			name:    "saves disabled channel draft and removes auth provider",
			channel: "telegram",
			request: ChannelValues{
				Enabled:    new(false),
				BotToken:   "draft-token",
				Recipients: []string{"123456"},
			},
			configure: func(settings *appsettings.Settings) {
				settings.Auth.AuthProviders = []string{"telegram"}
				settings.Channels["telegram"] = appsettings.Channel{
					Enabled:    new(true),
					BotToken:   "old-token",
					Recipients: appsettings.Recipients{"123456"},
				}
			},
			wantStatus:    http.StatusOK,
			wantProviders: []string{},
		},
		{
			name:       "rejects unknown channel",
			channel:    "unknown",
			request:    ChannelValues{Enabled: new(false)},
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "rejects missing enabled flag",
			channel:    "telegram",
			request:    ChannelValues{BotToken: "draft-token"},
			wantStatus: http.StatusUnprocessableEntity,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store, h := newTestHandler(t)
			if tt.configure != nil {
				if _, err := store.Update(t.Context(), func(current *appsettings.Settings) error {
					tt.configure(current)
					return nil
				}); err != nil {
					t.Fatalf("configure settings: %v", err)
				}
			}
			body, err := json.Marshal(tt.request)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			rec := putNotificationChannel(t, h, tt.channel, body)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if tt.wantStatus != http.StatusOK {
				return
			}

			snapshot := store.Snapshot()
			if got := snapshot.Channels[tt.channel].IsEnabled(); got != tt.wantEnabled {
				t.Fatalf("enabled = %v, want %v", got, tt.wantEnabled)
			}
			if tt.wantProviders != nil && !slices.Equal(snapshot.Auth.AuthProviders, tt.wantProviders) {
				t.Fatalf("AuthProviders = %#v, want %#v", snapshot.Auth.AuthProviders, tt.wantProviders)
			}
			if tt.wantEmailHost != "" && snapshot.Channels["email"].SMTPHost != tt.wantEmailHost {
				t.Fatalf("email SMTP host = %q, want %q", snapshot.Channels["email"].SMTPHost, tt.wantEmailHost)
			}
			if got := snapshot.ProxySettings().HTTPPort; got != appsettings.DefaultProxy().HTTPPort {
				t.Fatalf("proxy HTTP port = %d, want unchanged", got)
			}
		})
	}
}

func TestSettingsUpdatesValidateCurrentStateAtomically(t *testing.T) {
	t.Parallel()

	enabled := true
	disabled := false
	tests := []struct {
		name    string
		updates []func(*appsettings.Settings) error
	}{
		{
			name: "concurrent notification disables",
			updates: []func(*appsettings.Settings) error{
				func(current *appsettings.Settings) error {
					return applyNotificationChannelSettings(
						current,
						"lark",
						appsettings.Channel{Enabled: &disabled, Endpoint: "https://example.com/lark"},
					)
				},
				func(current *appsettings.Settings) error {
					return applyNotificationChannelSettings(
						current,
						"wecom",
						appsettings.Channel{Enabled: &disabled, Endpoint: "https://example.com/wecom"},
					)
				},
			},
		},
		{
			name: "concurrent auth save and provider disable",
			updates: []func(*appsettings.Settings) error{
				func(current *appsettings.Settings) error {
					return applyAuthSettings(current, appsettings.Auth{
						OTPRequired:   true,
						AuthProviders: []string{"lark"},
					})
				},
				func(current *appsettings.Settings) error {
					return applyNotificationChannelSettings(
						current,
						"lark",
						appsettings.Channel{Enabled: &disabled, Endpoint: "https://example.com/lark"},
					)
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			current := appsettings.Default()
			current.Auth = appsettings.Auth{
				OTPRequired:   true,
				AuthProviders: []string{"lark", "wecom"},
			}
			current.Channels = map[string]appsettings.Channel{
				"lark": {
					Enabled:  &enabled,
					Endpoint: "https://example.com/lark",
				},
				"wecom": {
					Enabled:  &enabled,
					Endpoint: "https://example.com/wecom",
				},
			}
			store := appsettings.NewMemoryStore(current)

			start := make(chan struct{})
			results := make(chan error, len(tt.updates))
			var wg sync.WaitGroup
			for _, update := range tt.updates {
				wg.Go(func() {
					<-start
					_, err := store.Update(t.Context(), update)
					results <- err
				})
			}
			close(start)
			wg.Wait()
			close(results)

			successes := 0
			for err := range results {
				if err == nil {
					successes++
				}
			}
			if successes != 1 {
				t.Fatalf("successful updates = %d, want 1", successes)
			}
			if err := validateSettings(store.Snapshot()); err != nil {
				t.Fatalf("stored settings are invalid: %v", err)
			}
		})
	}
}

func TestUpdateProxyPersistsSettingsWhenReloadFails(t *testing.T) {
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

	reqBody := ProxyValues{
		ListenAddress: "127.0.0.1",
		HTTPPort:      occupiedPort,
		SOCKS5Port:    occupiedPort,
		Password:      "new",
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	rec := putProxy(t, h, body)
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

func putAuth(t *testing.T, h *Handler, body []byte) *httptest.ResponseRecorder {
	t.Helper()

	e := echo.New()
	e.Validator = appvalidator.New()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/settings/auth", strings.NewReader(string(body)))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	if err := h.UpdateAuth(c); err != nil {
		t.Fatalf("UpdateAuth() error = %v", err)
	}
	return rec
}

func postAuthTest(t *testing.T, h *Handler, body []byte) *httptest.ResponseRecorder {
	t.Helper()

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/auth-tests", strings.NewReader(string(body)))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	if err := h.TestAuth(c); err != nil {
		t.Fatalf("TestAuth() error = %v", err)
	}
	return rec
}

func putProxy(t *testing.T, h *Handler, body []byte) *httptest.ResponseRecorder {
	t.Helper()

	e := echo.New()
	e.Validator = appvalidator.New()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/settings/proxy", strings.NewReader(string(body)))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	if err := h.UpdateProxy(c); err != nil {
		t.Fatalf("UpdateProxy() error = %v", err)
	}
	return rec
}

func putNotificationChannel(t *testing.T, h *Handler, channel string, body []byte) *httptest.ResponseRecorder {
	t.Helper()

	e := echo.New()
	e.Validator = appvalidator.New()
	req := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/settings/notifications/"+channel,
		strings.NewReader(string(body)),
	)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPathValues(echo.PathValues{{Name: "channel", Value: channel}})
	if err := h.UpdateNotificationChannel(c); err != nil {
		t.Fatalf("UpdateNotificationChannel() error = %v", err)
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
