package middleware

import (
	"log/slog"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/httpapi"
	"github.com/damonto/sigmo/internal/pkg/settings"
)

// Locale records the web UI language of each request, so that notifications
// sent later, such as a forwarded SMS, use the language the user last saw.
// Register it after Auth so that only authenticated requests can change it.
func Locale(store *settings.Store) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			if lang := httpapi.RequestLocale(c); lang != "" {
				if err := store.SetLocale(c.Request().Context(), lang); err != nil {
					// Notifications keep the previous language, and the request
					// itself does not depend on it, so this is not worth a 500.
					slog.Warn("record web ui locale", "locale", lang, "error", err)
				}
			}
			return next(c)
		}
	}
}
