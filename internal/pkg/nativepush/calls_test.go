package nativepush

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/damonto/sigmo/internal/pkg/storage"
)

func TestCallInvitationRechecksStateAndSubscription(t *testing.T) {
	for _, change := range []string{"ringing", "disabled", "calls disabled", "subscription removed", "answered", "owned", "ended", "call deleted", "expired"} {
		t.Run(change, func(t *testing.T) {
			s := pushStore(t)
			sub := storage.NativeSubscription{ID: "phone", InstallationID: "installation", SendToken: strings.Repeat("s", 32), EncryptionKey: make([]byte, 32), Calls: true, Enabled: true}
			if err := s.UpsertNativeSubscription(t.Context(), sub); err != nil {
				t.Fatal(err)
			}
			call := storage.Call{ID: "incoming", ProfileID: "SIM", ModemID: "modem", Route: "volte", Direction: "incoming", State: "ringing", Number: "10086", StartedAt: time.Now(), UpdatedAt: time.Now()}
			if err := s.SaveCall(t.Context(), call); err != nil {
				t.Fatal(err)
			}
			if err := s.EnqueueNativeCall(t.Context(), call); err != nil {
				t.Fatal(err)
			}
			items, err := s.NativeCallDeliveries(t.Context(), time.Now())
			if err != nil || len(items) != 1 {
				t.Fatalf("queued invitations=%d, error=%v", len(items), err)
			}
			item := items[0]
			switch change {
			case "disabled", "calls disabled":
				sub.Enabled = change != "disabled"
				sub.Calls = change != "calls disabled"
				err = s.UpsertNativeSubscription(t.Context(), sub)
			case "subscription removed":
				err = s.DeleteNativeSubscription(t.Context(), sub.ID)
			case "answered", "ended", "call deleted":
				call.State = "ended"
				if change == "answered" {
					call.State = "active"
				}
				err = s.SaveCall(t.Context(), call)
				if err == nil && change == "call deleted" {
					err = s.DeleteCall(t.Context(), call.ProfileID, call.ModemID, call.ID)
				}
			case "owned":
				_, err = s.ClaimCall(t.Context(), call.ID, "web")
			case "expired":
				item.ExpiresAt = time.Now().Add(-time.Second)
			}
			if err != nil {
				t.Fatal(err)
			}
			sent := 0
			d, err := New(Config{Storage: s, URL: "https://gateway.example", Client: &http.Client{Transport: pushTransport(func(req *http.Request) (*http.Response, error) {
				sent++
				var body deliveryRequest
				if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body.Kind != "call" || body.CallUUID != item.Content.CallUUID || body.Envelope == nil {
					t.Errorf("invalid call identity or encryption: %+v", body)
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("{}"))}, nil
			})}})
			if err != nil {
				t.Fatal(err)
			}
			if err := d.deliver(t.Context(), item); err != nil {
				t.Fatal(err)
			}
			want := 0
			if change == "ringing" {
				want = 1
			}
			if sent != want {
				t.Fatalf("gateway requests=%d, want %d", sent, want)
			}
		})
	}
}
