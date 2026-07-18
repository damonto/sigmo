package webpush

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/pkg/storage"
	push "github.com/damonto/sigmo/internal/pkg/webpush"
)

func TestHandlerWebPushState(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		body       string
		wantStatus int
		wantEnable bool
	}{
		{name: "get defaults enabled", method: http.MethodGet, wantStatus: http.StatusOK, wantEnable: true},
		{name: "disable", method: http.MethodPut, body: `{"enabled":false}`, wantStatus: http.StatusOK},
		{name: "missing enabled", method: http.MethodPut, body: `{}`, wantStatus: http.StatusUnprocessableEntity},
		{name: "invalid JSON", method: http.MethodPut, body: `{`, wantStatus: http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, closeStore := testClient(t)
			defer closeStore()
			h := New(client)
			req := httptest.NewRequest(tt.method, "/api/v1/web-push", strings.NewReader(tt.body))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			rec := httptest.NewRecorder()
			c := echo.New().NewContext(req, rec)
			var err error
			if tt.method == http.MethodGet {
				err = h.Get(c)
			} else {
				err = h.Update(c)
			}
			if err != nil {
				t.Fatalf("handler error = %v", err)
			}
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if tt.wantStatus == http.StatusOK {
				var response overviewResponse
				if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
					t.Fatalf("decode response: %v", err)
				}
				if response.Enabled != tt.wantEnable || response.PublicKey == "" {
					t.Fatalf("response = %+v", response)
				}
			}
		})
	}
}

func TestHandlerRejectsInvalidRegistration(t *testing.T) {
	client, closeStore := testClient(t)
	defer closeStore()
	h := New(client)
	tests := []struct {
		name string
		body map[string]any
	}{
		{name: "HTTP endpoint", body: map[string]any{"endpoint": "http://push.example/sub", "label": "Phone"}},
		{name: "private endpoint", body: validRegistrationBody(t, "https://127.0.0.1/push")},
		{name: "invalid keys", body: map[string]any{"endpoint": "https://push.example/sub", "label": "Phone", "keys": map[string]string{"p256dh": "bad", "auth": "bad"}}},
		{name: "empty label", body: map[string]any{"endpoint": "https://push.example/sub", "label": ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, err := json.Marshal(tt.body)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			req := httptest.NewRequest(http.MethodPost, "/api/v1/web-push/subscriptions", bytes.NewReader(body))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			rec := httptest.NewRecorder()
			c := echo.New().NewContext(req, rec)
			if err := h.Register(c); err != nil {
				t.Fatalf("Register() error = %v", err)
			}
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
			}
		})
	}
}

func validRegistrationBody(t *testing.T, endpoint string) map[string]any {
	t.Helper()
	_, p256dh, auth := pushTestSubscription(t)
	return map[string]any{
		"endpoint": endpoint,
		"label":    "Phone",
		"keys":     map[string]string{"p256dh": p256dh, "auth": auth},
	}
}

func pushTestSubscription(t *testing.T) (string, string, string) {
	t.Helper()
	privateKey, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	return "https://push.example/sub", base64.RawURLEncoding.EncodeToString(privateKey.PublicKey().Bytes()), base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("a", 16)))
}

func testClient(t *testing.T) (*push.Client, func()) {
	t.Helper()
	store, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "sigmo.db"))
	if err != nil {
		t.Fatalf("storage.Open() error = %v", err)
	}
	client, err := push.New(context.Background(), store)
	if err != nil {
		_ = store.Close()
		t.Fatalf("webpush.New() error = %v", err)
	}
	return client, func() { _ = store.Close() }
}
