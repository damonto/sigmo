package message

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/godbus/dbus/v5"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/storage"
	"github.com/damonto/sigmo/internal/pkg/wificalling"
)

type Service struct {
	store       *storage.Store
	wifiCalling wificalling.Coordinator
}

type modemDevice interface {
	modem() *mmodem.Modem
	profileID(context.Context) (string, error)
	sendSMS(context.Context, string, string) (*mmodem.SMS, error)
	listSMS(context.Context) ([]*mmodem.SMS, error)
	deleteSMS(context.Context, dbus.ObjectPath) error
	modemID() string
	phoneNumber() string
}

type realModemDevice struct {
	modemRef *mmodem.Modem
}

func New(store *storage.Store, wifiCalling wificalling.Coordinator) *Service {
	return &Service{store: store, wifiCalling: wifiCalling}
}

func (s *Service) ListConversations(ctx context.Context, modem *mmodem.Modem) ([]storage.Message, error) {
	return s.listConversations(ctx, realModemDevice{modemRef: modem})
}

func (s *Service) listConversations(ctx context.Context, device modemDevice) ([]storage.Message, error) {
	profileID, err := device.profileID(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.syncModemMessages(ctx, device, profileID); err != nil {
		return nil, err
	}
	return s.store.ListConversations(ctx, profileID)
}

func (s *Service) ListByParticipant(ctx context.Context, modem *mmodem.Modem, participant string) ([]storage.Message, error) {
	return s.listByParticipant(ctx, realModemDevice{modemRef: modem}, participant)
}

func (s *Service) listByParticipant(ctx context.Context, device modemDevice, participant string) ([]storage.Message, error) {
	if strings.TrimSpace(participant) == "" {
		return nil, ErrParticipantRequired
	}
	profileID, err := device.profileID(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.syncModemMessages(ctx, device, profileID); err != nil {
		return nil, err
	}
	return s.store.ListByParticipant(ctx, profileID, participant)
}

func (s *Service) DeleteByParticipant(ctx context.Context, modem *mmodem.Modem, participant string) error {
	return s.deleteByParticipant(ctx, realModemDevice{modemRef: modem}, participant)
}

func (s *Service) deleteByParticipant(ctx context.Context, device modemDevice, participant string) error {
	if strings.TrimSpace(participant) == "" {
		return ErrParticipantRequired
	}
	profileID, err := device.profileID(ctx)
	if err != nil {
		return err
	}
	messages, err := s.store.DeleteByParticipant(ctx, profileID, participant)
	if err != nil {
		return err
	}
	for _, msg := range messages {
		if msg.Source != storage.MessageSourceModem {
			continue
		}
		if err := device.deleteSMS(ctx, dbus.ObjectPath(msg.ExternalKey)); err != nil {
			return fmt.Errorf("delete message for %s: %w", participant, err)
		}
	}
	return nil
}

func (s *Service) Send(ctx context.Context, modem *mmodem.Modem, to string, text string) (string, error) {
	return s.send(ctx, realModemDevice{modemRef: modem}, to, text)
}

func (s *Service) send(ctx context.Context, device modemDevice, to string, text string) (string, error) {
	if strings.TrimSpace(to) == "" {
		return "", ErrRecipientRequired
	}
	if strings.TrimSpace(text) == "" {
		return "", ErrTextRequired
	}
	to, err := normalizeRecipient(ctx, device.modem(), to)
	if err != nil {
		return "", err
	}
	profileID, err := device.profileID(ctx)
	if err != nil {
		return "", err
	}
	settings, err := s.wifiCalling.Status(ctx, device.modem())
	if err != nil && !errors.Is(err, wificalling.ErrUnavailable) {
		return "", fmt.Errorf("read wifi calling status: %w", err)
	}
	if settings.Preferred && settings.Connected {
		msg, err := s.wifiCalling.SendSMS(ctx, device.modem(), to, text)
		if err != nil {
			return "", fmt.Errorf("send SMS to %s over wifi calling: %w", to, err)
		}
		if _, err := s.store.InsertMessage(ctx, msg); err != nil {
			return "", err
		}
		return to, nil
	}
	sms, err := device.sendSMS(ctx, to, text)
	if err != nil {
		if settings.Connected {
			msg, werr := s.wifiCalling.SendSMS(ctx, device.modem(), to, text)
			if werr == nil {
				_, ierr := s.store.InsertMessage(ctx, msg)
				return to, ierr
			}
		}
		return "", fmt.Errorf("send SMS to %s: %w", to, err)
	}
	if _, err := s.store.InsertMessage(ctx, messageFromModemSMS(device, profileID, sms)); err != nil {
		return "", err
	}
	return to, nil
}

func (s *Service) SyncModemMessages(ctx context.Context, modem *mmodem.Modem, profileID string) error {
	return s.syncModemMessages(ctx, realModemDevice{modemRef: modem}, profileID)
}

func (s *Service) syncModemMessages(ctx context.Context, device modemDevice, profileID string) error {
	messages, err := device.listSMS(ctx)
	if err != nil {
		return fmt.Errorf("list messages: %w", err)
	}
	for _, sms := range messages {
		if sms == nil {
			continue
		}
		if _, err := s.store.InsertMessage(ctx, messageFromModemSMS(device, profileID, sms)); err != nil {
			return err
		}
	}
	return nil
}

func messageFromModemSMS(device modemDevice, profileID string, sms *mmodem.SMS) storage.Message {
	incoming := sms.State == mmodem.SMSStateReceived || sms.State == mmodem.SMSStateReceiving
	remote := strings.TrimSpace(sms.Number)
	sender, recipient := device.phoneNumber(), remote
	if incoming {
		sender, recipient = remote, device.phoneNumber()
	}
	return storage.Message{
		ModemID:     device.modemID(),
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

func (d realModemDevice) modem() *mmodem.Modem {
	return d.modemRef
}

func (d realModemDevice) profileID(ctx context.Context) (string, error) {
	return d.modemRef.ProfileID(ctx)
}

func (d realModemDevice) sendSMS(ctx context.Context, to string, text string) (*mmodem.SMS, error) {
	return d.modemRef.Messaging().Send(ctx, to, text)
}

func (d realModemDevice) listSMS(ctx context.Context) ([]*mmodem.SMS, error) {
	return d.modemRef.Messaging().List(ctx)
}

func (d realModemDevice) deleteSMS(ctx context.Context, path dbus.ObjectPath) error {
	return d.modemRef.Messaging().Delete(ctx, path)
}

func (d realModemDevice) modemID() string {
	return d.modemRef.EquipmentIdentifier
}

func (d realModemDevice) phoneNumber() string {
	return d.modemRef.Number
}
