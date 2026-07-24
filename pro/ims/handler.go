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
	volte       Coordinator
}

type modemFinder interface {
	Find(context.Context, string) (*mmodem.Modem, error)
}

type UpdateSettingsRequest struct {
	Enabled bool `json:"enabled"`
}

type SettingsResponse struct {
	Enabled                         bool           `json:"enabled" jsonschema:"whether Wi-Fi Calling is enabled in Sigmo settings"`
	Connected                       bool           `json:"connected" jsonschema:"whether the modem currently has an active Wi-Fi Calling IMS connection"`
	State                           string         `json:"state" jsonschema:"current Wi-Fi Calling state, such as idle, connecting, connected, or disconnected"`
	DurationSeconds                 int64          `json:"durationSeconds" jsonschema:"elapsed time of the current Wi-Fi Calling connection in seconds"`
	EmergencyAddressUpdateAvailable bool           `json:"emergencyAddressUpdateAvailable" jsonschema:"whether an emergency-address update flow is available for this modem"`
	Websheet                        *websheet.Info `json:"websheet" jsonschema:"pending carrier interaction page; null when no websheet is pending"`
}

type UpdateVoLTESettingsRequest struct {
	Enabled            bool     `json:"enabled"`
	DataPath           DataPath `json:"dataPath"`
	SetIMSAPNAsDefault bool     `json:"setIMSAPNAsDefault"`
	EnablePCSCFViaPCO  bool     `json:"enablePCSCFViaPCO"`
}

type VoLTESettingsResponse struct {
	Enabled            bool     `json:"enabled" jsonschema:"whether VoLTE is enabled in Sigmo settings"`
	Connected          bool     `json:"connected" jsonschema:"whether the modem currently has an active VoLTE IMS connection"`
	State              string   `json:"state" jsonschema:"current VoLTE state, such as idle, connecting, connected, or disconnected"`
	DurationSeconds    int64    `json:"durationSeconds" jsonschema:"elapsed time of the current VoLTE connection in seconds"`
	ModemRegistered    bool     `json:"modemRegistered" jsonschema:"whether the modem reports IMS registration"`
	DataPath           DataPath `json:"dataPath" jsonschema:"data path used for VoLTE traffic, such as qmap or mbim"`
	SetIMSAPNAsDefault bool     `json:"setIMSAPNAsDefault" jsonschema:"whether the IMS APN profile is configured as the modem default"`
	EnablePCSCFViaPCO  bool     `json:"enablePCSCFViaPCO" jsonschema:"whether P-CSCF discovery through protocol configuration options is enabled"`
}

const (
	errorCodeGetSettingsFailed              = "get_ims_settings_failed"
	errorCodeUpdateSettingsInvalidRequest   = "update_ims_settings_invalid_request"
	errorCodeUpdateSettingsFailed           = "update_ims_settings_failed"
	errorCodeCreateSessionFailed            = "create_ims_session_failed"
	errorCodeDeleteSessionFailed            = "delete_ims_session_failed"
	errorCodeSessionUnavailable             = "ims_session_unavailable"
	errorCodeStartWebsheetFailed            = "start_ims_websheet_failed"
	errorCodeStartE911WebsheetFailed        = "start_ims_e911_websheet_failed"
	errorCodeWebsheetNotPending             = "ims_websheet_not_pending"
	errorCodeSetupPending                   = "ims_setup_pending"
	errorCodeSetupDenied                    = "ims_setup_denied"
	errorCodeWebsheetUnavailable            = "ims_websheet_unavailable"
	errorCodeGetVoLTESettingsFailed         = "get_volte_settings_failed"
	errorCodeUpdateVoLTEInvalidRequest      = "update_volte_settings_invalid_request"
	errorCodeUpdateVoLTESettingsFailed      = "update_volte_settings_failed"
	errorCodeVoLTEUnavailable               = "volte_unavailable"
	errorCodeVoLTEProfileOptionsUnsupported = "volte_profile_options_unsupported"
)

