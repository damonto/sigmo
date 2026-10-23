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
	"sync/atomic"
	"testing"
	"time"

	"github.com/labstack/echo/v5"

	appauth "github.com/damonto/sigmo/internal/app/auth"
	"github.com/damonto/sigmo/internal/app/forwarder"
	"github.com/damonto/sigmo/internal/app/httpapi"
	"github.com/damonto/sigmo/internal/pkg/internet"
	appsettings "github.com/damonto/sigmo/internal/pkg/settings"
	"github.com/damonto/sigmo/internal/pkg/storage"
	appvalidator "github.com/damonto/sigmo/internal/pkg/validator"
)

func TestSettingsSchema(t *testing.T) {
	t.Parallel()

	schema := settingsSchema()
	tokenValidity, ok := fieldSchema(schema.Auth, "sessionMaxDays")
	if !ok {
		t.Fatal("auth sessionMaxDays field not found")
	}
	if tokenValidity.Control != controlNumber || tokenValidity.Min == nil || *tokenValidity.Min != 1 {
		t.Fatalf("sessionMaxDays schema = %+v, want number with minimum 1", tokenValidity)
	}
	if tokenValidity.Max == nil || *tokenValidity.Max != appsettings.MaxSessionMaxDays {
		t.Fatalf("sessionMaxDays max = %v, want %d", tokenValidity.Max, appsettings.MaxSessionMaxDays)
	}
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
		`"otpChannels"`,
		`"mode"`,
		`"sessionMaxDays"`,
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
	for _, tt := range []struct {
		name string
		req  UpdateAuthRequest
		want int
	}{
		{"missing mode", UpdateAuthRequest{SessionMaxDays: 30}, 422},
		{"retired combined mode", UpdateAuthRequest{Mode: "sigmo", SessionMaxDays: 30}, 422},
		{"missing OTP destination", UpdateAuthRequest{Mode: "otp", SessionMaxDays: 30}, 422},
		{"OTP independently of notifications", UpdateAuthRequest{Mode: "otp", OTPChannels: []string{"http"}, SessionMaxDays: 90}, 200},
		{"OIDC accepts an empty admission list", UpdateAuthRequest{Mode: "oidc", OIDCIssuer: "https://issuer.example", OIDCClientID: "client", OIDCClientSecret: "secret", SessionMaxDays: 30}, 200},
		{"OIDC rejects a blank admission entry", UpdateAuthRequest{Mode: "oidc", OIDCIssuer: "https://issuer.example", OIDCClientID: "client", OIDCClientSecret: "secret", OIDCAllowedSubjects: []string{" "}, SessionMaxDays: 30}, 422},
		{"OIDC with admission list", UpdateAuthRequest{Mode: "oidc", OIDCIssuer: "https://issuer.example", OIDCClientID: "client", OIDCClientSecret: "secret", OIDCAllowedSubjects: []string{"owner"}, SessionMaxDays: 30}, 200},
		{"invalid lifetime", UpdateAuthRequest{Mode: "none", SessionMaxDays: 181}, 422},
		{"explicit anonymous", UpdateAuthRequest{Mode: "none", SessionMaxDays: 30}, 200},
	} {
		t.Run(tt.name, func(t *testing.T) {
			store, h := newTestHandler(t)
			if _, err := store.Update(t.Context(), func(s *appsettings.Settings) error {
				s.Channels["http"] = appsettings.Channel{Endpoint: "https://example.com"}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			body, err := json.Marshal(tt.req)
			if err != nil {
				t.Fatal(err)
			}
			rec := putAuth(t, h, body)
			if rec.Code != tt.want {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body)
			}
			if len(store.Snapshot().Notifications.Channels) != 0 {
				t.Fatal("auth enabled business notifications")
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
			body:       `{"channels":`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "rejects empty providers",
			body:       `{}`,
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "rejects missing provider",
			body:       `{"channels":["telegram"]}`,
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name: "rejects incomplete provider",
			body: `{"channels":["telegram"]}`,
			configure: func(settings *appsettings.Settings) {
				settings.Channels["telegram"] = appsettings.Channel{}
			},
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name: "rejects invalid provider configuration",
			body: `{"channels":["http"]}`,
			configure: func(settings *appsettings.Settings) {
				settings.Channels["http"] = appsettings.Channel{}
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
			name:          "succeeds when one selected provider delivers",
			providers:     []string{"http", "lark"},
			larkStatus:    http.StatusBadGateway,
			wantStatus:    http.StatusCreated,
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
					Endpoint: httpServer.URL,
				}
				current.Channels["lark"] = appsettings.Channel{
					Endpoint: larkServer.URL,
				}
				current.Channels["email"] = appsettings.Channel{}
				return nil
			}); err != nil {
				t.Fatalf("configure settings: %v", err)
			}
			before := store.Snapshot()
			body, err := json.Marshal(AuthTestRequest{Channels: tt.providers})
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

func TestOIDCTestCandidatePreservesSavedSecret(t *testing.T) {
	store, h := newTestHandler(t)
	if _, err := store.Update(t.Context(), func(current *appsettings.Settings) error {
		current.Auth = appsettings.Auth{
			Mode: "oidc", SessionMaxDays: 30,
			OIDC: appsettings.OIDC{
				Issuer: "https://issuer.example", ClientID: "client", ClientSecret: "saved",
				AllowedSubjects: []string{"owner"},
			},
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	base := UpdateAuthRequest{
		Mode: "oidc", SessionMaxDays: 30, OIDCIssuer: "https://issuer.example",
		OIDCClientID: "client", OIDCAllowedSubjects: []string{"owner"},
	}
	candidate, err := h.authTestCandidate(base, "oidc")
	if err != nil {
		t.Fatal(err)
	}
	if candidate.OIDC.ClientSecret != "saved" {
		t.Fatalf("client secret = %q, want saved", candidate.OIDC.ClientSecret)
	}
	base.OIDCClientID = "other-client"
	if _, err := h.authTestCandidate(base, "oidc"); err == nil {
		t.Fatal("changed client without a secret was accepted")
	}
}

func TestAuthTestAllowsEmptyAdmissionListForSubjectDiscovery(t *testing.T) {
	_, h := newTestHandler(t)
	for _, test := range []struct {
		mode string
		req  UpdateAuthRequest
	}{
		{
			mode: "oidc",
			req: UpdateAuthRequest{
				Mode: "oidc", SessionMaxDays: 30, OIDCIssuer: "https://issuer.example",
				OIDCClientID: "client", OIDCClientSecret: "secret",
			},
		},
		{
			mode: "cloudflare_access",
			req: UpdateAuthRequest{
				Mode: "cloudflare_access", SessionMaxDays: 30,
				AccessTeamDomain: "https://team.cloudflareaccess.com", AccessAudience: "aud",
			},
		},
	} {
		t.Run(test.mode, func(t *testing.T) {
			candidate, err := h.authTestCandidate(test.req, test.mode)
			if err != nil {
				t.Fatal(err)
			}
			if test.mode == "oidc" && len(candidate.OIDC.AllowedSubjects) != 0 {
				t.Fatal("test candidate changed the OIDC admission list")
			}
			if test.mode == "cloudflare_access" && len(candidate.CloudflareAccess.AllowedSubjects) != 0 {
				t.Fatal("test candidate changed the Access admission list")
			}
		})
	}
}

func TestCreateOIDCTestDoesNotPersistCandidate(t *testing.T) {
	store, h := newTestHandler(t)
	before := store.Snapshot()
	body, err := json.Marshal(OIDCTestSessionRequest{
		Client: "web", BasePath: "/sigmo",
		Auth: AuthValues{
			Mode: "oidc", SessionMaxDays: 30, OIDCIssuer: "https://issuer.example",
			OIDCClientID: "client", OIDCClientSecret: "draft-secret",
			OIDCAllowedSubjects: []string{"owner"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	e := echo.New()
	e.HTTPErrorHandler = httpapi.HTTPErrorHandler
	req := httptest.NewRequest(http.MethodPost, "https://sigmo.example/sigmo/api/v1/settings/auth-tests/oidc", strings.NewReader(string(body)))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	if err := h.CreateOIDCTest(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var response OIDCTestSessionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.ID == "" || !strings.HasPrefix(response.AuthorizationURL, "https://sigmo.example/sigmo/api/v1/settings/auth-tests/oidc/") {
		t.Fatalf("response = %+v", response)
	}
	if strings.Contains(rec.Body.String(), "draft-secret") {
		t.Fatal("response exposed the OIDC client secret")
	}
	statusRequest := httptest.NewRequest(http.MethodGet, "/api/v1/settings/auth-tests/oidc/"+response.ID, nil)
	statusRecorder := httptest.NewRecorder()
	statusContext := e.NewContext(statusRequest, statusRecorder)
	statusContext.SetPathValues(echo.PathValues{{Name: "id", Value: response.ID}})
	if err := h.GetOIDCTest(statusContext); err != nil {
		t.Fatal(err)
	}
	var status struct {
		Status    string    `json:"status"`
		ExpiresAt time.Time `json:"expiresAt"`
	}
	if err := json.Unmarshal(statusRecorder.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.Status != string(appauth.AuthTestStatusPending) || !status.ExpiresAt.After(time.Now()) {
		t.Fatalf("status = %+v", status)
	}
	if after := store.Snapshot(); !reflect.DeepEqual(after, before) {
		t.Fatal("creating an OIDC test changed stored settings")
	}
}

func TestCreateOIDCTestRequiresHTTPS(t *testing.T) {
	_, h := newTestHandler(t)
	body, err := json.Marshal(OIDCTestSessionRequest{
		Client: "web",
		Auth: AuthValues{
			Mode: "oidc", SessionMaxDays: 30, OIDCIssuer: "https://issuer.example",
			OIDCClientID: "client", OIDCClientSecret: "secret",
			OIDCAllowedSubjects: []string{"owner"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	e := echo.New()
	e.HTTPErrorHandler = httpapi.HTTPErrorHandler
	req := httptest.NewRequest(http.MethodPost, "http://sigmo.example/api/v1/settings/auth-tests/oidc", strings.NewReader(string(body)))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	if err := h.CreateOIDCTest(e.NewContext(req, rec)); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"error_code":"test_auth_https_required"`) {
		t.Fatalf("body = %s, want HTTPS-required problem code", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "require HTTPS") {
		t.Fatalf("body = %s, want HTTPS guidance", rec.Body.String())
	}
}

func TestCloudflareAccessTestReturnsMissingAssertionWithoutPersisting(t *testing.T) {
	store, h := newTestHandler(t)
	before := store.Snapshot()
	body, err := json.Marshal(UpdateAuthRequest{
		Mode: "cloudflare_access", SessionMaxDays: 30,
		AccessTeamDomain: "https://team.cloudflareaccess.com", AccessAudience: "aud",
		AccessAllowedSubjects: []string{"owner"},
	})
	if err != nil {
		t.Fatal(err)
	}
	e := echo.New()
	e.HTTPErrorHandler = httpapi.HTTPErrorHandler
	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/auth-tests/cloudflare-access", strings.NewReader(string(body)))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	if err := h.TestCloudflareAccess(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var result struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "failed" || result.Reason != "access_assertion_missing" {
		t.Fatalf("result = %+v", result)
	}
	if after := store.Snapshot(); !reflect.DeepEqual(after, before) {
		t.Fatal("Access test changed stored settings")
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
				current.Auth.OTP.Channels = []string{"telegram"}
				current.Channels["telegram"] = appsettings.Channel{}
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
			if !slices.Equal(snapshot.Auth.OTP.Channels, []string{"telegram"}) {
				t.Fatalf("AuthProviders = %#v, want preserved", snapshot.Auth.OTP.Channels)
			}
			if _, ok := snapshot.Channels["telegram"]; !ok {
				t.Fatal("notification channels were not preserved")
			}
		})
	}
}

func TestUpdateNotificationChannelValidatesBeforeSelection(t *testing.T) {
	for _, name := range []string{"telegram", "http", "email", "bark", "gotify", "sc3", "lark", "wecom"} {
		t.Run(name, func(t *testing.T) {
			store, h := newTestHandler(t)
			rec := putNotificationChannel(t, h, name, []byte(`{}`))
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422; body = %s", rec.Code, rec.Body)
			}
			if _, ok := store.Snapshot().Channels[name]; ok {
				t.Fatal("invalid channel was saved as configured")
			}
		})
	}
}

func TestUpdateNotificationChannelDoesNotSelectRoutes(t *testing.T) {
	store, h := newTestHandler(t)
	rec := putNotificationChannel(t, h, "http", []byte(`{"endpoint":"https://example.com/notify"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body)
	}
	cfg := store.Snapshot()
	if cfg.Channels["http"].Endpoint != "https://example.com/notify" {
		t.Fatal("valid channel was not saved")
	}
	if cfg.Auth.Mode != "none" || len(cfg.Auth.OTP.Channels) != 0 || len(cfg.Notifications.Channels) != 0 {
		t.Fatal("saving configuration changed authentication or notification routing")
	}

	rec = putNotificationChannel(t, h, "http", []byte(`{}`))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid update status = %d, want 422", rec.Code)
	}
	if store.Snapshot().Channels["http"].Endpoint != cfg.Channels["http"].Endpoint {
		t.Fatal("invalid update replaced an unused channel's valid configuration")
	}
}

func TestUpdateNotificationChannelPreservesRoutes(t *testing.T) {
	store, h := newTestHandler(t)
	if _, err := store.Update(t.Context(), func(s *appsettings.Settings) error {
		s.Channels["http"] = appsettings.Channel{Endpoint: "https://example.com/old"}
		s.Auth.Mode = "otp"

		s.Auth.OTP.Channels = []string{"http"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	rec := putNotificationChannel(t, h, "http", []byte(`{"endpoint":"https://example.com/new"}`))
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body)
	}
	cfg := store.Snapshot()
	if !slices.Equal(cfg.Auth.OTP.Channels, []string{"http"}) || len(cfg.Notifications.Channels) != 0 {
		t.Fatal("saving configuration changed channel routing")
	}
	rec = putNotificationChannel(t, h, "unknown", []byte(`{}`))
	if rec.Code != 422 {
		t.Fatalf("unknown channel status=%d", rec.Code)
	}
	rec = putNotificationChannel(t, h, "http", []byte(`{}`))
	if rec.Code != 422 {
		t.Fatalf("accepted invalid active channel: %d", rec.Code)
	}
	if store.Snapshot().Channels["http"].Endpoint != "https://example.com/new" {
		t.Fatal("invalid update replaced valid configuration")
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
	authStore, err := appauth.NewStore(testStorage(t))
	if err != nil {
		t.Fatalf("auth.NewStore() error = %v", err)
	}
	h := New(store, internetConnector, relay, authStore)

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
	authStore, err := appauth.NewStore(testStorage(t))
	if err != nil {
		t.Fatalf("auth.NewStore() error = %v", err)
	}
	return store, New(store, internetConnector, relay, authStore)
}

func putAuth(t *testing.T, h *Handler, body []byte) *httptest.ResponseRecorder {
	t.Helper()

	e := echo.New()
	e.Validator = newTestValidator(t)
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
	e.HTTPErrorHandler = httpapi.HTTPErrorHandler
	e.POST("/api/v1/settings/auth-tests", h.TestAuth)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/auth-tests", strings.NewReader(string(body)))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func putProxy(t *testing.T, h *Handler, body []byte) *httptest.ResponseRecorder {
	t.Helper()

	e := echo.New()
	e.Validator = newTestValidator(t)
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
	e.Validator = newTestValidator(t)
	req := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/settings/channels/"+channel,
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

func newTestValidator(t *testing.T) *appvalidator.Validator {
	t.Helper()
	requestValidator, err := appvalidator.New()
	if err != nil {
		t.Fatalf("appvalidator.New() error = %v", err)
	}
	return requestValidator
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

func TestOIDCIssuerAndSecretRemainBoundToExactClient(t *testing.T) {
	current := appsettings.Auth{OIDC: appsettings.OIDC{Issuer: "https://issuer.example/", ClientID: "client", ClientSecret: "saved"}}
	req := normalizeAuthValues(UpdateAuthRequest{OIDCIssuer: " https://issuer.example/ ", OIDCClientID: "client"})
	if req.OIDCIssuer != current.OIDC.Issuer {
		t.Fatal("OIDC issuer identifier lost its trailing slash")
	}
	if got := authSettingsFromRequest(current, req).OIDC.ClientSecret; got != "saved" {
		t.Fatal("same client lost its saved secret")
	}
	req.OIDCClientID = "different-client"
	if got := authSettingsFromRequest(current, req).OIDC.ClientSecret; got != "" {
		t.Fatal("secret moved to another client")
	}
}
