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

	messagepkg "github.com/damonto/sigmo/internal/pkg/message"
	"github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/notify"
	notifyevent "github.com/damonto/sigmo/internal/pkg/notify/event"
	"github.com/damonto/sigmo/internal/pkg/settings"
	"github.com/damonto/sigmo/internal/pkg/storage"
	"github.com/damonto/sigmo/internal/pkg/webpush"
	wwanmodem "github.com/damonto/wwan-go/modem"
)

const incomingNotificationFreshnessWindow = 30 * time.Minute

var modemSubscriptionRetryDelay = time.Second

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
	subscriptions map[string]relaySubscription
	equipment     map[string]string
	modems        map[string]string
	notifiedCalls map[string]struct{}
	wg            sync.WaitGroup
	stopping      bool
}

type relaySubscription struct {
	generation uint64
	cancel     context.CancelFunc
}

type modemSMSDeleter interface {
	Delete(context.Context, []modem.MessageRef) error
}

type modemSMSReceipt struct {
	stored  storage.Message
	refs    []modem.MessageRef
	deleter modemSMSDeleter
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
		subscriptions: make(map[string]relaySubscription),
		equipment:     make(map[string]string),
		modems:        make(map[string]string),
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
	unsubscribe, err := r.registry.Subscribe(func(event modem.ModemEvent) error {
		return r.handleModemEvent(ctx, event)
	})
	if err != nil {
		return fmt.Errorf("subscribing to modem registry: %w", err)
	}
	defer func() {
		unsubscribe()
		r.stopAll()
	}()

	modems, err := r.registry.Modems(ctx)
	if err != nil {
		return fmt.Errorf("listing modems: %w", err)
	}
	for path, m := range modems {
		r.addModem(ctx, path, m)
	}

	<-ctx.Done()
	return nil
}

func (r *Relay) handleModemEvent(ctx context.Context, event modem.ModemEvent) error {
	switch event.Type {
	case modem.ModemEventAdded, modem.ModemEventChanged:
		if event.Modem == nil {
			return nil
		}
		r.addModem(ctx, event.Path, event.Modem)
		if event.Type == modem.ModemEventChanged && event.Previous != nil {
			previousPath := event.PreviousPath
			if previousPath == "" {
				previousPath = event.Previous.Path()
			}
			if previousPath != event.Path {
				r.removeModem(previousPath, event.Previous.Generation())
			}
		}
	case modem.ModemEventRemoved:
		r.removeModem(event.Path, event.Generation)
	}
	return nil
}

func (r *Relay) addModem(ctx context.Context, path string, m *modem.Modem) {
	if m == nil || ctx.Err() != nil {
		return
	}
	if path == "" {
		path = m.Path()
	}
	var replaced []context.CancelFunc
	r.mu.Lock()
	if r.stopping {
		r.mu.Unlock()
		return
	}
	if m.EquipmentIdentifier != "" {
		if existingPath, ok := r.equipment[m.EquipmentIdentifier]; ok && existingPath != path {
			if old, ok := r.subscriptions[existingPath]; ok {
				replaced = append(replaced, old.cancel)
			}
			delete(r.subscriptions, existingPath)
			delete(r.modems, existingPath)
			delete(r.equipment, m.EquipmentIdentifier)
		}
	}
	if existing, ok := r.subscriptions[path]; ok && existing.generation == m.Generation() {
		r.mu.Unlock()
		return
	}
	if existing, ok := r.subscriptions[path]; ok {
		replaced = append(replaced, existing.cancel)
	}
	modemCtx, cancel := context.WithCancel(ctx)
	r.subscriptions[path] = relaySubscription{generation: m.Generation(), cancel: cancel}
	if m.EquipmentIdentifier != "" {
		r.equipment[m.EquipmentIdentifier] = path
		r.modems[path] = m.EquipmentIdentifier
	}
	r.wg.Add(1)
	r.mu.Unlock()
	for _, oldCancel := range replaced {
		oldCancel()
	}

	go func() {
		defer r.wg.Done()
		defer r.removeModem(path, m.Generation())
		r.runModemSubscription(modemCtx, m)
	}()
}

