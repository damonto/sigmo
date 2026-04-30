package euicc

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/httpapi"
	"github.com/damonto/sigmo/internal/pkg/config"
	"github.com/damonto/sigmo/internal/pkg/lpa"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

type Handler struct {
	cfg     *config.Config
	manager *mmodem.Manager
	euicc   *euicc
}

const (
	errorCodeEuiccNotSupported = "euicc_not_supported"
	errorCodeGetEUICCFailed    = "get_euicc_failed"
)

func New(cfg *config.Config, manager *mmodem.Manager) *Handler {
	return &Handler{
		cfg:     cfg,
		manager: manager,
		euicc:   newEUICC(cfg),
	}
}

func (h *Handler) Get(c *echo.Context) error {
	modem, err := h.manager.Find(c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeGetEUICCFailed)
	}
	response, err := h.euicc.Get(modem)
	if err != nil {
		if errors.Is(err, lpa.ErrNoSupportedAID) {
			return httpapi.NotFound(c, errorCodeEuiccNotSupported, err)
		}
		return httpapi.Internal(c, errorCodeGetEUICCFailed)
	}
	return c.JSON(http.StatusOK, response)
}
