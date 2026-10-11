package auth

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/auth"
	"github.com/damonto/sigmo/internal/app/httpapi"
	"github.com/damonto/sigmo/internal/pkg/settings"
)

type Handler struct {
	otp *otp
}

func New(settingsStore *settings.Store, store *auth.Store) *Handler {
	return &Handler{
		otp: newOTP(settingsStore, store),
	}
}

func (h *Handler) OTPRequirement(c *echo.Context) error {
	return c.JSON(http.StatusOK, OTPRequirementResponse{OTPRequired: h.otp.Required()})
}

func (h *Handler) SendOTP(c *echo.Context) error {
	if err := h.otp.Send(c.Request().Context(), httpapi.RequestLocale(c)); err != nil {
		if errors.Is(err, auth.ErrOTPCooldown) {
			return httpapi.TooManyRequests(c, "otp_cooldown", err)
		}
		if errors.Is(err, errAuthProviderRequired) || errors.Is(err, errAuthProviderUnavailable) {
			return httpapi.UnprocessableEntity(c, "auth_provider_invalid", err)
		}
		return httpapi.Internal(c, "send_otp_failed", err)
	}
	return c.NoContent(http.StatusCreated)
}

func (h *Handler) VerifyOTP(c *echo.Context) error {
	var req VerifyOTPRequest
	if err := httpapi.BindAndValidate(c, &req, "verify_otp_invalid_request"); err != nil {
		return err
	}
	token, err := h.otp.Verify(c.Request().Context(), req.Code)
	if err != nil {
		if errors.Is(err, errOTPNotRequired) {
			return httpapi.BadRequest(c, "otp_not_required", err)
		}
		if errors.Is(err, errInvalidOTP) {
			return httpapi.Unauthorized(c, "invalid_otp", err)
		}
		return httpapi.Internal(c, "verify_otp_failed", err)
	}
	return c.JSON(http.StatusOK, VerifyOTPResponse{Token: token})
}
