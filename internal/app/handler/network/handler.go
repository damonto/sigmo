package network

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	wwanmodem "github.com/damonto/wwan-go/modem"
	"github.com/labstack/echo/v5"

	appconnectivity "github.com/damonto/sigmo/internal/app/connectivity"
	"github.com/damonto/sigmo/internal/app/httpapi"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/networkprefs"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

type Handler struct {
	registry modemFinder
	networks *network
}

type modemFinder interface {
	Find(context.Context, string) (*mmodem.Modem, error)
}

type Config struct {
	Registry              *mmodem.Registry
	Preferences           *networkprefs.Store
	Store                 *storage.Store
	AirplaneModeLifecycle appconnectivity.AirplaneModeLifecycle
}

const (
	errorCodeListNetworksFailed      = "list_networks_failed"
	errorCodeStartNetworkScanFailed  = "start_network_scan_failed"
	errorCodeGetNetworkScanFailed    = "get_network_scan_failed"
	errorCodeNetworkScanNotFound     = "network_scan_not_found"
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
	errorCodeGetAirplaneModeFailed   = "get_airplane_mode_failed"
	errorCodeSetAirplaneModeFailed   = "set_airplane_mode_failed"
	errorCodeSetAirplaneModeInvalid  = "set_airplane_mode_invalid_request"
	errorCodeAirplaneModeUnsupported = "airplane_mode_unsupported"
)

var errNetworkRegistryRequired = errors.New("modem registry is required")

func New(cfg Config) (*Handler, error) {
	if cfg.Registry == nil {
		return nil, errNetworkRegistryRequired
	}
	networks, err := newNetwork(cfg.Preferences, cfg.Store, cfg.AirplaneModeLifecycle)
	if err != nil {
		return nil, err
	}
	return &Handler{
		registry: cfg.Registry,
		networks: networks,
	}, nil
}

func (h *Handler) Close() {
	if h != nil && h.networks != nil {
		h.networks.Close()
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

func (h *Handler) StartNetworkScan(c *echo.Context) error {
	header := c.Response().Header()
	header.Set("Cache-Control", "no-store")
	ctx := c.Request().Context()
	modem, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeStartNetworkScanFailed)
	}
	response, created, err := h.networks.StartScan(ctx, modem)
	if err != nil {
		return httpapi.Internal(c, errorCodeStartNetworkScanFailed, err)
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	header.Set("Location", strings.TrimRight(c.Request().URL.Path, "/")+"/"+url.PathEscape(response.ID))
	setNetworkScanRetryHeader(header, response.Status)
	return c.JSON(status, response)
}

func (h *Handler) GetNetworkScan(c *echo.Context) error {
	header := c.Response().Header()
	header.Set("Cache-Control", "no-store")
	ctx := c.Request().Context()
	modem, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeGetNetworkScanFailed)
	}
	response, err := h.networks.Scan(ctx, modem, c.Param("scanID"))
	if errors.Is(err, errNetworkScanNotFound) {
		return httpapi.NotFound(c, errorCodeNetworkScanNotFound, err)
	}
	if err != nil {
		return httpapi.Internal(c, errorCodeGetNetworkScanFailed, err)
	}
	setNetworkScanRetryHeader(header, response.Status)
	return c.JSON(http.StatusOK, response)
}

func setNetworkScanRetryHeader(header http.Header, status string) {
	header.Del("Retry-After")
	if status == networkScanStatusRunning {
		header.Set("Retry-After", "1")
	}
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
		if errors.Is(err, wwanmodem.ErrNotSupported) {
			return httpapi.BadRequest(c, errorCodeAirplaneModeUnsupported, err)
		}
		return httpapi.Internal(c, errorCodeSetAirplaneModeFailed, err)
	}
	return c.NoContent(http.StatusNoContent)
}
