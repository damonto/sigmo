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

const (
	errorCodeUpdateSIMReminderInvalidRequest = "update_sim_reminder_invalid_request"
	errorCodeUpdateSIMReminderInvalid        = "update_sim_reminder_invalid"
	errorCodeUpdateSIMReminderFailed         = "update_sim_reminder_failed"
	errorCodeDeleteSIMReminderFailed         = "delete_sim_reminder_failed"
	errorCodeSIMProfileNotFound              = "sim_profile_not_found"
)

var errSIMProfileNotFound = errors.New("SIM profile not found")

func (h *Handler) UpdateReminder(c *echo.Context) error {
	ctx := c.Request().Context()
	modem, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeUpdateSIMReminderFailed)
	}
	iccid := strings.TrimSpace(c.Param("iccid"))
	profileID, err := modem.ProfileID(ctx)
	if err != nil {
		return httpapi.Internal(c, errorCodeUpdateSIMReminderFailed, fmt.Errorf("read current SIM profile: %w", err))
	}
	if iccid == "" || iccid != profileID {
		return httpapi.NotFound(c, errorCodeSIMProfileNotFound, errSIMProfileNotFound)
	}

	var req reminder.UpdateRequest
	if err := httpapi.BindAndValidate(c, &req, errorCodeUpdateSIMReminderInvalidRequest); err != nil {
		return err
	}
	profileName := profileID
	if modem.Sim != nil && strings.TrimSpace(modem.Sim.OperatorName) != "" {
		profileName = modem.Sim.OperatorName
	}
	record, err := req.Record(reminder.ProfileTypePSIM, profileID, modem.EquipmentIdentifier, "", profileName)
	if err != nil {
		return httpapi.UnprocessableEntity(c, errorCodeUpdateSIMReminderInvalid, err)
	}
	if err := h.reminders.Save(ctx, record); err != nil {
		return httpapi.Internal(c, errorCodeUpdateSIMReminderFailed, err)
	}
	return c.JSON(http.StatusOK, reminder.DetailsFrom(record))
}

func (h *Handler) DeleteReminder(c *echo.Context) error {
	ctx := c.Request().Context()
	if _, err := h.registry.Find(ctx, c.Param("id")); err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeDeleteSIMReminderFailed)
	}
	iccid := strings.TrimSpace(c.Param("iccid"))
	if iccid == "" {
		return httpapi.NotFound(c, errorCodeSIMProfileNotFound, errSIMProfileNotFound)
	}
	if err := h.reminders.Delete(ctx, reminder.ProfileTypePSIM, iccid); err != nil {
		return httpapi.Internal(c, errorCodeDeleteSIMReminderFailed, err)
	}
	return c.NoContent(http.StatusNoContent)
}
