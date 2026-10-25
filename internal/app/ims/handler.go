package ims

import (
	"context"
	"errors"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/httpapi"
	"github.com/damonto/sigmo/internal/app/websheet"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

type Handler struct {
	registry     modemFinder
	connectivity connectivityHTTP
}

type modemFinder interface {
	Find(context.Context, string) (*mmodem.Modem, error)
}

type connectivityHTTP interface {
	WiFiCallingStatus(context.Context, *mmodem.Modem) (WiFiCallingStatus, error)
	ReplaceWiFiCallingSettings(context.Context, *mmodem.Modem, WiFiCallingSettings) error
	ReconnectWiFiCalling(context.Context, *mmodem.Modem) error
	DisconnectWiFiCalling(context.Context, *mmodem.Modem) error
	WiFiCallingEmergencyAddressUpdateAvailable(context.Context, *mmodem.Modem) bool
	StartWiFiCallingWebsheet(context.Context, *mmodem.Modem) (websheet.Info, error)
	StartWiFiCallingEmergencyAddressUpdate(context.Context, *mmodem.Modem) (websheet.Info, error)
	VoLTEStatus(context.Context, *mmodem.Modem) (VoLTEStatus, error)
	ReplaceVoLTESettings(context.Context, *mmodem.Modem, VoLTESettings) error
}

type wifiCallingStatusReader interface {
	WiFiCallingStatus(context.Context, *mmodem.Modem) (WiFiCallingStatus, error)
	WiFiCallingEmergencyAddressUpdateAvailable(context.Context, *mmodem.Modem) bool
}

type voLTEStatusReader interface {
	VoLTEStatus(context.Context, *mmodem.Modem) (VoLTEStatus, error)
}

type WiFiCallingSettingsRequest struct {
	Enabled  bool              `json:"enabled"`
	Underlay *UnderlaySettings `json:"underlay" validate:"required"`
}

type WiFiCallingSettingsResponse struct {
	LastError                       string           `json:"lastError,omitempty" jsonschema:"most recent IMS connection failure; cleared after successful registration"`
	Enabled                         bool             `json:"enabled" jsonschema:"whether Wi-Fi Calling is enabled in Sigmo settings"`
	Underlay                        UnderlaySettings `json:"underlay" jsonschema:"outer network selected for Wi-Fi Calling"`
	Connected                       bool             `json:"connected" jsonschema:"whether the modem currently has an active Wi-Fi Calling IMS connection"`
	State                           string           `json:"state" jsonschema:"current Wi-Fi Calling state, such as idle, connecting, connected, or disconnected"`
	DurationSeconds                 int64            `json:"durationSeconds" jsonschema:"elapsed time of the current Wi-Fi Calling connection in seconds"`
	EmergencyAddressUpdateAvailable bool             `json:"emergencyAddressUpdateAvailable" jsonschema:"whether an emergency-address update flow is available for this modem"`
	Websheet                        *websheet.Info   `json:"websheet" jsonschema:"pending carrier interaction page; null when no websheet is pending"`
}

type VoLTESettingsRequest struct {
	Enabled  bool     `json:"enabled"`
	DataPath DataPath `json:"dataPath"`
}

type VoLTESettingsResponse struct {
	Enabled         bool     `json:"enabled" jsonschema:"whether VoLTE is enabled in Sigmo settings"`
	Connected       bool     `json:"connected" jsonschema:"whether the modem currently has an active VoLTE IMS connection"`
	State           string   `json:"state" jsonschema:"current VoLTE state, such as idle, connecting, connected, or disconnected"`
	DurationSeconds int64    `json:"durationSeconds" jsonschema:"elapsed time of the current VoLTE connection in seconds"`
	ModemRegistered bool     `json:"modemRegistered" jsonschema:"whether the modem reports IMS registration"`
	DataPath        DataPath `json:"dataPath" jsonschema:"data path used for VoLTE traffic, such as qmap, legacy_bam_dmux, or qualcomm_410"`
}

func RegisterRoutes(group *echo.Group, registry *mmodem.Registry, connectivity *Connectivity) {
	h := &Handler{registry: registry, connectivity: connectivity}
	group.GET("/modems/:id/wifi-calling/settings", h.WiFiCallingSettings)
	group.PUT("/modems/:id/wifi-calling/settings", h.UpdateWiFiCallingSettings)
	group.POST("/modems/:id/wifi-calling/sessions", h.CreateWiFiCallingSession)
	group.DELETE("/modems/:id/wifi-calling/sessions/current", h.DeleteWiFiCallingSession)
	group.POST("/modems/:id/wifi-calling/websheets", h.CreateWiFiCallingWebsheet)
	group.POST("/modems/:id/wifi-calling/emergency-address-websheets", h.CreateWiFiCallingEmergencyAddressWebsheet)
	group.GET("/modems/:id/volte/settings", h.VoLTESettings)
	group.PUT("/modems/:id/volte/settings", h.UpdateVoLTESettings)
}

func ReadWiFiCallingSettings(ctx context.Context, modem *mmodem.Modem, connectivity wifiCallingStatusReader) (WiFiCallingSettingsResponse, error) {
	status, err := connectivity.WiFiCallingStatus(ctx, modem)
	if err != nil {
		return WiFiCallingSettingsResponse{}, err
	}
	return WiFiCallingSettingsResponse{
		LastError:                       status.LastError,
		Enabled:                         status.Enabled,
		Underlay:                        status.Underlay,
		Connected:                       status.Connected,
		State:                           status.State,
		DurationSeconds:                 status.DurationSeconds,
		EmergencyAddressUpdateAvailable: connectivity.WiFiCallingEmergencyAddressUpdateAvailable(ctx, modem),
		Websheet:                        status.Websheet,
	}, nil
}

func ReadVoLTESettings(ctx context.Context, modem *mmodem.Modem, connectivity voLTEStatusReader) (VoLTESettingsResponse, error) {
	return defaultManagedVoLTEOps().readSettings(ctx, modem, connectivity)
}

func (ops managedVoLTEOps) readSettings(ctx context.Context, modem *mmodem.Modem, connectivity voLTEStatusReader) (VoLTESettingsResponse, error) {
	status, err := connectivity.VoLTEStatus(ctx, modem)
	if err != nil {
		return VoLTESettingsResponse{}, err
	}
	modemRegistered := false
	if !modem.Snapshot().AirplaneMode() {
		modemStatus, err := ops.readStatus(ctx, modem)
		if err != nil {
			return VoLTESettingsResponse{}, err
		}
		modemRegistered = modemStatus.Occupied
	}
	return VoLTESettingsResponse{
		Enabled:         status.Enabled,
		Connected:       status.Connected,
		State:           status.State,
		DurationSeconds: status.DurationSeconds,
		ModemRegistered: modemRegistered,
		DataPath:        status.DataPath,
	}, nil
}

func (h *Handler) VoLTESettings(c *echo.Context) error {
	ctx := c.Request().Context()
	modem, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "get_volte_settings_failed")
	}
	response, err := ReadVoLTESettings(ctx, modem, h.connectivity)
	if err != nil {
		return httpapi.Internal(c, "get_volte_settings_failed", err)
	}
	return c.JSON(http.StatusOK, response)
}

