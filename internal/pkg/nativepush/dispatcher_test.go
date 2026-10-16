package nativepush

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/damonto/sigmo/internal/pkg/apierror"
	"github.com/damonto/sigmo/internal/pkg/settings"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

type pushTransport func(*http.Request) (*http.Response, error)

func (f pushTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func pushStore(t *testing.T) *storage.Store {
	t.Helper()
	s, err := storage.Open(t.Context(), filepath.Join(t.TempDir(), "push.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	return s
}

func TestDispatchEncryptionRevocationAndRetry(t *testing.T) {
	for _, tc := range []struct {
		name        string
		status      int
		code        string
		requireAuth bool
		wantRetry   bool
		wantRevoked bool
	}{
		{"encrypted delivery", 200, "", false, false, false},
		{"temporary rejection", 503, "push_provider_unavailable", false, true, false},
		{"permanent rejection", 422, "invalid_push_request", false, false, false},
		{"wrong method", 405, "method_not_allowed", false, false, false},
		{"provider rejected", 502, "push_provider_rejected", false, false, false},
		{"revoked grant", 401, "invalid_push_credentials", false, false, true},
		{"unregistered token", 410, "push_token_unregistered", false, false, true},
		{"unknown 401", 401, "", false, true, false},
		{"unknown 403", 403, "", false, true, false},
		{"wrong revoke status", 503, "invalid_push_credentials", false, true, false},
		{"expired backend access", 200, "", true, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := pushStore(t)
			sub := storage.NativeSubscription{ID: "subscription", InstallationID: "installation", BackendName: "Home", SendToken: strings.Repeat("s", 32), EncryptionKey: make([]byte, 32), SMS: true, Enabled: true}
			if err := s.UpsertNativeSubscription(t.Context(), sub); err != nil {
				t.Fatal(err)
			}
			msg := storage.Message{ModemID: "modem", ProfileID: "SIM", Source: storage.MessageSourceModem, ExternalKey: "first", Sender: "10086", Text: "private preview", Incoming: true, Timestamp: time.Now()}
			if _, err := s.InsertMessage(t.Context(), msg); err != nil {
				t.Fatal(err)
			}
			// Resolve the source SIM's latest label even after another SIM uses its modem.
			for _, profile := range []storage.SIMProfile{
				{ProfileID: "SIM", ModemID: "modem", Label: "Work"},
				{ProfileID: "other-SIM", ModemID: "modem", Label: "Travel"},
			} {
				if err := s.RecordSIMProfile(t.Context(), profile); err != nil {
					t.Fatal(err)
				}
			}
			var mu sync.Mutex
			var requests []deliveryRequest
			client := &http.Client{Transport: pushTransport(func(req *http.Request) (*http.Response, error) {
				if req.Header.Get("Authorization") != "Bearer "+sub.SendToken {
					t.Error("wrong grant")
				}
				var body deliveryRequest
				if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				mu.Lock()
				requests = append(requests, body)
				mu.Unlock()
				return problemResponse(t, tc.status, tc.code), nil
			})}
			cfg := settings.Default()
			cfg.Modems["modem"] = settings.Modem{Alias: "Mobile"}
			if tc.requireAuth {
				cfg.Auth.Mode = "otp"
			}
			d, err := New(Config{Storage: s, Settings: settings.NewMemoryStore(cfg), URL: "https://gateway.example", Client: client})
			if err != nil {
				t.Fatal(err)
			}
			if err := d.drain(t.Context()); err != nil {
				t.Fatal(err)
			}
			for _, body := range requests {
				if tc.requireAuth && body.Kind != "badge" {
					t.Fatal("expired access received content")
				}
				if body.Kind == "sms" {
					if body.Envelope == nil {
						t.Fatal("SMS was not encrypted")
					}
					plain, err := decryptPreview(sub.EncryptionKey, *body.Envelope)
					if err != nil || !strings.Contains(string(plain), msg.Text) {
						t.Fatalf("preview = %s, %v", plain, err)
					}
					var content storage.PushContent
					if err := json.Unmarshal(plain, &content); err != nil {
						t.Fatal(err)
					}
					if content.BackendName != "Home" || content.ModemName != "Mobile" || content.ProfileName != "Work" {
						t.Errorf("preview names = %q, %q, %q", content.BackendName, content.ModemName, content.ProfileName)
					}
				}
			}
			remaining, err := s.NativeDeliveries(t.Context(), time.Now().Add(time.Hour))
			if err != nil {
				t.Fatal(err)
			}
			if (len(remaining) > 0) != tc.wantRetry {
				t.Fatalf("pending=%d, wantRetry=%v", len(remaining), tc.wantRetry)
			}
			for _, item := range remaining {
				if item.Attempts != 1 {
					t.Errorf("attempts=%d", item.Attempts)
				}
			}
			subs, err := s.NativeSubscriptions(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if (len(subs) == 0) != tc.wantRevoked {
				t.Fatalf("subscriptions=%d", len(subs))
			}
		})
	}
}

func problemResponse(t *testing.T, status int, code string) *http.Response {
	t.Helper()
	if code == "" {
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("{}")), Header: make(http.Header)}
	}
	title, ok := apierror.Title(code)
	if !ok {
		t.Fatalf("unknown fixture code %q", code)
	}
	data, err := json.Marshal(apierror.Problem{Type: apierror.TypePrefix + code, Title: title, Status: status, ErrorCode: code, RequestID: "gateway-request"})
	if err != nil {
		t.Fatal(err)
	}
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {apierror.MediaType}, "X-Request-Id": {"gateway-request"}}, Body: io.NopCloser(strings.NewReader(string(data)))}
}

