package network

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/damonto/sigmo/internal/app/httpapi"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

type Handler struct {
	manager *mmodem.Manager
	service *Service
}

const (
	errorCodeModemNotFound         = "modem_not_found"
	errorCodeListNetworksFailed    = "list_networks_failed"
	errorCodeRegisterNetworkFailed = "register_network_failed"
	errorCodeOperatorCodeRequired  = "operator_code_required"
)

var errModemNotFound = errors.New("modem not found")

func New(manager *mmodem.Manager) *Handler {
	return &Handler{
		manager: manager,
		service: NewService(),
	}
}

func (h *Handler) List(c echo.Context) error {
	modem, err := h.findModem(c.Param("id"))
	if err != nil {
		return h.modemLookupError(c, err, errorCodeListNetworksFailed)
	}
	response, err := h.service.List(modem)
	if err != nil {
		return httpapi.Internal(c, errorCodeListNetworksFailed)
	}
	return c.JSON(http.StatusOK, response)
}

func (h *Handler) Register(c echo.Context) error {
	modem, err := h.findModem(c.Param("id"))
	if err != nil {
		return h.modemLookupError(c, err, errorCodeRegisterNetworkFailed)
	}
	operatorCode := c.Param("operatorCode")
	if err := h.service.Register(modem, operatorCode); err != nil {
		if errors.Is(err, errOperatorCodeRequired) {
			return httpapi.BadRequest(c, errorCodeOperatorCodeRequired, err)
		}
		return httpapi.Internal(c, errorCodeRegisterNetworkFailed)
	}
	return c.NoContent(http.StatusNoContent)
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
