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
		return httpapi.ModemLookupError(c, err, "list_networks_failed")
	}
	response, err := h.networks.List(ctx, modem)
	if err != nil {
		return httpapi.Internal(c, "list_networks_failed", err)
	}
	return c.JSON(http.StatusOK, response)
}

func (h *Handler) StartNetworkScan(c *echo.Context) error {
	header := c.Response().Header()
	header.Set("Cache-Control", "no-store")
	ctx := c.Request().Context()
	modem, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "start_network_scan_failed")
	}
	response, created, err := h.networks.StartScan(ctx, modem)
	if err != nil {
		return httpapi.Internal(c, "start_network_scan_failed", err)
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
		return httpapi.ModemLookupError(c, err, "get_network_scan_failed")
	}
	response, err := h.networks.Scan(ctx, modem, c.Param("scanID"))
	if errors.Is(err, errNetworkScanNotFound) {
		return httpapi.NotFound(c, "network_scan_not_found", err)
	}
	if err != nil {
		return httpapi.Internal(c, "get_network_scan_failed", err)
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

func (h *Handler) Registration(c *echo.Context) error {
	ctx := c.Request().Context()
	modem, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "get_registration_failed")
	}
	response, err := h.networks.Registration(ctx, modem)
	if err != nil {
		return httpapi.Internal(c, "get_registration_failed", err)
	}
	return c.JSON(http.StatusOK, response)
}

func (h *Handler) SetRegistration(c *echo.Context) error {
	ctx := c.Request().Context()
	modem, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "set_registration_failed")
	}
	var req SetRegistrationRequest
	if err := httpapi.BindJSON(c, &req, "set_registration_invalid_request"); err != nil {
		return err
	}
	if err := h.networks.SetRegistration(ctx, modem, req); err != nil {
		switch {
		case errors.Is(err, errRegistrationModeInvalid):
			return httpapi.BadRequest(c, "registration_mode_invalid", err)
		case errors.Is(err, errOperatorCodeRequired):
			return httpapi.BadRequest(c, "operator_code_required", err)
		default:
			return httpapi.Internal(c, "set_registration_failed", err)
		}
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) Modes(c *echo.Context) error {
	ctx := c.Request().Context()
	modem, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "get_modes_failed")
	}
	response, err := h.networks.Modes(ctx, modem)
	if err != nil {
		return httpapi.Internal(c, "get_modes_failed", err)
	}
	return c.JSON(http.StatusOK, response)
}

func (h *Handler) SetCurrentModes(c *echo.Context) error {
	ctx := c.Request().Context()
	modem, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "set_modes_failed")
	}
	var req SetCurrentModesRequest
	if err := httpapi.BindJSON(c, &req, "set_modes_invalid_request"); err != nil {
		return err
	}
	if err := h.networks.SetCurrentModes(ctx, modem, req); err != nil {
		if errors.Is(err, errUnsupportedMode) {
			return httpapi.BadRequest(c, "unsupported_mode", err)
		}
		return httpapi.Internal(c, "set_modes_failed", err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) Bands(c *echo.Context) error {
	ctx := c.Request().Context()
	modem, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "get_bands_failed")
	}
	response, err := h.networks.Bands(ctx, modem)
	if err != nil {
		return httpapi.Internal(c, "get_bands_failed", err)
	}
	return c.JSON(http.StatusOK, response)
}

func (h *Handler) SetCurrentBands(c *echo.Context) error {
	ctx := c.Request().Context()
	modem, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "set_bands_failed")
	}
	var req SetCurrentBandsRequest
	if err := httpapi.BindJSON(c, &req, "set_bands_invalid_request"); err != nil {
		return err
	}
	if err := h.networks.SetCurrentBands(ctx, modem, req); err != nil {
		switch {
		case errors.Is(err, errBandsRequired):
			return httpapi.BadRequest(c, "bands_required", err)
		case errors.Is(err, errUnsupportedBand):
			return httpapi.BadRequest(c, "unsupported_band", err)
		case errors.Is(err, errDuplicateBand):
			return httpapi.BadRequest(c, "duplicate_band", err)
		default:
			return httpapi.Internal(c, "set_bands_failed", err)
		}
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) AirplaneMode(c *echo.Context) error {
	ctx := c.Request().Context()
	modem, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "get_airplane_mode_failed")
	}
	response, err := h.networks.AirplaneMode(ctx, modem)
	if err != nil {
		return httpapi.Internal(c, "get_airplane_mode_failed", err)
	}
	return c.JSON(http.StatusOK, response)
}

func (h *Handler) SetAirplaneMode(c *echo.Context) error {
	ctx := c.Request().Context()
	modem, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "set_airplane_mode_failed")
	}
	var req SetAirplaneModeRequest
	if err := httpapi.BindJSON(c, &req, "set_airplane_mode_invalid_request"); err != nil {
		return err
	}
	if err := h.networks.SetAirplaneMode(ctx, modem, req); err != nil {
		if errors.Is(err, wwanmodem.ErrNotSupported) {
			return httpapi.BadRequest(c, "airplane_mode_unsupported", err)
		}
		return httpapi.Internal(c, "set_airplane_mode_failed", err)
	}
	return c.NoContent(http.StatusNoContent)
}
