package storage

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestConversationReadWatermarkAndSIMHistory(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	insert := func(profile, text string) Message {
		t.Helper()
		msg := Message{ProfileID: profile, ModemID: "modem", Source: MessageSourceModem, ExternalKey: text, Sender: "+8613800000000", Recipient: "10001", Text: text, Incoming: true, Timestamp: time.Now()}
		if _, err := s.InsertMessage(ctx, msg); err != nil {
			t.Fatal(err)
		}
		items, err := s.ListByParticipant(ctx, profile, msg.Sender)
		if err != nil {
			t.Fatal(err)
		}
		return items[len(items)-1]
	}
	first := insert("sim-a", "first")
	other := insert("sim-b", "other line")
	second := insert("sim-a", "arrived while reading")
	if first.ConversationID == other.ConversationID {
		t.Fatal("different SIMs share a conversation")
	}
	if err := s.UpdateConversationRead(ctx, ReadUpdate{ConversationID: first.ConversationID, ReadThroughMessageID: &first.ID}); err != nil {
		t.Fatal(err)
	}
	c, err := s.Conversation(ctx, first.ConversationID)
	if err != nil || c.UnreadCount != 1 {
		t.Fatalf("late arrival unread count = %d, error = %v", c.UnreadCount, err)
	}
	if err := s.UpdateConversationRead(ctx, ReadUpdate{ConversationID: first.ConversationID, ReadThroughMessageID: &other.ID}); !errors.Is(err, ErrInvalidReadUpdate) {
		t.Fatalf("foreign watermark error = %v", err)
	}
	if err := s.UpdateConversationRead(ctx, ReadUpdate{ConversationID: first.ConversationID, ReadThroughMessageID: &second.ID}); err != nil {
		t.Fatal(err)
	}
	marked := true
	if err := s.UpdateConversationRead(ctx, ReadUpdate{ConversationID: first.ConversationID, MarkedUnread: &marked}); err != nil {
		t.Fatal(err)
	}
	state, err := s.MessagesState(ctx)
	if err != nil || state.UnreadCount != 2 {
		t.Fatalf("manual unread plus other SIM = %d, error = %v", state.UnreadCount, err)
	}
	if err := s.RecordSIMProfile(ctx, SIMProfile{ProfileID: "sim-b", ModemID: "modem", Label: "Current SIM"}); err != nil {
		t.Fatal(err)
	}
	history, err := s.ConversationMessages(ctx, first.ConversationID)
	if err != nil || len(history) != 2 {
		t.Fatalf("inactive SIM history length = %d, error = %v", len(history), err)
	}
	for _, filter := range []ConversationFilter{{ModemID: "modem"}, {ProfileID: "sim-a"}, {Query: "arrived"}} {
		snapshot, err := s.Conversations(ctx, filter)
		if err != nil || len(snapshot.Items) == 0 || snapshot.UnreadCount != 2 {
			t.Fatalf("filtered snapshot = %+v, error = %v", snapshot, err)
		}
	}
	if err := s.DeleteConversation(ctx, first.ConversationID); err != nil {
		t.Fatal(err)
	}
	state, err = s.MessagesState(ctx)
	if err != nil || state.UnreadCount != 1 {
		t.Fatalf("badge after delete = %d, error = %v", state.UnreadCount, err)
	}
	if _, err := s.Conversation(ctx, other.ConversationID); err != nil {
		t.Fatalf("delete crossed SIM boundary: %v", err)
	}
}

