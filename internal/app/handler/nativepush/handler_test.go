package nativepush

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damonto/sigmo/internal/pkg/storage"
	"github.com/labstack/echo/v5"
)

func TestSubscriptionMetadataDoesNotExposeCredentials(t *testing.T) {
	s, err := storage.Open(t.Context(), filepath.Join(t.TempDir(), "push.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	h := New(s)
	e := echo.New()
	e.POST("/subscriptions", h.Register)
	e.GET("/subscriptions", h.List)
	value := registration{ID: "subscription-00001", InstallationID: "installation-00001", SendToken: strings.Repeat("s", 32), EncryptionKey: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32)), SMS: true, Enabled: true}
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/subscriptions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer login-secret")
	res := httptest.NewRecorder()
	e.ServeHTTP(res, req)
	if res.Code != 201 {
		t.Fatalf("register=%d: %s", res.Code, res.Body.String())
	}
	res = httptest.NewRecorder()
	e.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/subscriptions", nil))
	if res.Code != 200 {
		t.Fatalf("list=%d", res.Code)
	}
	for _, secret := range []string{value.SendToken, value.EncryptionKey, "login-secret", "authTokenHash", "encryptionKey", "sendToken"} {
		if strings.Contains(res.Body.String(), secret) {
			t.Fatalf("response exposed credential field %q", secret)
		}
	}
	if !strings.Contains(res.Body.String(), "backendId") {
		t.Fatal("backend identity missing")
	}
}
