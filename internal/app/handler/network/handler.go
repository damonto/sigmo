package network

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/httpapi"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

type Handler struct {
	registry *mmodem.Registry
	networks *network
}

const (
	errorCodeListNetworksFailed    = "list_networks_failed"
	errorCodeRegisterNetworkFailed = "register_network_failed"
	errorCodeOperatorCodeRequired  = "operator_code_required"
	errorCodeGetModesFailed        = "get_modes_failed"
	errorCodeSetModesFailed        = "set_modes_failed"
	errorCodeSetModesInvalid       = "set_modes_invalid_request"
	errorCodeUnsupportedMode       = "unsupported_mode"
	errorCodeGetBandsFailed        = "get_bands_failed"
	errorCodeSetBandsFailed        = "set_bands_failed"
	errorCodeSetBandsInvalid       = "set_bands_invalid_request"
	errorCodeBandsRequired         = "bands_required"
	errorCodeUnsupportedBand       = "unsupported_band"
	errorCodeDuplicateBand         = "duplicate_band"
	errorCodeAnyBandExclusive      = "any_band_exclusive"
)

func New(registry *mmodem.Registry, preferences *mmodem.NetworkPreferences) *Handler {
	return &Handler{
		registry: registry,
		networks: newNetwork(preferences),
	}
}

func (h *Handler) List(c *echo.Context) error {
	ctx := c.Request().Context()
	modem, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeListNetworksFailed)
	}
	response, err := h.networks.List(ctx, modem)
	if err != nil {
		return httpapi.Internal(c, errorCodeListNetworksFailed, err)
	}
	return c.JSON(http.StatusOK, response)
}

func (h *Handler) Register(c *echo.Context) error {
	ctx := c.Request().Context()
	modem, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeRegisterNetworkFailed)
	}
	operatorCode := c.Param("operatorCode")
	if err := h.networks.Register(ctx, modem, operatorCode); err != nil {
		if errors.Is(err, errOperatorCodeRequired) {
			return httpapi.BadRequest(c, errorCodeOperatorCodeRequired, err)
		}
		return httpapi.Internal(c, errorCodeRegisterNetworkFailed, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) Modes(c *echo.Context) error {
	ctx := c.Request().Context()
	modem, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeGetModesFailed)
	}
	response, err := h.networks.Modes(ctx, modem)
	if err != nil {
		return httpapi.Internal(c, errorCodeGetModesFailed, err)
	}
	return c.JSON(http.StatusOK, response)
}

func (h *Handler) SetCurrentModes(c *echo.Context) error {
	ctx := c.Request().Context()
	modem, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeSetModesFailed)
	}
	var req SetCurrentModesRequest
	if err := c.Bind(&req); err != nil {
		return httpapi.BadRequest(c, errorCodeSetModesInvalid, err)
	}
	if err := h.networks.SetCurrentModes(ctx, modem, req); err != nil {
		if errors.Is(err, errUnsupportedMode) {
			return httpapi.BadRequest(c, errorCodeUnsupportedMode, err)
		}
		return httpapi.Internal(c, errorCodeSetModesFailed, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) Bands(c *echo.Context) error {
	ctx := c.Request().Context()
	modem, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeGetBandsFailed)
	}
	response, err := h.networks.Bands(ctx, modem)
	if err != nil {
		return httpapi.Internal(c, errorCodeGetBandsFailed, err)
	}
	return c.JSON(http.StatusOK, response)
}

func (h *Handler) SetCurrentBands(c *echo.Context) error {
	ctx := c.Request().Context()
	modem, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeSetBandsFailed)
	}
	var req SetCurrentBandsRequest
	if err := c.Bind(&req); err != nil {
		return httpapi.BadRequest(c, errorCodeSetBandsInvalid, err)
	}
	if err := h.networks.SetCurrentBands(ctx, modem, req); err != nil {
		switch {
		case errors.Is(err, errBandsRequired):
			return httpapi.BadRequest(c, errorCodeBandsRequired, err)
		case errors.Is(err, errUnsupportedBand):
			return httpapi.BadRequest(c, errorCodeUnsupportedBand, err)
		case errors.Is(err, errDuplicateBand):
			return httpapi.BadRequest(c, errorCodeDuplicateBand, err)
		case errors.Is(err, errAnyBandExclusive):
			return httpapi.BadRequest(c, errorCodeAnyBandExclusive, err)
		default:
			return httpapi.Internal(c, errorCodeSetBandsFailed, err)
		}
	}
	return c.NoContent(http.StatusNoContent)
}
