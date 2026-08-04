package euicc

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/httpapi"
	"github.com/damonto/sigmo/internal/pkg/lpa"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/settings"
)

type Handler struct {
	registry *mmodem.Registry
	euicc    *euicc
}

const (
	errorCodeEuiccNotSupported = "euicc_not_supported"
	errorCodeGetEUICCFailed    = "get_euicc_failed"
)

func New(store *settings.Store, registry *mmodem.Registry) *Handler {
	return &Handler{
		registry: registry,
		euicc:    newEUICC(store),
	}
}

func (h *Handler) Get(c *echo.Context) error {
	ctx := c.Request().Context()
	modem, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeGetEUICCFailed)
	}
	response, err := h.euicc.Get(ctx, modem)
	if err != nil {
		if errors.Is(err, lpa.ErrNoSupportedAID) {
			return httpapi.NotFound(c, errorCodeEuiccNotSupported, err)
		}
		return httpapi.Internal(c, errorCodeGetEUICCFailed, err)
	}
	return c.JSON(http.StatusOK, response)
}
