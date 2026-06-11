package ussd

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/httpapi"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	pussd "github.com/damonto/sigmo/internal/pkg/ussd"
)

type Handler struct {
	registry *mmodem.Registry
	service  *pussd.Service
}

const executeTimeout = time.Minute

const (
	errorCodeExecuteUSSDInvalidRequest = "execute_ussd_invalid_request"
	errorCodeUSSDTimeout               = "ussd_timeout"
	errorCodeInvalidAction             = "invalid_action"
	errorCodeUSSDSessionNotReady       = "ussd_session_not_ready"
	errorCodeUnknownSessionStatus      = "unknown_session_status"
	errorCodeExecuteUSDDFailed         = "execute_ussd_failed"
)

var errExecuteTimeout = errors.New("ussd request timed out, please retry")

func New(registry *mmodem.Registry, route pussd.Route) *Handler {
	return &Handler{
		registry: registry,
		service:  pussd.New(route),
	}
}

func (h *Handler) Execute(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeExecuteUSDDFailed)
	}
	var req ExecuteRequest
	if err := httpapi.BindAndValidate(c, &req, errorCodeExecuteUSSDInvalidRequest); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), executeTimeout)
	defer cancel()

	response, err := h.execute(ctx, modem, req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return httpapi.RequestTimeout(c, errorCodeUSSDTimeout, errExecuteTimeout)
		}
		if errors.Is(err, context.Canceled) {
			return nil
		}
		if errors.Is(err, pussd.ErrInvalidAction) {
			return httpapi.BadRequest(c, errorCodeInvalidAction, err)
		}
		if errors.Is(err, pussd.ErrSessionNotReady) {
			return httpapi.BadRequest(c, errorCodeUSSDSessionNotReady, err)
		}
		if errors.Is(err, pussd.ErrUnknownSessionStatus) {
			return httpapi.BadRequest(c, errorCodeUnknownSessionStatus, err)
		}
		return httpapi.Internal(c, errorCodeExecuteUSDDFailed, err)
	}
	return c.JSON(http.StatusOK, response)
}

func (h *Handler) execute(ctx context.Context, modem *mmodem.Modem, req ExecuteRequest) (*ExecuteResponse, error) {
	reply, err := h.service.Execute(ctx, modem, req.Action, req.Code)
	if err != nil {
		return nil, err
	}
	return &ExecuteResponse{Reply: reply}, nil
}
