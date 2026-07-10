package network

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/httpapi"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	mdevice "github.com/damonto/sigmo/internal/pkg/modem/device"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

type Handler struct {
	registry *mmodem.Registry
	networks *network
}

const (
	errorCodeListNetworksFailed      = "list_networks_failed"
	errorCodeRegisterNetworkFailed   = "register_network_failed"
	errorCodeOperatorCodeRequired    = "operator_code_required"
	errorCodeGetModesFailed          = "get_modes_failed"
	errorCodeSetModesFailed          = "set_modes_failed"
	errorCodeSetModesInvalid         = "set_modes_invalid_request"
	errorCodeUnsupportedMode         = "unsupported_mode"
	errorCodeGetBandsFailed          = "get_bands_failed"
	errorCodeSetBandsFailed          = "set_bands_failed"
	errorCodeSetBandsInvalid         = "set_bands_invalid_request"
	errorCodeBandsRequired           = "bands_required"
	errorCodeUnsupportedBand         = "unsupported_band"
	errorCodeDuplicateBand           = "duplicate_band"
	errorCodeAnyBandExclusive        = "any_band_exclusive"
	errorCodeGetAirplaneModeFailed   = "get_airplane_mode_failed"
	errorCodeSetAirplaneModeFailed   = "set_airplane_mode_failed"
	errorCodeSetAirplaneModeInvalid  = "set_airplane_mode_invalid_request"
	errorCodeAirplaneModeUnsupported = "airplane_mode_unsupported"
	errorCodeGetVoLTEFailed          = "get_volte_failed"
	errorCodeSetVoLTEFailed          = "set_volte_failed"
	errorCodeSetVoLTEInvalid         = "set_volte_invalid_request"
	errorCodeVoLTEUnavailable        = "volte_unavailable"
)

func New(registry *mmodem.Registry, preferences *mmodem.NetworkPreferences, store *storage.Store) (*Handler, error) {
	networks, err := newNetwork(preferences, store)
	if err != nil {
		return nil, err
	}
	return &Handler{
		registry: registry,
		networks: networks,
	}, nil
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

func (h *Handler) AirplaneMode(c *echo.Context) error {
	ctx := c.Request().Context()
	modem, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeGetAirplaneModeFailed)
	}
	response, err := h.networks.AirplaneMode(ctx, modem)
	if err != nil {
		return httpapi.Internal(c, errorCodeGetAirplaneModeFailed, err)
	}
	return c.JSON(http.StatusOK, response)
}

func (h *Handler) SetAirplaneMode(c *echo.Context) error {
	ctx := c.Request().Context()
	modem, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeSetAirplaneModeFailed)
	}
	var req SetAirplaneModeRequest
	if err := c.Bind(&req); err != nil {
		return httpapi.BadRequest(c, errorCodeSetAirplaneModeInvalid, err)
	}
	if err := h.networks.SetAirplaneMode(ctx, modem, req); err != nil {
		if errors.Is(err, mdevice.ErrUnsupported) {
			return httpapi.BadRequest(c, errorCodeAirplaneModeUnsupported, err)
		}
		return httpapi.Internal(c, errorCodeSetAirplaneModeFailed, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) VoLTE(c *echo.Context) error {
	ctx := c.Request().Context()
	modem, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeGetVoLTEFailed)
	}
	response, err := h.networks.VoLTE(ctx, modem)
	if err != nil {
		return httpapi.Internal(c, errorCodeGetVoLTEFailed, err)
	}
	return c.JSON(http.StatusOK, response)
}

func (h *Handler) SetVoLTE(c *echo.Context) error {
	ctx := c.Request().Context()
	modem, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeSetVoLTEFailed)
	}
	var req SetVoLTERequest
	if err := c.Bind(&req); err != nil {
		return httpapi.BadRequest(c, errorCodeSetVoLTEInvalid, err)
	}
	if err := h.networks.SetVoLTE(ctx, modem, req); err != nil {
		if errors.Is(err, errVoLTEUnavailable) {
			return httpapi.BadRequest(c, errorCodeVoLTEUnavailable, err)
		}
		return httpapi.Internal(c, errorCodeSetVoLTEFailed, err)
	}
	return c.NoContent(http.StatusNoContent)
}