func (r *Relay) runModemSubscription(ctx context.Context, m *modem.Modem) {
	for ctx.Err() == nil {
		err := m.Messaging().Subscribe(ctx, func(message *modem.SMS) error {
			if !incomingModemSMS(message) {
				return nil
			}
			return r.forwardModemSMS(ctx, m, message)
		})
		if ctx.Err() != nil {
			return
		}
		slog.Error("modem message subscription stopped", "error", err, "imei", m.EquipmentIdentifier, "generation", m.Generation())
		if err := sleepRelayContext(ctx, modemSubscriptionRetryDelay); err != nil {
			return
		}
	}
}

func incomingModemSMS(message *modem.SMS) bool {
	return message != nil && incomingMessageState(message.State)
}

func sleepRelayContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (r *Relay) removeModem(path string, generation uint64) {
	var cancel context.CancelFunc
	r.mu.Lock()
	subscription, ok := r.subscriptions[path]
	if ok && generation != 0 && subscription.generation != generation {
		ok = false
	}
	if ok {
		cancel = subscription.cancel
		delete(r.subscriptions, path)
		if equipmentID, exists := r.modems[path]; exists {
			delete(r.modems, path)
			if r.equipment[equipmentID] == path {
				delete(r.equipment, equipmentID)
			}
		}
	}
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (r *Relay) stopAll() {
	r.mu.Lock()
	r.stopping = true
	subscriptions := slices.Collect(maps.Values(r.subscriptions))
	r.subscriptions = make(map[string]relaySubscription)
	r.equipment = make(map[string]string)
	r.modems = make(map[string]string)
	r.mu.Unlock()

	for _, subscription := range subscriptions {
		subscription.cancel()
	}
	r.wg.Wait()
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
	if !incomingModemSMS(message) {
		return nil
	}
	profileID, err := m.ProfileID(ctx)
	if err != nil {
		return err
	}
	stored := storageMessageFromModemSMS(ctx, m, profileID, message)
	return r.forwardStoredModemSMS(ctx, modemSMSReceipt{
		stored:  stored,
		refs:    message.Refs,
		deleter: m.Messaging(),
	})
}

func (r *Relay) forwardStoredModemSMS(ctx context.Context, receipt modemSMSReceipt) error {
	if !receipt.stored.Incoming || receipt.stored.Source != storage.MessageSourceModem {
		return nil
	}
	inserted, err := r.messages.InsertMessage(ctx, receipt.stored)
	if err != nil {
		return err
	}
	cleanupErr := r.deleteModemSMS(ctx, receipt)
	if !inserted {
		slog.Debug("skipping known modem SMS notification", "imei", receipt.stored.ModemID, "refs", receipt.refs)
		return cleanupErr
	}
	if !freshIncomingMessage(receipt.stored, time.Now()) {
		slog.Debug("skipping stale modem SMS notification", "imei", receipt.stored.ModemID, "refs", receipt.refs, "timestamp", receipt.stored.Timestamp)
		return cleanupErr
	}
	r.mu.Lock()
	notifier := r.notifier
	r.mu.Unlock()
	return errors.Join(cleanupErr, r.send(ctx, notifier, r.formatStoredMessage(receipt.stored.ModemID, receipt.stored)))
}

func (r *Relay) deleteModemSMS(ctx context.Context, receipt modemSMSReceipt) error {
	if len(receipt.refs) == 0 {
		return nil
	}
	if err := r.messages.DeleteModemMessageRefs(ctx, receipt.stored.ModemRefs); err != nil {
		return fmt.Errorf("delete stored modem SMS references: %w", err)
	}
	if err := receipt.deleter.Delete(ctx, receipt.refs); err != nil {
		return fmt.Errorf("delete modem SMS: %w", err)
	}
	return nil
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
	incoming := incomingMessageState(sms.State)
	remote := messagepkg.CanonicalAddress(ctx, m, sms.Number)
	number := m.Snapshot().Number
	sender, recipient := number, remote
	if incoming {
		sender, recipient = remote, number
	}
	return storage.Message{
		ModemID:     m.EquipmentIdentifier,
		ProfileID:   profileID,
		Source:      storage.MessageSourceModem,
		ExternalKey: messagepkg.ModemSMSKey(m.EquipmentIdentifier, sms),
		Sender:      sender,
		Recipient:   recipient,
		Text:        sms.Text,
		Timestamp:   sms.Timestamp,
		Status:      messageStateName(sms.State),
		Incoming:    incoming,
		ModemRefs:   messagepkg.StorageModemRefs(m.EquipmentIdentifier, sms.Generation, sms.Refs),
	}
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
