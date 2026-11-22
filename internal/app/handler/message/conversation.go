package message

import "github.com/damonto/sigmo/internal/pkg/storage"

// ResponsesFromMessages preserves order and represents an empty result as a JSON array.
func ResponsesFromMessages(messages []storage.Message) []MessageResponse {
	response := make([]MessageResponse, 0, len(messages))
	for _, msg := range messages {
		response = append(response, ResponseFromMessage(msg))
	}
	return response
}

// ResponseFromMessage includes stable history identity without exposing modem storage references.
func ResponseFromMessage(msg storage.Message) MessageResponse {
	return MessageResponse{
		ID:             msg.ID,
		ConversationID: msg.ConversationID,
		ProfileID:      msg.ProfileID,
		Sender:         msg.Sender,
		Recipient:      msg.Recipient,
		Text:           msg.Text,
		Timestamp:      msg.Timestamp,
		Status:         msg.Status,
		Incoming:       msg.Incoming,
		Routed:         msg.Routed,
	}
}
