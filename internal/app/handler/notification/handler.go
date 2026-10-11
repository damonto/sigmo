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
		if errors.Is(err, lpa.ErrNoSupportedAID) {
			return httpapi.NotFound(c, "euicc_not_supported", err)
		}
		return httpapi.Internal(c, "list_notifications_failed", err)
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
		if seErr := seRequestError(c, err); seErr != nil {
			return seErr
		}
		if errors.Is(err, lpa.ErrNoSupportedAID) {
			return httpapi.NotFound(c, "euicc_not_supported", err)
		}
		return httpapi.Internal(c, "resend_notification_failed", err)
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
		if seErr := seRequestError(c, err); seErr != nil {
			return seErr
		}
		if errors.Is(err, lpa.ErrNoSupportedAID) {
			return httpapi.NotFound(c, "euicc_not_supported", err)
		}
		return httpapi.Internal(c, "delete_notification_failed", err)
	}
	return c.NoContent(http.StatusNoContent)
}

func seRequestError(c *echo.Context, err error) error {
	if errors.Is(err, lpa.ErrSERequired) {
		return httpapi.BadRequest(c, "se_required", err)
	}
	if errors.Is(err, lpa.ErrSENotFound) {
		return httpapi.NotFound(c, "se_not_found", err)
	}
	return nil
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
