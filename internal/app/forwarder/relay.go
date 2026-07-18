package forwarder

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"

	messagepkg "github.com/damonto/sigmo/internal/pkg/message"
	"github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/notify"
	notifyevent "github.com/damonto/sigmo/internal/pkg/notify/event"
	"github.com/damonto/sigmo/internal/pkg/settings"
	"github.com/damonto/sigmo/internal/pkg/storage"
	"github.com/damonto/sigmo/internal/pkg/webpush"
)

const incomingNotificationFreshnessWindow = 30 * time.Minute

const (
	callDirectionIncoming = "incoming"
	callStateRinging      = "ringing"
)

type Relay struct {
	store         *settings.Store
	registry      *modem.Registry
	notifier      *notify.Notifier
	webPush       *webpush.Client
	messages      *storage.Store
	mu            sync.Mutex
	cancels       map[dbus.ObjectPath]context.CancelFunc
	equipment     map[string]dbus.ObjectPath
	modems        map[dbus.ObjectPath]string
	notifiedCalls map[string]struct{}
}

func New(store *settings.Store, registry *modem.Registry, messages *storage.Store, webPush *webpush.Client) (*Relay, error) {
	if messages == nil {
		return nil, errors.New("message storage is required")
	}
	current := store.Snapshot()
	notifier, err := notify.New(&current)
	if err != nil {
		return nil, fmt.Errorf("creating notifier: %w", err)
	}
	return &Relay{
		store:         store,
		registry:      registry,
		notifier:      notifier,
		webPush:       webPush,
		messages:      messages,
		cancels:       make(map[dbus.ObjectPath]context.CancelFunc),
		equipment:     make(map[string]dbus.ObjectPath),
		modems:        make(map[dbus.ObjectPath]string),
		notifiedCalls: make(map[string]struct{}),
	}, nil
}

func (r *Relay) Reload() error {
	current := r.store.Snapshot()
	notifier, err := notify.New(&current)
	if err != nil {
		return fmt.Errorf("creating notifier: %w", err)
	}
	r.mu.Lock()
	r.notifier = notifier
	r.mu.Unlock()
	return nil
}