func RegisterRoutes(group *echo.Group, registry *mmodem.Registry, wifiCalling Coordinator, volte Coordinator) {
	h := &Handler{registry: registry, wifiCalling: wifiCalling, volte: volte}
	group.GET("/modems/:id/wifi-calling/settings", h.Settings)
	group.PUT("/modems/:id/wifi-calling/settings", h.UpdateSettings)
	group.POST("/modems/:id/wifi-calling/sessions", h.CreateSession)
	group.DELETE("/modems/:id/wifi-calling/sessions/current", h.DeleteSession)
	group.POST("/modems/:id/wifi-calling/websheets", h.StartWebsheet)
	group.POST("/modems/:id/wifi-calling/emergency-address-websheets", h.StartEmergencyAddressWebsheet)
	group.GET("/modems/:id/volte/settings", h.VoLTESettings)
	group.PUT("/modems/:id/volte/settings", h.UpdateVoLTESettings)
}

func ReadWiFiCallingSettings(ctx context.Context, modem *mmodem.Modem, coordinator Coordinator) (SettingsResponse, error) {
	status, err := coordinator.Status(ctx, modem)
	if err != nil {
		return SettingsResponse{}, err
	}
	return SettingsResponse{
		Enabled:                         status.Enabled,
		Connected:                       status.Connected,
		State:                           status.State,
		DurationSeconds:                 status.DurationSeconds,
		EmergencyAddressUpdateAvailable: coordinator.EmergencyAddressUpdateAvailable(ctx, modem),
		Websheet:                        status.Websheet,
	}, nil
}

func ReadVoLTESettings(ctx context.Context, modem *mmodem.Modem, coordinator Coordinator) (VoLTESettingsResponse, error) {
	status, err := coordinator.Status(ctx, modem)
	if err != nil {
		return VoLTESettingsResponse{}, err
	}
	modemStatus, err := readVoLTEStatus(ctx, modem)
	if err != nil {
		return VoLTESettingsResponse{}, err
	}
	return VoLTESettingsResponse{
		Enabled:            status.Enabled,
		Connected:          status.Connected,
		State:              status.State,
		DurationSeconds:    status.DurationSeconds,
		ModemRegistered:    modemStatus.Occupied,
		DataPath:           status.DataPath,
		SetIMSAPNAsDefault: status.SetIMSAPNAsDefault,
		EnablePCSCFViaPCO:  status.EnablePCSCFViaPCO,
	}, nil
}

func (h *Handler) VoLTESettings(c *echo.Context) error {
	ctx := c.Request().Context()
	modem, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeGetVoLTESettingsFailed)
	}
	response, err := ReadVoLTESettings(ctx, modem, h.volte)
	if err != nil {
		return httpapi.Internal(c, errorCodeGetVoLTESettingsFailed, err)
	}
	return c.JSON(http.StatusOK, response)
}

func (h *Handler) UpdateVoLTESettings(c *echo.Context) error {
	ctx := c.Request().Context()
	modem, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeUpdateVoLTESettingsFailed)
	}
	var req UpdateVoLTESettingsRequest
	if err := c.Bind(&req); err != nil {
		return httpapi.BadRequest(c, errorCodeUpdateVoLTEInvalidRequest, err)
	}
	if err := UpdateVoLTESettings(ctx, modem, h.volte, Settings{
		Enabled:            req.Enabled,
		DataPath:           req.DataPath,
		SetIMSAPNAsDefault: req.SetIMSAPNAsDefault,
		EnablePCSCFViaPCO:  req.EnablePCSCFViaPCO,
	}); err != nil {
		switch {
		case errors.Is(err, ErrVoLTEDataPathRequired), errors.Is(err, ErrVoLTEDataPathUnsupported):
			return httpapi.UnprocessableEntity(c, errorCodeUpdateVoLTEInvalidRequest, err)
		case errors.Is(err, ErrVoLTEProfileOptionsUnsupported):
			return httpapi.UnprocessableEntity(c, errorCodeVoLTEProfileOptionsUnsupported, err)
		case errors.Is(err, ErrUnavailable):
			return httpapi.BadRequest(c, errorCodeVoLTEUnavailable, err)
		default:
			return httpapi.Internal(c, errorCodeUpdateVoLTESettingsFailed, err)
		}
	}
	return c.NoContent(http.StatusNoContent)
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
		Enabled: req.Enabled,
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
	response, err := ReadWiFiCallingSettings(c.Request().Context(), modem, h.wifiCalling)
	if err != nil {
		return httpapi.Internal(c, errorCodeGetSettingsFailed, err)
	}
	return c.JSON(http.StatusOK, response)
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
