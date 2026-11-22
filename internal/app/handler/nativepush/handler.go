// Package nativepush registers installation-specific grants issued by the public gateway.
package nativepush

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"regexp"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/httpapi"
	"github.com/damonto/sigmo/internal/app/middleware"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

// Handler exposes authenticated registration metadata without returning encryption keys.
type Handler struct{ store *storage.Store }

// New binds registrations to the same database as incoming SMS transactions.
func New(store *storage.Store) *Handler { return &Handler{store: store} }

type overview struct {
	storage.MessageState
	Subscriptions []storage.NativeSubscription `json:"subscriptions"`
}

type registration struct {
	ID             string `json:"id"`
	InstallationID string `json:"installationId"`
	Label          string `json:"label"`
	BackendName    string `json:"backendName"`
	SendToken      string `json:"sendToken"`
	EncryptionKey  string `json:"encryptionKey"`
	SMS            bool   `json:"sms"`
	Reminders      bool   `json:"reminders"`
	Calls          bool   `json:"calls"`
	Enabled        bool   `json:"enabled"`
}

var identifierPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,128}$`)

// List includes the stable backend identity needed to deduplicate multiple connection URLs.
func (h *Handler) List(c *echo.Context) error {
	state, err := h.store.MessagesState(c.Request().Context())
	if err != nil {
		return httpapi.Internal(c, "native_push_state_failed", err)
	}
	subs, err := h.store.NativeSubscriptions(c.Request().Context())
	if err != nil {
		return httpapi.Internal(c, "native_push_state_failed", err)
	}
	return c.JSON(http.StatusOK, overview{MessageState: state, Subscriptions: subs})
}

// Register is idempotent for an installation and never accepts a caller-provided gateway URL.
func (h *Handler) Register(c *echo.Context) error {
	var req registration
	if err := httpapi.BindJSON(c, &req, "invalid_native_push_subscription"); err != nil {
		return err
	}
	key, err := base64.StdEncoding.DecodeString(req.EncryptionKey)
	if err != nil || len(key) != 32 ||
		!identifierPattern.MatchString(req.ID) || !identifierPattern.MatchString(req.InstallationID) ||
		len(req.SendToken) < 32 || len(req.SendToken) > 128 ||
		len(req.Label) > 128 || len(req.BackendName) > 256 {
		return httpapi.UnprocessableEntity(c, "invalid_native_push_subscription", errors.New("invalid native push subscription"))
	}
	binding, err := h.requestSession(c)
	if err != nil {
		return httpapi.Internal(c, "bind_push_session_failed", err)
	}
	sub := storage.NativeSubscription{
		ID:             req.ID,
		InstallationID: req.InstallationID,
		Label:          req.Label,
		BackendName:    req.BackendName,
		SendToken:      req.SendToken,
		EncryptionKey:  key,
		AuthSessionID:  binding,
		SMS:            req.SMS,
		Reminders:      req.Reminders,
		Calls:          req.Calls,
		Enabled:        req.Enabled,
	}
	if err := h.store.UpsertNativeSubscription(c.Request().Context(), sub); err != nil {
		return httpapi.Internal(c, "register_native_push_failed", err)
	}
	return c.JSON(http.StatusCreated, sub)
}

func (h *Handler) requestSession(c *echo.Context) (string, error) {
	session := middleware.CurrentSession(c)
	if session.Method != "cloudflare_access" {
		return session.ID, nil
	}
	// Edge authorization is a lease bounded by the assertion, never an indefinite push grant.
	session.ID, session.CreatedAt = rand.Text(), time.Now()
	if err := h.store.CreateAuthSession(c.Request().Context(), session, nil); err != nil {
		return "", err
	}
	return session.ID, nil
}
