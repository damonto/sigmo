package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestAppState(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)

	tests := []struct {
		name  string
		scope string
		key   string
		value bool
	}{
		{name: "enabled", scope: "profile:891", key: "wifi_calling.enabled", value: true},
		{name: "preferred", scope: "profile:891", key: "wifi_calling.preferred", value: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := store.Put(ctx, tt.scope, tt.key, tt.value); err != nil {
				t.Fatalf("Put() error = %v", err)
			}
			var got bool
			if err := store.Get(ctx, tt.scope, tt.key, &got); err != nil {
				t.Fatalf("Get() error = %v", err)
			}
			if got != tt.value {
				t.Fatalf("Get() = %v, want %v", got, tt.value)
			}
		})
	}
}

func TestMessages(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	base := time.Date(2026, 5, 24, 10, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		message    Message
		wantInsert bool
	}{
		{
			name: "new modem message",
			message: Message{
				ProfileID:   "891",
				Source:      MessageSourceModem,
				ExternalKey: "/sms/1",
				Sender:      "+100",
				Recipient:   "+200",
				Text:        "hello",
				Timestamp:   base,
				Status:      "received",
				Incoming:    true,
			},
			wantInsert: true,
		},
		{
			name: "duplicate modem message",
			message: Message{
				ProfileID:   "891",
				Source:      MessageSourceModem,
				ExternalKey: "/sms/1",
				Sender:      "+100",
				Recipient:   "+200",
				Text:        "hello",
				Timestamp:   base,
				Status:      "received",
				Incoming:    true,
			},
			wantInsert: false,
		},
		{
			name: "wifi calling message",
			message: Message{
				ProfileID:   "891",
				Source:      MessageSourceWiFiCalling,
				ExternalKey: "sms-1",
				Sender:      "+200",
				Recipient:   "+100",
				Text:        "reply",
				Timestamp:   base.Add(time.Minute),
				Status:      "sent",
				WiFiCalling: true,
			},
			wantInsert: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inserted, err := store.InsertMessage(ctx, tt.message)
			if err != nil {
				t.Fatalf("InsertMessage() error = %v", err)
			}
			if inserted != tt.wantInsert {
				t.Fatalf("InsertMessage() = %v, want %v", inserted, tt.wantInsert)
			}
		})
	}

	t.Run("conversation latest", func(t *testing.T) {
		conversations, err := store.ListConversations(ctx, "891")
		if err != nil {
			t.Fatalf("ListConversations() error = %v", err)
		}
		if len(conversations) != 1 {
			t.Fatalf("ListConversations() length = %d, want 1", len(conversations))
		}
		if conversations[0].Text != "reply" {
			t.Fatalf("latest conversation text = %q, want reply", conversations[0].Text)
		}
	})

	t.Run("thread order", func(t *testing.T) {
		messages, err := store.ListByParticipant(ctx, "891", "+100")
		if err != nil {
			t.Fatalf("ListByParticipant() error = %v", err)
		}
		if len(messages) != 2 {
			t.Fatalf("ListByParticipant() length = %d, want 2", len(messages))
		}
		if messages[0].Text != "hello" || messages[1].Text != "reply" {
			t.Fatalf("thread order = %q, %q; want hello, reply", messages[0].Text, messages[1].Text)
		}
	})
}

func testStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(context.Background(), filepath.Join(t.TempDir(), "sigmo.db"))
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
