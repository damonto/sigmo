package modem

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/httpapi"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/reminder"
)

// ReminderHandler manages reminders for physical SIM profiles.
type ReminderHandler struct {
	registry  *mmodem.Registry
	reminders *reminder.Scheduler
}

// NewReminderHandler creates physical SIM reminder endpoints.
func NewReminderHandler(registry *mmodem.Registry, reminders *reminder.Scheduler) *ReminderHandler {
	return &ReminderHandler{registry: registry, reminders: reminders}
}

var errSIMProfileNotFound = errors.New("SIM profile not found")

func (h *ReminderHandler) UpdateReminder(c *echo.Context) error {
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

func (h *ReminderHandler) DeleteReminder(c *echo.Context) error {
	ctx := c.Request().Context()
	modem, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "delete_sim_reminder_failed")
	}
	iccid := strings.TrimSpace(c.Param("iccid"))
	if iccid == "" {
		return httpapi.NotFound(c, "sim_profile_not_found", errSIMProfileNotFound)
	}
	if err := h.reminders.DeleteForProfile(ctx, reminder.ProfileTypePSIM, iccid, modem.EquipmentIdentifier, ""); err != nil {
		if errors.Is(err, reminder.ErrProfileMismatch) {
			return httpapi.NotFound(c, "sim_profile_not_found", errSIMProfileNotFound)
		}
		return httpapi.Internal(c, "delete_sim_reminder_failed", err)
	}
	return c.NoContent(http.StatusNoContent)
}
