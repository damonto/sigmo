package euicc

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/httpapi"
	"github.com/damonto/sigmo/internal/pkg/lpa"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

// Handler adapts secure-element inventory to HTTP.
type Handler struct {
	registry *mmodem.Registry
	euicc    *euicc
}

// NewHandler creates secure-element endpoints from an application service.
func NewHandler(service *Service) *Handler {
	return &Handler{registry: service.registry, euicc: service.euicc}
}

func (h *Handler) Get(c *echo.Context) error {
	ctx := c.Request().Context()
	modem, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "get_euicc_failed")
	}
	response, err := h.euicc.Get(ctx, modem)
	if err != nil {
		if errors.Is(err, lpa.ErrNoSupportedAID) {
			return httpapi.NotFound(c, "euicc_not_supported", err)
		}
		return httpapi.Internal(c, "get_euicc_failed", err)
	}
	return c.JSON(http.StatusOK, response)
}
