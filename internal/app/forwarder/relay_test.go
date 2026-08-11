package forwarder

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/damonto/sigmo/internal/pkg/modem"
	notifyevent "github.com/damonto/sigmo/internal/pkg/notify/event"
	"github.com/damonto/sigmo/internal/pkg/settings"
	"github.com/damonto/sigmo/internal/pkg/storage"
	"github.com/damonto/sigmo/internal/pkg/webpush"
	wwanmodem "github.com/damonto/wwan-go/modem"
)

func TestNewRequiresMessageStorage(t *testing.T) {
	tests := []struct {
		name string
	}{
		{name: "nil message storage"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(settings.NewMemoryStore(settings.Default()), nil, nil, nil)
			if err == nil {
				t.Fatal("New() error = nil, want error")
			}
		})
	}
}

func TestFreshIncomingCall(t *testing.T) {
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		call storage.Call
		want bool
	}{
		{
			name: "recent incoming ringing call",
			call: storage.Call{
				Direction: "incoming",
				State:     "ringing",
				StartedAt: now.Add(-29 * time.Minute),
			},
			want: true,
		},
		{
			name: "old incoming ringing call",
			call: storage.Call{
				Direction: "incoming",
				State:     "ringing",
				StartedAt: now.Add(-31 * time.Minute),
			},
		},
		{
			name: "outgoing call",
			call: storage.Call{
				Direction: "outgoing",
				State:     "ringing",
				StartedAt: now,
			},
		},
		{
			name: "answered incoming call",
			call: storage.Call{
				Direction: "incoming",
				State:     "active",
				StartedAt: now,
			},
		},
		{
			name: "unknown timestamp",
			call: storage.Call{
				Direction: "incoming",
				State:     "ringing",
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := freshIncomingCall(tt.call, now); got != tt.want {
				t.Fatalf("freshIncomingCall() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestForwardCallNotifiesIncomingRingingOnce(t *testing.T) {
	ctx := t.Context()

	var got []struct {
		Kind    notifyevent.Kind      `json:"kind"`
		Payload notifyevent.CallEvent `json:"payload"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		var payload struct {
			Kind    notifyevent.Kind      `json:"kind"`
			Payload notifyevent.CallEvent `json:"payload"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		got = append(got, payload)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "sigmo.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()

	current := settings.Default()
	current.Channels = map[string]settings.Channel{
		"http": {Endpoint: server.URL},
	}
	current.Modems = map[string]settings.Modem{
		"modem-1": {Alias: "Office SIM"},
	}
	relay, err := New(settings.NewMemoryStore(current), nil, db, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	call := storage.Call{
		ID:        "call-1",
		ModemID:   "modem-1",
		Direction: "incoming",
		Number:    "+12242255559",
		State:     "ringing",
		StartedAt: time.Now(),
	}
	if err := relay.ForwardCall(ctx, call); err != nil {
		t.Fatalf("ForwardCall() first error = %v", err)
	}
	if err := relay.ForwardCall(ctx, call); err != nil {
		t.Fatalf("ForwardCall() second error = %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("notifications = %d, want 1", len(got))
	}
	if got[0].Kind != notifyevent.KindCall {
		t.Fatalf("kind = %q, want %q", got[0].Kind, notifyevent.KindCall)
	}
	if got[0].Payload.From != "+12242255559" || got[0].Payload.Modem != "Office SIM" {
		t.Fatalf("payload = %+v, want caller and modem alias", got[0].Payload)
	}
}

func TestForwardCallKeepsDedupeWhenWebPushFails(t *testing.T) {
	ctx := t.Context()
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "sigmo.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()
	if _, err := db.UpsertPushSubscription(ctx, storage.PushSubscription{
		ID:       "broken",
		Endpoint: "https://push.example.test/subscription",
		P256DH:   "invalid",
		Auth:     "invalid",
		Label:    "Broken browser",
	}); err != nil {
		t.Fatalf("UpsertPushSubscription() error = %v", err)
	}
	pushClient, err := webpush.New(ctx, db)
	if err != nil {
		t.Fatalf("webpush.New() error = %v", err)
	}
	current := settings.Default()
	current.Channels = map[string]settings.Channel{"http": {Endpoint: server.URL}}
	relay, err := New(settings.NewMemoryStore(current), nil, db, pushClient)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	call := storage.Call{
		ID:        "call-with-broken-push",
		ModemID:   "modem-1",
		Direction: "incoming",
		State:     "ringing",
		StartedAt: time.Now(),
	}

	for range 2 {
		if err := relay.ForwardCall(ctx, call); err != nil {
			t.Fatalf("ForwardCall() error = %v", err)
		}
	}
	if requests != 1 {
		t.Fatalf("third-party notifications = %d, want 1", requests)
	}
}

func TestFreshIncomingMessage(t *testing.T) {
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		message storage.Message
		want    bool
	}{
		{
			name: "recent incoming",
			message: storage.Message{
				Timestamp: now.Add(-29 * time.Minute),
				Incoming:  true,
			},
			want: true,
		},
		{
			name: "old incoming",
			message: storage.Message{
				Timestamp: now.Add(-31 * time.Minute),
				Incoming:  true,
			},
		},
		{
			name: "future incoming",
			message: storage.Message{
				Timestamp: now.Add(31 * time.Minute),
				Incoming:  true,
			},
		},
		{
			name: "outgoing",
			message: storage.Message{
				Timestamp: now.Add(-time.Hour),
			},
			want: true,
		},
		{
			name: "unknown timestamp",
			message: storage.Message{
				Incoming: true,
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := freshIncomingMessage(tt.message, now); got != tt.want {
				t.Fatalf("freshIncomingMessage() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestModemSMSReady(t *testing.T) {
	tests := []struct {
		name  string
		modem *modem.Modem
		want  bool
	}{
		{name: "ready", modem: &modem.Modem{Status: wwanmodem.Status{SIM: wwanmodem.SIMStateReady}}, want: true},
		{name: "PIN locked", modem: &modem.Modem{Status: wwanmodem.Status{SIM: wwanmodem.SIMStateLocked}}},
		{name: "unknown", modem: new(modem.Modem)},
		{name: "missing modem"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := modemSMSReady(tt.modem); got != tt.want {
				t.Fatalf("modemSMSReady() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestForwardStoredModemSMSStoresCleansAndNotifies(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name              string
		source            string
		incoming          bool
		timestamp         time.Time
		refs              []modem.MessageRef
		preinsert         bool
		wantStored        int
		wantDeleteCalls   int
		wantNotifications int32
	}{
		{
			name:      "fresh multipart",
			incoming:  true,
			timestamp: now,
			refs: []modem.MessageRef{
				{Storage: wwanmodem.MessageStorageSIM, ID: 0},
				{Storage: wwanmodem.MessageStorageDevice, ID: 9},
			},
			wantStored:        1,
			wantDeleteCalls:   1,
			wantNotifications: 1,
		},
		{
			name:            "stale replay",
			incoming:        true,
			timestamp:       now.Add(-time.Hour),
			refs:            []modem.MessageRef{{Storage: wwanmodem.MessageStorageSIM, ID: 10}},
			wantStored:      1,
			wantDeleteCalls: 1,
		},
		{
			name:            "known replay",
			incoming:        true,
			timestamp:       now,
			refs:            []modem.MessageRef{{Storage: wwanmodem.MessageStorageDevice, ID: 11}},
			preinsert:       true,
			wantStored:      1,
			wantDeleteCalls: 1,
		},
		{
			name:              "flash message without refs",
			incoming:          true,
			timestamp:         now,
			wantStored:        1,
			wantNotifications: 1,
		},
		{
			name:      "outgoing message",
			timestamp: now,
			refs:      []modem.MessageRef{{Storage: wwanmodem.MessageStorageDevice, ID: 12}},
		},
		{
			name:      "routed IMS message",
			source:    storage.MessageSourceRouted,
			incoming:  true,
			timestamp: now,
			refs:      []modem.MessageRef{{Storage: wwanmodem.MessageStorageDevice, ID: 13}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			relay, store, notifications := newSMSRelay(t)
			source := tt.source
			if source == "" {
				source = storage.MessageSourceModem
			}
			storedRefs := make([]storage.ModemMessageRef, 0, len(tt.refs))
			for _, ref := range tt.refs {
				storedRefs = append(storedRefs, storage.ModemMessageRef{
					ModemID:    "modem-1",
					Generation: 7,
					Storage:    uint8(ref.Storage),
					ID:         ref.ID,
				})
			}
			stored := storage.Message{
				ModemID:     "modem-1",
				ProfileID:   "profile-a",
				Source:      source,
				ExternalKey: tt.name,
				Sender:      "+100",
				Recipient:   "+200",
				Text:        tt.name,
				Timestamp:   tt.timestamp,
				Status:      "received",
				Incoming:    tt.incoming,
				ModemRefs:   storedRefs,
			}
			if tt.preinsert {
				inserted, err := store.InsertMessage(ctx, stored)
				if err != nil {
					t.Fatalf("InsertMessage() error = %v", err)
				}
				if !inserted {
					t.Fatal("InsertMessage() = false, want true")
				}
			}

			refsClearedBeforeDelete := false
			deleter := &fakeModemSMSDeleter{
				beforeDelete: func() {
					messages, err := store.ListByParticipant(ctx, "profile-a", "+100")
					if err != nil {
						t.Fatalf("ListByParticipant() before delete error = %v", err)
					}
					refsClearedBeforeDelete = len(messages) == 1 && len(messages[0].ModemRefs) == 0
				},
			}
			err := relay.forwardStoredModemSMS(ctx, modemSMSReceipt{stored: stored, refs: tt.refs, deleter: deleter})
			if err != nil {
				t.Fatalf("forwardStoredModemSMS() error = %v", err)
			}
			if len(deleter.calls) != tt.wantDeleteCalls {
				t.Fatalf("modem delete calls = %d, want %d", len(deleter.calls), tt.wantDeleteCalls)
			}
			if tt.wantDeleteCalls == 1 {
				if !slices.Equal(deleter.calls[0], tt.refs) {
					t.Fatalf("deleted refs = %v, want %v", deleter.calls[0], tt.refs)
				}
				if !refsClearedBeforeDelete {
					t.Fatal("database refs were not cleared before modem deletion")
				}
			}
			messages, err := store.ListByParticipant(ctx, "profile-a", "+100")
			if err != nil {
				t.Fatalf("ListByParticipant() error = %v", err)
			}
			if len(messages) != tt.wantStored {
				t.Fatalf("stored messages = %d, want %d", len(messages), tt.wantStored)
			}
			if len(messages) == 1 && len(messages[0].ModemRefs) != 0 {
				t.Fatalf("stored modem refs = %v, want none", messages[0].ModemRefs)
			}
			if got := notifications.Load(); got != tt.wantNotifications {
				t.Fatalf("notifications = %d, want %d", got, tt.wantNotifications)
			}
		})
	}
}

func TestForwardStoredModemSMSRetriesDeleteWithoutDuplicateNotification(t *testing.T) {
	ctx := t.Context()
	relay, store, notifications := newSMSRelay(t)
	deleteErr := errors.New("modem storage busy")
	refs := []modem.MessageRef{{Storage: wwanmodem.MessageStorageDevice, ID: 21}}
	stored := storage.Message{
		ModemID:     "modem-1",
		ProfileID:   "profile-a",
		Source:      storage.MessageSourceModem,
		ExternalKey: "retry-delete",
		Sender:      "+100",
		Recipient:   "+200",
		Text:        "retry",
		Timestamp:   time.Now(),
		Status:      "received",
		Incoming:    true,
		ModemRefs: []storage.ModemMessageRef{
			{ModemID: "modem-1", Generation: 7, Storage: uint8(wwanmodem.MessageStorageDevice), ID: 21},
		},
	}
	deleter := &fakeModemSMSDeleter{errs: []error{deleteErr, nil}}
	receipt := modemSMSReceipt{stored: stored, refs: refs, deleter: deleter}

	err := relay.forwardStoredModemSMS(ctx, receipt)
	if !errors.Is(err, deleteErr) {
		t.Fatalf("forwardStoredModemSMS() first error = %v, want %v", err, deleteErr)
	}
	if got := notifications.Load(); got != 1 {
		t.Fatalf("notifications after failed delete = %d, want 1", got)
	}
	messages, err := store.ListByParticipant(ctx, "profile-a", "+100")
	if err != nil {
		t.Fatalf("ListByParticipant() error = %v", err)
	}
	if len(messages) != 1 || len(messages[0].ModemRefs) != 0 {
		t.Fatalf("stored message after failed delete = %+v, want persisted without modem refs", messages)
	}

	if err := relay.forwardStoredModemSMS(ctx, receipt); err != nil {
		t.Fatalf("forwardStoredModemSMS() retry error = %v", err)
	}
	if len(deleter.calls) != 2 {
		t.Fatalf("modem delete calls = %d, want 2", len(deleter.calls))
	}
	if got := notifications.Load(); got != 1 {
		t.Fatalf("notifications after retry = %d, want 1", got)
	}
}

func TestForwardStoredModemSMSDoesNotDeleteBeforeInsert(t *testing.T) {
	relay, _, notifications := newSMSRelay(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	deleter := new(fakeModemSMSDeleter)
	err := relay.forwardStoredModemSMS(ctx, modemSMSReceipt{
		stored: storage.Message{
			ModemID:     "modem-1",
			ProfileID:   "profile-a",
			Source:      storage.MessageSourceModem,
			ExternalKey: "insert-canceled",
			Sender:      "+100",
			Recipient:   "+200",
			Text:        "keep on modem",
			Timestamp:   time.Now(),
			Status:      "received",
			Incoming:    true,
			ModemRefs: []storage.ModemMessageRef{
				{ModemID: "modem-1", Generation: 7, Storage: uint8(wwanmodem.MessageStorageSIM), ID: 30},
			},
		},
		refs:    []modem.MessageRef{{Storage: wwanmodem.MessageStorageSIM, ID: 30}},
		deleter: deleter,
	})
	if err == nil {
		t.Fatal("forwardStoredModemSMS() error = nil, want insert error")
	}
	if len(deleter.calls) != 0 {
		t.Fatalf("modem delete calls = %d, want 0", len(deleter.calls))
	}
	if got := notifications.Load(); got != 0 {
		t.Fatalf("notifications = %d, want 0", got)
	}
}

func TestRemoveModemDoesNotReleaseNewGenerationOwnership(t *testing.T) {
	const (
		path      = "/sys/devices/modem-1"
		equipment = "imei-1"
	)
	newCanceled := false
	relay := &Relay{
		subscriptions: map[string]relaySubscription{
			path: {generation: 2, cancel: func() { newCanceled = true }},
		},
		equipment: map[string]string{equipment: path},
		modems:    map[string]string{path: equipment},
	}

	relay.removeModem(path, 1)

	if newCanceled {
		t.Fatal("stale generation canceled the replacement subscription")
	}
	if got := relay.subscriptions[path].generation; got != 2 {
		t.Fatalf("subscription generation = %d, want 2", got)
	}
	if relay.equipment[equipment] != path || relay.modems[path] != equipment {
		t.Fatalf("replacement ownership was removed: equipment=%v modems=%v", relay.equipment, relay.modems)
	}

	relay.removeModem(path, 2)
	if !newCanceled {
		t.Fatal("matching generation did not cancel the subscription")
	}
	if len(relay.subscriptions) != 0 || len(relay.equipment) != 0 || len(relay.modems) != 0 {
		t.Fatalf("matching generation left ownership behind: subscriptions=%v equipment=%v modems=%v", relay.subscriptions, relay.equipment, relay.modems)
	}
}

func TestChangedModemRemovesPreviousPathWithoutEquipmentID(t *testing.T) {
	const previousPath = "/sys/devices/modem-old"
	canceled := false
	relay := &Relay{
		subscriptions: map[string]relaySubscription{
			previousPath: {cancel: func() { canceled = true }},
		},
		equipment: make(map[string]string),
		modems:    make(map[string]string),
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := relay.handleModemEvent(ctx, modem.ModemEvent{
		Type:         modem.ModemEventChanged,
		Modem:        new(modem.Modem),
		Previous:     new(modem.Modem),
		Path:         "/sys/devices/modem-new",
		PreviousPath: previousPath,
	})
	if err != nil {
		t.Fatalf("handleModemEvent() error = %v", err)
	}
	if !canceled {
		t.Fatal("path change did not cancel the previous subscription")
	}
	if _, ok := relay.subscriptions[previousPath]; ok {
		t.Fatal("path change left the previous subscription registered")
	}
}

func newSMSRelay(t *testing.T) (*Relay, *storage.Store, *atomic.Int32) {
	t.Helper()
	notifications := new(atomic.Int32)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		notifications.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	store, err := storage.Open(t.Context(), filepath.Join(t.TempDir(), "sigmo.db"))
	if err != nil {
		t.Fatalf("storage.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("Store.Close() error = %v", err)
		}
	})
	current := settings.Default()
	current.Channels = map[string]settings.Channel{"http": {Endpoint: server.URL}}
	relay, err := New(settings.NewMemoryStore(current), nil, store, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return relay, store, notifications
}

type fakeModemSMSDeleter struct {
	calls        [][]modem.MessageRef
	errs         []error
	beforeDelete func()
}

func (f *fakeModemSMSDeleter) Delete(_ context.Context, refs []modem.MessageRef) error {
	if f.beforeDelete != nil {
		f.beforeDelete()
	}
	f.calls = append(f.calls, slices.Clone(refs))
	index := len(f.calls) - 1
	if index < len(f.errs) {
		return f.errs[index]
	}
	return nil
}
