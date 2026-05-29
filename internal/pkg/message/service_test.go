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
	"github.com/damonto/sigmo/internal/pkg/websheet"
	"github.com/damonto/sigmo/internal/pkg/wificalling"
)

func TestSendRoutesMessages(t *testing.T) {
	tests := []struct {
		name          string
		status        wificalling.Status
		statusErr     error
		sendErr       error
		wantTo        string
		wantErr       string
		wantWiFiSends int
		wantModemSend int
	}{
		{
			name:          "preferred wifi calling sends without modem",
			status:        wificalling.Status{Settings: wificalling.Settings{Preferred: true}, Connected: true},
			wantTo:        "777",
			wantWiFiSends: 1,
		},
		{
			name:          "connected wifi calling fallback after modem send fails",
			status:        wificalling.Status{Connected: true},
			sendErr:       errors.New("modem rejected message"),
			wantTo:        "777",
			wantWiFiSends: 1,
			wantModemSend: 1,
		},
		{
			name:      "wifi calling status error stops send",
			statusErr: errors.New("settings unavailable"),
			wantErr:   "read wifi calling status: settings unavailable",
		},
		{
			name:          "modem error is returned when wifi calling is disconnected",
			sendErr:       errors.New("modem rejected message"),
			wantErr:       "send SMS to 777: modem rejected message",
			wantModemSend: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			store := testStore(t)
			wifiCalling := &fakeWiFiCalling{
				status:    tt.status,
				statusErr: tt.statusErr,
				message: storage.Message{
					ModemID:     "modem-1",
					ProfileID:   "profile-a",
					Source:      storage.MessageSourceWiFiCalling,
					ExternalKey: "wifi-message-" + tt.name,
					Sender:      "+12025550199",
					Recipient:   "777",
					Text:        "BAL",
					Timestamp:   time.Date(2026, 5, 29, 10, 0, 0, 0, time.UTC),
					Status:      "sent",
					WiFiCalling: true,
				},
			}
			device := &fakeModemDevice{
				id:      "modem-1",
				profile: "profile-a",
				number:  "+12025550199",
				sendErr: tt.sendErr,
			}
			service := New(store, wifiCalling)

			got, err := service.send(ctx, device, "777", "BAL")
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("send() error = %v, want %q", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("send() error = %v", err)
			}
			if got != tt.wantTo {
				t.Fatalf("send() = %q, want %q", got, tt.wantTo)
			}
			if wifiCalling.sendSMSCalls != tt.wantWiFiSends {
				t.Fatalf("Wi-Fi Calling sends = %d, want %d", wifiCalling.sendSMSCalls, tt.wantWiFiSends)
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
	service := New(store, &fakeWiFiCalling{})
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
			Source:      storage.MessageSourceWiFiCalling,
			ExternalKey: "wifi-message-1",
			Sender:      "+12025550199",
			Recipient:   "777",
			Text:        "BAL",
			Timestamp:   time.Date(2026, 5, 29, 11, 1, 0, 0, time.UTC),
			WiFiCalling: true,
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
	service := New(store, &fakeWiFiCalling{})
	messages := []storage.Message{
		{
			ProfileID:   "profile-a",
			Source:      storage.MessageSourceWiFiCalling,
			ExternalKey: "wifi-message-1",
			Sender:      "+12025550199",
			Recipient:   "777",
			Text:        "balance",
			Timestamp:   time.Date(2026, 5, 29, 11, 0, 0, 0, time.UTC),
			WiFiCalling: true,
		},
		{
			ProfileID:   "profile-a",
			Source:      storage.MessageSourceWiFiCalling,
			ExternalKey: "wifi-message-2",
			Sender:      "+12025550199",
			Recipient:   "888",
			Text:        "promo",
			Timestamp:   time.Date(2026, 5, 29, 11, 1, 0, 0, time.UTC),
			WiFiCalling: true,
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

type fakeWiFiCalling struct {
	status       wificalling.Status
	statusErr    error
	message      storage.Message
	sendSMSCalls int
}

func (fakeWiFiCalling) Run(context.Context, *mmodem.Registry) error { return nil }

func (fakeWiFiCalling) Settings(context.Context, *mmodem.Modem) (wificalling.Settings, error) {
	return wificalling.Settings{}, nil
}

func (fakeWiFiCalling) UpdateSettings(context.Context, *mmodem.Modem, wificalling.Settings) error {
	return nil
}

func (f fakeWiFiCalling) Status(context.Context, *mmodem.Modem) (wificalling.Status, error) {
	return f.status, f.statusErr
}

func (fakeWiFiCalling) StartWebsheet(context.Context, *mmodem.Modem) (websheet.Info, error) {
	return websheet.Info{}, nil
}

func (f *fakeWiFiCalling) SendSMS(context.Context, *mmodem.Modem, string, string) (storage.Message, error) {
	f.sendSMSCalls++
	return f.message, nil
}

func (fakeWiFiCalling) ExecuteUSSD(context.Context, *mmodem.Modem, string, string) (string, error) {
	return "", nil
}

func (fakeWiFiCalling) DialCall(context.Context, *mmodem.Modem, string) (wificalling.VoiceCall, error) {
	return wificalling.VoiceCall{}, nil
}

func (fakeWiFiCalling) AnswerCall(context.Context, *mmodem.Modem, string) (wificalling.VoiceCall, error) {
	return wificalling.VoiceCall{}, nil
}

func (fakeWiFiCalling) RejectCall(context.Context, *mmodem.Modem, string) (wificalling.VoiceCall, error) {
	return wificalling.VoiceCall{}, nil
}

func (fakeWiFiCalling) HangupCall(context.Context, *mmodem.Modem, string) (wificalling.VoiceCall, error) {
	return wificalling.VoiceCall{}, nil
}

func (fakeWiFiCalling) OpenCallMedia(context.Context, *mmodem.Modem, string) (wificalling.MediaSession, error) {
	return nil, nil
}

func (fakeWiFiCalling) SubscribeVoiceEvents(wificalling.VoiceEventFunc) func() {
	return func() {}
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
