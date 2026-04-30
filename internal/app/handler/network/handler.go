package network

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/httpapi"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

type Handler struct {
	manager  *mmodem.Manager
	networks *network
}

const (
	errorCodeListNetworksFailed    = "list_networks_failed"
	errorCodeRegisterNetworkFailed = "register_network_failed"
	errorCodeOperatorCodeRequired  = "operator_code_required"
)

func New(manager *mmodem.Manager) *Handler {
	return &Handler{
		manager:  manager,
		networks: newNetwork(),
	}
}

func (h *Handler) List(c *echo.Context) error {
	modem, err := h.manager.Find(c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeListNetworksFailed)
	}
	response, err := h.networks.List(modem)
	if err != nil {
		return httpapi.Internal(c, errorCodeListNetworksFailed)
	}
	return c.JSON(http.StatusOK, response)
}

func (h *Handler) Register(c *echo.Context) error {
	modem, err := h.manager.Find(c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeRegisterNetworkFailed)
	}
	operatorCode := c.Param("operatorCode")
	if err := h.networks.Register(modem, operatorCode); err != nil {
		if errors.Is(err, errOperatorCodeRequired) {
			return httpapi.BadRequest(c, errorCodeOperatorCodeRequired, err)
		}
		return httpapi.Internal(c, errorCodeRegisterNetworkFailed)
	}
	return c.NoContent(http.StatusNoContent)
}
