package modem

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/httpapi"
	"github.com/damonto/sigmo/internal/pkg/internet"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/settings"
	"github.com/damonto/sigmo/internal/pkg/wificalling"
)

type Handler struct {
	registry    *mmodem.Registry
	catalog     *catalog
	simSlot     *simSlot
	msisdn      *msisdn
	settings    *modemSettings
	internet    *internet.Connector
	wifiCalling wificalling.Coordinator
}

const (
	switchSimSlotTimeout = time.Minute
	updateMSISDNTimeout  = time.Minute
)

const (
	errorCodeListModemsFailed                        = "list_modems_failed"
	errorCodeGetModemFailed                          = "get_modem_failed"
	errorCodeSwitchSimSlotFailed                     = "switch_sim_slot_failed"
	errorCodeSimIdentifierRequired                   = "sim_identifier_required"
	errorCodeSimSlotsUnavailable                     = "sim_slots_unavailable"
	errorCodeSimSlotNotFound                         = "sim_slot_not_found"
	errorCodeSimSlotAlreadyActive                    = "sim_slot_already_active"
	errorCodeSimSlotSwitchTimeout                    = "sim_slot_switch_timeout"
	errorCodeUnlockSIMInvalidRequest                 = "unlock_sim_invalid_request"
	errorCodeUnlockSIMNotRequired                    = "unlock_sim_not_required"
	errorCodeUnlockSIMUnsupportedLock                = "unlock_sim_unsupported_lock"
	errorCodeUnlockSIMFailed                         = "unlock_sim_failed"
	errorCodeEnableModemAfterUnlockFailed            = "enable_modem_after_unlock_failed"
	errorCodeUpdateMSISDNInvalidRequest              = "update_msisdn_invalid_request"
	errorCodeUpdateMSISDNFailed                      = "update_msisdn_failed"
	errorCodeInvalidPhoneNumber                      = "invalid_phone_number"
	errorCodeUpdateSettingsInvalidRequest            = "update_settings_invalid_request"
	errorCodeUpdateSettingsFailed                    = "update_settings_failed"
	errorCodeCompatibleRequired                      = "compatible_required"
	errorCodeGetSettingsFailed                       = "get_settings_failed"
	errorCodeGetWiFiCallingSettingsFailed            = "get_wifi_calling_settings_failed"
	errorCodeUpdateWiFiCallingSettingsInvalidRequest = "update_wifi_calling_settings_invalid_request"
	errorCodeUpdateWiFiCallingSettingsFailed         = "update_wifi_calling_settings_failed"
	errorCodeStartWiFiCallingWebsheetFailed          = "start_wifi_calling_websheet_failed"
	errorCodeStartWiFiCallingE911WebsheetFailed      = "start_wifi_calling_e911_websheet_failed"
	errorCodeWiFiCallingWebsheetNotPending           = "wifi_calling_websheet_not_pending"
	errorCodeWiFiCallingSetupPending                 = "wifi_calling_setup_pending"
	errorCodeWiFiCallingSetupDenied                  = "wifi_calling_setup_denied"
	errorCodeWiFiCallingWebsheetUnavailable          = "wifi_calling_websheet_unavailable"
)

var (
	errSwitchSimSlotTimeout = errors.New("switching SIM slot timed out, please refresh to confirm the active slot")
	errUpdateMSISDNTimeout  = errors.New("updating MSISDN timed out, please refresh to confirm the active slot")
)

func New(store *settings.Store, registry *mmodem.Registry, internetConnector *internet.Connector, wifiCalling wificalling.Coordinator) *Handler {
	return &Handler{
		registry:    registry,
		catalog:     newCatalog(store, registry, wifiCalling),
		simSlot:     newSIMSlot(registry),
		msisdn:      newMSISDN(store, registry),
		settings:    newSettings(store),
		internet:    internetConnector,
		wifiCalling: wifiCalling,
	}
}

