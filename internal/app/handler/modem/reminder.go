package modem

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/httpapi"
	"github.com/damonto/sigmo/internal/pkg/reminder"
)

var errSIMProfileNotFound = errors.New("SIM profile not found")

func (h *Handler) UpdateReminder(c *echo.Context) error {
	ctx := c.Request().Context()
	modem, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "update_sim_reminder_failed")
	}
	iccid := strings.TrimSpace(c.Param("iccid"))
	profileID, err := modem.ProfileID(ctx)
	if err != nil {
		return httpapi.Internal(c, "update_sim_reminder_failed", fmt.Errorf("read current SIM profile: %w", err))
	}
	if iccid == "" || iccid != profileID {
		return httpapi.NotFound(c, "sim_profile_not_found", errSIMProfileNotFound)
	}

	var req reminder.UpdateRequest
	if err := httpapi.BindAndValidate(c, &req, "update_sim_reminder_invalid_request"); err != nil {
		return err
	}
	profileName := profileID
	if sim := modem.Snapshot().SIM; sim != nil && strings.TrimSpace(sim.OperatorName) != "" {
		profileName = sim.OperatorName
	}
	record, err := req.Record(reminder.ProfileTypePSIM, profileID, modem.EquipmentIdentifier, "", profileName)
	if err != nil {
		return httpapi.UnprocessableEntity(c, "update_sim_reminder_invalid", err)
	}
	if err := h.reminders.Save(ctx, record); err != nil {
		return httpapi.Internal(c, "update_sim_reminder_failed", err)
	}
	return c.JSON(http.StatusOK, reminder.DetailsFrom(record))
}

func (h *Handler) DeleteReminder(c *echo.Context) error {
	ctx := c.Request().Context()
	if _, err := h.registry.Find(ctx, c.Param("id")); err != nil {
		return httpapi.ModemLookupError(c, err, "delete_sim_reminder_failed")
	}
	iccid := strings.TrimSpace(c.Param("iccid"))
	if iccid == "" {
		return httpapi.NotFound(c, "sim_profile_not_found", errSIMProfileNotFound)
	}
	if err := h.reminders.Delete(ctx, reminder.ProfileTypePSIM, iccid); err != nil {
		return httpapi.Internal(c, "delete_sim_reminder_failed", err)
	}
	return c.NoContent(http.StatusNoContent)
}
