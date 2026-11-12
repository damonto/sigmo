package modem

import (
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/httpapi"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

// Handler serves modem inventory and detailed observations.
type Handler struct {
	service  *Service
	registry *mmodem.Registry
}

// NewHandler creates modem inventory endpoints.
func NewHandler(service *Service) *Handler {
	return &Handler{service: service, registry: service.registry}
}

func (h *Handler) List(c *echo.Context) error {
	response, err := h.service.catalog.List(c.Request().Context())
	if err != nil {
		return httpapi.Internal(c, "list_modems_failed", err)
	}
	return c.JSON(http.StatusOK, response)
}

func (h *Handler) Get(c *echo.Context) error {
	ctx := c.Request().Context()
	modem, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "get_modem_failed")
	}
	response, err := h.service.catalog.Get(ctx, modem)
	if err != nil {
		return httpapi.Internal(c, "get_modem_failed", err)
	}
	return c.JSON(http.StatusOK, response)
}
