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
)

type Relay struct {
	store     *config.Store
	manager   *modem.Manager
	notifier  *notify.Notifier
	mu        sync.Mutex
	cancels   map[dbus.ObjectPath]context.CancelFunc
	equipment map[string]dbus.ObjectPath
	modems    map[dbus.ObjectPath]string
}

func New(store *config.Store, manager *modem.Manager) (*Relay, error) {
	cfg := store.Snapshot()
	notifier, err := notify.New(&cfg)
	if err != nil {
		return nil, fmt.Errorf("creating notifier: %w", err)
	}
	return &Relay{
		store:     store,
		manager:   manager,
		notifier:  notifier,
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
	modems, err := r.manager.Modems()
	if err != nil {
		return fmt.Errorf("listing modems: %w", err)
	}
	for path, m := range modems {
		r.addModem(ctx, path, m)
	}

	unsubscribe, err := r.manager.Subscribe(func(event modem.ModemEvent) error {
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
		return fmt.Errorf("subscribing to modem manager: %w", err)
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
			return r.forward(modemCtx, m, message)
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

func (r *Relay) forward(ctx context.Context, m *modem.Modem, message *modem.SMS) error {
	incoming := message.State == modem.SMSStateReceived || message.State == modem.SMSStateReceiving
	if incoming && !message.Timestamp.IsZero() && time.Since(message.Timestamp) > 30*time.Minute {
		slog.Info("skipping SMS notification older than 30 minutes", "timestamp", message.Timestamp, "modem", m.EquipmentIdentifier)
		return nil
	}
	r.mu.Lock()
	notifier := r.notifier
	r.mu.Unlock()
	return notifier.Send(ctx, r.formatMessage(m, message))
}

func (r *Relay) formatMessage(m *modem.Modem, message *modem.SMS) notifyevent.SMSEvent {
	incoming := message.State == modem.SMSStateReceived || message.State == modem.SMSStateReceiving
	sender, recipient := message.Number, m.Number
	if !incoming {
		sender, recipient = recipient, sender
	}
	return notifyevent.SMSEvent{
		Modem:    r.modemName(m),
		From:     sender,
		To:       recipient,
		Time:     message.Timestamp,
		Text:     strings.TrimSpace(message.Text),
		Incoming: incoming,
	}
}

func (r *Relay) modemName(m *modem.Modem) string {
	if alias := strings.TrimSpace(r.store.FindModem(m.EquipmentIdentifier).Alias); alias != "" {
		return alias
	}
	return strings.TrimSpace(m.Model)
}
