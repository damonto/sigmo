package auth

import (
	"cmp"
	"errors"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/auth"
	"github.com/damonto/sigmo/internal/app/handler/authpage"
	"github.com/damonto/sigmo/internal/app/httpapi"
	"github.com/damonto/sigmo/internal/app/middleware"
	"github.com/damonto/sigmo/internal/pkg/notify"
	notifyevent "github.com/damonto/sigmo/internal/pkg/notify/event"
	"github.com/damonto/sigmo/internal/pkg/settings"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

// Handler exposes the authentication protocol shared by Web and native clients.
type Handler struct {
	settings *settings.Store
	store    *auth.Store
}

// New uses the same authentication service as the API middleware.
func New(settingsStore *settings.Store, store *auth.Store) *Handler {
	return &Handler{settings: settingsStore, store: store}
}

// Methods exposes capabilities without secrets or admission lists.
func (h *Handler) Methods(c *echo.Context) error {
	cfg := h.settings.Snapshot().Auth
	methods := []string{}
	if cfg.HasMethod("otp") || cfg.HasMethod("oidc") {
		methods = append(methods, cfg.Mode)
	}
	c.Response().Header().Set("Cache-Control", "no-store")
	return c.JSON(http.StatusOK, struct {
		Version int      `json:"version"`
		Mode    string   `json:"mode"`
		Methods []string `json:"methods"`
	}{1, cfg.Mode, methods})
}

func (h *Handler) allow(c *echo.Context) (bool, error) {
	c.Response().Header().Set("Cache-Control", "no-store")
	if !middleware.SameOrigin(c) {
		return false, httpapi.Error(c, http.StatusForbidden, "origin_not_allowed", "request origin is not allowed")
	}
	// Forwarded IP headers are not a trust boundary. Operators may share a proxy
	// allowance; an untrusted caller must never choose a fresh rate-limit bucket.
	source, _, err := net.SplitHostPort(c.Request().RemoteAddr)
	if err != nil {
		source = c.Request().RemoteAddr
	}
	if err := h.store.AllowLogin(source + ":" + c.Path()); err != nil {
		c.Response().Header().Set("Retry-After", "60")
		return false, httpapi.TooManyRequests(c, "login_rate_limited", err)
	}
	return true, nil
}

// CreateOTPChallenge sends only to configured authentication destinations.
func (h *Handler) CreateOTPChallenge(c *echo.Context) error {
	if allowed, err := h.allow(c); !allowed {
		return err
	}
	cfg := h.settings.Snapshot()
	if !cfg.Auth.HasMethod("otp") {
		return httpapi.Error(c, http.StatusForbidden, "method_not_allowed", "OTP is not enabled")
	}
	notifier, err := notify.ForChannels(&cfg, cfg.Auth.OTP.Channels)
	if err != nil {
		return httpapi.Internal(c, "otp_delivery_failed", err)
	}
	id, code, err := h.store.IssueOTP(cfg.Auth.PolicyID())
	if err != nil {
		return h.loginError(c, err)
	}
	if err := notifier.SendOTP(c.Request().Context(), cmp.Or(httpapi.RequestLocale(c), h.settings.Locale()), notifyevent.OTPEvent{Code: code}); err != nil {
		h.store.DiscardOTP(id)
		return httpapi.Internal(c, "otp_delivery_failed", err)
	}
	return c.JSON(http.StatusCreated, struct {
		ID        string    `json:"id"`
		ExpiresAt time.Time `json:"expiresAt"`
	}{id, time.Now().Add(5 * time.Minute)})
}

// CreateSession consumes an OTP challenge or an app-specific OIDC handoff.
func (h *Handler) CreateSession(c *echo.Context) error {
	if allowed, err := h.allow(c); !allowed {
		return err
	}
	var req struct {
		Method       string `json:"method"`
		Client       string `json:"client"`
		ChallengeID  string `json:"challengeId"`
		Code         string `json:"code"`
		CodeVerifier string `json:"codeVerifier"`
	}
	if err := httpapi.BindJSON(c, &req, "invalid_login_request"); err != nil {
		return err
	}
	if req.Client != "web" && req.Client != "native" {
		return h.loginError(c, auth.ErrInvalidLogin)
	}
	cfg := h.settings.Snapshot().Auth
	var identity storage.AuthSession
	switch req.Method {
	case "otp":
		if err := h.store.VerifyOTP(auth.OTPAttempt{Policy: cfg, ID: req.ChallengeID, Code: req.Code}); err != nil {
			return h.loginError(c, err)
		}
		identity = storage.AuthSession{Method: "otp", Issuer: "sigmo", Subject: "owner", PolicyID: cfg.PolicyID(), ExpiresAt: time.Now().Add(time.Duration(cfg.SessionMaxDays) * 24 * time.Hour)}
	case "oidc":
		if req.Client != "native" {
			return h.loginError(c, auth.ErrInvalidLogin)
		}
		var err error
		identity, err = h.store.CompleteHandoff(auth.HandoffRequest{Policy: cfg, Code: req.Code, Verifier: req.CodeVerifier})
		if err != nil {
			return h.loginError(c, err)
		}
	default:
		return h.loginError(c, auth.ErrInvalidLogin)
	}
	tokens, err := h.store.IssueSession(c.Request().Context(), auth.SessionIssue{Session: identity, Native: req.Client == "native"})
	if err != nil {
		return httpapi.Internal(c, "create_session_failed", err)
	}
	if req.Client == "native" {
		return c.JSON(http.StatusCreated, tokens)
	}
	setSessionCookie(c, tokens)
	return c.NoContent(http.StatusCreated)
}

// RefreshSession rotates native credentials without replaying any business request.
func (h *Handler) RefreshSession(c *echo.Context) error {
	if allowed, err := h.allow(c); !allowed {
		return err
	}
	var req struct {
		RefreshToken string `json:"refreshToken"`
	}
	if err := httpapi.BindJSON(c, &req, "invalid_refresh_request"); err != nil {
		return err
	}
	tokens, err := h.store.Refresh(c.Request().Context(), auth.SessionRequest{Policy: h.settings.Snapshot().Auth, Token: req.RefreshToken})
	if err != nil {
		return h.loginError(c, err)
	}
	return c.JSON(http.StatusOK, tokens)
}

// CurrentSession reports the identity already verified by the middleware.
func (h *Handler) CurrentSession(c *echo.Context) error {
	identity := middleware.CurrentSession(c)
	c.Response().Header().Set("Cache-Control", "no-store")
	return c.JSON(http.StatusOK, struct {
		Method    string    `json:"method"`
		Subject   string    `json:"subject"`
		ExpiresAt time.Time `json:"expiresAt"`
	}{identity.Method, identity.Subject, identity.ExpiresAt})
}

// DeleteSession revokes server state before the client discards its local credential.
func (h *Handler) DeleteSession(c *echo.Context) error {
	identity := middleware.CurrentSession(c)
	if identity.ID != "" {
		if err := h.store.Revoke(c.Request().Context(), identity.ID); err != nil {
			return httpapi.Internal(c, "revoke_session_failed", err)
		}
	}
	c.SetCookie(&http.Cookie{Name: middleware.SessionCookie, Value: "", Path: "/", MaxAge: -1, Secure: c.Scheme() == "https", HttpOnly: true, SameSite: http.SameSiteLaxMode})
	return c.NoContent(http.StatusNoContent)
}

// CreateBrowserSession creates a bounded browser login transaction.
func (h *Handler) CreateBrowserSession(c *echo.Context) error {
	if allowed, err := h.allow(c); !allowed {
		return err
	}
	var req struct {
		Client        string `json:"client"`
		BasePath      string `json:"basePath"`
		State         string `json:"state"`
		CodeChallenge string `json:"codeChallenge"`
	}
	if err := httpapi.BindJSON(c, &req, "invalid_browser_request"); err != nil {
		return err
	}
	if req.Client != "web" && req.Client != "native" {
		return h.loginError(c, auth.ErrInvalidLogin)
	}
	target, err := h.store.CreateBrowserLogin(auth.BrowserRequest{
		Policy: h.settings.Snapshot().Auth, Origin: browserOrigin(c), BasePath: req.BasePath,
		Native: req.Client == "native", State: req.State, CodeChallenge: req.CodeChallenge,
	})
	if err != nil {
		return h.loginError(c, err)
	}
	return c.JSON(http.StatusCreated, struct {
		AuthorizationURL string `json:"authorizationUrl"`
	}{target})
}

// StartBrowserSession establishes the browser cookie before visiting the identity provider.
func (h *Handler) StartBrowserSession(c *echo.Context) error {
	if allowed, err := h.allow(c); !allowed {
		return err
	}
	result, err := h.store.StartBrowserLogin(c.Request().Context(), auth.BrowserStartRequest{
		Policy: h.settings.Snapshot().Auth, ID: c.Param("id"), Origin: browserOrigin(c),
	})
	if err != nil {
		return h.loginError(c, err)
	}
	c.SetCookie(&http.Cookie{Name: "sigmo_login_" + result.State, Value: result.Binding, Path: "/", MaxAge: 300, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	c.Response().Header().Set("Referrer-Policy", "no-referrer")
	return c.Redirect(http.StatusSeeOther, result.URL)
}

// OIDCCallback never forwards provider tokens to a browser or native app.
func (h *Handler) OIDCCallback(c *echo.Context) error {
	c.Response().Header().Set("Cache-Control", "no-store")
	c.Response().Header().Set("Referrer-Policy", "no-referrer")
	query := c.Request().URL.Query()
	state := query.Get("state")
	if len(query["state"]) == 1 && len(state) <= 128 && h.store.IsOIDCTest(state) {
		cookie, _ := c.Cookie("sigmo_login_" + state)
		binding := ""
		if cookie != nil {
			binding = cookie.Value
			c.SetCookie(&http.Cookie{Name: cookie.Name, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
		}
		code := ""
		if len(query["code"]) == 1 {
			code = query.Get("code")
		}
		completion := h.store.CompleteOIDCTest(c.Request().Context(), auth.OIDCTestCallback{
			Origin: browserOrigin(c), State: state, Code: code, Binding: binding,
		})
		if completion.Native {
			target := auth.NativeRedirectURL + "?" + url.Values{
				"code":  {completion.TestID},
				"state": {completion.NativeState},
			}.Encode()
			return c.Redirect(http.StatusSeeOther, target)
		}
		return authpage.Write(c, completion.Result.Status == auth.AuthTestStatusSucceeded, h.settings.Locale())
	}
	if len(query["state"]) != 1 || len(query["code"]) != 1 || query.Has("error") || len(state) > 128 {
		return h.loginError(c, auth.ErrInvalidLogin)
	}
	cookie, err := c.Cookie("sigmo_login_" + state)
	if err != nil {
		return h.loginError(c, auth.ErrInvalidLogin)
	}
	c.SetCookie(&http.Cookie{Name: cookie.Name, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	cfg := h.settings.Snapshot().Auth
	result, err := h.store.CompleteBrowserLogin(c.Request().Context(), auth.BrowserCallback{
		Policy: cfg, Origin: browserOrigin(c), State: state, Code: c.QueryParam("code"), Binding: cookie.Value,
	})
	if err != nil {
		return h.loginError(c, err)
	}
	if h.settings.Snapshot().Auth.PolicyID() != cfg.PolicyID() {
		return h.loginError(c, auth.ErrInvalidLogin)
	}
	if result.Native {
		return c.Redirect(http.StatusSeeOther, result.RedirectURL)
	}
	tokens, err := h.store.IssueSession(c.Request().Context(), auth.SessionIssue{Session: result.Identity})
	if err != nil {
		return httpapi.Internal(c, "create_session_failed", err)
	}
	setSessionCookie(c, tokens)
	return c.Redirect(http.StatusSeeOther, result.RedirectURL)
}

func browserOrigin(c *echo.Context) string {
	// Echo v5 accepts scheme headers only from trusted peers. Proxies must preserve
	// Host and overwrite X-Forwarded-Proto; forwarded host headers are not used.
	return c.Scheme() + "://" + c.Request().Host
}

func (h *Handler) loginError(c *echo.Context, err error) error {
	if errors.Is(err, auth.ErrRateLimited) {
		return httpapi.TooManyRequests(c, "login_rate_limited", err)
	}
	if errors.Is(err, auth.ErrInvalidLogin) || errors.Is(err, storage.ErrInvalidSession) {
		return httpapi.Error(c, http.StatusUnauthorized, "invalid_session", "invalid or expired login")
	}
	return httpapi.Internal(c, "authentication_failed", err)
}

func setSessionCookie(c *echo.Context, tokens auth.Tokens) {
	c.SetCookie(&http.Cookie{Name: middleware.SessionCookie, Value: tokens.AccessToken, Path: "/", Expires: tokens.ExpiresAt, HttpOnly: true, Secure: c.Scheme() == "https", SameSite: http.SameSiteLaxMode})
}
