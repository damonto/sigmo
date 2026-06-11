package message

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/storage"
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
			ctx := context.Background()
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
	ctx := context.Background()
	store := testStore(t)
	device := &fakeModemDevice{profile: "profile-a"}
	service := New(store, &fakeRoute{})
	messages := []storage.Message{
		{
			ProfileID:   "profile-a",
			Source:      storage.MessageSourceModem,
			ExternalKey: "/org/freedesktop/ModemManager1/SMS/1",
			Sender:      "777",
			Recipient:   "+12025550199",
			Text:        "balance",
			Timestamp:   time.Date(2026, 5, 29, 11, 0, 0, 0, time.UTC),
			Incoming:    true,
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
	if len(device.deleted) != 1 || device.deleted[0] != dbus.ObjectPath("/org/freedesktop/ModemManager1/SMS/1") {
		t.Fatalf("deleted paths = %v, want modem SMS only", device.deleted)
	}
}

func TestListConversationsPassesSearchQueryToStorage(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	device := &fakeModemDevice{profile: "profile-a"}
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

	got, err := service.listConversations(ctx, device, "balance")
	if err != nil {
		t.Fatalf("listConversations() error = %v", err)
	}
	if len(got) != 1 || got[0].Recipient != "777" {
		t.Fatalf("listConversations() = %+v, want only 777 balance conversation", got)
	}
}

type fakeModemDevice struct {
	id        string
	profile   string
	number    string
	sendErr   error
	sendCalls int
	deleted   []dbus.ObjectPath
}

func (f *fakeModemDevice) modem() *mmodem.Modem { return nil }

func (f *fakeModemDevice) profileID(context.Context) (string, error) {
	return f.profile, nil
}

func (f *fakeModemDevice) sendSMS(context.Context, string, string) (*mmodem.SMS, error) {
	f.sendCalls++
	return nil, f.sendErr
}

func (f *fakeModemDevice) listSMS(context.Context) ([]*mmodem.SMS, error) {
	return nil, nil
}

func (f *fakeModemDevice) deleteSMS(_ context.Context, path dbus.ObjectPath) error {
	f.deleted = append(f.deleted, path)
	return nil
}

func (f *fakeModemDevice) modemID() string { return f.id }

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
	store, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "sigmo.db"))
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
