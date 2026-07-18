package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/auth"
	"github.com/damonto/sigmo/internal/pkg/settings"
	"github.com/damonto/sigmo/internal/pkg/storage"
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

			db := openAuthTestStorage(t)
			authStore, err := auth.NewStore(db)
			if err != nil {
				t.Fatalf("auth.NewStore() error = %v", err)
			}
			settingsStore := settings.NewMemoryStore(&settings.Settings{Auth: settings.Auth{OTPRequired: tt.required}})
			token := ""
			if tt.withToken {
				issued, _, err := authStore.IssueToken(t.Context(), 30*24*time.Hour)
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
			handler := Auth(authStore, settingsStore)(func(c *echo.Context) error {
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

func TestAuthReturnsInternalErrorWhenStorageFails(t *testing.T) {
	db := openAuthTestStorage(t)
	authStore, err := auth.NewStore(db)
	if err != nil {
		t.Fatalf("auth.NewStore() error = %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	handler := Auth(
		authStore,
		settings.NewMemoryStore(&settings.Settings{Auth: settings.Auth{OTPRequired: true}}),
	)(func(c *echo.Context) error {
		return c.NoContent(http.StatusNoContent)
	})

	if err := handler(c); err != nil {
		t.Fatalf("handler() error = %v", err)
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if !strings.Contains(rec.Body.String(), `"error_code":"validate_token_failed"`) {
		t.Fatalf("body = %s, want validate_token_failed", rec.Body.String())
	}
}

func openAuthTestStorage(t *testing.T) *storage.Store {
	t.Helper()
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "sigmo.db"))
	if err != nil {
		t.Fatalf("storage.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})
	return db
}
