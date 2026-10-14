package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"

	appauth "github.com/damonto/sigmo/internal/app/auth"
	"github.com/damonto/sigmo/internal/app/httpapi"
	"github.com/damonto/sigmo/internal/pkg/settings"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

func TestBrowserSessionUsesRequestAddress(t *testing.T) {
	for _, tt := range []struct {
		name, address, peer, origin, proto, forwardedHost, basePath string
		wantStatus                                                  int
		wantBase                                                    string
	}{
		{name: "direct HTTPS", address: "https://sigmo.example", wantStatus: 201, wantBase: "https://sigmo.example"},
		{name: "alternate host and port", address: "https://alternate.example:8443", wantStatus: 201, wantBase: "https://alternate.example:8443"},
		{name: "proxy terminates TLS", address: "http://sigmo.example", peer: "127.0.0.1:1234", origin: "https://sigmo.example", proto: "https", wantStatus: 201, wantBase: "https://sigmo.example"},
		{name: "proxy strips path prefix", address: "http://sigmo.example", peer: "10.0.0.2:1234", proto: "https", basePath: "/sigmo", wantStatus: 201, wantBase: "https://sigmo.example/sigmo"},
		{name: "ignore forwarded host", address: "https://sigmo.example", forwardedHost: "evil.example", wantStatus: 201, wantBase: "https://sigmo.example"},
		{name: "reject forged scheme", address: "http://sigmo.example", peer: "203.0.113.1:1234", proto: "https", wantStatus: 401},
		{name: "reject plain HTTP", address: "http://sigmo.example", wantStatus: 401},
		{name: "reject cross origin", address: "https://sigmo.example", origin: "https://evil.example", wantStatus: 403},
		{name: "reject redirected prefix", address: "https://sigmo.example", basePath: "//evil.example", wantStatus: 401},
	} {
		for _, client := range []string{"web", "native"} {
			t.Run(tt.name+"/"+client, func(t *testing.T) {
				db, err := storage.Open(t.Context(), filepath.Join(t.TempDir(), "auth.db"))
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := db.Close(); err != nil {
						t.Error(err)
					}
				})
				s, err := appauth.NewStore(db)
				if err != nil {
					t.Fatal(err)
				}
				cfg := settings.Default()
				cfg.Auth.Mode = "oidc"
				h := New(settings.NewMemoryStore(cfg), s)
				e := echo.New()
				e.HTTPErrorHandler = httpapi.HTTPErrorHandler
				e.POST("/api/v1/auth/browser-sessions", h.CreateBrowserSession)
				body, err := json.Marshal(map[string]string{
					"client": client, "basePath": tt.basePath,
					"state": strings.Repeat("s", 32), "codeChallenge": strings.Repeat("a", 43),
				})
				if err != nil {
					t.Fatal(err)
				}
				req := httptest.NewRequest(http.MethodPost, tt.address+"/api/v1/auth/browser-sessions", strings.NewReader(string(body)))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Origin", tt.origin)
				req.Header.Set("X-Forwarded-Proto", tt.proto)
				req.Header.Set("X-Forwarded-Host", tt.forwardedHost)
				if tt.peer != "" {
					req.RemoteAddr = tt.peer
				}
				rec := httptest.NewRecorder()
				e.ServeHTTP(rec, req)
				if rec.Code != tt.wantStatus {
					t.Fatalf("status = %d, want %d: %s", rec.Code, tt.wantStatus, rec.Body)
				}
				if tt.wantStatus != http.StatusCreated {
					return
				}
				var response struct {
					AuthorizationURL string `json:"authorizationUrl"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
					t.Fatal(err)
				}
				if !strings.HasPrefix(response.AuthorizationURL, tt.wantBase+"/api/v1/auth/browser-sessions/") {
					t.Fatalf("authorization URL = %q, want base %q", response.AuthorizationURL, tt.wantBase)
				}
			})
		}
	}
}
