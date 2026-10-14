package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/auth"
	"github.com/damonto/sigmo/internal/app/httpapi"
	"github.com/damonto/sigmo/internal/pkg/settings"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

// SessionCookie names the host-only browser session credential.
const SessionCookie = "sigmo_session"
const sessionKey = "sigmo.auth.session"

// CurrentSession returns the identity verified by the authentication middleware.
func CurrentSession(c *echo.Context) storage.AuthSession {
	session, _ := c.Get(sessionKey).(storage.AuthSession)
	return session
}

// SameOrigin rejects browser requests from another origin, including sibling subdomains.
func SameOrigin(c *echo.Context) bool {
	origin := c.Request().Header.Get("Origin")
	if origin == "" {
		return c.Request().Header.Get("Sec-Fetch-Site") != "cross-site"
	}
	expected := c.Scheme() + "://" + c.Request().Host
	return origin == expected
}

// Auth applies the selected trust boundary and never treats OTP being disabled as anonymous.
func Auth(store *auth.Store, settingsStore *settings.Store) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			cfg := settingsStore.Snapshot().Auth
			if !SameOrigin(c) {
				return httpapi.Error(c, http.StatusForbidden, "origin_not_allowed", "request origin is not allowed")
			}
			var session storage.AuthSession
			var err error
			switch cfg.Mode {
			case "none":
				return next(c)
			case "cloudflare_access":
				if c.Request().Header.Get("Authorization") == "" && isMutation(c.Request().Method) && c.Request().Header.Get("Origin") == "" {
					return httpapi.Error(c, http.StatusForbidden, "origin_required", "browser mutations require an origin")
				}
				session = CurrentSession(c)
				if session.Method != "cloudflare_access" {
					session, err = store.VerifyAccess(c.Request().Context(), auth.AccessRequest{Policy: cfg, Assertion: c.Request().Header.Get("Cf-Access-Jwt-Assertion")})
				}
			case "otp", "oidc":
				raw, kind := "", "access"
				header := c.Request().Header.Get("Authorization")
				if header != "" {
					raw, _ = strings.CutPrefix(header, "Bearer ")
				} else if cookie, cookieErr := c.Cookie(SessionCookie); cookieErr == nil {
					raw, kind = cookie.Value, "web"
					if c.Request().Method != "GET" && c.Request().Method != "HEAD" && c.Request().Method != "OPTIONS" && c.Request().Header.Get("Origin") == "" {
						return httpapi.Error(c, http.StatusForbidden, "origin_required", "browser mutations require an origin")
					}
				}
				if raw == "" {
					return httpapi.Error(c, http.StatusUnauthorized, "invalid_session", "authentication is required")
				}
				session, err = store.Authenticate(c.Request().Context(), auth.SessionRequest{Policy: cfg, Token: raw, Kind: kind})
			default:
				return httpapi.Error(c, http.StatusServiceUnavailable, "invalid_auth_policy", "authentication policy is invalid")
			}
			if errors.Is(err, storage.ErrInvalidSession) {
				return httpapi.Error(c, http.StatusUnauthorized, "invalid_session", "authentication is required")
			}
			if err != nil {
				return httpapi.Internal(c, "validate_session_failed", err)
			}
			c.Set(sessionKey, session)
			if strings.EqualFold(c.Request().Header.Get("Upgrade"), "websocket") {
				// Long-lived operations must observe logout and policy changes as well as expiry.
				ctx, cancel := context.WithDeadline(c.Request().Context(), session.ExpiresAt)
				var wg sync.WaitGroup
				wg.Go(func() {
					ticker := time.NewTicker(15 * time.Second)
					defer ticker.Stop()
					for {
						select {
						case <-ctx.Done():
							return
						case <-ticker.C:
							current := settingsStore.Snapshot().Auth
							if current.PolicyID() != cfg.PolicyID() || !store.SessionActive(ctx, session) {
								cancel()
								return
							}
						}
					}
				})
				defer func() { cancel(); wg.Wait() }()
				c.SetRequest(c.Request().WithContext(ctx))
			}
			return next(c)
		}
	}
}

func isMutation(method string) bool {
	return method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions
}

// ResourceAccessBoundary permits credential-free preflights for capability URLs.
// Resource owners must validate the capability on every method, and their OPTIONS
// handlers must return only CORS metadata. Actual requests always verify Access.
func ResourceAccessBoundary(store *auth.Store, settingsStore *settings.Store) echo.MiddlewareFunc {
	boundary := AccessBoundary(store, settingsStore)
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		protected := boundary(next)
		return func(c *echo.Context) error {
			if c.Request().Method == http.MethodOptions {
				return next(c)
			}
			return protected(c)
		}
	}
}

// AccessBoundary protects discovery and login routes too when Access owns the entry point.
func AccessBoundary(store *auth.Store, settingsStore *settings.Store) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			cfg := settingsStore.Snapshot().Auth
			if cfg.Mode == "cloudflare_access" {
				session, err := store.VerifyAccess(c.Request().Context(), auth.AccessRequest{Policy: cfg, Assertion: c.Request().Header.Get("Cf-Access-Jwt-Assertion")})
				if errors.Is(err, storage.ErrInvalidSession) {
					return httpapi.Error(c, http.StatusUnauthorized, "invalid_session", "authentication is required")
				}
				if err != nil {
					return httpapi.Internal(c, "validate_session_failed", err)
				}
				c.Set(sessionKey, session)
			}
			return next(c)
		}
	}
}