func TestConversationMigrationPreservesHistoryAndReadBaseline(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	msg := Message{ProfileID: "legacy-sim", Source: MessageSourceModem, ExternalKey: "old", Sender: "10086", Recipient: "+8613800000000", Text: "historical", Incoming: true, Timestamp: time.Now().Add(-time.Hour)}
	if _, err := s.InsertMessage(ctx, msg); err != nil {
		t.Fatal(err)
	}
	// Recreate the previous schema around real stored SMS rows.
	removeCommunicationSchema(t, s)
	for _, statement := range []string{
		`DROP TABLE goose_db_version`, // Historical releases had no migration ledger.
		`DROP TABLE native_push_outbox`, `DROP TABLE native_push_subscriptions`, `DROP TABLE conversations`, `DROP TABLE sim_profiles`, `DROP TABLE message_state`, `DROP INDEX idx_messages_conversation`, `ALTER TABLE messages DROP COLUMN conversation_id`, `ALTER TABLE messages DROP COLUMN modem_id`,
	} {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	before, err := s.MessagesState(ctx)
	if err != nil || before.UnreadCount != 0 {
		t.Fatalf("historical badge = %+v, error = %v", before, err)
	}
	unknown, err := s.Conversations(ctx, ConversationFilter{Unassigned: true})
	if err != nil || len(unknown.Items) != 1 {
		t.Fatalf("unassigned history = %+v, error = %v", unknown, err)
	}
	if got := unknown.Items[0].LineNumber; got != msg.Recipient {
		t.Fatalf("historical line number = %q, want %q", got, msg.Recipient)
	}
	msg.ExternalKey, msg.Text, msg.Timestamp = "new", "new incoming", time.Now()
	if _, err := s.InsertMessage(ctx, msg); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	after, err := s.MessagesState(ctx)
	if err != nil || after.UnreadCount != 1 || before.BackendID != after.BackendID {
		t.Fatalf("repeated migration changed identity/read state: %+v, error = %v", after, err)
	}
}

func TestNativeOutboxCommitsWithMessageAndReadState(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	sub := NativeSubscription{ID: "subscription-id", InstallationID: "installation-id", SendToken: strings.Repeat("s", 32), EncryptionKey: []byte(strings.Repeat("k", 32)), SMS: true, Reminders: true, Enabled: true}
	if err := s.UpsertNativeSubscription(ctx, sub); err != nil {
		t.Fatal(err)
	}
	msg := Message{ProfileID: "sim", ModemID: "modem", Source: MessageSourceModem, ExternalKey: "message", Sender: "10086", Text: "hello", Incoming: true, Timestamp: time.Now()}
	if _, err := s.InsertMessage(ctx, msg); err != nil {
		t.Fatal(err)
	}
	deliveries, err := s.NativeDeliveries(ctx, time.Now())
	if err != nil || len(deliveries) != 2 {
		t.Fatalf("deliveries = %d, error = %v", len(deliveries), err)
	}
	var oldBadge NativeDelivery
	for _, d := range deliveries {
		if d.Content.Kind == "badge" {
			oldBadge = d
		}
	}
	items, err := s.ListByParticipant(ctx, "sim", "10086")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateConversationRead(ctx, ReadUpdate{ConversationID: items[0].ConversationID, ReadThroughMessageID: &items[0].ID}); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishNativeDelivery(ctx, oldBadge); err != nil {
		t.Fatal(err)
	}
	deliveries, err = s.NativeDeliveries(ctx, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	foundNewBadge := false
	for _, d := range deliveries {
		if d.Content.Kind == "badge" && d.Revision > oldBadge.Revision {
			foundNewBadge = true
		}
	}
	if !foundNewBadge {
		t.Fatal("completion removed a newer badge update")
	}
	if _, err := s.db.ExecContext(ctx, `CREATE TRIGGER reject_notification BEFORE INSERT ON native_push_outbox BEGIN SELECT RAISE(ABORT, 'test transaction failure'); END`); err != nil {
		t.Fatal(err)
	}
	msg.ExternalKey, msg.Text = "rejected", "must roll back"
	if _, err := s.InsertMessage(ctx, msg); err == nil {
		t.Fatal("injected transaction failure was ignored")
	}
	items, err = s.ListByParticipant(ctx, "sim", "10086")
	if err != nil || len(items) != 1 {
		t.Fatalf("partial SMS transaction persisted: %d, %v", len(items), err)
	}
}

func TestReminderClaimQueuesOneNativeEvent(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	sub := NativeSubscription{ID: "subscription", InstallationID: "installation", SendToken: strings.Repeat("s", 32), EncryptionKey: []byte(strings.Repeat("k", 32)), Reminders: true, Enabled: true}
	if err := s.UpsertNativeSubscription(ctx, sub); err != nil {
		t.Fatal(err)
	}
	r := Reminder{ProfileType: "esim", ProfileID: "sim", ModemID: "modem", SEID: "se", Content: "Renew", NextAt: time.Now().Add(-time.Minute)}
	if err := s.UpsertReminder(ctx, r); err != nil {
		t.Fatal(err)
	}
	r, _, err := s.Reminder(ctx, r.ProfileType, r.ProfileID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.ClaimReminder(ctx, r); err != nil || !ok {
		t.Fatalf("claim = %v, %v", ok, err)
	}
	if _, ok, err := s.ClaimReminder(ctx, r); err != nil || ok {
		t.Fatalf("duplicate claim = %v, %v", ok, err)
	}
	deliveries, err := s.NativeDeliveries(ctx, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, d := range deliveries {
		if d.Content.Kind == "reminder" {
			count++
			if d.Content.SEID != "se" {
				t.Fatal("reminder lost secure-element identity")
			}
		}
	}
	if count != 1 {
		t.Fatalf("reminder deliveries = %d", count)
	}
}

func TestReadAcknowledgmentCancellation(t *testing.T) {
	s := testStore(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	marked := true
	if err := s.UpdateConversationRead(ctx, ReadUpdate{ConversationID: "missing", MarkedUnread: &marked}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled read error = %v", err)
	}
}

func TestConversationDeleteDoesNotReturnOnModemSync(t *testing.T) {
	s := testStore(t)
	msg := Message{ProfileID: "sim-a", ModemID: "modem", Source: MessageSourceModem, ExternalKey: "old-ref", Sender: "10086", Recipient: "own", Text: "delete this", Incoming: true, Timestamp: time.Now()}
	if _, err := s.InsertMessage(t.Context(), msg); err != nil {
		t.Fatal(err)
	}
	other := msg
	other.ExternalKey = "other"
	other.Sender = "20000"
	other.Text = "keep this"
	if _, err := s.InsertMessage(t.Context(), other); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteConversation(t.Context(), ConversationID(msg.ProfileID, msg.Sender)); err != nil {
		t.Fatal(err)
	}
	msg.ExternalKey = "new-generation-ref"
	msg.ProfileID = "sim-b"
	if inserted, err := s.InsertMessage(t.Context(), msg); err != nil || inserted {
		t.Fatalf("modem reimport = %v, %v", inserted, err)
	}
	snapshot, err := s.Conversations(t.Context(), ConversationFilter{})
	if err != nil || len(snapshot.Items) != 1 || snapshot.Items[0].Participant != other.Sender {
		t.Fatalf("remaining conversation = %+v, %v", snapshot, err)
	}
}

func TestNativeRegistrationIsIdempotent(t *testing.T) {
	s := testStore(t)
	sub := NativeSubscription{ID: "subscription", InstallationID: "installation", SendToken: strings.Repeat("s", 32), EncryptionKey: make([]byte, 32), SMS: true, Enabled: true}
	if err := s.UpsertNativeSubscription(t.Context(), sub); err != nil {
		t.Fatal(err)
	}
	before, err := s.MessagesState(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertNativeSubscription(t.Context(), sub); err != nil {
		t.Fatal(err)
	}
	after, err := s.MessagesState(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("repeated registration advanced state: %+v -> %+v", before, after)
	}
}

func TestConversationWatermarkIsIndependentOfTimestampOrder(t *testing.T) {
	s := testStore(t)
	for i := range 2 {
		msg := Message{ProfileID: "sim", Source: MessageSourceModem, ExternalKey: strings.Repeat("x", i+1), Sender: "10086", Text: "hello", Timestamp: time.Now().Add(-time.Duration(i) * time.Hour), Incoming: true}
		if _, err := s.InsertMessage(t.Context(), msg); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := s.Conversations(t.Context(), ConversationFilter{})
	if err != nil {
		t.Fatal(err)
	}
	c := snapshot.Items[0]
	if c.LastMessageID <= c.Latest.ID {
		t.Fatalf("latest timestamp unexpectedly used as read watermark: %+v", c)
	}
	if err := s.UpdateConversationRead(t.Context(), ReadUpdate{ConversationID: c.ID, ReadThroughMessageID: &c.LastMessageID}); err != nil {
		t.Fatal(err)
	}
	state, err := s.MessagesState(t.Context())
	if err != nil || state.UnreadCount != 0 {
		t.Fatalf("read snapshot: %+v, %v", state, err)
	}
}