func TestDispatchCancellationAndAlreadyRead(t *testing.T) {
	s := pushStore(t)
	sub := storage.NativeSubscription{ID: "subscription", InstallationID: "installation", SendToken: strings.Repeat("s", 32), EncryptionKey: make([]byte, 32), SMS: true, Enabled: true}
	if err := s.UpsertNativeSubscription(t.Context(), sub); err != nil {
		t.Fatal(err)
	}
	if _, err := s.InsertMessage(t.Context(), storage.Message{ProfileID: "SIM", Source: storage.MessageSourceModem, ExternalKey: "first", Sender: "10086", Text: "read", Incoming: true, Timestamp: time.Now()}); err != nil {
		t.Fatal(err)
	}
	page, err := s.ConversationMessagesPage(t.Context(), storage.ConversationID("SIM", "10086"), storage.HistoryFilter{})
	if err != nil || len(page.Items) != 1 || page.NextCursor != "" {
		t.Fatalf("conversation page = %+v, error = %v", page, err)
	}
	items := page.Items
	if err := s.UpdateConversationRead(t.Context(), storage.ReadUpdate{ConversationID: items[0].ConversationID, ReadThroughMessageID: &items[0].ID}); err != nil {
		t.Fatal(err)
	}
	d, err := New(Config{Storage: s, URL: "https://gateway.example", Client: &http.Client{Transport: pushTransport(func(req *http.Request) (*http.Response, error) {
		var body deliveryRequest
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.Kind != "badge" || body.UnreadCount != 0 {
			t.Errorf("sent already read message: %+v", body)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("{}"))}, nil
	})}})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.drain(t.Context()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := d.Run(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestSMSDeliveryUsesCurrentSubscription(t *testing.T) {
	for _, change := range []string{"rotated credentials", "disabled", "SMS disabled", "removed"} {
		t.Run(change, func(t *testing.T) {
			s := pushStore(t)
			sub := storage.NativeSubscription{
				ID:             "subscription",
				InstallationID: "phone",
				SendToken:      strings.Repeat("s", 32),
				EncryptionKey:  bytes.Repeat([]byte{1}, 32),
				SMS:            true,
				Enabled:        true,
			}
			if err := s.UpsertNativeSubscription(t.Context(), sub); err != nil {
				t.Fatal(err)
			}
			msg := storage.Message{
				ProfileID:   "SIM",
				Source:      storage.MessageSourceModem,
				ExternalKey: "incoming",
				Sender:      "10086",
				Text:        "notification preview",
				Incoming:    true,
				Timestamp:   time.Now(),
			}
			if _, err := s.InsertMessage(t.Context(), msg); err != nil {
				t.Fatal(err)
			}
			items, err := s.NativeDeliveries(t.Context(), time.Now())
			if err != nil {
				t.Fatal(err)
			}
			var queued storage.NativeDelivery
			for _, item := range items {
				if item.Content.Kind == "sms" {
					queued = item
				}
			}
			if queued.EventID == "" {
				t.Fatal("SMS notification was not queued")
			}
			switch change {
			case "rotated credentials":
				sub.SendToken = strings.Repeat("r", 32)
				sub.EncryptionKey = bytes.Repeat([]byte{2}, 32)
			case "disabled":
				sub.Enabled = false
			case "SMS disabled":
				sub.SMS = false
			}
			if change == "removed" {
				if err := s.DeleteNativeSubscription(t.Context(), sub.ID); err != nil {
					t.Fatal(err)
				}
			} else if err := s.UpsertNativeSubscription(t.Context(), sub); err != nil {
				t.Fatal(err)
			}
			sent := 0
			client := &http.Client{Transport: pushTransport(func(req *http.Request) (*http.Response, error) {
				sent++
				if req.Header.Get("Authorization") != "Bearer "+sub.SendToken {
					t.Error("delivery used a stale send token")
				}
				var body deliveryRequest
				if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
					t.Errorf("decode delivery: %v", err)
				} else if body.Envelope == nil {
					t.Error("delivery omitted its encrypted content")
				} else if plain, err := decryptPreview(sub.EncryptionKey, *body.Envelope); err != nil || !bytes.Contains(plain, []byte(msg.Text)) {
					t.Errorf("delivery did not use the current encryption key: %v", err)
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader("{}")),
				}, nil
			})}
			d, err := New(Config{Storage: s, URL: "https://gateway.example", Client: client})
			if err != nil {
				t.Fatal(err)
			}
			if err := d.deliver(t.Context(), queued); err != nil {
				t.Fatal(err)
			}
			want := 0
			if change == "rotated credentials" {
				want = 1
			}
			if sent != want {
				t.Fatalf("gateway requests = %d, want %d", sent, want)
			}
		})
	}
}
