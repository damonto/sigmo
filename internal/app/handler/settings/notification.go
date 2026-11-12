package settings

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/forwarder"
	"github.com/damonto/sigmo/internal/app/httpapi"
	appsettings "github.com/damonto/sigmo/internal/pkg/settings"
)

// NotificationHandler manages delivery channels and business-event routing.
type NotificationHandler struct {
	store *appsettings.Store
	relay *forwarder.Relay
}

// NewNotificationHandler creates notification settings endpoints.
func NewNotificationHandler(store *appsettings.Store, relay *forwarder.Relay) *NotificationHandler {
	return &NotificationHandler{store: store, relay: relay}
}

func (h *NotificationHandler) UpdateNotificationChannel(c *echo.Context) error {
	name := strings.ToLower(strings.TrimSpace(c.Param("channel")))
	if _, ok := allowedChannelNames()[name]; !ok {
		return httpapi.UnprocessableEntity(
			c,
			"update_notification_invalid",
			fmt.Errorf("notification channel %q is not supported", name),
		)
	}

	var req ChannelValues
	if err := httpapi.BindJSON(c, &req, "update_notification_invalid_request"); err != nil {
		return err
	}
	req = filterChannelValue(name, normalizeChannelValue(req))
	if err := c.Validate(&req); err != nil {
		return httpapi.UnprocessableEntity(c, "update_notification_invalid", err)
	}

	channel := channelSettingsFromValues(name, req)
	var invalidErr error
	saved, err := h.store.Update(c.Request().Context(), func(current *appsettings.Settings) error {
		invalidErr = applyNotificationChannelSettings(current, name, channel)
		return invalidErr
	})
	if invalidErr != nil {
		return httpapi.UnprocessableEntity(c, "update_notification_invalid", invalidErr)
	}
	if err != nil {
		return httpapi.Internal(c, "update_notification_failed", fmt.Errorf("save notification channel: %w", err))
	}

	if err := h.relay.Reload(); err != nil {
		return httpapi.Internal(c, "reload_notification_relay_failed", fmt.Errorf("saved notification channel, reload notification relay: %w", err))
	}

	return c.JSON(http.StatusOK, responseFromSettings(saved))
}

func (h *NotificationHandler) UpdateNotifications(c *echo.Context) error {
	var req appsettings.Notifications
	if err := httpapi.BindJSON(c, &req, "invalid_notification_settings"); err != nil {
		return err
	}
	req.Channels = normalizeNames(trimNames(req.Channels))
	var invalid error
	saved, err := h.store.Update(c.Request().Context(), func(current *appsettings.Settings) error {
		current.Notifications = req
		invalid = validateSettings(*current)
		return invalid
	})
	if invalid != nil {
		return httpapi.UnprocessableEntity(c, "invalid_notification_settings", invalid)
	}
	if err != nil {
		return httpapi.Internal(c, "save_notifications_failed", err)
	}
	if err := h.relay.Reload(); err != nil {
		return httpapi.Internal(c, "reload_notifications_failed", err)
	}
	return c.JSON(http.StatusOK, responseFromSettings(saved))
}
