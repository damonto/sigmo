package message

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/httpapi"
	pmessage "github.com/damonto/sigmo/internal/pkg/message"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

type Handler struct {
	registry *mmodem.Registry
	messages *pmessage.Messenger
	store    *storage.Store
}

func New(registry *mmodem.Registry, store *storage.Store, route pmessage.Route) *Handler {
	return &Handler{
		registry: registry,
		messages: pmessage.New(store, route),
		store:    store,
	}
}

func (h *Handler) Send(c *echo.Context) error {
	ctx := c.Request().Context()
	modem, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "send_message_failed")
	}
	var req SendMessageRequest
	if err := httpapi.BindJSON(c, &req, "send_message_invalid_request"); err != nil {
		return err
	}
	to, err := h.messages.SendWithProfile(ctx, modem, pmessage.SendRequest{
		To:                req.To,
		Text:              req.Text,
		ExpectedProfileID: req.ExpectedProfileID,
	})
	if err != nil {
		return writeSendMessageError(c, err)
	}
	response := SendMessageResponse{To: to}
	if req.ExpectedProfileID != "" {
		response.ConversationID = storage.ConversationID(req.ExpectedProfileID, to)
	}
	return c.JSON(http.StatusOK, response)
}

func writeSendMessageError(c *echo.Context, err error) error {
	if errors.Is(err, pmessage.ErrSendUnconfirmed) {
		return httpapi.Error(c, http.StatusGatewayTimeout, "message_send_unconfirmed", pmessage.ErrSendUnconfirmed.Error())
	}
	if errors.Is(err, pmessage.ErrProfileChanged) {
		return httpapi.Error(c, http.StatusConflict, "message_profile_changed", err.Error())
	}
	if errors.Is(err, pmessage.ErrRecipientRequired) {
		return httpapi.BadRequest(c, "recipient_required", err)
	}
	if errors.Is(err, pmessage.ErrRecipientInvalid) {
		return httpapi.BadRequest(c, "invalid_recipient", err)
	}
	if errors.Is(err, pmessage.ErrTextRequired) {
		return httpapi.BadRequest(c, "text_required", err)
	}
	if errors.Is(err, pmessage.ErrRouteNotConnected) {
		return httpapi.Error(c, http.StatusServiceUnavailable, "message_route_not_connected", err.Error())
	}
	return httpapi.Internal(c, "send_message_failed", err)
}
