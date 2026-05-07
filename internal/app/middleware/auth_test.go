package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/auth"
	"github.com/damonto/sigmo/internal/pkg/config"
)

func TestAuth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		required   bool
		withToken  bool
		wantStatus int
	}{
		{
			name:       "disabled allows request without token",
			wantStatus: http.StatusNoContent,
		},
		{
			name:       "enabled rejects missing token",
			required:   true,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "enabled accepts valid token",
			required:   true,
			withToken:  true,
			wantStatus: http.StatusNoContent,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			authStore := auth.NewStore()
			configStore := config.NewStore(&config.Config{App: config.App{OTPRequired: tt.required}})
			token := ""
			if tt.withToken {
				issued, _, err := authStore.IssueToken()
				if err != nil {
					t.Fatalf("IssueToken() error = %v", err)
				}
				token = issued
			}

			e := echo.New()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if token != "" {
				req.Header.Set("Authorization", "Bearer "+token)
			}
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			handler := Auth(authStore, configStore)(func(c *echo.Context) error {
				return c.NoContent(http.StatusNoContent)
			})

			if err := handler(c); err != nil {
				t.Fatalf("handler() error = %v", err)
			}
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
		})
	}
}
