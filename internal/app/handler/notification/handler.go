package notification

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	sgp22 "github.com/damonto/euicc-go/v2"
	"github.com/labstack/echo/v4"

	"github.com/damonto/sigmo/internal/app/httpapi"
	"github.com/damonto/sigmo/internal/pkg/config"
	"github.com/damonto/sigmo/internal/pkg/lpa"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

type Handler struct {
	manager *mmodem.Manager
	service *Service
}

const (
	errorCodeModemNotFound            = "modem_not_found"
	errorCodeEuiccNotSupported        = "euicc_not_supported"
	errorCodeListNotificationsFailed  = "list_notifications_failed"
	errorCodeSequenceNumberRequired   = "sequence_number_required"
	errorCodeInvalidSequenceNumber    = "invalid_sequence_number"
	errorCodeResendNotificationFailed = "resend_notification_failed"
	errorCodeDeleteNotificationFailed = "delete_notification_failed"
)

var (
	errModemNotFound    = errors.New("modem not found")
	errSequenceRequired = errors.New("sequence number is required")
	errInvalidSequence  = errors.New("invalid sequence number")
)

func New(cfg *config.Config, manager *mmodem.Manager) *Handler {
	return &Handler{
		manager: manager,
		service: NewService(cfg),
	}
}

func (h *Handler) List(c echo.Context) error {
	modem, err := h.findModem(c.Param("id"))
	if err != nil {
		return h.modemLookupError(c, err, errorCodeListNotificationsFailed)
	}
	response, err := h.service.List(modem)
	if err != nil {
		if errors.Is(err, lpa.ErrNoSupportedAID) {
			return httpapi.NotFound(c, errorCodeEuiccNotSupported, err)
		}
		return httpapi.Internal(c, errorCodeListNotificationsFailed)
	}
	return c.JSON(http.StatusOK, response)
}

func (h *Handler) Resend(c echo.Context) error {
	modem, err := h.findModem(c.Param("id"))
	if err != nil {
		return h.modemLookupError(c, err, errorCodeResendNotificationFailed)
	}
	sequence, err := sequenceFromParam(c)
	if err != nil {
		if errors.Is(err, errSequenceRequired) {
			return httpapi.BadRequest(c, errorCodeSequenceNumberRequired, err)
		}
		return httpapi.BadRequest(c, errorCodeInvalidSequenceNumber, err)
	}
	if err := h.service.Resend(modem, sequence); err != nil {
		if errors.Is(err, lpa.ErrNoSupportedAID) {
			return httpapi.NotFound(c, errorCodeEuiccNotSupported, err)
		}
		return httpapi.Internal(c, errorCodeResendNotificationFailed)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) Delete(c echo.Context) error {
	modem, err := h.findModem(c.Param("id"))
	if err != nil {
		return h.modemLookupError(c, err, errorCodeDeleteNotificationFailed)
	}
	sequence, err := sequenceFromParam(c)
	if err != nil {
		if errors.Is(err, errSequenceRequired) {
			return httpapi.BadRequest(c, errorCodeSequenceNumberRequired, err)
		}
		return httpapi.BadRequest(c, errorCodeInvalidSequenceNumber, err)
	}
	if err := h.service.Delete(modem, sequence); err != nil {
		if errors.Is(err, lpa.ErrNoSupportedAID) {
			return httpapi.NotFound(c, errorCodeEuiccNotSupported, err)
		}
		return httpapi.Internal(c, errorCodeDeleteNotificationFailed)
	}
	return c.NoContent(http.StatusNoContent)
}

func sequenceFromParam(c echo.Context) (sgp22.SequenceNumber, error) {
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

func (h *Handler) modemLookupError(c echo.Context, err error, internalErrorCode string) error {
	if errors.Is(err, errModemNotFound) {
		return httpapi.NotFound(c, errorCodeModemNotFound, err)
	}
	return httpapi.Internal(c, internalErrorCode)
}

func (h *Handler) findModem(id string) (*mmodem.Modem, error) {
	modems, err := h.manager.Modems()
	if err != nil {
		return nil, fmt.Errorf("listing modems: %w", err)
	}
	for _, modem := range modems {
		if modem.EquipmentIdentifier == id {
			return modem, nil
		}
	}
	return nil, fmt.Errorf("%w: %s", errModemNotFound, id)
}