func (h *Handler) List(c *echo.Context) error {
	response, err := h.catalog.List(c.Request().Context())
	if err != nil {
		return httpapi.Internal(c, errorCodeListModemsFailed, err)
	}
	return c.JSON(http.StatusOK, response)
}

func (h *Handler) Get(c *echo.Context) error {
	ctx := c.Request().Context()
	modem, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeGetModemFailed)
	}
	response, err := h.catalog.Get(ctx, modem)
	if err != nil {
		return httpapi.Internal(c, errorCodeGetModemFailed, err)
	}
	return c.JSON(http.StatusOK, response)
}

func (h *Handler) UnlockSIM(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeUnlockSIMFailed)
	}
	var req UnlockSIMRequest
	if err := c.Bind(&req); err != nil {
		return httpapi.BadRequest(c, errorCodeUnlockSIMInvalidRequest, err)
	}
	if err := modem.UnlockSIMPinAndEnable(c.Request().Context(), req.PIN); err != nil {
		return unlockSIMError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func unlockSIMError(c *echo.Context, err error) error {
	switch {
	case errors.Is(err, mmodem.ErrSIMPinRequired):
		return httpapi.BadRequest(c, errorCodeUnlockSIMInvalidRequest, err)
	case errors.Is(err, mmodem.ErrSIMUnlockNotRequired):
		return httpapi.BadRequest(c, errorCodeUnlockSIMNotRequired, err)
	case errors.Is(err, mmodem.ErrSIMUnlockUnsupportedLock):
		return httpapi.BadRequest(c, errorCodeUnlockSIMUnsupportedLock, err)
	case errors.Is(err, mmodem.ErrEnableAfterSIMUnlock):
		return httpapi.Internal(c, errorCodeEnableModemAfterUnlockFailed, err)
	default:
		return httpapi.Internal(c, errorCodeUnlockSIMFailed, err)
	}
}

func (h *Handler) SwitchSimSlot(c *echo.Context) error {
	requestCtx := c.Request().Context()
	modem, err := h.registry.Find(requestCtx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeSwitchSimSlotFailed)
	}
	slotIndex, err := h.simSlot.targetIndex(requestCtx, modem, c.Param("identifier"))
	if err != nil {
		if errors.Is(err, errSimIdentifierRequired) {
			return httpapi.BadRequest(c, errorCodeSimIdentifierRequired, err)
		}
		if errors.Is(err, errSimSlotsUnavailable) {
			return httpapi.BadRequest(c, errorCodeSimSlotsUnavailable, err)
		}
		if errors.Is(err, errSimSlotNotFound) {
			return httpapi.BadRequest(c, errorCodeSimSlotNotFound, err)
		}
		if errors.Is(err, errSimSlotAlreadyActive) {
			return httpapi.BadRequest(c, errorCodeSimSlotAlreadyActive, err)
		}
		return httpapi.Internal(c, errorCodeSwitchSimSlotFailed, err)
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), switchSimSlotTimeout)
	defer cancel()

	if err := h.internet.Restore(ctx, modem); err != nil {
		return httpapi.Internal(c, errorCodeSwitchSimSlotFailed, err)
	}
	if err := h.simSlot.Switch(ctx, modem, slotIndex); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return httpapi.RequestTimeout(c, errorCodeSimSlotSwitchTimeout, errSwitchSimSlotTimeout)
		}
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return httpapi.Internal(c, errorCodeSwitchSimSlotFailed, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) UpdateMSISDN(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeUpdateMSISDNFailed)
	}
	var req UpdateMSISDNRequest
	if err := httpapi.BindAndValidate(c, &req, errorCodeUpdateMSISDNInvalidRequest); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), updateMSISDNTimeout)
	defer cancel()

	if err := h.msisdn.Update(ctx, modem, req.Number); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return httpapi.RequestTimeout(c, "msisdn_update_timeout", errUpdateMSISDNTimeout)
		}
		if errors.Is(err, errMSISDNInvalidNumber) {
			return httpapi.BadRequest(c, errorCodeInvalidPhoneNumber, err)
		}
		return httpapi.Internal(c, errorCodeUpdateMSISDNFailed, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) UpdateSettings(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeUpdateSettingsFailed)
	}
	var req UpdateModemSettingsRequest
	if err := httpapi.BindAndValidate(c, &req, errorCodeUpdateSettingsInvalidRequest); err != nil {
		return err
	}
	if err := h.settings.Update(c.Request().Context(), modem, req); err != nil {
		if errors.Is(err, errCompatibleRequired) {
			return httpapi.BadRequest(c, errorCodeCompatibleRequired, err)
		}
		return httpapi.Internal(c, errorCodeUpdateSettingsFailed, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) GetSettings(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeGetSettingsFailed)
	}
	response := h.settings.Get(modem)
	return c.JSON(http.StatusOK, response)
}

