package esim

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/labstack/echo/v5"

	appesim "github.com/damonto/sigmo/internal/app/esim"
	"github.com/damonto/sigmo/internal/app/httpapi"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/reminder"
)

// ReminderHandler manages reminders attached to installed profiles.
type ReminderHandler struct {
	registry  *mmodem.Registry
	service   *appesim.Service
	reminders *reminder.Scheduler
}

// NewReminderHandler creates reminder endpoints independently of provisioning.
func NewReminderHandler(registry *mmodem.Registry, service *appesim.Service, reminders *reminder.Scheduler) *ReminderHandler {
	return &ReminderHandler{registry: registry, service: service, reminders: reminders}
}

var errESIMReminderProfileNotFound = errors.New("eSIM profile not found")

func (h *ReminderHandler) UpdateReminder(c *echo.Context) error {
	ctx := c.Request().Context()
	modem, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "update_esim_reminder_failed")
	}
	iccid, err := iccidFromParam(c)
	if err != nil {
		return httpapi.BadRequest(c, "update_esim_reminder_invalid_request", err)
	}
	profiles, err := h.service.ProfileInventory(ctx, modem)
	if err != nil {
		return httpapi.Internal(c, "update_esim_reminder_failed", fmt.Errorf("list eSIM profiles: %w", err))
	}
	profileName, ok := appesim.ProfileName(profiles, c.Param("seId"), iccid.String())
	if !ok {
		return httpapi.NotFound(c, "esim_reminder_profile_not_found", errESIMReminderProfileNotFound)
	}

	var req reminder.UpdateRequest
	if err := httpapi.BindAndValidate(c, &req, "update_esim_reminder_invalid_request"); err != nil {
		return err
	}
	record, err := req.Record(reminder.ProfileTypeESIM, iccid.String(), modem.EquipmentIdentifier, c.Param("seId"), profileName)
	if err != nil {
		return httpapi.UnprocessableEntity(c, "update_esim_reminder_invalid", err)
	}
	if err := h.reminders.Save(ctx, record); err != nil {
		return httpapi.Internal(c, "update_esim_reminder_failed", err)
	}
	return c.JSON(http.StatusOK, reminder.DetailsFrom(record))
}

func (h *ReminderHandler) DeleteReminder(c *echo.Context) error {
	ctx := c.Request().Context()
	modem, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "delete_esim_reminder_failed")
	}
	iccid, err := iccidFromParam(c)
	if err != nil {
		return httpapi.BadRequest(c, "delete_esim_reminder_failed", err)
	}
	if err := h.reminders.DeleteForProfile(ctx, reminder.ProfileTypeESIM, iccid.String(), modem.EquipmentIdentifier, c.Param("seId")); err != nil {
		if errors.Is(err, reminder.ErrProfileMismatch) {
			return httpapi.NotFound(c, "esim_reminder_profile_not_found", errESIMReminderProfileNotFound)
		}
		return httpapi.Internal(c, "delete_esim_reminder_failed", err)
	}
	return c.NoContent(http.StatusNoContent)
}
