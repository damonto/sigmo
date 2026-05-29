package message

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/httpapi"
	pmessage "github.com/damonto/sigmo/internal/pkg/message"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/storage"
	"github.com/damonto/sigmo/internal/pkg/wificalling"
)

type Handler struct {
	registry *mmodem.Registry
	messages *pmessage.Service
}

const (
	errorCodeListMessagesFailed        = "list_messages_failed"
	errorCodeListMessageThreadFailed   = "list_message_thread_failed"
	errorCodeParticipantRequired       = "participant_required"
	errorCodeInvalidParticipant        = "invalid_participant"
	errorCodeSendMessageInvalidRequest = "send_message_invalid_request"
	errorCodeRecipientRequired         = "recipient_required"
	errorCodeRecipientInvalid          = "invalid_recipient"
	errorCodeTextRequired              = "text_required"
	errorCodeSendMessageFailed         = "send_message_failed"
	errorCodeDeleteMessageThreadFailed = "delete_message_thread_failed"
)

func New(registry *mmodem.Registry, store *storage.Store, wifiCalling wificalling.Coordinator) *Handler {
	return &Handler{
		registry: registry,
		messages: pmessage.New(store, wifiCalling),
	}
}

func (h *Handler) List(c *echo.Context) error {
	ctx := c.Request().Context()
	modem, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeListMessagesFailed)
	}
	response, err := h.messages.ListConversations(ctx, modem, c.QueryParam("q"))
	if err != nil {
		return httpapi.Internal(c, errorCodeListMessagesFailed, err)
	}
	return c.JSON(http.StatusOK, buildConversationResponses(response))
}

func (h *Handler) ListByParticipant(c *echo.Context) error {
	ctx := c.Request().Context()
	modem, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeListMessageThreadFailed)
	}
	participant, err := participantFromParam(c)
	if err != nil {
		if errors.Is(err, pmessage.ErrParticipantRequired) {
			return httpapi.BadRequest(c, errorCodeParticipantRequired, err)
		}
		return httpapi.BadRequest(c, errorCodeInvalidParticipant, err)
	}
	response, err := h.messages.ListByParticipant(ctx, modem, participant)
	if err != nil {
		if errors.Is(err, pmessage.ErrParticipantRequired) {
			return httpapi.BadRequest(c, errorCodeParticipantRequired, err)
		}
		return httpapi.Internal(c, errorCodeListMessageThreadFailed, err)
	}
	return c.JSON(http.StatusOK, buildThreadResponses(response))
}

func (h *Handler) Send(c *echo.Context) error {
	ctx := c.Request().Context()
	modem, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeSendMessageFailed)
	}
	var req SendMessageRequest
	if err := httpapi.BindAndValidate(c, &req, errorCodeSendMessageInvalidRequest); err != nil {
		return err
	}
	to, err := h.messages.Send(ctx, modem, req.To, req.Text)
	if err != nil {
		return writeSendMessageError(c, err)
	}
	return c.JSON(http.StatusOK, SendMessageResponse{To: to})
}

func writeSendMessageError(c *echo.Context, err error) error {
	if errors.Is(err, pmessage.ErrRecipientRequired) {
		return httpapi.BadRequest(c, errorCodeRecipientRequired, err)
	}
	if errors.Is(err, pmessage.ErrRecipientInvalid) {
		return httpapi.BadRequest(c, errorCodeRecipientInvalid, err)
	}
	if errors.Is(err, pmessage.ErrTextRequired) {
		return httpapi.BadRequest(c, errorCodeTextRequired, err)
	}
	return httpapi.Internal(c, errorCodeSendMessageFailed, err)
}

func (h *Handler) DeleteByParticipant(c *echo.Context) error {
	ctx := c.Request().Context()
	modem, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeDeleteMessageThreadFailed)
	}
	participant, err := participantFromParam(c)
	if err != nil {
		if errors.Is(err, pmessage.ErrParticipantRequired) {
			return httpapi.BadRequest(c, errorCodeParticipantRequired, err)
		}
		return httpapi.BadRequest(c, errorCodeInvalidParticipant, err)
	}
	if err := h.messages.DeleteByParticipant(ctx, modem, participant); err != nil {
		if errors.Is(err, pmessage.ErrParticipantRequired) {
			return httpapi.BadRequest(c, errorCodeParticipantRequired, err)
		}
		return httpapi.Internal(c, errorCodeDeleteMessageThreadFailed, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func participantFromParam(c *echo.Context) (string, error) {
	raw := c.Param("participant")
	if raw == "" {
		return "", pmessage.ErrParticipantRequired
	}
	participant, err := url.PathUnescape(raw)
	if err != nil {
		return "", fmt.Errorf("invalid participant %q: %w", raw, err)
	}
	return participant, nil
}
