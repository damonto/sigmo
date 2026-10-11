package message

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/httpapi"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

// ConversationResponse describes one SIM's history and its currently available send route.
type ConversationResponse struct {
	storage.Conversation
	LatestMessage MessageResponse `json:"latestMessage"`
	CanSend       bool            `json:"canSend"`
}

// ConversationsResponse includes an authoritative backend-wide badge, even when filtered.
type ConversationsResponse struct {
	storage.MessageState
	Items []ConversationResponse `json:"items"`
}

// Conversations lists archived threads without depending on their SIM being active.
func (h *Handler) Conversations(c *echo.Context) error {
	ctx := c.Request().Context()
	active := make(map[string]string)
	if h.registry != nil {
		modems, err := h.registry.Modems(ctx)
		if err != nil {
			return httpapi.Internal(c, "list_conversations_failed", err)
		}
		for _, modem := range modems {
			snapshot := modem.Snapshot()
			for _, slot := range snapshot.Slots {
				if slot == nil {
					continue
				}
				if err := h.store.RecordMessageLine(ctx, storage.MessageLine{ProfileID: slot.Identifier, ModemID: modem.EquipmentIdentifier}); err != nil {
					return httpapi.Internal(c, "list_conversations_failed", err)
				}
			}
			if snapshot.SIM == nil || snapshot.SIM.Identifier == "" {
				continue
			}
			profileID := snapshot.SIM.Identifier
			active[profileID] = modem.EquipmentIdentifier
			if err := h.store.RecordMessageLine(ctx, storage.MessageLine{ProfileID: profileID, ModemID: modem.EquipmentIdentifier, Number: snapshot.Number}); err != nil {
				return httpapi.Internal(c, "list_conversations_failed", err)
			}
		}
	}
	snapshot, err := h.store.Conversations(ctx, storage.ConversationFilter{ModemID: c.QueryParam("modemId"), ProfileID: c.QueryParam("profileId"), Query: c.QueryParam("q"), Unassigned: c.QueryParam("unassigned") == "true"})
	if err != nil {
		return httpapi.Internal(c, "list_conversations_failed", err)
	}
	response := ConversationsResponse{MessageState: snapshot.MessageState, Items: []ConversationResponse{}}
	for _, conversation := range snapshot.Items {
		response.Items = append(response.Items, ConversationResponse{Conversation: conversation, LatestMessage: ResponseFromMessage(conversation.Latest), CanSend: conversation.ModemID != "" && active[conversation.ProfileID] == conversation.ModemID})
	}
	return c.JSON(http.StatusOK, response)
}

// ConversationMessages reads a stable thread ID rather than whichever SIM is currently active.
func (h *Handler) ConversationMessages(c *echo.Context) error {
	messages, err := h.store.ConversationMessages(c.Request().Context(), c.Param("conversationId"))
	if err != nil {
		return historyError(c, err)
	}
	return c.JSON(http.StatusOK, ResponsesFromMessages(messages))
}

// UpdateConversation acknowledges a displayed watermark or manually marks a thread unread.
func (h *Handler) UpdateConversation(c *echo.Context) error {
	var update storage.ReadUpdate
	if err := httpapi.BindJSON(c, &update, "invalid_conversation_read_update"); err != nil {
		return err
	}
	update.ConversationID = c.Param("conversationId")
	if err := h.store.UpdateConversationRead(c.Request().Context(), update); err != nil {
		return historyError(c, err)
	}
	state, err := h.store.MessagesState(c.Request().Context())
	if err != nil {
		return historyError(c, err)
	}
	return c.JSON(http.StatusOK, state)
}

// DeleteConversation deletes archived history without switching the active SIM.
func (h *Handler) DeleteConversation(c *echo.Context) error {
	if err := h.store.DeleteConversation(c.Request().Context(), c.Param("conversationId")); err != nil {
		return historyError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func historyError(c *echo.Context, err error) error {
	if errors.Is(err, storage.ErrNotFound) {
		return httpapi.Error(c, http.StatusNotFound, "conversation_not_found", "conversation not found")
	}
	if errors.Is(err, storage.ErrInvalidReadUpdate) {
		return httpapi.BadRequest(c, "invalid_conversation_read_update", err)
	}
	return httpapi.Internal(c, "conversation_operation_failed", err)
}
