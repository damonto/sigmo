package message

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/storage"
	wwanmodem "github.com/damonto/wwan-go/modem"
)

func TestSendRoutesMessages(t *testing.T) {
	tests := []struct {
		name          string
		status        RouteStatus
		statusErr     error
		sendErr       error
		routeSendErr  error
		wantTo        string
		wantErr       string
		wantErrIs     error
		wantRouteSend int
		wantModemSend int
	}{
		{
			name:          "preferred route sends without modem",
			status:        RouteStatus{Preferred: true, Connected: true},
			wantTo:        "777",
			wantRouteSend: 1,
		},
		{
			name:          "connected route fallback after modem send fails",
			status:        RouteStatus{Connected: true},
			sendErr:       errors.New("modem rejected message"),
			wantTo:        "777",
			wantRouteSend: 1,
			wantModemSend: 1,
		},
		{
			name:      "route status error stops send",
			statusErr: errors.New("settings unavailable"),
			wantErr:   "read message route status: settings unavailable",
		},
		{
			name:          "preferred route disconnected",
			status:        RouteStatus{Preferred: true, Connected: true},
			routeSendErr:  ErrRouteNotConnected,
			wantErr:       "send SMS to 777 over selected route: message route is not connected",
			wantErrIs:     ErrRouteNotConnected,
			wantRouteSend: 1,
		},
		{
			name:          "modem error is returned when route is disconnected",
			sendErr:       errors.New("modem rejected message"),
			wantErr:       "send SMS to 777: modem rejected message",
			wantModemSend: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			store := testStore(t)
			route := &fakeRoute{
				status:    tt.status,
				statusErr: tt.statusErr,
				message: storage.Message{
					ModemID:     "modem-1",
					ProfileID:   "profile-a",
					Source:      storage.MessageSourceRouted,
					ExternalKey: "wifi-message-" + tt.name,
					Sender:      "+12025550199",
					Recipient:   "777",
					Text:        "BAL",
					Timestamp:   time.Date(2026, 5, 29, 10, 0, 0, 0, time.UTC),
					Status:      "sent",
					Routed:      true,
				},
				sendErr: tt.routeSendErr,
			}
			device := &fakeModemDevice{
				id:      "modem-1",
				profile: "profile-a",
				number:  "+12025550199",
				sendErr: tt.sendErr,
			}
			service := New(store, route)

			got, err := service.send(ctx, device, "777", "BAL")
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("send() error = %v, want %q", err, tt.wantErr)
				}
				if tt.wantErrIs != nil && !errors.Is(err, tt.wantErrIs) {
					t.Fatalf("send() error = %v, want %v", err, tt.wantErrIs)
				}
			} else if err != nil {
				t.Fatalf("send() error = %v", err)
			}
			pref, prefErr := store.PreferredCommunication(ctx, "777")
			if prefErr != nil {
				t.Fatal(prefErr)
			}
			wantProfile := ""
			if err == nil {
				wantProfile = "profile-a"
			}
			if pref.ProfileID != wantProfile {
				t.Fatalf("successful send preference = %q, want %q", pref.ProfileID, wantProfile)
			}
			if got != tt.wantTo {
				t.Fatalf("send() = %q, want %q", got, tt.wantTo)
			}
			if route.sendSMSCalls != tt.wantRouteSend {
				t.Fatalf("route sends = %d, want %d", route.sendSMSCalls, tt.wantRouteSend)
			}
			wantStatusApplies := tt.wantRouteSend
			if tt.wantErr != "" {
				wantStatusApplies = 0
			}
			if route.applySMSStatusCalls != wantStatusApplies {
				t.Fatalf("route status applies = %d, want %d", route.applySMSStatusCalls, wantStatusApplies)
			}
			if device.sendCalls != tt.wantModemSend {
				t.Fatalf("modem sends = %d, want %d", device.sendCalls, tt.wantModemSend)
			}
		})
	}
}

