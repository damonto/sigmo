package message

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/godbus/dbus/v5"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

type Service struct {
	store *storage.Store
	route Route
}

type RouteStatus struct {
	Preferred bool
	Connected bool
}

type Route interface {
	Status(context.Context, *mmodem.Modem) (RouteStatus, error)
	SendSMS(context.Context, *mmodem.Modem, string, string) (storage.Message, error)
	ApplyPendingSMSStatus(context.Context, storage.Message) error
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

func New(store *storage.Store, route Route) *Service {
	return &Service{store: store, route: route}
}

func (s *Service) ListConversations(ctx context.Context, modem *mmodem.Modem, query string) ([]storage.Message, error) {
	return s.listConversations(ctx, realModemDevice{modemRef: modem}, query)
}

func (s *Service) listConversations(ctx context.Context, device modemDevice, query string) ([]storage.Message, error) {
	profileID, err := device.profileID(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.syncModemMessages(ctx, device, profileID); err != nil {
		return nil, err
	}
	return s.store.ListConversations(ctx, profileID, query)
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
	to, err := normalizeSMSAddress(to)
	if err != nil {
		return "", err
	}
	profileID, err := device.profileID(ctx)
	if err != nil {
		return "", err
	}
	status, err := s.routeStatus(ctx, device.modem())
	if err != nil {
		return "", fmt.Errorf("read message route status: %w", err)
	}
	if status.Preferred && status.Connected {
		msg, err := s.route.SendSMS(ctx, device.modem(), to, text)
		if err != nil {
			return "", mapRouteSendError(to, err)
		}
		if err := s.insertSentMessage(ctx, msg); err != nil {
			return "", err
		}
		return to, nil
	}
	sms, err := device.sendSMS(ctx, to, text)
	if err != nil {
		if status.Connected && s.route != nil {
			msg, routeErr := s.route.SendSMS(ctx, device.modem(), to, text)
			if routeErr == nil {
				return to, s.insertSentMessage(ctx, msg)
			}
		}
		return "", fmt.Errorf("send SMS to %s: %w", to, err)
	}
	if err := s.insertSentMessage(ctx, messageFromModemSMS(device, profileID, sms)); err != nil {
		return "", err
	}
	return to, nil
}

func (s *Service) routeStatus(ctx context.Context, modem *mmodem.Modem) (RouteStatus, error) {
	if s.route == nil {
		return RouteStatus{}, nil
	}
	status, err := s.route.Status(ctx, modem)
	if err != nil && !errors.Is(err, ErrRouteUnavailable) {
		return RouteStatus{}, err
	}
	return status, nil
}

func mapRouteSendError(to string, err error) error {
	if errors.Is(err, ErrRouteNotConnected) {
		return fmt.Errorf("send SMS to %s over selected route: %w", to, ErrRouteNotConnected)
	}
	return fmt.Errorf("send SMS to %s over selected route: %w", to, err)
}

func (s *Service) insertSentMessage(ctx context.Context, msg storage.Message) error {
	inserted, err := s.store.InsertMessage(ctx, msg)
	if err != nil {
		return err
	}
	if !inserted {
		slog.Warn("sent SMS was not inserted",
			"profile_id", msg.ProfileID,
			"source", msg.Source,
			"external_key", msg.ExternalKey,
			"recipient", msg.Recipient,
			"timestamp", msg.Timestamp,
		)
	}
	if s.route != nil {
		if err := s.route.ApplyPendingSMSStatus(ctx, msg); err != nil {
			return fmt.Errorf("apply routed SMS status: %w", err)
		}
	}
	return nil
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
