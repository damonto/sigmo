package notification

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	sgp22 "github.com/damonto/euicc-go/v2"
	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/httpapi"
	"github.com/damonto/sigmo/internal/pkg/lpa"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

type Handler struct {
	registry      *mmodem.Registry
	notifications *notification
}

var (
	errSequenceRequired = errors.New("sequence number is required")
	errInvalidSequence  = errors.New("invalid sequence number")
)

func New(registry *mmodem.Registry, clients *lpa.Pool) *Handler {
	return &Handler{
		registry:      registry,
		notifications: newNotification(clients),
	}
}

func (h *Handler) List(c *echo.Context) error {
	ctx := c.Request().Context()
	modem, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "list_notifications_failed")
	}
	response, err := h.notifications.List(ctx, modem)
	if err != nil {
		return httpapi.LPAError(c, err, "list_notifications_failed")
	}
	return c.JSON(http.StatusOK, response)
}

func (h *Handler) Resend(c *echo.Context) error {
	ctx := c.Request().Context()
	modem, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "resend_notification_failed")
	}
	sequence, err := sequenceFromParam(c)
	if err != nil {
		if errors.Is(err, errSequenceRequired) {
			return httpapi.BadRequest(c, "sequence_number_required", err)
		}
		return httpapi.BadRequest(c, "invalid_sequence_number", err)
	}
	if err := h.notifications.Resend(ctx, modem, c.Param("seId"), sequence); err != nil {
		return httpapi.LPAError(c, err, "resend_notification_failed")
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) Delete(c *echo.Context) error {
	ctx := c.Request().Context()
	modem, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "delete_notification_failed")
	}
	sequence, err := sequenceFromParam(c)
	if err != nil {
		if errors.Is(err, errSequenceRequired) {
			return httpapi.BadRequest(c, "sequence_number_required", err)
		}
		return httpapi.BadRequest(c, "invalid_sequence_number", err)
	}
	if err := h.notifications.Delete(ctx, modem, c.Param("seId"), sequence); err != nil {
		return httpapi.LPAError(c, err, "delete_notification_failed")
	}
	return c.NoContent(http.StatusNoContent)
}

func sequenceFromParam(c *echo.Context) (sgp22.SequenceNumber, error) {
	raw := strings.TrimSpace(c.Param("sequence"))
	if raw == "" {
		return 0, errSequenceRequired
	}
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w %q: %w", errInvalidSequence, raw, err)
	}
	return sgp22.SequenceNumber(value), nil
}