func TestDeleteByParticipantDeletesOnlyModemMessagesFromBackend(t *testing.T) {
	ctx := t.Context()
	store := testStore(t)
	device := &fakeModemDevice{profile: "profile-a"}
	service := New(store, &fakeRoute{})
	messages := []storage.Message{
		{
			ModemID:     "modem-1",
			ProfileID:   "profile-a",
			Source:      storage.MessageSourceModem,
			ExternalKey: "modem-1:1/1,1/2,",
			Sender:      "777",
			Recipient:   "+12025550199",
			Text:        "balance",
			Timestamp:   time.Date(2026, 5, 29, 11, 0, 0, 0, time.UTC),
			Incoming:    true,
			ModemRefs: []storage.ModemMessageRef{
				{ModemID: "modem-1", Storage: uint8(wwanmodem.MessageStorageSIM), ID: 1},
				{ModemID: "modem-1", Storage: uint8(wwanmodem.MessageStorageSIM), ID: 2},
			},
		},
		{
			ProfileID:   "profile-a",
			Source:      storage.MessageSourceRouted,
			ExternalKey: "wifi-message-1",
			Sender:      "+12025550199",
			Recipient:   "777",
			Text:        "BAL",
			Timestamp:   time.Date(2026, 5, 29, 11, 1, 0, 0, time.UTC),
			Routed:      true,
		},
	}
	for _, msg := range messages {
		if _, err := store.InsertMessage(ctx, msg); err != nil {
			t.Fatalf("InsertMessage() error = %v", err)
		}
	}

	if err := service.deleteByParticipant(ctx, device, "777"); err != nil {
		t.Fatalf("deleteByParticipant() error = %v", err)
	}
	wantRefs := []mmodem.MessageRef{
		{Storage: wwanmodem.MessageStorageSIM, ID: 1},
		{Storage: wwanmodem.MessageStorageSIM, ID: 2},
	}
	if len(device.deleted) != 1 || !slices.Equal(device.deleted[0], wantRefs) {
		t.Fatalf("deleted refs = %v, want %v", device.deleted, wantRefs)
	}
}

func TestDeleteByParticipantSkipsStaleModemGenerationRefs(t *testing.T) {
	ctx := t.Context()
	store := testStore(t)
	device := &fakeModemDevice{profile: "profile-a", generationValue: 2}
	service := New(store, &fakeRoute{})
	base := time.Date(2026, 7, 31, 11, 0, 0, 0, time.UTC)
	for _, msg := range []storage.Message{
		{
			ModemID:     "modem-1",
			ProfileID:   "profile-a",
			Source:      storage.MessageSourceModem,
			ExternalKey: "g1:1",
			Sender:      "777",
			Recipient:   "+12025550199",
			Text:        "old",
			Timestamp:   base,
			Incoming:    true,
			ModemRefs: []storage.ModemMessageRef{
				{ModemID: "modem-1", Generation: 1, Storage: uint8(wwanmodem.MessageStorageSIM), ID: 1},
			},
		},
		{
			ModemID:     "modem-1",
			ProfileID:   "profile-a",
			Source:      storage.MessageSourceModem,
			ExternalKey: "g2:2",
			Sender:      "777",
			Recipient:   "+12025550199",
			Text:        "new",
			Timestamp:   base.Add(time.Second),
			Incoming:    true,
			ModemRefs: []storage.ModemMessageRef{
				{ModemID: "modem-1", Generation: 2, Storage: uint8(wwanmodem.MessageStorageSIM), ID: 2},
			},
		},
	} {
		if id, err := store.InsertMessage(ctx, msg); err != nil || id == 0 {
			t.Fatalf("InsertMessage() = %v, %v", id, err)
		}
	}

	if err := service.deleteByParticipant(ctx, device, "777"); err != nil {
		t.Fatalf("deleteByParticipant() error = %v", err)
	}
	want := []mmodem.MessageRef{{Storage: wwanmodem.MessageStorageSIM, ID: 2}}
	if len(device.deleted) != 1 || !slices.Equal(device.deleted[0], want) {
		t.Fatalf("deleted refs = %v, want current generation %v", device.deleted, want)
	}
}

func TestStorageModemRefsPreservesMultipartReferences(t *testing.T) {
	refs := []mmodem.MessageRef{
		{Storage: wwanmodem.MessageStorageSIM, ID: 9},
		{Storage: wwanmodem.MessageStorageDevice, ID: 3},
	}
	want := []storage.ModemMessageRef{
		{ModemID: "modem-1", Generation: 7, Storage: uint8(wwanmodem.MessageStorageSIM), ID: 9},
		{ModemID: "modem-1", Generation: 7, Storage: uint8(wwanmodem.MessageStorageDevice), ID: 3},
	}
	if got := StorageModemRefs("modem-1", 7, refs); !slices.Equal(got, want) {
		t.Fatalf("StorageModemRefs() = %v, want %v", got, want)
	}
}

