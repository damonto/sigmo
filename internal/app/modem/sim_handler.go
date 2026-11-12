package modem

import (
	"context"
	"errors"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/httpapi"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

// SIMHandler manages SIM unlocking, PIN protection, slot selection, and numbers.
type SIMHandler struct {
	registry *mmodem.Registry
	service  *Service
}

// NewSIMHandler creates SIM endpoints using the shared modem service.
func NewSIMHandler(service *Service) *SIMHandler {
	return &SIMHandler{registry: service.registry, service: service}
}

func (h *SIMHandler) UnlockSIM(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "unlock_sim_failed")
	}
	var req UnlockSIMRequest
	if err := httpapi.BindJSON(c, &req, "unlock_sim_invalid_request"); err != nil {
		return err
	}
	if err := modem.UnlockSIM(c.Request().Context(), mmodem.SIMUnlock{PIN: req.PIN, PUK: req.PUK, NewPIN: req.NewPIN, SIMToken: req.SIMToken}); err != nil {
		return unlockSIMError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

// SetSIMSlot replaces the selected physical SIM slot.
func (h *SIMHandler) SetSIMSlot(c *echo.Context) error {
	requestCtx := c.Request().Context()
	modem, err := h.registry.Find(requestCtx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "switch_sim_slot_failed")
	}
	var req struct {
		Slot uint32 `json:"slot"`
	}
	if err := httpapi.BindJSON(c, &req, "sim_slot_required"); err != nil {
		return err
	}
	if req.Slot == 0 {
		return httpapi.BadRequest(c, "sim_slot_required", errSIMSlotRequired)
	}

	if err := h.service.switchSIM(c.Request().Context(), modem, req.Slot); err != nil {
		switch {
		case errors.Is(err, errSIMSlotRequired):
			return httpapi.BadRequest(c, "sim_slot_required", err)
		case errors.Is(err, errSIMSlotsUnavailable):
			return httpapi.BadRequest(c, "sim_slots_unavailable", err)
		case errors.Is(err, errSIMSlotNotFound):
			return httpapi.BadRequest(c, "sim_slot_not_found", err)
		case errors.Is(err, errSIMSlotAlreadyActive):
			return httpapi.BadRequest(c, "sim_slot_already_active", err)
		case errors.Is(err, context.DeadlineExceeded):
			return httpapi.RequestTimeout(c, "sim_slot_switch_timeout", errSwitchSIMSlotTimeout)
		case errors.Is(err, context.Canceled):
			return nil
		default:
			return httpapi.Internal(c, "switch_sim_slot_failed", err)
		}
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *SIMHandler) UpdateMSISDN(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "update_msisdn_failed")
	}
	var req UpdateMSISDNRequest
	if err := httpapi.BindAndValidate(c, &req, "update_msisdn_invalid_request"); err != nil {
		return err
	}

	if err := h.service.updateNumber(c.Request().Context(), modem, req.Number); err != nil {
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

func unlockSIMError(c *echo.Context, err error) error {
	if status, code, detail := SIMPINProblem(err); code != "" {
		return httpapi.Error(c, status, code, detail)
	}
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
