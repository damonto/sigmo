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
	"github.com/damonto/sigmo/internal/pkg/config"
	"github.com/damonto/sigmo/internal/pkg/lpa"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

type Handler struct {
	registry      *mmodem.Registry
	notifications *notification
}

const (
	errorCodeEuiccNotSupported        = "euicc_not_supported"
	errorCodeListNotificationsFailed  = "list_notifications_failed"
	errorCodeSequenceNumberRequired   = "sequence_number_required"
	errorCodeInvalidSequenceNumber    = "invalid_sequence_number"
	errorCodeResendNotificationFailed = "resend_notification_failed"
	errorCodeDeleteNotificationFailed = "delete_notification_failed"
)

var (
	errSequenceRequired = errors.New("sequence number is required")
	errInvalidSequence  = errors.New("invalid sequence number")
)

func New(store *config.Store, registry *mmodem.Registry) *Handler {
	return &Handler{
		registry:      registry,
		notifications: newNotification(store),
	}
}

func (h *Handler) List(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeListNotificationsFailed)
	}
	response, err := h.notifications.List(modem)
	if err != nil {
		if errors.Is(err, lpa.ErrNoSupportedAID) {
			return httpapi.NotFound(c, errorCodeEuiccNotSupported, err)
		}
		return httpapi.Internal(c, errorCodeListNotificationsFailed, err)
	}
	return c.JSON(http.StatusOK, response)
}

func (h *Handler) Resend(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeResendNotificationFailed)
	}
	sequence, err := sequenceFromParam(c)
	if err != nil {
		if errors.Is(err, errSequenceRequired) {
			return httpapi.BadRequest(c, errorCodeSequenceNumberRequired, err)
		}
		return httpapi.BadRequest(c, errorCodeInvalidSequenceNumber, err)
	}
	if err := h.notifications.Resend(modem, sequence); err != nil {
		if errors.Is(err, lpa.ErrNoSupportedAID) {
			return httpapi.NotFound(c, errorCodeEuiccNotSupported, err)
		}
		return httpapi.Internal(c, errorCodeResendNotificationFailed, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) Delete(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeDeleteNotificationFailed)
	}
	sequence, err := sequenceFromParam(c)
	if err != nil {
		if errors.Is(err, errSequenceRequired) {
			return httpapi.BadRequest(c, errorCodeSequenceNumberRequired, err)
		}
		return httpapi.BadRequest(c, errorCodeInvalidSequenceNumber, err)
	}
	if err := h.notifications.Delete(modem, sequence); err != nil {
		if errors.Is(err, lpa.ErrNoSupportedAID) {
			return httpapi.NotFound(c, errorCodeEuiccNotSupported, err)
		}
		return httpapi.Internal(c, errorCodeDeleteNotificationFailed, err)
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
