package message

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/storage"
	wwanmodem "github.com/damonto/wwan-go/modem"
)

type Messenger struct {
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
	deleteSMS(context.Context, []mmodem.MessageRef) error
	modemID() string
	generation() uint64
	phoneNumber() string
}

type realModemDevice struct {
	modemRef *mmodem.Modem
}

func New(store *storage.Store, route Route) *Messenger {
	return &Messenger{store: store, route: route}
}

// ListConversations returns stored history for the modem's current SIM.
func (s *Messenger) ListConversations(ctx context.Context, modem *mmodem.Modem, query string) ([]storage.Message, error) {
	profileID, err := modem.ProfileID(ctx)
	if err != nil {
		return nil, err
	}
	return s.store.ListConversations(ctx, profileID, query)
}

// ListByParticipant returns stored messages for a participant on the modem's current SIM.
func (s *Messenger) ListByParticipant(ctx context.Context, modem *mmodem.Modem, participant string) ([]storage.Message, error) {
	if strings.TrimSpace(participant) == "" {
		return nil, ErrParticipantRequired
	}
	profileID, err := modem.ProfileID(ctx)
	if err != nil {
		return nil, err
	}
	return s.store.ListByParticipant(ctx, profileID, participant)
}

func (s *Messenger) DeleteByParticipant(ctx context.Context, modem *mmodem.Modem, participant string) error {
	return s.deleteByParticipant(ctx, realModemDevice{modemRef: modem}, participant)
}

func (s *Messenger) deleteByParticipant(ctx context.Context, device modemDevice, participant string) error {
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
		refs := make([]mmodem.MessageRef, 0, len(msg.ModemRefs))
		for _, ref := range msg.ModemRefs {
			if ref.Generation != 0 && ref.Generation != device.generation() {
				continue
			}
			refs = append(refs, mmodem.MessageRef{Storage: wwanMessageStorage(ref.Storage), ID: ref.ID})
		}
		if len(refs) == 0 {
			continue
		}
		if err := device.deleteSMS(ctx, refs); err != nil {
			return fmt.Errorf("delete message for %s: %w", participant, err)
		}
	}
	return nil
}

func (s *Messenger) Send(ctx context.Context, modem *mmodem.Modem, to string, text string) (string, error) {
	return s.send(ctx, realModemDevice{modemRef: modem}, to, text)
}

// SendRequest optionally pins submission to a profile or a particular SIM activation.
type SendRequest struct {
	To                string
	Text              string
	ExpectedProfileID string
	ExpectedSIM       mmodem.SIMIdentity
}

// Sender submits an SMS through a specific IMS access.
type Sender func(context.Context, *mmodem.Modem, string, string) (storage.Message, error)

type sendRoute struct {
	fixed  bool
	sender Sender
	check  func(context.Context) error
}

// SendWithProfile reserves the SIM through validation and submission, including IMS routing.
func (s *Messenger) SendWithProfile(ctx context.Context, modem *mmodem.Modem, req SendRequest) (string, error) {
	return s.sendWithProfile(ctx, modem, req, sendRoute{})
}

// SendWithProfileVia uses only the given sender, or the modem if sender is nil.
// The optional check runs on the reserved SIM before any submission.
func (s *Messenger) SendWithProfileVia(ctx context.Context, modem *mmodem.Modem, req SendRequest, sender Sender, check func(context.Context) error) (string, error) {
	return s.sendWithProfile(ctx, modem, req, sendRoute{fixed: true, sender: sender, check: check})
}

func (s *Messenger) sendWithProfile(ctx context.Context, modem *mmodem.Modem, req SendRequest, route sendRoute) (string, error) {
	device := realModemDevice{modemRef: modem}
	if !route.fixed && req.ExpectedProfileID == "" && req.ExpectedSIM.ICCID == "" {
		return s.sendUsing(ctx, device, req.To, req.Text, route)
	}
	release, err := modem.ReserveSIMSlot(ctx)
	if err != nil {
		return "", fmt.Errorf("reserve message SIM: %w", err)
	}
	defer release()
	if req.ExpectedSIM.ICCID != "" && !modem.MatchesSIMActivation(req.ExpectedSIM) {
		return "", ErrProfileChanged
	}
	if req.ExpectedProfileID != "" {
		profileID, err := modem.ProfileID(ctx)
		if err != nil {
			return "", fmt.Errorf("read message SIM: %w", err)
		}
		if profileID != req.ExpectedProfileID {
			return "", ErrProfileChanged
		}
	}
	return s.sendUsing(ctx, device, req.To, req.Text, route)
}

func (s *Messenger) send(ctx context.Context, device modemDevice, to, text string) (string, error) {
	return s.sendUsing(ctx, device, to, text, sendRoute{})
}

