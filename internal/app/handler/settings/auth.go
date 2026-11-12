package settings

import (
	"cmp"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/auth"
	"github.com/damonto/sigmo/internal/app/handler/authpage"
	"github.com/damonto/sigmo/internal/app/httpapi"
	"github.com/damonto/sigmo/internal/pkg/notify"
	notifyevent "github.com/damonto/sigmo/internal/pkg/notify/event"
	appsettings "github.com/damonto/sigmo/internal/pkg/settings"
)

// AuthHandler manages authentication configuration and candidate-policy tests.
type AuthHandler struct {
	store     *appsettings.Store
	authStore *auth.Store
}

// NewAuthHandler creates authentication settings endpoints.
func NewAuthHandler(store *appsettings.Store, authStore *auth.Store) *AuthHandler {
	return &AuthHandler{store: store, authStore: authStore}
}

var errAuthTestFailed = errors.New("authentication provider test failed")

func (h *AuthHandler) UpdateAuth(c *echo.Context) error {
	var req UpdateAuthRequest
	if err := httpapi.BindJSON(c, &req, "update_auth_invalid_request"); err != nil {
		return err
	}
	req = normalizeAuthValues(req)
	if err := c.Validate(&req); err != nil {
		return httpapi.UnprocessableEntity(c, "update_auth_invalid", err)
	}

	var invalidErr error
	saved, err := h.store.Update(c.Request().Context(), func(current *appsettings.Settings) error {
		auth := authSettingsFromRequest(current.Auth, req)
		invalidErr = applyAuthSettings(current, auth)
		return invalidErr
	})
	if invalidErr != nil {
		return httpapi.UnprocessableEntity(c, "update_auth_invalid", invalidErr)
	}
	if err != nil {
		return httpapi.Internal(c, "update_auth_failed", fmt.Errorf("save auth settings: %w", err))
	}

	return c.JSON(http.StatusOK, responseFromSettings(saved))
}

func (h *AuthHandler) TestAuth(c *echo.Context) error {
	var req AuthTestRequest
	if err := httpapi.BindJSON(c, &req, "test_auth_invalid_request"); err != nil {
		return err
	}
	providers := normalizeNames(trimNames(req.Channels))
	candidate := appsettings.Default().Clone()
	candidate.Auth.Mode = "otp"
	candidate.Auth.OTP.Channels = providers
	current := h.store.Snapshot()
	for _, provider := range providers {
		if channel, ok := current.Channels[provider]; ok {
			candidate.Channels[provider] = channel
		}
	}
	if err := validateSettings(candidate); err != nil {
		return httpapi.UnprocessableEntity(c, "test_auth_invalid", err)
	}

	notifier, err := notify.ForChannels(&candidate, providers)
	if err != nil {
		slog.Error("create authentication test notifier", "error", err, "providers", providers)
		return httpapi.UnprocessableEntity(c, "test_auth_failed", errAuthTestFailed)
	}
	lang := cmp.Or(httpapi.RequestLocale(c), h.store.Locale())
	if err := notifier.SendOTP(c.Request().Context(), lang, notifyevent.OTPEvent{Code: "000000"}); err != nil {
		slog.Error("send authentication provider test", "error", err, "providers", providers)
		return httpapi.UnprocessableEntity(c, "test_auth_failed", errAuthTestFailed)
	}

	return c.NoContent(http.StatusNoContent)
}

func (h *AuthHandler) CreateOIDCTest(c *echo.Context) error {
	var req OIDCTestSessionRequest
	if err := httpapi.BindJSON(c, &req, "test_auth_invalid_request"); err != nil {
		return err
	}
	if req.Client != "web" && req.Client != "native" {
		return httpapi.UnprocessableEntity(c, "test_auth_invalid", errors.New("test client must be web or native"))
	}
	candidate, err := h.authTestCandidate(UpdateAuthRequest(req.Auth), "oidc")
	if err != nil {
		return httpapi.UnprocessableEntity(c, "test_auth_invalid", err)
	}
	origin := c.Scheme() + "://" + c.Request().Host
	if c.Scheme() != "https" {
		return httpapi.UnprocessableEntity(
			c,
			"test_auth_https_required",
			errors.New("OIDC authentication tests require HTTPS; open Sigmo through its HTTPS URL"),
		)
	}
	id, expiresAt, err := h.authStore.CreateOIDCTest(auth.OIDCTestRequest{
		Policy: candidate, Origin: origin, BasePath: req.BasePath,
		Native: req.Client == "native", State: req.State,
	})
	if errors.Is(err, auth.ErrRateLimited) {
		return httpapi.TooManyRequests(c, "too_many_requests", err)
	}
	if err != nil {
		return httpapi.UnprocessableEntity(c, "test_auth_invalid", err)
	}
	target := origin + req.BasePath + "/api/v1/settings/auth-tests/oidc/" + url.PathEscape(id) + "/browser"
	httpapi.SetResourceLocation(c, id)
	return c.JSON(http.StatusCreated, OIDCTestSessionResponse{
		ID: id, AuthorizationURL: target, ExpiresAt: expiresAt,
	})
}

func (h *AuthHandler) StartOIDCTest(c *echo.Context) error {
	result, err := h.authStore.StartOIDCTest(c.Request().Context(), auth.OIDCTestStartRequest{
		ID: c.Param("id"), Origin: c.Scheme() + "://" + c.Request().Host,
	})
	if err != nil {
		status, _ := h.authStore.OIDCTestResult(c.Param("id"))
		slog.Error("start OIDC test", "error", err, "test_id", c.Param("id"))
		return authpage.Write(c, status.Status == auth.AuthTestStatusSucceeded, h.store.Locale())
	}
	c.SetCookie(&http.Cookie{
		Name: "sigmo_login_" + result.State, Value: result.Binding, Path: "/", MaxAge: 300,
		HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,
	})
	c.Response().Header().Set("Referrer-Policy", "no-referrer")
	return c.Redirect(http.StatusSeeOther, result.URL)
}

func (h *AuthHandler) GetOIDCTest(c *echo.Context) error {
	result, ok := h.authStore.OIDCTestResult(c.Param("id"))
	if !ok {
		return httpapi.NotFound(c, "resource_not_found", errors.New("authentication test was not found or has expired"))
	}
	return c.JSON(http.StatusOK, result)
}

func (h *AuthHandler) TestCloudflareAccess(c *echo.Context) error {
	var req UpdateAuthRequest
	if err := httpapi.BindJSON(c, &req, "test_auth_invalid_request"); err != nil {
		return err
	}
	candidate, err := h.authTestCandidate(req, "cloudflare_access")
	if err != nil {
		return httpapi.UnprocessableEntity(c, "test_auth_invalid", err)
	}
	result := h.authStore.TestCloudflareAccess(
		c.Request().Context(),
		candidate,
		c.Request().Header.Get("Cf-Access-Jwt-Assertion"),
	)
	return c.JSON(http.StatusOK, result)
}

func (h *AuthHandler) authTestCandidate(req UpdateAuthRequest, mode string) (appsettings.Auth, error) {
	req = normalizeAuthValues(req)
	if req.Mode != mode {
		return appsettings.Auth{}, fmt.Errorf("test policy mode must be %s", mode)
	}
	current := h.store.Snapshot()
	candidate := authSettingsFromRequest(current.Auth, req)
	current.Auth = candidate
	if err := validateSettingsWithOptions(current, appsettings.ValidationOptions{
		AllowEmptyAccessSubjects: mode == "cloudflare_access",
	}); err != nil {
		return appsettings.Auth{}, err
	}
	return candidate, nil
}
