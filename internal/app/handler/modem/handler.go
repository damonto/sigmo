package modem

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/httpapi"
	"github.com/damonto/sigmo/internal/pkg/config"
	"github.com/damonto/sigmo/internal/pkg/internet"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

type Handler struct {
	manager  *mmodem.Manager
	catalog  *catalog
	simSlot  *simSlot
	msisdn   *msisdn
	settings *settings
	internet *internet.Connector
}

const (
	switchSimSlotTimeout = time.Minute
	updateMSISDNTimeout  = time.Minute
)

const (
	errorCodeListModemsFailed             = "list_modems_failed"
	errorCodeGetModemFailed               = "get_modem_failed"
	errorCodeSwitchSimSlotFailed          = "switch_sim_slot_failed"
	errorCodeSimIdentifierRequired        = "sim_identifier_required"
	errorCodeSimSlotsUnavailable          = "sim_slots_unavailable"
	errorCodeSimSlotNotFound              = "sim_slot_not_found"
	errorCodeSimSlotAlreadyActive         = "sim_slot_already_active"
	errorCodeSimSlotSwitchTimeout         = "sim_slot_switch_timeout"
	errorCodeUpdateMSISDNInvalidRequest   = "update_msisdn_invalid_request"
	errorCodeUpdateMSISDNFailed           = "update_msisdn_failed"
	errorCodeInvalidPhoneNumber           = "invalid_phone_number"
	errorCodeUpdateSettingsInvalidRequest = "update_settings_invalid_request"
	errorCodeUpdateSettingsFailed         = "update_settings_failed"
	errorCodeCompatibleRequired           = "compatible_required"
	errorCodeGetSettingsFailed            = "get_settings_failed"
)

var (
	errSwitchSimSlotTimeout = errors.New("switching SIM slot timed out, please refresh to confirm the active slot")
	errUpdateMSISDNTimeout  = errors.New("updating MSISDN timed out, please refresh to confirm the active slot")
)

func New(cfg *config.Config, manager *mmodem.Manager, internetConnector *internet.Connector) *Handler {
	return &Handler{
		manager:  manager,
		catalog:  newCatalog(cfg, manager),
		simSlot:  newSIMSlot(manager),
		msisdn:   newMSISDN(cfg, manager),
		settings: newSettings(cfg),
		internet: internetConnector,
	}
}

func (h *Handler) List(c *echo.Context) error {
	response, err := h.catalog.List()
	if err != nil {
		return httpapi.Internal(c, errorCodeListModemsFailed)
	}
	return c.JSON(http.StatusOK, response)
}

func (h *Handler) Get(c *echo.Context) error {
	modem, err := h.manager.Find(c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeGetModemFailed)
	}
	response, err := h.catalog.Get(modem)
	if err != nil {
		return httpapi.Internal(c, errorCodeGetModemFailed)
	}
	return c.JSON(http.StatusOK, response)
}

func (h *Handler) SwitchSimSlot(c *echo.Context) error {
	modem, err := h.manager.Find(c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeSwitchSimSlotFailed)
	}
	slotIndex, err := h.simSlot.targetIndex(modem, c.Param("identifier"))
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
		return httpapi.Internal(c, errorCodeSwitchSimSlotFailed)
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), switchSimSlotTimeout)
	defer cancel()

	if err := h.internet.Restore(modem); err != nil {
		return httpapi.Internal(c, errorCodeSwitchSimSlotFailed)
	}
	if err := h.simSlot.Switch(ctx, modem, slotIndex); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return httpapi.RequestTimeout(c, errorCodeSimSlotSwitchTimeout, errSwitchSimSlotTimeout)
		}
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return httpapi.Internal(c, errorCodeSwitchSimSlotFailed)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) UpdateMSISDN(c *echo.Context) error {
	modem, err := h.manager.Find(c.Param("id"))
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
		return httpapi.Internal(c, errorCodeUpdateMSISDNFailed)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) UpdateSettings(c *echo.Context) error {
	modem, err := h.manager.Find(c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeUpdateSettingsFailed)
	}
	var req UpdateModemSettingsRequest
	if err := httpapi.BindAndValidate(c, &req, errorCodeUpdateSettingsInvalidRequest); err != nil {
		return err
	}
	if err := h.settings.Update(modem.EquipmentIdentifier, req); err != nil {
		if errors.Is(err, errCompatibleRequired) {
			return httpapi.BadRequest(c, errorCodeCompatibleRequired, err)
		}
		return httpapi.Internal(c, errorCodeUpdateSettingsFailed)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) GetSettings(c *echo.Context) error {
	modem, err := h.manager.Find(c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeGetSettingsFailed)
	}
	response := h.settings.Get(modem.EquipmentIdentifier)
	return c.JSON(http.StatusOK, response)
}
