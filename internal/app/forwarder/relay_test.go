package forwarder

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	notifyevent "github.com/damonto/sigmo/internal/pkg/notify/event"
	"github.com/damonto/sigmo/internal/pkg/settings"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

func TestNewRequiresMessageStorage(t *testing.T) {
	tests := []struct {
		name string
	}{
		{name: "nil message storage"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(settings.NewMemoryStore(settings.Default()), nil, nil)
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
	ctx := context.Background()

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
	relay, err := New(settings.NewMemoryStore(current), nil, db)
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
