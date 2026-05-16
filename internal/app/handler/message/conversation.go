package message

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

type message struct{}

var (
	errParticipantRequired = errors.New("participant is required")
)

func newMessage() *message {
	return &message{}
}

func (m *message) ListConversations(ctx context.Context, modem *mmodem.Modem) ([]MessageResponse, error) {
	messages, err := modem.Messaging().List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}

	latest := make(map[string]*mmodem.SMS, len(messages))
	for _, sms := range messages {
		key := strings.TrimSpace(sms.Number)
		existing, ok := latest[key]
		if !ok || sms.Timestamp.After(existing.Timestamp) {
			latest[key] = sms
		}
	}

	response := make([]MessageResponse, 0, len(latest))
	for _, sms := range latest {
		response = append(response, buildMessageResponse(sms))
	}

	slices.SortFunc(response, func(a, b MessageResponse) int {
		if a.ID == b.ID {
			return 0
		}
		if a.ID > b.ID {
			return -1
		}
		return 1
	})
	return response, nil
}

func (m *message) ListByParticipant(ctx context.Context, modem *mmodem.Modem, participant string) ([]MessageResponse, error) {
	if strings.TrimSpace(participant) == "" {
		return nil, errParticipantRequired
	}
	messages, err := modem.Messaging().List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}

	response := make([]MessageResponse, 0, len(messages))
	for _, sms := range messages {
		if strings.TrimSpace(sms.Number) != participant {
			continue
		}
		response = append(response, buildMessageResponse(sms))
	}
	slices.SortFunc(response, func(a, b MessageResponse) int {
		if a.ID == b.ID {
			return 0
		}
		if a.ID < b.ID {
			return -1
		}
		return 1
	})
	return response, nil
}

func (m *message) DeleteByParticipant(ctx context.Context, modem *mmodem.Modem, participant string) error {
	if strings.TrimSpace(participant) == "" {
		return errParticipantRequired
	}
	messages, err := modem.Messaging().List(ctx)
	if err != nil {
		return fmt.Errorf("list messages: %w", err)
	}
	messaging := modem.Messaging()
	for _, sms := range messages {
		if strings.TrimSpace(sms.Number) != participant {
			continue
		}
		if err := messaging.Delete(ctx, sms.Path()); err != nil {
			return fmt.Errorf("delete message for %s: %w", participant, err)
		}
	}
	return nil
}

func buildMessageResponse(sms *mmodem.SMS) MessageResponse {
	incoming := sms.State == mmodem.SMSStateReceived || sms.State == mmodem.SMSStateReceiving
	remote := strings.TrimSpace(sms.Number)
	return MessageResponse{
		ID:        messageID(sms),
		Sender:    remote,
		Recipient: remote,
		Text:      sms.Text,
		Timestamp: sms.Timestamp,
		Status:    strings.ToLower(sms.State.String()),
		Incoming:  incoming,
	}
}

func messageID(sms *mmodem.SMS) int64 {
	path := string(sms.Path())
	if path == "" {
		return 0
	}
	idx := strings.LastIndex(path, "/")
	if idx == -1 || idx+1 >= len(path) {
		return 0
	}
	id, err := strconv.ParseInt(path[idx+1:], 10, 64)
	if err != nil {
		return 0
	}
	return id
}
