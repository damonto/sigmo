package forwarder

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/damonto/sigmo/internal/pkg/config"
	"github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/notify"
	notifyevent "github.com/damonto/sigmo/internal/pkg/notify/event"
	"github.com/damonto/sigmo/internal/pkg/storage"
	"github.com/damonto/sigmo/internal/pkg/wificalling"
)

const incomingSMSFreshnessWindow = 30 * time.Minute

type Relay struct {
	store     *config.Store
	registry  *modem.Registry
	notifier  *notify.Notifier
	messages  *storage.Store
	mu        sync.Mutex
	cancels   map[dbus.ObjectPath]context.CancelFunc
	equipment map[string]dbus.ObjectPath
	modems    map[dbus.ObjectPath]string
}

func New(store *config.Store, registry *modem.Registry, messages *storage.Store) (*Relay, error) {
	if messages == nil {
		return nil, errors.New("message storage is required")
	}
	cfg := store.Snapshot()
	notifier, err := notify.New(&cfg)
	if err != nil {
		return nil, fmt.Errorf("creating notifier: %w", err)
	}
	return &Relay{
		store:     store,
		registry:  registry,
		notifier:  notifier,
		messages:  messages,
		cancels:   make(map[dbus.ObjectPath]context.CancelFunc),
		equipment: make(map[string]dbus.ObjectPath),
		modems:    make(map[dbus.ObjectPath]string),
	}, nil
}

func (r *Relay) Reload() error {
	cfg := r.store.Snapshot()
	notifier, err := notify.New(&cfg)
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
			slog.Error("modem message subscription stopped", "error", err, "modem", m.EquipmentIdentifier)
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
	cancels := make([]context.CancelFunc, 0, len(r.cancels))
	for _, cancel := range r.cancels {
		cancels = append(cancels, cancel)
	}
	r.cancels = make(map[dbus.ObjectPath]context.CancelFunc)
	r.equipment = make(map[string]dbus.ObjectPath)
	r.modems = make(map[dbus.ObjectPath]string)
	r.mu.Unlock()

	for _, cancel := range cancels {
		cancel()
	}
}

func (r *Relay) ForwardWiFiCallingSMS(ctx context.Context, incoming wificalling.IncomingSMS) error {
	if !freshIncomingMessage(incoming.Message, time.Now()) {
		slog.Debug("skipping stale Wi-Fi Calling SMS", "modem", incoming.ModemID, "externalKey", incoming.Message.ExternalKey, "timestamp", incoming.Message.Timestamp)
		return nil
	}
	inserted, err := r.messages.InsertMessage(ctx, incoming.Message)
	if err != nil {
		return err
	}
	if !inserted {
		slog.Debug("skipping known Wi-Fi Calling SMS", "modem", incoming.ModemID, "externalKey", incoming.Message.ExternalKey)
		return nil
	}
	r.mu.Lock()
	notifier := r.notifier
	r.mu.Unlock()
	return notifier.Send(ctx, r.formatStoredMessage(incoming.ModemID, incoming.Message))
}

func (r *Relay) forwardModemSMS(ctx context.Context, m *modem.Modem, message *modem.SMS) error {
	profileID, err := m.ProfileID(ctx)
	if err != nil {
		return err
	}
	stored := storageMessageFromModemSMS(m, profileID, message)
	if !freshIncomingMessage(stored, time.Now()) {
		slog.Debug("skipping stale modem SMS", "modem", m.EquipmentIdentifier, "path", message.Path(), "timestamp", message.Timestamp)
		return nil
	}
	inserted, err := r.messages.InsertMessage(ctx, stored)
	if err != nil {
		return err
	}
	if !inserted {
		slog.Debug("skipping known modem SMS", "modem", m.EquipmentIdentifier, "path", message.Path())
		return nil
	}
	r.mu.Lock()
	notifier := r.notifier
	r.mu.Unlock()
	return notifier.Send(ctx, r.formatStoredMessage(m.EquipmentIdentifier, stored))
}

func freshIncomingMessage(message storage.Message, now time.Time) bool {
	if !message.Incoming || message.Timestamp.IsZero() {
		return true
	}
	diff := now.Sub(message.Timestamp)
	if diff < 0 {
		diff = -diff
	}
	return diff <= incomingSMSFreshnessWindow
}

func (r *Relay) formatStoredMessage(modemID string, message storage.Message) notifyevent.SMSEvent {
	return notifyevent.SMSEvent{
		Modem:    r.modemLabel(modemID),
		From:     message.Sender,
		To:       message.Recipient,
		Time:     message.Timestamp,
		Text:     strings.TrimSpace(message.Text),
		Incoming: message.Incoming,
	}
}

func (r *Relay) modemLabel(modemID string) string {
	if alias := strings.TrimSpace(r.store.FindModem(modemID).Alias); alias != "" {
		return alias
	}
	return strings.TrimSpace(modemID)
}

func storageMessageFromModemSMS(m *modem.Modem, profileID string, sms *modem.SMS) storage.Message {
	incoming := sms.State == modem.SMSStateReceived || sms.State == modem.SMSStateReceiving
	remote := strings.TrimSpace(sms.Number)
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