func (h *Handler) UpdateVoLTESettings(c *echo.Context) error {
	ctx := c.Request().Context()
	modem, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "update_volte_settings_failed")
	}
	var req VoLTESettingsRequest
	if err := c.Bind(&req); err != nil {
		return httpapi.BadRequest(c, "update_volte_settings_invalid_request", err)
	}
	if err := h.connectivity.ReplaceVoLTESettings(ctx, modem, VoLTESettings{
		Enabled:  req.Enabled,
		DataPath: req.DataPath,
	}); err != nil {
		switch {
		case errors.Is(err, ErrVoLTEDataPathRequired), errors.Is(err, ErrVoLTEDataPathUnsupported):
			return httpapi.UnprocessableEntity(c, "update_volte_settings_invalid_request", err)
		case errors.Is(err, ErrVoLTEAirplaneMode):
			return httpapi.Error(c, http.StatusConflict, "volte_airplane_mode", err.Error())
		case errors.Is(err, ErrUnavailable):
			return httpapi.BadRequest(c, "volte_unavailable", err)
		default:
			return httpapi.Internal(c, "update_volte_settings_failed", err)
		}
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) UpdateWiFiCallingSettings(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "update_ims_settings_failed")
	}
	var req WiFiCallingSettingsRequest
	if err := httpapi.BindAndValidate(c, &req, "update_ims_settings_invalid_request"); err != nil {
		return err
	}
	if req.Underlay == nil {
		return httpapi.UnprocessableEntity(c, "update_ims_settings_invalid_request", ErrInvalidWiFiCallingUnderlay)
	}
	if err := h.connectivity.ReplaceWiFiCallingSettings(c.Request().Context(), modem, WiFiCallingSettings{
		Enabled:  req.Enabled,
		Underlay: *req.Underlay,
	}); err != nil {
		if errors.Is(err, ErrInvalidWiFiCallingUnderlay) {
			return httpapi.UnprocessableEntity(c, "update_ims_settings_invalid_request", err)
		}
		return httpapi.Internal(c, "update_ims_settings_failed", err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) WiFiCallingSettings(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "get_ims_settings_failed")
	}
	response, err := ReadWiFiCallingSettings(c.Request().Context(), modem, h.connectivity)
	if err != nil {
		return httpapi.Internal(c, "get_ims_settings_failed", err)
	}
	return c.JSON(http.StatusOK, response)
}

