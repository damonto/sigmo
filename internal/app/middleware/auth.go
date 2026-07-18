package middleware

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/auth"
	"github.com/damonto/sigmo/internal/app/httpapi"
	"github.com/damonto/sigmo/internal/pkg/settings"
)

const bearerPrefix = "Bearer "

func Auth(store *auth.Store, settingsStore *settings.Store) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			if !settingsStore.OTPRequired() {
				return next(c)
			}
			header := c.Request().Header.Get("Authorization")
			token := ""
			if after, ok := strings.CutPrefix(header, bearerPrefix); ok {
				token = strings.TrimSpace(after)
			}
			if token == "" {
				token = strings.TrimSpace(c.QueryParam("token"))
			}
			valid, err := store.ValidateToken(c.Request().Context(), token)
			if err != nil {
				return httpapi.Internal(c, "validate_token_failed", err)
			}
			if !valid {
				return httpapi.Error(c, http.StatusUnauthorized, "invalid_token", "missing or invalid token")
			}
			return next(c)
		}
	}
}
