package webpush

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	notifyevent "github.com/damonto/sigmo/internal/pkg/notify/event"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

func TestClientPersistsVAPIDKeyAndEnabledState(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "sigmo.db"))
	if err != nil {
		t.Fatalf("storage.Open() error = %v", err)
	}
	defer store.Close()

	first, err := New(ctx, store)
	if err != nil {
		t.Fatalf("New() first error = %v", err)
	}
	firstOverview, err := first.Overview(ctx)
	if err != nil {
		t.Fatalf("Overview() first error = %v", err)
	}
	if !firstOverview.Enabled || firstOverview.PublicKey == "" {
		t.Fatalf("first overview = %+v, want enabled key", firstOverview)
	}
	if err := first.SetEnabled(ctx, false); err != nil {
		t.Fatalf("SetEnabled() error = %v", err)
	}
	second, err := New(ctx, store)
	if err != nil {
		t.Fatalf("New() second error = %v", err)
	}
	secondOverview, err := second.Overview(ctx)
	if err != nil {
		t.Fatalf("Overview() second error = %v", err)
	}
	if secondOverview.Enabled {
		t.Fatal("enabled = true after persisted disable")
	}
	if secondOverview.PublicKey != firstOverview.PublicKey {
		t.Fatal("VAPID public key changed after reopening client")
	}
}

func TestClientRemovesExpiredSubscriptions(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "sigmo.db"))
	if err != nil {
		t.Fatalf("storage.Open() error = %v", err)
	}
	defer store.Close()
	client, err := New(ctx, store)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	endpoint, p256dh, auth := testSubscription(t)
	if _, err := store.UpsertPushSubscription(ctx, storage.PushSubscription{
		ID:       "expired",
		Endpoint: endpoint,
		P256DH:   p256dh,
		Auth:     auth,
		Label:    "Test browser",
	}); err != nil {
		t.Fatalf("UpsertPushSubscription() error = %v", err)
	}
	client.setHTTPClient(fakeHTTPClient{status: http.StatusGone})
	event := notifyevent.SMSEvent{
		ID:       "sms-1",
		ModemID:  "modem-1",
		Modem:    "Office",
		From:     "10086",
		Text:     "hello",
		Incoming: true,
	}
	if err := client.Send(ctx, event); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	items, err := store.ListPushSubscriptions(ctx)
	if err != nil {
		t.Fatalf("ListPushSubscriptions() error = %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("subscriptions = %d, want 0 after HTTP 410", len(items))
	}
}

func TestClientSendsOnlySupportedEvents(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "sigmo.db"))
	if err != nil {
		t.Fatalf("storage.Open() error = %v", err)
	}
	defer store.Close()
	webPush, err := New(ctx, store)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	endpoint, p256dh, auth := testSubscription(t)
	if _, err := store.UpsertPushSubscription(ctx, storage.PushSubscription{
		ID:       "subscription-1",
		Endpoint: endpoint,
		P256DH:   p256dh,
		Auth:     auth,
		Label:    "Test browser",
	}); err != nil {
		t.Fatalf("UpsertPushSubscription() error = %v", err)
	}
	client := &countingHTTPClient{}
	webPush.setHTTPClient(client)

	tests := []struct {
		name  string
		event notifyevent.Event
		want  int
	}{
		{name: "incoming sms", event: notifyevent.SMSEvent{ID: "sms", ModemID: "modem", Incoming: true}, want: 1},
		{name: "outgoing sms", event: notifyevent.SMSEvent{ID: "sms", ModemID: "modem"}},
		{name: "incoming ringing call", event: notifyevent.CallEvent{ID: "call", ModemID: "modem", Incoming: true, State: "ringing"}, want: 1},
		{name: "reminder", event: notifyevent.ReminderEvent{ProfileType: "esim", ProfileID: "iccid", ModemID: "modem", ScheduledAt: timeNow()}, want: 1},
		{name: "otp", event: notifyevent.OTPEvent{Code: "123456"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client.calls = 0
			if err := webPush.Send(ctx, tt.event); err != nil {
				t.Fatalf("Send() error = %v", err)
			}
			if client.calls != tt.want {
				t.Fatalf("HTTP calls = %d, want %d", client.calls, tt.want)
			}
		})
	}
}

func TestClientDeliveryIsolation(t *testing.T) {
	tests := []struct {
		name      string
		enabled   bool
		endpoints []string
		failures  map[string]error
		wantCalls int
		wantErr   bool
	}{
		{
			name:      "disabled client sends nothing",
			endpoints: []string{"https://push.example.test/device"},
		},
		{
			name:      "one device failure does not stop another device",
			enabled:   true,
			endpoints: []string{"https://push.example.test/broken", "https://push.example.test/healthy"},
			failures:  map[string]error{"https://push.example.test/broken": errors.New("endpoint unavailable")},
			wantCalls: 2,
			wantErr:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "sigmo.db"))
			if err != nil {
				t.Fatalf("storage.Open() error = %v", err)
			}
			defer store.Close()
			client, err := New(ctx, store)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			if !tt.enabled {
				if err := client.SetEnabled(ctx, false); err != nil {
					t.Fatalf("SetEnabled() error = %v", err)
				}
			}
			_, p256dh, auth := testSubscription(t)
			for index, endpoint := range tt.endpoints {
				if _, err := store.UpsertPushSubscription(ctx, storage.PushSubscription{
					ID: endpoint, Endpoint: endpoint, P256DH: p256dh, Auth: auth, Label: string(rune('A' + index)),
				}); err != nil {
					t.Fatalf("UpsertPushSubscription() error = %v", err)
				}
			}
			httpClient := &endpointHTTPClient{failures: tt.failures}
			client.setHTTPClient(httpClient)
			err = client.Send(ctx, notifyevent.SMSEvent{ID: "sms", ModemID: "modem", Incoming: true})
			if (err != nil) != tt.wantErr {
				t.Fatalf("Send() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got := httpClient.callCount(); got != tt.wantCalls {
				t.Fatalf("HTTP calls = %d, want %d", got, tt.wantCalls)
			}
		})
	}
}

func testSubscription(t *testing.T) (string, string, string) {
	t.Helper()
	privateKey, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	return "https://push.example.test/send/subscription", base64.RawURLEncoding.EncodeToString(privateKey.PublicKey().Bytes()), base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("a", authSecretSize)))
}

type fakeHTTPClient struct {
	status int
}

func (f fakeHTTPClient) Do(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: f.status, Body: io.NopCloser(strings.NewReader("expired"))}, nil
}

type countingHTTPClient struct {
	calls int
}

type endpointHTTPClient struct {
	mu       sync.Mutex
	calls    int
	failures map[string]error
}

func (c *endpointHTTPClient) Do(req *http.Request) (*http.Response, error) {
	c.mu.Lock()
	c.calls++
	err := c.failures[req.URL.String()]
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: http.StatusCreated, Body: io.NopCloser(strings.NewReader(""))}, nil
}

func (c *endpointHTTPClient) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func (c *countingHTTPClient) Do(*http.Request) (*http.Response, error) {
	c.calls++
	return &http.Response{StatusCode: http.StatusCreated, Body: io.NopCloser(strings.NewReader(""))}, nil
}

func timeNow() (value time.Time) {
	return time.Unix(1_700_000_000, 0)
}
