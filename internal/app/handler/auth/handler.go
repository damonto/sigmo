package auth

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/damonto/sigmo/internal/app/auth"
	"github.com/damonto/sigmo/internal/app/httpapi"
	"github.com/damonto/sigmo/internal/pkg/config"
)

type Handler struct {
	service *Service
}

const (
	errorCodeOTPCooldown             = "otp_cooldown"
	errorCodeInvalidVerifyOTPRequest = "verify_otp_invalid_request"
	errorCodeInvalidOTP              = "invalid_otp"
	errorCodeSendOTPFailed           = "send_otp_failed"
	errorCodeVerifyOTPFailed         = "verify_otp_failed"
)

func New(cfg *config.Config, store *auth.Store) *Handler {
	return &Handler{
		service: NewService(cfg, store),
	}
}

func (h *Handler) OTPRequirement(c echo.Context) error {
	return c.JSON(http.StatusOK, OTPRequirementResponse{OTPRequired: h.service.OTPRequired()})
}

func (h *Handler) SendOTP(c echo.Context) error {
	if err := h.service.SendOTP(); err != nil {
		if errors.Is(err, auth.ErrOTPCooldown) {
			return httpapi.TooManyRequests(c, errorCodeOTPCooldown, err)
		}
		return httpapi.Internal(c, errorCodeSendOTPFailed)
	}
	return c.NoContent(http.StatusCreated)
}

func (h *Handler) VerifyOTP(c echo.Context) error {
	var req VerifyOTPRequest
	if err := httpapi.BindAndValidate(c, &req, errorCodeInvalidVerifyOTPRequest); err != nil {
		return err
	}
	token, err := h.service.VerifyOTP(req.Code)
	if err != nil {
		if errors.Is(err, errInvalidOTP) {
			return httpapi.Unauthorized(c, errorCodeInvalidOTP, err)
		}
		return httpapi.Internal(c, errorCodeVerifyOTPFailed)
	}
	return c.JSON(http.StatusOK, VerifyOTPResponse{Token: token})
}
