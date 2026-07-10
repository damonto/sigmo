//go:build ims

package ims

import (
	"context"
	"errors"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/httpapi"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/pro/websheet"
)

type Handler struct {
	registry    modemFinder
	wifiCalling Coordinator
}

type modemFinder interface {
	Find(context.Context, string) (*mmodem.Modem, error)
}

type UpdateSettingsRequest struct {
	Enabled   bool `json:"enabled"`
	Preferred bool `json:"preferred"`
}

type SettingsResponse struct {
	Enabled                         bool           `json:"enabled"`
	Preferred                       bool           `json:"preferred"`
	Connected                       bool           `json:"connected"`
	State                           string         `json:"state"`
	DurationSeconds                 int64          `json:"durationSeconds"`
	EmergencyAddressUpdateAvailable bool           `json:"emergencyAddressUpdateAvailable"`
	Websheet                        *websheet.Info `json:"websheet,omitempty"`
}

const (
	errorCodeGetSettingsFailed            = "get_ims_settings_failed"
	errorCodeUpdateSettingsInvalidRequest = "update_ims_settings_invalid_request"
	errorCodeUpdateSettingsFailed         = "update_ims_settings_failed"
	errorCodeCreateSessionFailed          = "create_ims_session_failed"
	errorCodeDeleteSessionFailed          = "delete_ims_session_failed"
	errorCodeSessionUnavailable           = "ims_session_unavailable"
	errorCodeStartWebsheetFailed          = "start_ims_websheet_failed"
	errorCodeStartE911WebsheetFailed      = "start_ims_e911_websheet_failed"
	errorCodeWebsheetNotPending           = "ims_websheet_not_pending"
	errorCodeSetupPending                 = "ims_setup_pending"
	errorCodeSetupDenied                  = "ims_setup_denied"
	errorCodeWebsheetUnavailable          = "ims_websheet_unavailable"
)

func RegisterRoutes(group *echo.Group, registry *mmodem.Registry, wifiCalling Coordinator) {
	h := &Handler{registry: registry, wifiCalling: wifiCalling}
	group.GET("/modems/:id/wifi-calling/settings", h.Settings)
	group.PUT("/modems/:id/wifi-calling/settings", h.UpdateSettings)
	group.POST("/modems/:id/wifi-calling/sessions", h.CreateSession)
	group.DELETE("/modems/:id/wifi-calling/sessions/current", h.DeleteSession)
	group.POST("/modems/:id/wifi-calling/websheets", h.StartWebsheet)
	group.POST("/modems/:id/wifi-calling/emergency-address-websheets", h.StartEmergencyAddressWebsheet)
}

func (h *Handler) UpdateSettings(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeUpdateSettingsFailed)
	}
	var req UpdateSettingsRequest
	if err := httpapi.BindAndValidate(c, &req, errorCodeUpdateSettingsInvalidRequest); err != nil {
		return err
	}
	if err := h.wifiCalling.UpdateSettings(c.Request().Context(), modem, Settings{
		Enabled:   req.Enabled,
		Preferred: req.Preferred,
	}); err != nil {
		return httpapi.Internal(c, errorCodeUpdateSettingsFailed, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) Settings(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeGetSettingsFailed)
	}
	status, err := h.wifiCalling.Status(c.Request().Context(), modem)
	if err != nil {
		return httpapi.Internal(c, errorCodeGetSettingsFailed, err)
	}
	return c.JSON(http.StatusOK, SettingsResponse{
		Enabled:                         status.Enabled,
		Preferred:                       status.Preferred,
		Connected:                       status.Connected,
		State:                           status.State,
		DurationSeconds:                 status.DurationSeconds,
		EmergencyAddressUpdateAvailable: h.wifiCalling.EmergencyAddressUpdateAvailable(c.Request().Context(), modem),
		Websheet:                        status.Websheet,
	})
}

func (h *Handler) CreateSession(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeCreateSessionFailed)
	}
	if err := h.wifiCalling.Reconnect(c.Request().Context(), modem); err != nil {
		if errors.Is(err, ErrNotConnected) || errors.Is(err, ErrUnavailable) {
			return httpapi.BadRequest(c, errorCodeSessionUnavailable, err)
		}
		return httpapi.Internal(c, errorCodeCreateSessionFailed, err)
	}
	return c.NoContent(http.StatusAccepted)
}

func (h *Handler) DeleteSession(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeDeleteSessionFailed)
	}
	if err := h.wifiCalling.Disconnect(c.Request().Context(), modem); err != nil {
		return httpapi.Internal(c, errorCodeDeleteSessionFailed, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) StartWebsheet(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeStartWebsheetFailed)
	}
	info, err := h.wifiCalling.StartWebsheet(c.Request().Context(), modem)
	if err != nil {
		if errors.Is(err, ErrWebsheetNotPending) {
			return httpapi.BadRequest(c, errorCodeWebsheetNotPending, err)
		}
		return httpapi.Internal(c, errorCodeStartWebsheetFailed, err)
	}
	return c.JSON(http.StatusCreated, info)
}

func (h *Handler) StartEmergencyAddressWebsheet(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeStartE911WebsheetFailed)
	}
	info, err := h.wifiCalling.StartEmergencyAddressUpdate(c.Request().Context(), modem)
	if err != nil {
		return wifiCallingWebsheetStartError(c, errorCodeStartE911WebsheetFailed, err)
	}
	return c.JSON(http.StatusCreated, info)
}

func wifiCallingWebsheetStartError(c *echo.Context, fallbackCode string, err error) error {
	switch {
	case errors.Is(err, ErrWiFiCallingSetupPending):
		return httpapi.TooManyRequests(c, errorCodeSetupPending, err)
	case errors.Is(err, ErrWiFiCallingSetupDenied):
		return httpapi.BadRequest(c, errorCodeSetupDenied, err)
	case errors.Is(err, ErrUnavailable), errors.Is(err, ErrWebsheetUnavailable):
		return httpapi.BadRequest(c, errorCodeWebsheetUnavailable, err)
	default:
		return httpapi.Internal(c, fallbackCode, err)
	}
}
