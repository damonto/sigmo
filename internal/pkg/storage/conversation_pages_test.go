package storage

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestConversationPagesPreserveSnapshotAndOriginalSIM(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	stamp := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	insert := func(profile, text string, date time.Time) {
		t.Helper()
		if _, err := s.InsertMessage(ctx, Message{ProfileID: profile, ModemID: "modem", Source: MessageSourceModem,
			ExternalKey: text, Sender: "10086", Recipient: "10010", Text: text, Timestamp: date, Incoming: true}); err != nil {
			t.Fatal(err)
		}
	}
	for i := range 65 {
		insert("old-sim", fmt.Sprintf("message-%d", i), stamp.Add(time.Duration(i/25)*time.Second))
	}
	insert("old-sim", "older timestamp with a larger ID", stamp.Add(-time.Hour))
	insert("new-sim", "other conversation", stamp.Add(time.Hour))
	id := ConversationID("old-sim", "10086")
	first, err := s.ConversationMessagesPage(ctx, id, HistoryFilter{Limit: 20})
	if err != nil || len(first.Items) != 20 || first.NextCursor == "" || first.Conversation.UnreadCount != 66 || first.UnreadCount != 67 {
		t.Fatalf("first page = %+v, error = %v", first, err)
	}
	insert("old-sim", "late import with an old timestamp", stamp.Add(time.Second))
	if err := s.MarkProfileDeletion(ctx, "old-sim", "deleted"); err != nil {
		t.Fatal(err)
	}
	if err := s.AssignHistory(ctx, HistoryAssignment{SourceProfileID: "old-sim", TargetProfileID: "new-sim"}); err != nil {
		t.Fatal(err)
	}
	seen := make(map[int64]bool)
	page := first
	for {
		for i, message := range page.Items {
			if message.ConversationID != id || message.ProfileID != "old-sim" || seen[message.ID] || message.Text == "late import with an old timestamp" {
				t.Fatalf("incorrect source or duplicate: %+v", message)
			}
			if i > 0 && (message.Timestamp.Before(page.Items[i-1].Timestamp) || message.Timestamp.Equal(page.Items[i-1].Timestamp) && message.ID <= page.Items[i-1].ID) {
				t.Fatal("page is not chronologically ordered")
			}
			seen[message.ID] = true
		}
		if page.NextCursor == "" {
			break
		}
		page, err = s.ConversationMessagesPage(ctx, id, HistoryFilter{Limit: 20, Cursor: page.NextCursor})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(seen) != 66 {
		t.Fatalf("read %d original messages, want 66", len(seen))
	}
	if _, err := s.ConversationMessagesPage(ctx, id, HistoryFilter{Cursor: "invalid"}); !errors.Is(err, ErrInvalidHistoryCursor) {
		t.Fatalf("invalid cursor error = %v", err)
	}
	if _, err := s.ConversationMessagesPage(ctx, "missing", HistoryFilter{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing conversation error = %v", err)
	}
}
