package euicc

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/damonto/sigmo/internal/app/httpapi"
	"github.com/damonto/sigmo/internal/pkg/config"
	"github.com/damonto/sigmo/internal/pkg/lpa"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

type Handler struct {
	cfg     *config.Config
	manager *mmodem.Manager
	service *Service
}

const (
	errorCodeModemNotFound     = "modem_not_found"
	errorCodeEuiccNotSupported = "euicc_not_supported"
	errorCodeGetEUICCFailed    = "get_euicc_failed"
)

var errModemNotFound = errors.New("modem not found")

func New(cfg *config.Config, manager *mmodem.Manager) *Handler {
	return &Handler{
		cfg:     cfg,
		manager: manager,
		service: NewService(cfg),
	}
}

func (h *Handler) Get(c echo.Context) error {
	modem, err := h.findModem(c.Param("id"))
	if err != nil {
		return h.modemLookupError(c, err, errorCodeGetEUICCFailed)
	}
	response, err := h.service.Get(modem)
	if err != nil {
		if errors.Is(err, lpa.ErrNoSupportedAID) {
			return httpapi.NotFound(c, errorCodeEuiccNotSupported, err)
		}
		return httpapi.Internal(c, errorCodeGetEUICCFailed)
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
