package middleware

import (
	"net/http"

	"github.com/damonto/sigmo/internal/app/feature"
	"github.com/damonto/sigmo/internal/app/httpapi"
	"github.com/labstack/echo/v5"
)

// RequireFeature checks the current entitlement before a feature handler runs.
func RequireFeature(policy feature.Policy, name feature.Name) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			if !policy.Allows(name) {
				return httpapi.Error(c, http.StatusForbidden, "license_entitlement_inactive", "an active Pro authorization is required")
			}
			return next(c)
		}
	}
}
