package esim

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/httpapi"
	"github.com/damonto/sigmo/internal/pkg/reminder"
)

var errESIMReminderProfileNotFound = errors.New("eSIM profile not found")

func (h *Handler) UpdateReminder(c *echo.Context) error {
	ctx := c.Request().Context()
	modem, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "update_esim_reminder_failed")
	}
	iccid, err := iccidFromParam(c)
	if err != nil {
		return httpapi.BadRequest(c, "update_esim_reminder_invalid_request", err)
	}
	profiles, err := h.profile.List(ctx, modem)
	if err != nil {
		return httpapi.Internal(c, "update_esim_reminder_failed", fmt.Errorf("list eSIM profiles: %w", err))
	}
	profileName, ok := findProfileName(profiles, c.Param("seId"), iccid.String())
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

func (h *Handler) DeleteReminder(c *echo.Context) error {
	ctx := c.Request().Context()
	if _, err := h.registry.Find(ctx, c.Param("id")); err != nil {
		return httpapi.ModemLookupError(c, err, "delete_esim_reminder_failed")
	}
	iccid, err := iccidFromParam(c)
	if err != nil {
		return httpapi.BadRequest(c, "delete_esim_reminder_failed", err)
	}
	if err := h.reminders.Delete(ctx, reminder.ProfileTypeESIM, iccid.String()); err != nil {
		return httpapi.Internal(c, "delete_esim_reminder_failed", err)
	}
	return c.NoContent(http.StatusNoContent)
}

func findProfileName(profiles *ProfilesResponse, seID, iccid string) (string, bool) {
	if profiles == nil {
		return "", false
	}
	for _, group := range profiles.SEs {
		if strings.TrimSpace(group.ID) != strings.TrimSpace(seID) {
			continue
		}
		for _, profile := range group.Profiles {
			if profile.ICCID == iccid {
				return profile.Name, true
			}
		}
	}
	return "", false
}