func (h *Handler) UpdateWiFiCallingSettings(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeUpdateWiFiCallingSettingsFailed)
	}
	var req UpdateWiFiCallingSettingsRequest
	if err := httpapi.BindAndValidate(c, &req, errorCodeUpdateWiFiCallingSettingsInvalidRequest); err != nil {
		return err
	}
	if err := h.wifiCalling.UpdateSettings(c.Request().Context(), modem, wificalling.Settings{
		Enabled:   req.Enabled,
		Preferred: req.Preferred,
	}); err != nil {
		return httpapi.Internal(c, errorCodeUpdateWiFiCallingSettingsFailed, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) GetWiFiCallingSettings(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeGetWiFiCallingSettingsFailed)
	}
	status, err := h.wifiCalling.Status(c.Request().Context(), modem)
	if err != nil {
		return httpapi.Internal(c, errorCodeGetWiFiCallingSettingsFailed, err)
	}
	return c.JSON(http.StatusOK, WiFiCallingSettingsResponse{
		Enabled:                         status.Enabled,
		Preferred:                       status.Preferred,
		Connected:                       status.Connected,
		State:                           status.State,
		DurationSeconds:                 status.DurationSeconds,
		EmergencyAddressUpdateAvailable: h.wifiCalling.EmergencyAddressUpdateAvailable(c.Request().Context(), modem),
		Websheet:                        status.Websheet,
	})
}

func (h *Handler) StartWiFiCallingWebsheet(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeStartWiFiCallingWebsheetFailed)
	}
	info, err := h.wifiCalling.StartWebsheet(c.Request().Context(), modem)
	if err != nil {
		if errors.Is(err, wificalling.ErrWebsheetNotPending) {
			return httpapi.BadRequest(c, errorCodeWiFiCallingWebsheetNotPending, err)
		}
		return httpapi.Internal(c, errorCodeStartWiFiCallingWebsheetFailed, err)
	}
	return c.JSON(http.StatusCreated, info)
}

func (h *Handler) StartWiFiCallingEmergencyAddressWebsheet(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeStartWiFiCallingE911WebsheetFailed)
	}
	info, err := h.wifiCalling.StartEmergencyAddressUpdate(c.Request().Context(), modem)
	if err != nil {
		return h.wifiCallingWebsheetStartError(c, errorCodeStartWiFiCallingE911WebsheetFailed, err)
	}
	return c.JSON(http.StatusCreated, info)
}

func (h *Handler) wifiCallingWebsheetStartError(c *echo.Context, fallbackCode string, err error) error {
	switch {
	case errors.Is(err, wificalling.ErrWFCSetupPending):
		return httpapi.TooManyRequests(c, errorCodeWiFiCallingSetupPending, err)
	case errors.Is(err, wificalling.ErrWFCSetupDenied):
		return httpapi.BadRequest(c, errorCodeWiFiCallingSetupDenied, err)
	case errors.Is(err, wificalling.ErrUnavailable), errors.Is(err, wificalling.ErrWebsheetUnavailable):
		return httpapi.BadRequest(c, errorCodeWiFiCallingWebsheetUnavailable, err)
	default:
		return httpapi.Internal(c, fallbackCode, err)
	}
}
