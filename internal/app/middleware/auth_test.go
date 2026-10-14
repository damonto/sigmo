package middleware

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/auth"
	"github.com/damonto/sigmo/internal/pkg/settings"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

func TestAuthenticationBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, mode, transport, method, origin string
		want                                  int
	}{
		{"anonymous", "none", "", "GET", "", 204},
		{"missing credential", "otp", "", "GET", "", 401},
		{"native bearer", "otp", "bearer", "GET", "", 204},
		{"query credential rejected", "otp", "query", "GET", "", 401},
		{"cookie read", "otp", "cookie", "GET", "", 204},
		{"cookie mutation missing origin", "otp", "cookie", "POST", "", 403},
		{"cookie same origin", "otp", "cookie", "POST", "https://sigmo.example", 204},
		{"cookie cross origin", "otp", "cookie", "POST", "https://evil.example", 403},
		{"native cannot use cookie credential", "otp", "wrong-kind", "GET", "", 401},
		{"Access cannot be bypassed by app token", "cloudflare_access", "bearer", "GET", "", 401},
		{"unknown policy fails closed", "unknown", "", "GET", "", 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := openAuthTestStorage(t)
			s, err := auth.NewStore(db)
			if err != nil {
				t.Fatal(err)
			}
			cfg := settings.Default()
			cfg.Auth.Mode = tc.mode
			store := settings.NewMemoryStore(cfg)
			cfg.Auth = store.Snapshot().Auth
			tokens, err := s.IssueSession(t.Context(), auth.SessionIssue{Native: tc.transport != "cookie" && tc.transport != "wrong-kind", Session: storage.AuthSession{Method: "otp", PolicyID: cfg.Auth.PolicyID(), ExpiresAt: time.Now().Add(time.Hour)}})
			if err != nil {
				t.Fatal(err)
			}
			target := "https://sigmo.example/"
			if tc.transport == "query" {
				target += "?token=" + tokens.AccessToken
			}
			req := httptest.NewRequest(tc.method, target, nil)
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			if tc.transport == "bearer" || tc.transport == "wrong-kind" {
				req.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
			}
			if tc.transport == "cookie" {
				req.AddCookie(&http.Cookie{Name: SessionCookie, Value: tokens.AccessToken})
			}
			rec := httptest.NewRecorder()
			e := echo.New()
			if err := Auth(s, store)(func(c *echo.Context) error { return c.NoContent(204) })(e.NewContext(req, rec)); err != nil {
				t.Fatal(err)
			}
			if rec.Code != tc.want {
				t.Fatalf("status=%d want=%d body=%s", rec.Code, tc.want, rec.Body)
			}
		})
	}
}
func openAuthTestStorage(t *testing.T) *storage.Store {
	t.Helper()
	s, err := storage.Open(t.Context(), filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	return s
}

func TestResourcePreflightDoesNotBypassActualAccessRequests(t *testing.T) {
	s, err := auth.NewStore(openAuthTestStorage(t))
	if err != nil {
		t.Fatal(err)
	}
	cfg := settings.Default()
	cfg.Auth.Mode = "cloudflare_access"
	store := settings.NewMemoryStore(cfg)
	for _, tc := range []struct {
		name     string
		boundary echo.MiddlewareFunc
		method   string
		want     int
	}{
		{"resource preflight", ResourceAccessBoundary(s, store), http.MethodOptions, http.StatusNoContent},
		{"resource read", ResourceAccessBoundary(s, store), http.MethodGet, http.StatusUnauthorized},
		{"resource mutation", ResourceAccessBoundary(s, store), http.MethodPost, http.StatusUnauthorized},
		{"general preflight", AccessBoundary(s, store), http.MethodOptions, http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := echo.New()
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(tc.method, "/", nil)
			req.Header.Set("Origin", "null")
			if err := tc.boundary(func(c *echo.Context) error { return c.NoContent(http.StatusNoContent) })(e.NewContext(req, rec)); err != nil {
				t.Fatal(err)
			}
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
		})
	}
}