func (s *Messenger) sendUsing(ctx context.Context, device modemDevice, to, text string, route sendRoute) (recipient string, sendErr error) {
	if strings.TrimSpace(to) == "" {
		return "", ErrRecipientRequired
	}
	if strings.TrimSpace(text) == "" {
		return "", ErrTextRequired
	}
	to, err := NormalizeSMSAddress(to)
	if err != nil {
		return "", err
	}
	to = CanonicalAddress(ctx, device.modem(), to)
	profileID, err := device.profileID(ctx)
	if err != nil {
		return "", err
	}
	// A deferred success check covers legacy and pinned sends, including IMS fallback.
	defer func() {
		if sendErr != nil {
			return
		}
		if err := s.store.RememberCommunication(ctx, storage.CommunicationPreference{Participant: recipient, ProfileID: profileID}); err != nil {
			// The carrier already accepted the SMS; a send failure would invite duplicates.
			slog.WarnContext(ctx, "remember sent message line", "error", err)
		}
	}()
	if route.check != nil {
		if err := route.check(ctx); err != nil {
			return "", err
		}
	}
	var status RouteStatus
	if !route.fixed {
		status, err = s.routeStatus(ctx, device.modem())
		if err != nil {
			return "", fmt.Errorf("read message route status: %w", err)
		}
		if status.Preferred && status.Connected {
			route.sender = s.route.SendSMS
		}
	}
	if route.sender != nil {
		msg, err := route.sender(ctx, device.modem(), to, text)
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
		// A timeout or disconnected transport may have already submitted the SMS.
		// Switching routes in that case could send the same message twice.
		if uncertainSubmission(err) {
			return "", fmt.Errorf("send SMS to %s: %w: %w", to, ErrSendUnconfirmed, err)
		}
		if status.Connected && s.route != nil {
			msg, routeErr := s.route.SendSMS(ctx, device.modem(), to, text)
			if routeErr == nil {
				return to, s.insertSentMessage(ctx, msg)
			}
			if uncertainSubmission(routeErr) {
				return "", mapRouteSendError(to, routeErr)
			}
		}
		return "", fmt.Errorf("send SMS to %s: %w", to, err)
	}
	if err := s.insertSentMessage(ctx, messageFromModemSMS(ctx, device, profileID, sms)); err != nil {
		return "", err
	}
	return to, nil
}

func (s *Messenger) routeStatus(ctx context.Context, modem *mmodem.Modem) (RouteStatus, error) {
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
	if uncertainSubmission(err) {
		return fmt.Errorf("send SMS to %s over selected route: %w: %w", to, ErrSendUnconfirmed, err)
	}
	if errors.Is(err, ErrRouteNotConnected) {
		return fmt.Errorf("send SMS to %s over selected route: %w", to, ErrRouteNotConnected)
	}
	return fmt.Errorf("send SMS to %s over selected route: %w", to, err)
}

func uncertainSubmission(err error) bool {
	var networkError net.Error
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) ||
		(errors.As(err, &networkError) && networkError.Timeout())
}

func (s *Messenger) insertSentMessage(ctx context.Context, msg storage.Message) error {
	if _, err := s.store.InsertMessage(ctx, msg); err != nil {
		return err
	}
	if s.route != nil {
		if err := s.route.ApplyPendingSMSStatus(ctx, msg); err != nil {
			return fmt.Errorf("apply routed SMS status: %w", err)
		}
	}
	return nil
}

func messageFromModemSMS(ctx context.Context, device modemDevice, profileID string, sms *mmodem.SMS) storage.Message {
	incoming := incomingMessageState(sms.State)
	remote := CanonicalAddress(ctx, device.modem(), sms.Number)
	sender, recipient := device.phoneNumber(), remote
	if incoming {
		sender, recipient = remote, device.phoneNumber()
	}
	return storage.Message{
		ModemID:   device.modemID(),
		ProfileID: profileID,
		Source:    storage.MessageSourceModem,
		Sender:    sender,
		Recipient: recipient,
		Text:      sms.Text,
		Timestamp: sms.Timestamp,
		Status:    messageStateName(sms.State),
		Incoming:  incoming,
		ModemRefs: StorageModemRefs(device.modemID(), sms.Generation, sms.Refs),
	}
}

func StorageModemRefs(modemID string, generation uint64, refs []mmodem.MessageRef) []storage.ModemMessageRef {
	result := make([]storage.ModemMessageRef, 0, len(refs))
	for _, ref := range refs {
		result = append(result, storage.ModemMessageRef{ModemID: modemID, Generation: generation, Storage: uint8(ref.Storage), ID: ref.ID})
	}
	return result
}

func wwanMessageStorage(storage uint8) wwanmodem.MessageStorage {
	return wwanmodem.MessageStorage(storage)
}

func incomingMessageState(state wwanmodem.MessageState) bool {
	return state == wwanmodem.MessageStateReceivedUnread || state == wwanmodem.MessageStateReceivedRead
}

func messageStateName(state wwanmodem.MessageState) string {
	switch state {
	case wwanmodem.MessageStateReceivedUnread:
		return "received-unread"
	case wwanmodem.MessageStateReceivedRead:
		return "received-read"
	case wwanmodem.MessageStateStoredUnsent:
		return "stored-unsent"
	case wwanmodem.MessageStateStoredSent:
		return "stored-sent"
	default:
		return "unknown"
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

func (d realModemDevice) deleteSMS(ctx context.Context, refs []mmodem.MessageRef) error {
	return d.modemRef.Messaging().Delete(ctx, refs)
}

func (d realModemDevice) modemID() string {
	return d.modemRef.EquipmentIdentifier
}

func (d realModemDevice) generation() uint64 {
	if d.modemRef == nil {
		return 0
	}
	return d.modemRef.Generation()
}

func (d realModemDevice) phoneNumber() string {
	return d.modemRef.Snapshot().Number
}
