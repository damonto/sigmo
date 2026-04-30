package message

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/httpapi"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

type Handler struct {
	manager  *mmodem.Manager
	messages *message
}

const (
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

func New(manager *mmodem.Manager) *Handler {
	return &Handler{
		manager:  manager,
		messages: newMessage(),
	}
}

func (h *Handler) List(c *echo.Context) error {
	modem, err := h.manager.Find(c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeListMessagesFailed)
	}
	response, err := h.messages.ListConversations(modem)
	if err != nil {
		return httpapi.Internal(c, errorCodeListMessagesFailed)
	}
	return c.JSON(http.StatusOK, response)
}

func (h *Handler) ListByParticipant(c *echo.Context) error {
	modem, err := h.manager.Find(c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeListMessageThreadFailed)
	}
	participant, err := participantFromParam(c)
	if err != nil {
		if errors.Is(err, errParticipantRequired) {
			return httpapi.BadRequest(c, errorCodeParticipantRequired, err)
		}
		return httpapi.BadRequest(c, errorCodeInvalidParticipant, err)
	}
	response, err := h.messages.ListByParticipant(modem, participant)
	if err != nil {
		if errors.Is(err, errParticipantRequired) {
			return httpapi.BadRequest(c, errorCodeParticipantRequired, err)
		}
		return httpapi.Internal(c, errorCodeListMessageThreadFailed)
	}
	return c.JSON(http.StatusOK, response)
}

func (h *Handler) Send(c *echo.Context) error {
	modem, err := h.manager.Find(c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeSendMessageFailed)
	}
	var req SendMessageRequest
	if err := httpapi.BindAndValidate(c, &req, errorCodeSendMessageInvalidRequest); err != nil {
		return err
	}
	if err := h.messages.Send(modem, req.To, req.Text); err != nil {
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

func (h *Handler) DeleteByParticipant(c *echo.Context) error {
	modem, err := h.manager.Find(c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeDeleteMessageThreadFailed)
	}
	participant, err := participantFromParam(c)
	if err != nil {
		if errors.Is(err, errParticipantRequired) {
			return httpapi.BadRequest(c, errorCodeParticipantRequired, err)
		}
		return httpapi.BadRequest(c, errorCodeInvalidParticipant, err)
	}
	if err := h.messages.DeleteByParticipant(modem, participant); err != nil {
		if errors.Is(err, errParticipantRequired) {
			return httpapi.BadRequest(c, errorCodeParticipantRequired, err)
		}
		return httpapi.Internal(c, errorCodeDeleteMessageThreadFailed)
	}
	return c.NoContent(http.StatusNoContent)
}

func participantFromParam(c *echo.Context) (string, error) {
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