func (h *Handler) CreateWiFiCallingSession(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "create_ims_session_failed")
	}
	if err := h.connectivity.ReconnectWiFiCalling(c.Request().Context(), modem); err != nil {
		if errors.Is(err, ErrNotConnected) || errors.Is(err, ErrUnavailable) {
			return httpapi.BadRequest(c, "ims_session_unavailable", err)
		}
		return httpapi.Internal(c, "create_ims_session_failed", err)
	}
	return c.NoContent(http.StatusAccepted)
}

func (h *Handler) DeleteWiFiCallingSession(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "delete_ims_session_failed")
	}
	if err := h.connectivity.DisconnectWiFiCalling(c.Request().Context(), modem); err != nil {
		return httpapi.Internal(c, "delete_ims_session_failed", err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) CreateWiFiCallingWebsheet(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "start_ims_websheet_failed")
	}
	info, err := h.connectivity.StartWiFiCallingWebsheet(c.Request().Context(), modem)
	if err != nil {
		if errors.Is(err, ErrWebsheetNotPending) {
			return httpapi.BadRequest(c, "ims_websheet_not_pending", err)
		}
		return httpapi.Internal(c, "start_ims_websheet_failed", err)
	}
	return c.JSON(http.StatusCreated, info)
}

func (h *Handler) CreateWiFiCallingEmergencyAddressWebsheet(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "start_ims_e911_websheet_failed")
	}
	info, err := h.connectivity.StartWiFiCallingEmergencyAddressUpdate(c.Request().Context(), modem)
	if err != nil {
		return wifiCallingWebsheetStartError(c, "start_ims_e911_websheet_failed", err)
	}
	return c.JSON(http.StatusCreated, info)
}

func wifiCallingWebsheetStartError(c *echo.Context, fallbackCode string, err error) error {
	switch {
	case errors.Is(err, ErrWiFiCallingSetupPending):
		return httpapi.TooManyRequests(c, "ims_setup_pending", err)
	case errors.Is(err, ErrWiFiCallingSetupDenied):
		return httpapi.BadRequest(c, "ims_setup_denied", err)
	case errors.Is(err, ErrUnavailable), errors.Is(err, ErrWebsheetUnavailable):
		return httpapi.BadRequest(c, "ims_websheet_unavailable", err)
	default:
		return httpapi.Internal(c, fallbackCode, err)
	}
}
