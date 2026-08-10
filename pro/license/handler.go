package license

import (
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/httpapi"
)

type statusResponse struct {
	Authorized bool   `json:"authorized"`
	DeviceID   string `json:"deviceId"`
}

type pairingResponse struct {
	ID            string    `json:"id"`
	ActivationURL string    `json:"activationUrl"`
	Status        string    `json:"status"`
	ExpiresAt     time.Time `json:"expiresAt"`
}

func (c *Controller) RegisterActivationRoutes(group *echo.Group) {
	group.GET("/license", c.getLicense)
	group.POST("/license/pairings", c.createPairing)
	group.GET("/license/pairings/:id", c.getPairing)
}

func (c *Controller) RegisterStatusRoute(group *echo.Group) {
	group.GET("/license", c.getLicense)
}

func (c *Controller) getLicense(ctx *echo.Context) error {
	ctx.Response().Header().Set("Cache-Control", "no-store")
	return ctx.JSON(http.StatusOK, statusResponse{
		Authorized: c.Authorized(),
		DeviceID:   c.identity.DeviceID,
	})
}

func (c *Controller) createPairing(ctx *echo.Context) error {
	ctx.Response().Header().Set("Cache-Control", "no-store")
	if c.baseURL == "" || len(c.licensePublicKey) == 0 {
		return httpapi.Error(ctx, http.StatusServiceUnavailable, "license_service_unavailable", "Pro authorization service is not configured")
	}
	var remote pairing
	_, err := c.doJSON(ctx.Request().Context(), http.MethodPost, "/v1/license-pairings", map[string]string{
		"deviceId":  c.identity.DeviceID,
		"publicKey": base64.RawStdEncoding.EncodeToString(c.identity.PublicKey),
	}, "", &remote)
	if err != nil {
		return respondServiceError(ctx, "license_pairing_failed", err)
	}
	if err := validateCreatedPairing(remote, time.Now()); err != nil {
		return httpapi.Error(ctx, http.StatusBadGateway, "license_pairing_invalid_response", err.Error())
	}
	c.mu.Lock()
	c.prunePairingsLocked(time.Now())
	c.pairings[remote.ID] = pairingSession{
		PollToken:     remote.PollToken,
		ActivationURL: remote.ActivationURL,
		ExpiresAt:     remote.ExpiresAt,
	}
	c.mu.Unlock()
	return ctx.JSON(http.StatusCreated, pairingResponse{
		ID:            remote.ID,
		ActivationURL: remote.ActivationURL,
		Status:        remote.Status,
		ExpiresAt:     remote.ExpiresAt,
	})
}

func (c *Controller) getPairing(ctx *echo.Context) error {
	ctx.Response().Header().Set("Cache-Control", "no-store")
	id := ctx.Param("id")
	now := time.Now()
	c.mu.Lock()
	c.prunePairingsLocked(now)
	session, ok := c.pairings[id]
	c.mu.Unlock()
	if !ok {
		return httpapi.Error(ctx, http.StatusNotFound, "license_pairing_not_found", "pairing is unknown or expired")
	}
	var remote pairing
	status, err := c.doJSON(ctx.Request().Context(), http.MethodGet, "/v1/license-pairings/"+url.PathEscape(id), nil, session.PollToken, &remote)
	if err != nil {
		if status == http.StatusGone || status == http.StatusNotFound {
			c.mu.Lock()
			delete(c.pairings, id)
			c.mu.Unlock()
		}
		return respondServiceError(ctx, "license_pairing_failed", err)
	}
	if err := validatePolledPairing(id, remote); err != nil {
		return httpapi.Error(ctx, http.StatusBadGateway, "license_pairing_invalid_response", err.Error())
	}
	restart := false
	if remote.Status == "active" && remote.Lease != nil {
		lease, err := c.verifyLease(*remote.Lease, time.Now())
		if err != nil {
			return httpapi.Error(ctx, http.StatusBadGateway, "license_lease_invalid", "license lease is invalid")
		}
		if err := c.saveLease(ctx.Request().Context(), *remote.Lease); err != nil {
			return httpapi.Internal(ctx, "license_save_failed", err)
		}
		c.setLease(lease, remote.Lease)
		c.mu.Lock()
		delete(c.pairings, id)
		c.mu.Unlock()
		restart = c.restart != nil
	}
	if remote.Status == "expired" {
		c.mu.Lock()
		delete(c.pairings, id)
		c.mu.Unlock()
	}
	if err := ctx.JSON(http.StatusOK, pairingResponse{
		ID:            remote.ID,
		ActivationURL: session.ActivationURL,
		Status:        remote.Status,
		ExpiresAt:     remote.ExpiresAt,
	}); err != nil {
		return err
	}
	if restart {
		// Echo has written the response at this point. Graceful shutdown lets the
		// active request finish before the authorized server replaces it.
		c.restartHealthy()
	}
	return nil
}

func (c *Controller) prunePairingsLocked(now time.Time) {
	for id, session := range c.pairings {
		if !now.Before(session.ExpiresAt) {
			delete(c.pairings, id)
		}
	}
}

func validateCreatedPairing(remote pairing, now time.Time) error {
	if strings.TrimSpace(remote.ID) == "" || strings.TrimSpace(remote.PollToken) == "" || remote.Status != "pending" {
		return errors.New("authorization service returned an incomplete pairing")
	}
	if !remote.ExpiresAt.After(now) {
		return errors.New("authorization service returned an expired pairing")
	}
	activationURL, err := url.Parse(remote.ActivationURL)
	if err != nil || activationURL.Scheme != "https" || !strings.EqualFold(activationURL.Hostname(), "t.me") || activationURL.Port() != "" || activationURL.User != nil || activationURL.Fragment != "" {
		return errors.New("authorization service returned an invalid Telegram activation URL")
	}
	bot := strings.TrimPrefix(activationURL.Path, "/")
	query, err := url.ParseQuery(activationURL.RawQuery)
	starts := query["start"]
	if err != nil || activationURL.Path != "/"+bot || !validTelegramBotUsername(bot) || len(query) != 1 || len(starts) != 1 || starts[0] != remote.ID {
		return errors.New("authorization service returned an invalid Telegram deep link")
	}
	return nil
}

func validTelegramBotUsername(value string) bool {
	if len(value) < 5 || len(value) > 32 || !strings.EqualFold(value[len(value)-3:], "bot") {
		return false
	}
	for index, character := range value {
		if character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z' {
			continue
		}
		if index > 0 && (character >= '0' && character <= '9' || character == '_') {
			continue
		}
		return false
	}
	return true
}

func validatePolledPairing(id string, remote pairing) error {
	if remote.ID != id || remote.ExpiresAt.IsZero() {
		return errors.New("authorization service returned mismatched pairing metadata")
	}
	switch remote.Status {
	case "pending", "expired":
		return nil
	case "active":
		if remote.Lease == nil {
			return errors.New("authorization service returned an active pairing without a lease")
		}
		return nil
	default:
		return errors.New("authorization service returned an unknown pairing status")
	}
}

func respondServiceError(ctx *echo.Context, fallbackCode string, err error) error {
	var remote *serviceError
	if errors.As(err, &remote) {
		return httpapi.Error(ctx, remote.StatusCode, remote.ErrorCode, remote.Message)
	}
	return httpapi.Error(ctx, http.StatusBadGateway, fallbackCode, err.Error())
}
