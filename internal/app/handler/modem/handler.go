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
	"github.com/damonto/sigmo/internal/pkg/lpa"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/reminder"
	"github.com/damonto/sigmo/internal/pkg/settings"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

// Config supplies the modem endpoints and their optional overview data sources.
type Config struct {
	Storage            *storage.Store
	Store              *settings.Store
	Registry           *mmodem.Registry
	Internet           *internet.Connector
	Reminders          *reminder.Scheduler
	LPA                *lpa.Pool
	OverviewExtensions []modemstatus.Extension
}

// Handler serves modem resources using the shared registry and LPA pool.
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

var (
	errSwitchSIMSlotTimeout = errors.New("switching SIM slot timed out, please refresh to confirm the active slot")
	errUpdateMSISDNTimeout  = errors.New("updating MSISDN timed out, please refresh to confirm the active slot")
)

// New creates modem endpoints with the configured overview readers.
func New(cfg Config) *Handler {
	catalog := newCatalog(cfg.Store, cfg.Registry, cfg.OverviewExtensions...)
	catalog.storage = cfg.Storage
	catalog.internet = cfg.Internet
	catalog.reminders = cfg.Reminders
	if cfg.LPA != nil {
		catalog.profiles = cfg.LPA
	}
	return &Handler{
		registry:  cfg.Registry,
		catalog:   catalog,
		simSlot:   newSIMSlot(cfg.Registry),
		msisdn:    newMSISDN(cfg.Registry),
		settings:  newSettings(cfg.Store),
		internet:  cfg.Internet,
		reminders: cfg.Reminders,
	}
}

func (h *Handler) List(c *echo.Context) error {
	response, err := h.catalog.List(c.Request().Context())
	if err != nil {
		return httpapi.Internal(c, "list_modems_failed", err)
	}
	return c.JSON(http.StatusOK, response)
}

func (h *Handler) Get(c *echo.Context) error {
	ctx := c.Request().Context()
	modem, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "get_modem_failed")
	}
	response, err := h.catalog.Get(ctx, modem)
	if err != nil {
		return httpapi.Internal(c, "get_modem_failed", err)
	}
	return c.JSON(http.StatusOK, response)
}

func (h *Handler) UnlockSIM(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "unlock_sim_failed")
	}
	var req UnlockSIMRequest
	if err := httpapi.BindJSON(c, &req, "unlock_sim_invalid_request"); err != nil {
		return err
	}
	if err := modem.UnlockSIMPINAndEnable(c.Request().Context(), req.PIN); err != nil {
		return unlockSIMError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func unlockSIMError(c *echo.Context, err error) error {
	switch {
	case errors.Is(err, mmodem.ErrSIMPINRequired):
		return httpapi.BadRequest(c, "unlock_sim_invalid_request", err)
	case errors.Is(err, mmodem.ErrSIMUnlockNotRequired):
		return httpapi.BadRequest(c, "unlock_sim_not_required", err)
	case errors.Is(err, mmodem.ErrEnableAfterSIMUnlock):
		return httpapi.Internal(c, "enable_modem_after_unlock_failed", err)
	default:
		return httpapi.Internal(c, "unlock_sim_failed", err)
	}
}

func (h *Handler) SwitchSIMSlot(c *echo.Context) error {
	requestCtx := c.Request().Context()
	modem, err := h.registry.Find(requestCtx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "switch_sim_slot_failed")
	}
	slotValue, parseErr := strconv.ParseUint(c.Param("slot"), 10, 32)
	if parseErr != nil || slotValue == 0 {
		return httpapi.BadRequest(c, "sim_slot_required", errSIMSlotRequired)
	}
	slotIndex, err := h.simSlot.targetIndex(modem, uint32(slotValue))
	if err != nil {
		if errors.Is(err, errSIMSlotRequired) {
			return httpapi.BadRequest(c, "sim_slot_required", err)
		}
		if errors.Is(err, errSIMSlotsUnavailable) {
			return httpapi.BadRequest(c, "sim_slots_unavailable", err)
		}
		if errors.Is(err, errSIMSlotNotFound) {
			return httpapi.BadRequest(c, "sim_slot_not_found", err)
		}
		if errors.Is(err, errSIMSlotAlreadyActive) {
			return httpapi.BadRequest(c, "sim_slot_already_active", err)
		}
		return httpapi.Internal(c, "switch_sim_slot_failed", err)
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), switchSIMSlotTimeout)
	defer cancel()

	if err := h.internet.Restore(ctx, modem); err != nil {
		return httpapi.Internal(c, "switch_sim_slot_failed", err)
	}
	if err := h.simSlot.Switch(ctx, modem, slotIndex); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return httpapi.RequestTimeout(c, "sim_slot_switch_timeout", errSwitchSIMSlotTimeout)
		}
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return httpapi.Internal(c, "switch_sim_slot_failed", err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) UpdateMSISDN(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "update_msisdn_failed")
	}
	var req UpdateMSISDNRequest
	if err := httpapi.BindAndValidate(c, &req, "update_msisdn_invalid_request"); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), updateMSISDNTimeout)
	defer cancel()

	if err := h.msisdn.Update(ctx, modem, req.Number); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return httpapi.RequestTimeout(c, "msisdn_update_timeout", errUpdateMSISDNTimeout)
		}
		if errors.Is(err, errMSISDNInvalidNumber) {
			return httpapi.BadRequest(c, "invalid_phone_number", err)
		}
		return httpapi.Internal(c, "update_msisdn_failed", err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) UpdateSettings(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "update_settings_failed")
	}
	var req UpdateModemSettingsRequest
	if err := httpapi.BindAndValidate(c, &req, "update_settings_invalid_request"); err != nil {
		return err
	}
	if err := h.settings.Update(c.Request().Context(), modem, req); err != nil {
		return httpapi.Internal(c, "update_settings_failed", err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) Settings(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "get_settings_failed")
	}
	response := h.settings.Get(modem)
	return c.JSON(http.StatusOK, response)
}
