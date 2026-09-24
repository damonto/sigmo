package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/auth"
	"github.com/damonto/sigmo/internal/app/httpapi"
	"github.com/damonto/sigmo/internal/pkg/locale"
	"github.com/damonto/sigmo/internal/pkg/settings"
)

func TestLocale(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		otpRequired bool
		header      string
		wantStatus  int
		want        locale.Tag
	}{
		{
			name:       "records the web ui language",
			header:     "zh-CN",
			wantStatus: http.StatusNoContent,
			want:       locale.Chinese,
		},
		{
			name:       "keeps the previous language without the header",
			wantStatus: http.StatusNoContent,
			want:       locale.English,
		},
		{
			name:       "ignores unsupported languages",
			header:     "fr-FR",
			wantStatus: http.StatusNoContent,
			want:       locale.English,
		},
		{
			name:        "ignores unauthenticated requests",
			otpRequired: true,
			header:      "zh",
			wantStatus:  http.StatusUnauthorized,
			want:        locale.English,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			authStore, err := auth.NewStore(openAuthTestStorage(t))
			if err != nil {
				t.Fatalf("auth.NewStore() error = %v", err)
			}
			settingsStore := settings.NewMemoryStore(&settings.Settings{Auth: settings.Auth{OTPRequired: tt.otpRequired}})
			if err := settingsStore.SetLocale(t.Context(), locale.English); err != nil {
				t.Fatalf("SetLocale() error = %v", err)
			}

			// Same order as the router: Auth decides first.
			e := echo.New()
			g := e.Group("")
			g.Use(Auth(authStore, settingsStore), Locale(settingsStore))
			g.GET("/", func(c *echo.Context) error {
				return c.NoContent(http.StatusNoContent)
			})

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.header != "" {
				req.Header.Set(httpapi.LocaleHeader, tt.header)
			}
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if got := settingsStore.Locale(); got != tt.want {
				t.Fatalf("Locale() = %q, want %q", got, tt.want)
			}
		})
	}
}
