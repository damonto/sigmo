package auth

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/auth"
	"github.com/damonto/sigmo/internal/app/httpapi"
	"github.com/damonto/sigmo/internal/pkg/config"
)

type Handler struct {
	otp *otp
}

const (
	errorCodeOTPCooldown             = "otp_cooldown"
	errorCodeOTPNotRequired          = "otp_not_required"
	errorCodeInvalidVerifyOTPRequest = "verify_otp_invalid_request"
	errorCodeInvalidOTP              = "invalid_otp"
	errorCodeSendOTPFailed           = "send_otp_failed"
	errorCodeVerifyOTPFailed         = "verify_otp_failed"
)

func New(cfg *config.Config, store *auth.Store) *Handler {
	return &Handler{
		otp: newOTP(cfg, store),
	}
}

func (h *Handler) OTPRequirement(c *echo.Context) error {
	return c.JSON(http.StatusOK, OTPRequirementResponse{OTPRequired: h.otp.Required()})
}

func (h *Handler) SendOTP(c *echo.Context) error {
	if err := h.otp.Send(c.Request().Context()); err != nil {
		if errors.Is(err, auth.ErrOTPCooldown) {
			return httpapi.TooManyRequests(c, errorCodeOTPCooldown, err)
		}
		return httpapi.Internal(c, errorCodeSendOTPFailed)
	}
	return c.NoContent(http.StatusCreated)
}

func (h *Handler) VerifyOTP(c *echo.Context) error {
	var req VerifyOTPRequest
	if err := httpapi.BindAndValidate(c, &req, errorCodeInvalidVerifyOTPRequest); err != nil {
		return err
	}
	token, err := h.otp.Verify(req.Code)
	if err != nil {
		if errors.Is(err, errOTPNotRequired) {
			return httpapi.BadRequest(c, errorCodeOTPNotRequired, err)
		}
		if errors.Is(err, errInvalidOTP) {
			return httpapi.Unauthorized(c, errorCodeInvalidOTP, err)
		}
		return httpapi.Internal(c, errorCodeVerifyOTPFailed)
	}
	return c.JSON(http.StatusOK, VerifyOTPResponse{Token: token})
}
