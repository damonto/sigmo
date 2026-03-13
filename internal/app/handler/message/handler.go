package message

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/labstack/echo/v4"

	"github.com/damonto/sigmo/internal/app/httpapi"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

type Handler struct {
	manager *mmodem.Manager
	service *Service
}

const (
	errorCodeModemNotFound             = "modem_not_found"
	errorCodeListMessagesFailed        = "list_messages_failed"
	errorCodeListMessageThreadFailed   = "list_message_thread_failed"
	errorCodeParticipantRequired       = "participant_required"
	errorCodeInvalidParticipant        = "invalid_participant"
	errorCodeSendMessageInvalidRequest = "send_message_invalid_request"
	errorCodeRecipientRequired         = "recipient_required"
	errorCodeTextRequired              = "text_required"
	errorCodeSendMessageFailed         = "send_message_failed"
	errorCodeDeleteMessageThreadFailed = "delete_message_thread_failed"
)

var errModemNotFound = errors.New("modem not found")

func New(manager *mmodem.Manager) *Handler {
	return &Handler{
		manager: manager,
		service: NewService(),
	}
}

func (h *Handler) List(c echo.Context) error {
	modem, err := h.findModem(c.Param("id"))
	if err != nil {
		return h.modemLookupError(c, err, errorCodeListMessagesFailed)
	}
	response, err := h.service.ListConversations(modem)
	if err != nil {
		return httpapi.Internal(c, errorCodeListMessagesFailed)
	}
	return c.JSON(http.StatusOK, response)
}

func (h *Handler) ListByParticipant(c echo.Context) error {
	modem, err := h.findModem(c.Param("id"))
	if err != nil {
		return h.modemLookupError(c, err, errorCodeListMessageThreadFailed)
	}
	participant, err := participantFromParam(c)
	if err != nil {
		if errors.Is(err, errParticipantRequired) {
			return httpapi.BadRequest(c, errorCodeParticipantRequired, err)
		}
		return httpapi.BadRequest(c, errorCodeInvalidParticipant, err)
	}
	response, err := h.service.ListByParticipant(modem, participant)
	if err != nil {
		if errors.Is(err, errParticipantRequired) {
			return httpapi.BadRequest(c, errorCodeParticipantRequired, err)
		}
		return httpapi.Internal(c, errorCodeListMessageThreadFailed)
	}
	return c.JSON(http.StatusOK, response)
}

func (h *Handler) Send(c echo.Context) error {
	modem, err := h.findModem(c.Param("id"))
	if err != nil {
		return h.modemLookupError(c, err, errorCodeSendMessageFailed)
	}
	var req SendMessageRequest
	if err := httpapi.BindAndValidate(c, &req, errorCodeSendMessageInvalidRequest); err != nil {
		return err
	}
	if err := h.service.Send(modem, req.To, req.Text); err != nil {
		if errors.Is(err, errRecipientRequired) {
			return httpapi.BadRequest(c, errorCodeRecipientRequired, err)
		}
		if errors.Is(err, errTextRequired) {
			return httpapi.BadRequest(c, errorCodeTextRequired, err)
		}
		return httpapi.Internal(c, errorCodeSendMessageFailed)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) DeleteByParticipant(c echo.Context) error {
	modem, err := h.findModem(c.Param("id"))
	if err != nil {
		return h.modemLookupError(c, err, errorCodeDeleteMessageThreadFailed)
	}
	participant, err := participantFromParam(c)
	if err != nil {
		if errors.Is(err, errParticipantRequired) {
			return httpapi.BadRequest(c, errorCodeParticipantRequired, err)
		}
		return httpapi.BadRequest(c, errorCodeInvalidParticipant, err)
	}
	if err := h.service.DeleteByParticipant(modem, participant); err != nil {
		if errors.Is(err, errParticipantRequired) {
			return httpapi.BadRequest(c, errorCodeParticipantRequired, err)
		}
		return httpapi.Internal(c, errorCodeDeleteMessageThreadFailed)
	}
	return c.NoContent(http.StatusNoContent)
}

func participantFromParam(c echo.Context) (string, error) {
	raw := c.Param("participant")
	if raw == "" {
		return "", errParticipantRequired
	}
	participant, err := url.PathUnescape(raw)
	if err != nil {
		return "", fmt.Errorf("invalid participant %q: %w", raw, err)
	}
	return participant, nil
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
