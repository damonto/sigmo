package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"

	appauth "github.com/damonto/sigmo/internal/app/auth"
	"github.com/damonto/sigmo/internal/app/httpapi"
	"github.com/damonto/sigmo/internal/app/middleware"
	"github.com/damonto/sigmo/internal/pkg/settings"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

func TestOTPHTTPFlow(t *testing.T) {
	for _, client := range []string{"web", "native"} {
		t.Run(client, func(t *testing.T) {
			var code string
			delivery := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Payload struct {
						Code string `json:"code"`
					} `json:"payload"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				code = body.Payload.Code
				w.WriteHeader(200)
			}))
			defer delivery.Close()
			db, err := storage.Open(t.Context(), filepath.Join(t.TempDir(), "auth.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := db.Close(); err != nil {
					t.Error(err)
				}
			})
			cfg := settings.Default()
			cfg.Auth.Mode = "otp"

			cfg.Auth.OTP.Channels = []string{"http"}
			cfg.Channels["http"] = settings.Channel{Endpoint: delivery.URL}
			store := settings.NewMemoryStore(cfg)
			s, err := appauth.NewStore(db)
			if err != nil {
				t.Fatal(err)
			}
			h := New(store, s)
			e := echo.New()
			e.HTTPErrorHandler = httpapi.HTTPErrorHandler
			e.POST("/challenges", h.CreateOTPChallenge)
			e.POST("/sessions", h.CreateSession)
			e.POST("/refreshes", h.RefreshSession)
			e.GET("/current", h.CurrentSession, middleware.Auth(s, store))
			e.DELETE("/current", h.DeleteSession, middleware.Auth(s, store))
			send := func(method, path, body, token string, cookie *http.Cookie) *httptest.ResponseRecorder {
				req := httptest.NewRequest(method, "https://sigmo.example"+path, strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Origin", "https://sigmo.example")
				if token != "" {
					req.Header.Set("Authorization", "Bearer "+token)
				}
				if cookie != nil {
					req.AddCookie(cookie)
				}
				rec := httptest.NewRecorder()
				e.ServeHTTP(rec, req)
				return rec
			}
			challenge := send("POST", "/challenges", "{}", "", nil)
			if challenge.Code != 201 || code == "" {
				t.Fatalf("challenge %d %s", challenge.Code, challenge.Body)
			}
			var issued struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(challenge.Body.Bytes(), &issued); err != nil {
				t.Fatal(err)
			}
			body, err := json.Marshal(map[string]string{"method": "otp", "client": client, "challengeId": issued.ID, "code": code})
			if err != nil {
				t.Fatal(err)
			}
			response := send("POST", "/sessions", string(body), "", nil)
			if response.Code != 201 {
				t.Fatalf("login %d %s", response.Code, response.Body)
			}
			var tokens appauth.Tokens
			var cookie *http.Cookie
			if client == "web" {
				if response.Body.Len() != 0 {
					t.Fatal("browser received raw credentials")
				}
				cookies := response.Result().Cookies()
				if len(cookies) != 1 {
					t.Fatal("missing cookie")
				}
				cookie = cookies[0]
				if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.Domain != "" {
					t.Fatalf("unsafe cookie: %+v", cookie)
				}
			} else {
				if err := json.Unmarshal(response.Body.Bytes(), &tokens); err != nil {
					t.Fatal(err)
				}
				if tokens.AccessToken == "" || tokens.RefreshToken == "" {
					t.Fatal("missing native credentials")
				}
			}
			if rec := send("GET", "/current", "", tokens.AccessToken, cookie); rec.Code != 200 {
				t.Fatalf("session %d %s", rec.Code, rec.Body)
			}
			if rec := send("POST", "/sessions", string(body), "", nil); rec.Code != 401 {
				t.Fatal("OTP replay accepted")
			}
			if rec := send("DELETE", "/current", "", tokens.AccessToken, cookie); rec.Code != 204 {
				t.Fatal("logout failed")
			}
			if rec := send("GET", "/current", "", tokens.AccessToken, cookie); rec.Code != 401 {
				t.Fatal("logout did not revoke server state")
			}
			if bytes.Contains(challenge.Body.Bytes(), []byte(code)) {
				t.Fatal("challenge response exposed OTP")
			}
		})
	}
}
