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
	ussd     *pussd.Executor
}

const executeTimeout = time.Minute

var errExecuteTimeout = errors.New("ussd request timed out, please retry")

func New(registry *mmodem.Registry, route pussd.Route) *Handler {
	return &Handler{
		registry: registry,
		ussd:     pussd.New(route),
	}
}

func (h *Handler) Execute(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "execute_ussd_failed")
	}
	var req ExecuteRequest
	if err := httpapi.BindAndValidate(c, &req, "execute_ussd_invalid_request"); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), executeTimeout)
	defer cancel()

	response, err := h.execute(ctx, modem, req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return httpapi.RequestTimeout(c, "ussd_timeout", errExecuteTimeout)
		}
		if errors.Is(err, context.Canceled) {
			return nil
		}
		if errors.Is(err, pussd.ErrInvalidAction) {
			return httpapi.BadRequest(c, "invalid_action", err)
		}
		if errors.Is(err, pussd.ErrSessionNotReady) {
			return httpapi.BadRequest(c, "ussd_session_not_ready", err)
		}
		if errors.Is(err, pussd.ErrUnknownSessionStatus) {
			return httpapi.BadRequest(c, "unknown_session_status", err)
		}
		return httpapi.Internal(c, "execute_ussd_failed", err)
	}
	return c.JSON(http.StatusOK, response)
}

func (h *Handler) execute(ctx context.Context, modem *mmodem.Modem, req ExecuteRequest) (*ExecuteResponse, error) {
	reply, err := h.ussd.Execute(ctx, modem, req.Action, req.Code)
	if err != nil {
		return nil, err
	}
	return &ExecuteResponse{Reply: reply}, nil
}
