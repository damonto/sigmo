package message

import (
	"slices"

	"github.com/damonto/sigmo/internal/pkg/storage"
)

func buildConversationResponses(messages []storage.Message) []MessageResponse {
	response := ResponsesFromMessages(messages)
	slices.SortFunc(response, func(a, b MessageResponse) int {
		if a.ID == b.ID {
			return 0
		}
		if a.ID > b.ID {
			return -1
		}
		return 1
	})
	return response
}

func buildThreadResponses(messages []storage.Message) []MessageResponse {
	response := ResponsesFromMessages(messages)
	slices.SortFunc(response, func(a, b MessageResponse) int {
		if a.ID == b.ID {
			return 0
		}
		if a.ID < b.ID {
			return -1
		}
		return 1
	})
	return response
}

func ResponsesFromMessages(messages []storage.Message) []MessageResponse {
	response := make([]MessageResponse, 0, len(messages))
	for _, msg := range messages {
		response = append(response, ResponseFromMessage(msg))
	}
	return response
}

func ResponseFromMessage(msg storage.Message) MessageResponse {
	return MessageResponse{
		ID:        msg.ID,
		Sender:    msg.Sender,
		Recipient: msg.Recipient,
		Text:      msg.Text,
		Timestamp: msg.Timestamp,
		Status:    msg.Status,
		Incoming:  msg.Incoming,
		Routed:    msg.Routed,
	}
}