func (r *Relay) Run(ctx context.Context) error {
	modems, err := r.registry.Modems(ctx)
	if err != nil {
		return fmt.Errorf("listing modems: %w", err)
	}
	for path, m := range modems {
		r.addModem(ctx, path, m)
	}

	unsubscribe, err := r.registry.Subscribe(func(event modem.ModemEvent) error {
		switch event.Type {
		case modem.ModemEventAdded:
			if event.Modem == nil {
				return nil
			}
			r.addModem(ctx, event.Path, event.Modem)
		case modem.ModemEventRemoved:
			r.removeModem(event.Path)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("subscribing to modem registry: %w", err)
	}
	defer unsubscribe()

	<-ctx.Done()
	r.stopAll()
	return nil
}

func (r *Relay) addModem(ctx context.Context, path dbus.ObjectPath, m *modem.Modem) {
	if ctx.Err() != nil {
		return
	}
	r.mu.Lock()
	if m.EquipmentIdentifier != "" {
		if existingPath, ok := r.equipment[m.EquipmentIdentifier]; ok && existingPath != path {
			if oldCancel := r.cancels[existingPath]; oldCancel != nil {
				defer oldCancel()
			}
			delete(r.cancels, existingPath)
			delete(r.modems, existingPath)
			delete(r.equipment, m.EquipmentIdentifier)
		}
	}
	if _, ok := r.cancels[path]; ok {
		r.mu.Unlock()
		return
	}
	modemCtx, cancel := context.WithCancel(ctx)
	r.cancels[path] = cancel
	if m.EquipmentIdentifier != "" {
		r.equipment[m.EquipmentIdentifier] = path
		r.modems[path] = m.EquipmentIdentifier
	}
	r.mu.Unlock()

	go func() {
		if err := m.Messaging().Subscribe(modemCtx, func(message *modem.SMS) error {
			return r.forwardModemSMS(modemCtx, m, message)
		}); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("modem message subscription stopped", "error", err, "imei", m.EquipmentIdentifier)
		}
		r.removeModem(path)
	}()
}

func (r *Relay) removeModem(path dbus.ObjectPath) {
	var cancel context.CancelFunc
	r.mu.Lock()
	cancel = r.cancels[path]
	delete(r.cancels, path)
	if equipmentID, ok := r.modems[path]; ok {
		delete(r.modems, path)
		delete(r.equipment, equipmentID)
	}
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (r *Relay) stopAll() {
	r.mu.Lock()
	cancels := slices.Collect(maps.Values(r.cancels))
	r.cancels = make(map[dbus.ObjectPath]context.CancelFunc)
	r.equipment = make(map[string]dbus.ObjectPath)
	r.modems = make(map[dbus.ObjectPath]string)
	r.mu.Unlock()

	for _, cancel := range cancels {
		cancel()
	}
}

func (r *Relay) ForwardRoutedSMS(ctx context.Context, modemID string, message storage.Message) error {
	if !freshIncomingMessage(message, time.Now()) {
		slog.Debug("skipping stale routed SMS", "imei", modemID, "externalKey", message.ExternalKey, "timestamp", message.Timestamp)
		return nil
	}
	inserted, err := r.messages.InsertMessage(ctx, message)
	if err != nil {
		return err
	}
	if !inserted {
		slog.Debug("skipping known routed SMS", "imei", modemID, "externalKey", message.ExternalKey)
		return nil
	}
	r.mu.Lock()
	notifier := r.notifier
	r.mu.Unlock()
	return r.send(ctx, notifier, r.formatStoredMessage(modemID, message))
}

func (r *Relay) ForwardCall(ctx context.Context, call storage.Call) error {
	if !freshIncomingCall(call, time.Now()) {
		return nil
	}
	if !r.reserveCallNotification(call.ID) {
		slog.Debug("skipping known incoming call", "imei", call.ModemID, "call_id", call.ID)
		return nil
	}

	r.mu.Lock()
	notifier := r.notifier
	r.mu.Unlock()
	if err := r.send(ctx, notifier, r.formatStoredCall(call)); err != nil {
		r.releaseCallNotification(call.ID)
		return err
	}
	return nil
}

func (r *Relay) forwardModemSMS(ctx context.Context, m *modem.Modem, message *modem.SMS) error {
	profileID, err := m.ProfileID(ctx)
	if err != nil {
		return err
	}
	stored := storageMessageFromModemSMS(ctx, m, profileID, message)
	if !freshIncomingMessage(stored, time.Now()) {
		slog.Debug("skipping stale modem SMS", "imei", m.EquipmentIdentifier, "path", message.Path(), "timestamp", message.Timestamp)
		return nil
	}
	inserted, err := r.messages.InsertMessage(ctx, stored)
	if err != nil {
		return err
	}
	if !inserted {
		slog.Debug("skipping known modem SMS", "imei", m.EquipmentIdentifier, "path", message.Path())
		return nil
	}
	r.mu.Lock()
	notifier := r.notifier
	r.mu.Unlock()
	return r.send(ctx, notifier, r.formatStoredMessage(m.EquipmentIdentifier, stored))
}

func freshIncomingMessage(message storage.Message, now time.Time) bool {
	if !message.Incoming || message.Timestamp.IsZero() {
		return true
	}
	diff := now.Sub(message.Timestamp)
	if diff < 0 {
		diff = -diff
	}
	return diff <= incomingNotificationFreshnessWindow
}

func freshIncomingCall(call storage.Call, now time.Time) bool {
	if call.Direction != callDirectionIncoming || call.State != callStateRinging {
		return false
	}
	timestamp := call.StartedAt
	if timestamp.IsZero() {
		timestamp = call.UpdatedAt
	}
	if timestamp.IsZero() {
		return true
	}
	diff := now.Sub(timestamp)
	if diff < 0 {
		diff = -diff
	}
	return diff <= incomingNotificationFreshnessWindow
}

func (r *Relay) formatStoredMessage(modemID string, message storage.Message) notifyevent.SMSEvent {
	return notifyevent.SMSEvent{
		ID:       storage.MessageFingerprint(message),
		ModemID:  modemID,
		Modem:    r.modemLabel(modemID),
		From:     message.Sender,
		To:       message.Recipient,
		Time:     message.Timestamp,
		Text:     strings.TrimSpace(message.Text),
		Incoming: message.Incoming,
	}
}

func (r *Relay) formatStoredCall(call storage.Call) notifyevent.CallEvent {
	return notifyevent.CallEvent{
		ID:       call.ID,
		ModemID:  call.ModemID,
		Modem:    r.modemLabel(call.ModemID),
		From:     strings.TrimSpace(call.Number),
		Time:     call.StartedAt,
		State:    call.State,
		Incoming: call.Direction == callDirectionIncoming,
	}
}

func (r *Relay) send(ctx context.Context, notifier *notify.Notifier, event notifyevent.Event) error {
	var wg sync.WaitGroup
	var notifierErr, webPushErr error
	wg.Go(func() {
		notifierErr = notifier.Send(ctx, event)
	})
	if r.webPush != nil {
		wg.Go(func() {
			webPushErr = r.webPush.Send(ctx, event)
		})
	}
	wg.Wait()
	if webPushErr != nil {
		slog.Warn("send web push", "kind", event.Kind(), "error", webPushErr)
	}
	return notifierErr
}

func (r *Relay) reserveCallNotification(callID string) bool {
	callID = strings.TrimSpace(callID)
	if callID == "" {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.notifiedCalls[callID]; ok {
		return false
	}
	r.notifiedCalls[callID] = struct{}{}
	return true
}

func (r *Relay) releaseCallNotification(callID string) {
	r.mu.Lock()
	delete(r.notifiedCalls, strings.TrimSpace(callID))
	r.mu.Unlock()
}

func (r *Relay) modemLabel(modemID string) string {
	if alias := strings.TrimSpace(r.store.FindModem(modemID).Alias); alias != "" {
		return alias
	}
	return strings.TrimSpace(modemID)
}

func storageMessageFromModemSMS(ctx context.Context, m *modem.Modem, profileID string, sms *modem.SMS) storage.Message {
	incoming := sms.State == modem.SMSStateReceived || sms.State == modem.SMSStateReceiving
	remote := messagepkg.CanonicalAddress(ctx, m, sms.Number)
	sender, recipient := m.Number, remote
	if incoming {
		sender, recipient = remote, m.Number
	}
	return storage.Message{
		ModemID:     m.EquipmentIdentifier,
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
