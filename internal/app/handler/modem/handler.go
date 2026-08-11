package modem

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/httpapi"
	"github.com/damonto/sigmo/internal/app/modemstatus"
	"github.com/damonto/sigmo/internal/pkg/internet"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/reminder"
	"github.com/damonto/sigmo/internal/pkg/settings"
)

type Handler struct {
	registry  *mmodem.Registry
	catalog   *catalog
	simSlot   *simSlot
	msisdn    *msisdn
	settings  *modemSettings
	internet  *internet.Connector
	reminders *reminder.Scheduler
}

const (
	switchSIMSlotTimeout = time.Minute
	updateMSISDNTimeout  = time.Minute
)

const (
	errorCodeListModemsFailed             = "list_modems_failed"
	errorCodeGetModemFailed               = "get_modem_failed"
	errorCodeSwitchSIMSlotFailed          = "switch_sim_slot_failed"
	errorCodeSIMSlotRequired              = "sim_slot_required"
	errorCodeSIMSlotsUnavailable          = "sim_slots_unavailable"
	errorCodeSIMSlotNotFound              = "sim_slot_not_found"
	errorCodeSIMSlotAlreadyActive         = "sim_slot_already_active"
	errorCodeSIMSlotSwitchTimeout         = "sim_slot_switch_timeout"
	errorCodeUnlockSIMInvalidRequest      = "unlock_sim_invalid_request"
	errorCodeUnlockSIMNotRequired         = "unlock_sim_not_required"
	errorCodeUnlockSIMFailed              = "unlock_sim_failed"
	errorCodeEnableModemAfterUnlockFailed = "enable_modem_after_unlock_failed"
	errorCodeUpdateMSISDNInvalidRequest   = "update_msisdn_invalid_request"
	errorCodeUpdateMSISDNFailed           = "update_msisdn_failed"
	errorCodeInvalidPhoneNumber           = "invalid_phone_number"
	errorCodeUpdateSettingsInvalidRequest = "update_settings_invalid_request"
	errorCodeUpdateSettingsFailed         = "update_settings_failed"
	errorCodeGetSettingsFailed            = "get_settings_failed"
)

var (
	errSwitchSIMSlotTimeout = errors.New("switching SIM slot timed out, please refresh to confirm the active slot")
	errUpdateMSISDNTimeout  = errors.New("updating MSISDN timed out, please refresh to confirm the active slot")
)

func New(store *settings.Store, registry *mmodem.Registry, internetConnector *internet.Connector, reminders *reminder.Scheduler, overviewExtensions ...modemstatus.Extension) *Handler {
	catalog := newCatalog(store, registry, overviewExtensions...)
	catalog.internet = internetConnector
	catalog.reminders = reminders
	return &Handler{
		registry:  registry,
		catalog:   catalog,
		simSlot:   newSIMSlot(registry),
		msisdn:    newMSISDN(registry),
		settings:  newSettings(store),
		internet:  internetConnector,
		reminders: reminders,
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
	if err := modem.UnlockSIMPINAndEnable(c.Request().Context(), req.PIN); err != nil {
		return unlockSIMError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func unlockSIMError(c *echo.Context, err error) error {
	switch {
	case errors.Is(err, mmodem.ErrSIMPINRequired):
		return httpapi.BadRequest(c, errorCodeUnlockSIMInvalidRequest, err)
	case errors.Is(err, mmodem.ErrSIMUnlockNotRequired):
		return httpapi.BadRequest(c, errorCodeUnlockSIMNotRequired, err)
	case errors.Is(err, mmodem.ErrEnableAfterSIMUnlock):
		return httpapi.Internal(c, errorCodeEnableModemAfterUnlockFailed, err)
	default:
		return httpapi.Internal(c, errorCodeUnlockSIMFailed, err)
	}
}

func (h *Handler) SwitchSIMSlot(c *echo.Context) error {
	requestCtx := c.Request().Context()
	modem, err := h.registry.Find(requestCtx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeSwitchSIMSlotFailed)
	}
	slotValue, parseErr := strconv.ParseUint(c.Param("slot"), 10, 32)
	if parseErr != nil || slotValue == 0 {
		return httpapi.BadRequest(c, errorCodeSIMSlotRequired, errSIMSlotRequired)
	}
	slotIndex, err := h.simSlot.targetIndex(modem, uint32(slotValue))
	if err != nil {
		if errors.Is(err, errSIMSlotRequired) {
			return httpapi.BadRequest(c, errorCodeSIMSlotRequired, err)
		}
		if errors.Is(err, errSIMSlotsUnavailable) {
			return httpapi.BadRequest(c, errorCodeSIMSlotsUnavailable, err)
		}
		if errors.Is(err, errSIMSlotNotFound) {
			return httpapi.BadRequest(c, errorCodeSIMSlotNotFound, err)
		}
		if errors.Is(err, errSIMSlotAlreadyActive) {
			return httpapi.BadRequest(c, errorCodeSIMSlotAlreadyActive, err)
		}
		return httpapi.Internal(c, errorCodeSwitchSIMSlotFailed, err)
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), switchSIMSlotTimeout)
	defer cancel()

	if err := h.internet.Restore(ctx, modem); err != nil {
		return httpapi.Internal(c, errorCodeSwitchSIMSlotFailed, err)
	}
	if err := h.simSlot.Switch(ctx, modem, slotIndex); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return httpapi.RequestTimeout(c, errorCodeSIMSlotSwitchTimeout, errSwitchSIMSlotTimeout)
		}
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return httpapi.Internal(c, errorCodeSwitchSIMSlotFailed, err)
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
		return httpapi.Internal(c, errorCodeUpdateSettingsFailed, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) Settings(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeGetSettingsFailed)
	}
	response := h.settings.Get(modem)
	return c.JSON(http.StatusOK, response)
}
