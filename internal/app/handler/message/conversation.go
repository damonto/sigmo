package message

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/godbus/dbus/v5"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/storage"
	"github.com/damonto/sigmo/internal/pkg/wificalling"
)

type message struct {
	store       *storage.Store
	wifiCalling wificalling.Coordinator
}

var (
	errParticipantRequired = errors.New("participant is required")
)

func newMessage(store *storage.Store, wifiCalling wificalling.Coordinator) *message {
	return &message{store: store, wifiCalling: wifiCalling}
}

func (m *message) ListConversations(ctx context.Context, modem *mmodem.Modem) ([]MessageResponse, error) {
	profileID, err := modem.ProfileID(ctx)
	if err != nil {
		return nil, err
	}
	if err := m.SyncModemMessages(ctx, modem, profileID); err != nil {
		return nil, err
	}
	messages, err := m.store.ListConversations(ctx, profileID)
	if err != nil {
		return nil, err
	}
	response := buildMessageResponses(messages)
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
	profileID, err := modem.ProfileID(ctx)
	if err != nil {
		return nil, err
	}
	if err := m.SyncModemMessages(ctx, modem, profileID); err != nil {
		return nil, err
	}
	messages, err := m.store.ListByParticipant(ctx, profileID, participant)
	if err != nil {
		return nil, err
	}
	response := buildMessageResponses(messages)
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
	profileID, err := modem.ProfileID(ctx)
	if err != nil {
		return err
	}
	messages, err := m.store.DeleteByParticipant(ctx, profileID, participant)
	if err != nil {
		return err
	}
	messaging := modem.Messaging()
	for _, msg := range messages {
		if msg.Source != storage.MessageSourceModem {
			continue
		}
		if err := messaging.Delete(ctx, dbus.ObjectPath(msg.ExternalKey)); err != nil {
			return fmt.Errorf("delete message for %s: %w", participant, err)
		}
	}
	return nil
}

func (m *message) SyncModemMessages(ctx context.Context, modem *mmodem.Modem, profileID string) error {
	messages, err := modem.Messaging().List(ctx)
	if err != nil {
		return fmt.Errorf("list messages: %w", err)
	}
	for _, sms := range messages {
		if sms == nil {
			continue
		}
		if _, err := m.store.InsertMessage(ctx, messageFromModemSMS(modem, profileID, sms)); err != nil {
			return err
		}
	}
	return nil
}

func buildMessageResponses(messages []storage.Message) []MessageResponse {
	response := make([]MessageResponse, 0, len(messages))
	for _, msg := range messages {
		response = append(response, buildMessageResponse(msg))
	}
	return response
}

func buildMessageResponse(msg storage.Message) MessageResponse {
	return MessageResponse{
		ID:          msg.ID,
		Sender:      msg.Sender,
		Recipient:   msg.Recipient,
		Text:        msg.Text,
		Timestamp:   msg.Timestamp,
		Status:      msg.Status,
		Incoming:    msg.Incoming,
		WiFiCalling: msg.WiFiCalling,
	}
}

func messageFromModemSMS(modem *mmodem.Modem, profileID string, sms *mmodem.SMS) storage.Message {
	incoming := sms.State == mmodem.SMSStateReceived || sms.State == mmodem.SMSStateReceiving
	remote := strings.TrimSpace(sms.Number)
	sender, recipient := modem.Number, remote
	if incoming {
		sender, recipient = remote, modem.Number
	}
	return storage.Message{
		ModemID:     modem.EquipmentIdentifier,
		ProfileID:   profileID,
		Source:      storage.MessageSourceModem,
		ExternalKey: string(sms.Path()),
		Sender:      sender,
		Recipient:   recipient,
		Text:        sms.Text,
		Timestamp:   sms.Timestamp,
		Status:      strings.ToLower(sms.State.String()),
		Incoming:    incoming,
	}
}