func TestListConversationsPassesSearchQueryToStorage(t *testing.T) {
	ctx := t.Context()
	store := testStore(t)
	device := &mmodem.Modem{SIM: &mmodem.SIM{Identifier: "profile-a"}}
	service := New(store, &fakeRoute{})
	messages := []storage.Message{
		{
			ProfileID:   "profile-a",
			Source:      storage.MessageSourceRouted,
			ExternalKey: "wifi-message-1",
			Sender:      "+12025550199",
			Recipient:   "777",
			Text:        "balance",
			Timestamp:   time.Date(2026, 5, 29, 11, 0, 0, 0, time.UTC),
			Routed:      true,
		},
		{
			ProfileID:   "profile-a",
			Source:      storage.MessageSourceRouted,
			ExternalKey: "wifi-message-2",
			Sender:      "+12025550199",
			Recipient:   "888",
			Text:        "promo",
			Timestamp:   time.Date(2026, 5, 29, 11, 1, 0, 0, time.UTC),
			Routed:      true,
		},
	}
	for _, msg := range messages {
		if _, err := store.InsertMessage(ctx, msg); err != nil {
			t.Fatalf("InsertMessage() error = %v", err)
		}
	}

	got, err := service.ListConversations(ctx, device, "balance")
	if err != nil {
		t.Fatalf("ListConversations() error = %v", err)
	}
	if len(got) != 1 || got[0].Recipient != "777" {
		t.Fatalf("ListConversations() = %+v, want only 777 balance conversation", got)
	}
}

func TestHistoryReadsUseStoredMessages(t *testing.T) {
	ctx := t.Context()
	store := testStore(t)
	service := New(store, nil)
	// Cached SIM identity is sufficient for reading history without a modem transport.
	device := &mmodem.Modem{EquipmentIdentifier: "modem-1", SIM: &mmodem.SIM{Identifier: "profile-a"}}
	msg := storage.Message{
		ModemID: "modem-1", ProfileID: "profile-a", Source: storage.MessageSourceModem,
		Sender: "777", Recipient: "+12025550199", Text: "balance", Incoming: true,
		Timestamp: time.Now(), Status: "received",
	}
	id, err := store.InsertMessage(ctx, msg)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateConversationRead(ctx, storage.ReadUpdate{
		ConversationID: storage.ConversationID(msg.ProfileID, msg.Sender), ReadThroughMessageID: &id,
	}); err != nil {
		t.Fatal(err)
	}
	before, err := store.MessagesState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		conversations, err := service.ListConversations(ctx, device, "balance")
		if err != nil || len(conversations) != 1 || conversations[0].ID != id {
			t.Fatalf("ListConversations() = %+v, %v", conversations, err)
		}
		messages, err := service.ListByParticipant(ctx, device, msg.Sender)
		if err != nil || len(messages) != 1 || messages[0].ID != id {
			t.Fatalf("ListByParticipant() = %+v, %v", messages, err)
		}
	}
	after, err := store.MessagesState(ctx)
	if err != nil || after != before {
		t.Fatalf("history reads changed message state: %+v, %v; want %+v", after, err, before)
	}
	if err := service.DeleteByParticipant(ctx, device, msg.Sender); err != nil {
		t.Fatal(err)
	}
	messages, err := service.ListByParticipant(ctx, device, msg.Sender)
	if err != nil || len(messages) != 0 {
		t.Fatalf("history after deletion = %+v, %v", messages, err)
	}
}

type fakeModemDevice struct {
	id              string
	profile         string
	number          string
	generationValue uint64
	sendErr         error
	sendCalls       int
	deleted         [][]mmodem.MessageRef
}

func (f *fakeModemDevice) modem() *mmodem.Modem { return nil }

func (f *fakeModemDevice) profileID(context.Context) (string, error) {
	return f.profile, nil
}

func (f *fakeModemDevice) sendSMS(context.Context, string, string) (*mmodem.SMS, error) {
	f.sendCalls++
	return nil, f.sendErr
}

func (f *fakeModemDevice) deleteSMS(_ context.Context, refs []mmodem.MessageRef) error {
	f.deleted = append(f.deleted, slices.Clone(refs))
	return nil
}

func (f *fakeModemDevice) modemID() string { return f.id }

func (f *fakeModemDevice) generation() uint64 { return f.generationValue }

func (f *fakeModemDevice) phoneNumber() string { return f.number }

type fakeRoute struct {
	status              RouteStatus
	statusErr           error
	message             storage.Message
	sendErr             error
	sendSMSCalls        int
	applySMSStatusCalls int
}

func (f fakeRoute) Status(context.Context, *mmodem.Modem) (RouteStatus, error) {
	return f.status, f.statusErr
}

func (f *fakeRoute) SendSMS(context.Context, *mmodem.Modem, string, string) (storage.Message, error) {
	f.sendSMSCalls++
	return f.message, f.sendErr
}

func (f *fakeRoute) ApplyPendingSMSStatus(context.Context, storage.Message) error {
	f.applySMSStatusCalls++
	return nil
}

func testStore(t *testing.T) *storage.Store {
	t.Helper()
	store, err := storage.Open(t.Context(), filepath.Join(t.TempDir(), "sigmo.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})
	return store
}
