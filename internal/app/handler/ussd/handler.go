package ussd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/damonto/sigmo/internal/app/httpapi"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

type Handler struct {
	manager *mmodem.Manager
	service *Service
}

const executeTimeout = time.Minute

const (
	errorCodeModemNotFound             = "modem_not_found"
	errorCodeExecuteUSSDInvalidRequest = "execute_ussd_invalid_request"
	errorCodeUSSDTimeout               = "ussd_timeout"
	errorCodeInvalidAction             = "invalid_action"
	errorCodeUSSDSessionNotReady       = "ussd_session_not_ready"
	errorCodeUnknownSessionStatus      = "unknown_session_status"
	errorCodeExecuteUSDDFailed         = "execute_ussd_failed"
)

var errModemNotFound = errors.New("modem not found")

var errExecuteTimeout = errors.New("ussd request timed out, please retry")

func New(manager *mmodem.Manager) *Handler {
	return &Handler{
		manager: manager,
		service: NewService(),
	}
}

func (h *Handler) Execute(c echo.Context) error {
	modem, err := h.findModem(c.Param("id"))
	if err != nil {
		return h.modemLookupError(c, err, errorCodeExecuteUSDDFailed)
	}
	var req ExecuteRequest
	if err := httpapi.BindAndValidate(c, &req, errorCodeExecuteUSSDInvalidRequest); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), executeTimeout)
	defer cancel()

	response, err := h.service.Execute(ctx, modem, req.Action, req.Code)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return httpapi.RequestTimeout(c, errorCodeUSSDTimeout, errExecuteTimeout)
		}
		if errors.Is(err, context.Canceled) {
			return nil
		}
		if errors.Is(err, errInvalidAction) {
			return httpapi.BadRequest(c, errorCodeInvalidAction, err)
		}
		if errors.Is(err, errSessionNotReady) {
			return httpapi.BadRequest(c, errorCodeUSSDSessionNotReady, err)
		}
		if errors.Is(err, errUnknownSessionStatus) {
			return httpapi.BadRequest(c, errorCodeUnknownSessionStatus, err)
		}
		return httpapi.Internal(c, errorCodeExecuteUSDDFailed)
	}
	return c.JSON(http.StatusOK, response)
}

func (h *Handler) modemLookupError(c echo.Context, err error, internalErrorCode string) error {
	if errors.Is(err, errModemNotFound) {
		return httpapi.NotFound(c, errorCodeModemNotFound, err)
	}
	return httpapi.Internal(c, internalErrorCode)
}

func (h *Handler) findModem(id string) (*mmodem.Modem, error) {
	modems, err := h.manager.Modems()
	if err != nil {
		return nil, fmt.Errorf("listing modems: %w", err)
	}
	for _, modem := range modems {
		if modem.EquipmentIdentifier == id {
			return modem, nil
		}
	}
	return nil, fmt.Errorf("%w: %s", errModemNotFound, id)
}
