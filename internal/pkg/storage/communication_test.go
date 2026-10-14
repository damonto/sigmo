package storage

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestContactLifecycle(t *testing.T) {
	s := testStore(t)
	contact, err := s.SaveContact(t.Context(), Contact{Name: " Alice ", Notes: "Private note", Numbers: []ContactNumber{{Label: "mobile", Number: "415 555 2671", Region: "US"}, {Label: "work", Number: "+442079460018"}}})
	if err != nil {
		t.Fatal(err)
	}
	if contact.Name != "Alice" || len(contact.Numbers) != 2 || contact.Numbers[0].Key != "+14155552671" {
		t.Fatalf("saved contact = %+v", contact)
	}
	found, err := s.Contacts(t.Context(), "Alice")
	if err != nil || len(found) != 1 {
		t.Fatalf("search = %+v, %v", found, err)
	}
	contact.Name = "Alice Updated"
	contact.Numbers = contact.Numbers[:1]
	if _, err := s.SaveContact(t.Context(), contact); err != nil {
		t.Fatal(err)
	}
	updated, err := s.Contact(t.Context(), contact.ID)
	if err != nil || updated.Name != contact.Name || len(updated.Numbers) != 1 {
		t.Fatalf("updated = %+v, %v", updated, err)
	}
	if err := s.DeleteContact(t.Context(), contact.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Contact(t.Context(), contact.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted contact error = %v", err)
	}
	for _, tc := range []struct {
		name    string
		contact Contact
		invalid bool
	}{
		{"name only", Contact{Name: "Name only"}, false},
		{"empty", Contact{}, true},
		{"empty number", Contact{Name: "Empty phone", Numbers: []ContactNumber{{}}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.SaveContact(t.Context(), tc.contact)
			if errors.Is(err, ErrInvalidContact) != tc.invalid {
				t.Fatalf("save error = %v", err)
			}
		})
	}
}

func TestProfileReconciliationDoesNotOverwriteNewerDeletion(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	if err := s.RecordSIMProfile(ctx, SIMProfile{ProfileID: "source", ModemID: "modem"}); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkProfileDeletion(ctx, "source", "pending"); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkProfileDeletion(ctx, "source", "deleted"); err != nil {
		t.Fatal(err)
	}
	// A delayed inventory still reports the card present from before deletion.
	changed, err := s.ReconcileProfileDeletion(ctx, ProfileReconciliation{ProfileID: "source", ExpectedState: "pending", NewState: ""})
	if err != nil || changed {
		t.Fatalf("stale reconciliation = %v, %v", changed, err)
	}
	lines, err := s.SIMLines(ctx)
	if err != nil || len(lines) != 1 || lines[0].DeletionState != "deleted" {
		t.Fatalf("deleted lines = %+v, %v", lines, err)
	}
	// A new observation can confirm that the same profile has been reinstalled.
	changed, err = s.ReconcileProfileDeletion(ctx, ProfileReconciliation{ProfileID: "source", ExpectedState: "deleted", NewState: ""})
	if err != nil || !changed {
		t.Fatalf("fresh reconciliation = %v, %v", changed, err)
	}
}

func insertThreadMessage(t *testing.T, s *Store, profile, number, key string) {
	t.Helper()
	if _, err := s.InsertMessage(t.Context(), Message{ProfileID: profile, ModemID: "modem-" + profile, Source: MessageSourceModem, ExternalKey: key, Sender: number, Recipient: "+14155550000", Text: key, Incoming: true, Status: "received", Timestamp: time.Now()}); err != nil {
		t.Fatal(err)
	}
}

func TestThreadsKeepPerMessageReadsAndDeletionTombstones(t *testing.T) {
	s := testStore(t)
	insertThreadMessage(t, s, "a", "+14155552671", "first")
	insertThreadMessage(t, s, "b", "+14155552671", "second")
	insertThreadMessage(t, s, "a", "10086", "other")
	page, err := s.MessageThreads(t.Context(), HistoryFilter{})
	if err != nil || len(page.Items) != 2 || page.UnreadCount != 3 {
		t.Fatalf("threads = %+v, %v", page, err)
	}
	id := ThreadID("+14155552671")
	messages, err := s.ThreadMessages(t.Context(), id, HistoryFilter{})
	if err != nil || len(messages.Items) != 2 {
		t.Fatalf("messages = %+v, %v", messages, err)
	}
	if err := s.ReadThread(t.Context(), id, ThreadRead{MessageIDs: []int64{messages.Items[1].ID}}); err != nil {
		t.Fatal(err)
	}
	state, err := s.MessagesState(t.Context())
	if err != nil || state.UnreadCount != 2 {
		t.Fatalf("badge = %+v, %v", state, err)
	}
	source, err := s.Conversation(t.Context(), messages.Items[0].ConversationID)
	if err != nil || source.UnreadCount != 1 {
		t.Fatalf("other source conversation was marked read: %+v, %v", source, err)
	}
	if err := s.ReadThread(t.Context(), ThreadID("10086"), ThreadRead{MessageIDs: []int64{messages.Items[0].ID}}); !errors.Is(err, ErrInvalidReadUpdate) {
		t.Fatalf("foreign message read = %v", err)
	}
	if err := s.DeleteThread(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	var deleted int
	if err := s.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM deleted_messages`).Scan(&deleted); err != nil || deleted != 2 {
		t.Fatalf("tombstones = %d, %v", deleted, err)
	}
	state, err = s.MessagesState(t.Context())
	if err != nil || state.UnreadCount != 1 {
		t.Fatalf("badge after deletion = %+v, %v", state, err)
	}
}

func TestHistoryAssignmentPreservesSourceAndMovesPreferences(t *testing.T) {
	s := testStore(t)
	insertThreadMessage(t, s, "a", "10086", "old")
	for _, id := range []string{"a", "b", "c"} {
		if err := s.RecordSIMProfile(t.Context(), SIMProfile{ProfileID: id, ModemID: "modem-" + id, Label: "label-" + id}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SaveCall(t.Context(), Call{ID: "call-a", ProfileID: "a", ModemID: "modem-a", Number: "10086", Route: "volte", Direction: "outgoing", State: "ended"}); err != nil {
		t.Fatal(err)
	}
	before, err := s.Call(t.Context(), "call-a")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RememberCommunication(t.Context(), CommunicationPreference{Participant: "10086", ProfileID: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := s.AssignHistory(t.Context(), HistoryAssignment{SourceProfileID: "a", TargetProfileID: "b"}); !errors.Is(err, ErrInvalidAssignment) {
		t.Fatalf("accepted live source: %v", err)
	}
	for _, pair := range []HistoryAssignment{{SourceProfileID: "a", TargetProfileID: "b"}, {SourceProfileID: "b", TargetProfileID: "c"}} {
		if err := s.MarkProfileDeletion(t.Context(), pair.SourceProfileID, "deleted"); err != nil {
			t.Fatal(err)
		}
		for range 2 {
			if err := s.AssignHistory(t.Context(), pair); err != nil {
				t.Fatal(err)
			}
		}
	}
	after, err := s.Call(t.Context(), "call-a")
	if err != nil || after.ProfileID != "a" || after.HistoryProfileID != "c" || after.CallUUID != before.CallUUID || after.ModemID != before.ModemID || after.LineLabel != "label-a" {
		t.Fatalf("assigned call = %+v, %v", after, err)
	}
	messages, err := s.ThreadMessages(t.Context(), ThreadID("10086"), HistoryFilter{ProfileID: "c"})
	if err != nil || len(messages.Items) != 1 || messages.Items[0].ProfileID != "a" || messages.Items[0].Read {
		t.Fatalf("assigned messages = %+v, %v", messages, err)
	}
	pref, err := s.PreferredCommunication(t.Context(), "10086")
	if err != nil || pref.ProfileID != "c" {
		t.Fatalf("preference = %+v, %v", pref, err)
	}
}

func TestHistoryAssignmentRollback(t *testing.T) {
	s := testStore(t)
	insertThreadMessage(t, s, "a", "10086", "old")
	if err := s.RecordSIMProfile(t.Context(), SIMProfile{ProfileID: "b", ModemID: "modem-b"}); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkProfileDeletion(t.Context(), "a", "deleted"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(t.Context(), `CREATE TRIGGER reject_assignment BEFORE UPDATE OF deletion_state ON sim_profiles BEGIN SELECT RAISE(ABORT, 'injected failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.AssignHistory(t.Context(), HistoryAssignment{SourceProfileID: "a", TargetProfileID: "b"}); err == nil {
		t.Fatal("assignment succeeded")
	}
	page, err := s.ThreadMessages(t.Context(), ThreadID("10086"), HistoryFilter{})
	if err != nil || page.Items[0].HistoryProfileID != "a" {
		t.Fatalf("partial assignment = %+v, %v", page, err)
	}
}

func TestCallHistoryIncludesRemovedSIMsAndPages(t *testing.T) {
	s := testStore(t)
	for i := range 53 {
		if err := s.SaveCall(t.Context(), Call{ID: fmt.Sprint(i), ProfileID: "removed", ModemID: "unplugged", Number: "10086", Route: "volte", Direction: "incoming", State: "ended", StartedAt: time.Now().Add(time.Duration(i) * time.Second)}); err != nil {
			t.Fatal(err)
		}
	}
	first, cursor, err := s.CallHistory(t.Context(), HistoryFilter{})
	if err != nil || len(first) != 50 || cursor == "" {
		t.Fatalf("first page = %d, %q, %v", len(first), cursor, err)
	}
	second, cursor, err := s.CallHistory(t.Context(), HistoryFilter{Cursor: cursor})
	if err != nil || len(second) != 3 || cursor != "" {
		t.Fatalf("second page = %d, %q, %v", len(second), cursor, err)
	}
}

func TestThreadPaginationIgnoresConcurrentArrivals(t *testing.T) {
	s := testStore(t)
	for i := range 5 {
		_, err := s.InsertMessage(t.Context(), Message{ProfileID: "a", Source: MessageSourceModem, ExternalKey: fmt.Sprint(i), Sender: "10086", Text: fmt.Sprint(i), Incoming: true, Timestamp: time.Date(2026, 1, 1, 0, i, 0, 0, time.UTC)})
		if err != nil {
			t.Fatal(err)
		}
	}
	first, err := s.ThreadMessages(t.Context(), ThreadID("10086"), HistoryFilter{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	insertThreadMessage(t, s, "b", "10086", "concurrent arrival")
	second, err := s.ThreadMessages(t.Context(), ThreadID("10086"), HistoryFilter{Limit: 2, Cursor: first.NextCursor})
	if err != nil || len(second.Items) != 2 || second.Items[0].Text != "1" || second.Items[1].Text != "2" {
		t.Fatalf("second page = %+v, %v", second, err)
	}
	state, err := s.MessagesState(t.Context())
	if err != nil || state.UnreadCount != 6 {
		t.Fatalf("reading pages acknowledged unseen messages: %+v, %v", state, err)
	}
}

func TestThreadKeysDoNotRewriteSourceIdentity(t *testing.T) {
	s := testStore(t)
	if err := s.RecordSIMProfile(t.Context(), SIMProfile{ProfileID: "a", ModemID: "modem-a", Region: "US"}); err != nil {
		t.Fatal(err)
	}
	insertThreadMessage(t, s, "a", "(415) 555-2671", "formatted")
	insertThreadMessage(t, s, "b", "+14155552671", "international")
	insertThreadMessage(t, s, "b", "4155552671", "unknown region")
	page, err := s.MessageThreads(t.Context(), HistoryFilter{})
	if err != nil || len(page.Items) != 2 {
		t.Fatalf("normalized threads = %+v, %v", page, err)
	}
	id := ConversationID("a", "(415) 555-2671")
	thread, err := s.ThreadForConversation(t.Context(), id)
	if err != nil || thread != ThreadID("+14155552671") {
		t.Fatalf("notification resolution = %q, %v", thread, err)
	}
	messages, err := s.ThreadMessages(t.Context(), thread, HistoryFilter{})
	if err != nil || len(messages.Items) != 2 || messages.Items[0].ConversationID != id || messages.Items[0].Sender != "(415) 555-2671" {
		t.Fatalf("source identity = %+v, %v", messages, err)
	}
	for _, number := range []string{"10086", "BANK2025550123", "1-800-FLOWERS", "*123#"} {
		if got := ParticipantKey(number, "US"); got != number {
			t.Errorf("ParticipantKey(%q) = %q", number, got)
		}
	}
}

func TestThreadSummaryUsesChronologyRatherThanImportOrder(t *testing.T) {
	s := testStore(t)
	for i, stamp := range []time.Time{time.Now(), time.Now().Add(-time.Hour)} {
		if _, err := s.InsertMessage(t.Context(), Message{ProfileID: "a", Source: MessageSourceModem, ExternalKey: fmt.Sprint(i), Sender: "10086", Text: fmt.Sprint(i), Incoming: true, Timestamp: stamp}); err != nil {
			t.Fatal(err)
		}
	}
	page, err := s.MessageThreads(t.Context(), HistoryFilter{})
	if err != nil || len(page.Items) != 1 || page.Items[0].LatestMessage.Text != "0" {
		t.Fatalf("summary = %+v, %v", page, err)
	}
}

func TestLateHistoryFollowsCompletedAssignment(t *testing.T) {
	s := testStore(t)
	for _, id := range []string{"a", "b"} {
		if err := s.RecordSIMProfile(t.Context(), SIMProfile{ProfileID: id, ModemID: id}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.MarkProfileDeletion(t.Context(), "a", "deleted"); err != nil {
		t.Fatal(err)
	}
	if err := s.AssignHistory(t.Context(), HistoryAssignment{SourceProfileID: "a", TargetProfileID: "b"}); err != nil {
		t.Fatal(err)
	}
	insertThreadMessage(t, s, "a", "10086", "late")
	if err := s.RememberCommunication(t.Context(), CommunicationPreference{Participant: "10086", ProfileID: "a"}); err != nil {
		t.Fatal(err)
	}
	page, err := s.ThreadMessages(t.Context(), ThreadID("10086"), HistoryFilter{ProfileID: "b"})
	if err != nil || len(page.Items) != 1 || page.Items[0].ProfileID != "a" {
		t.Fatalf("late history = %+v, %v", page, err)
	}
	pref, err := s.PreferredCommunication(t.Context(), "10086")
	if err != nil || pref.ProfileID != "b" {
		t.Fatalf("late preference = %+v, %v", pref, err)
	}
	call, saved, err := s.SaveCallPreservingTerminal(t.Context(), Call{
		ID: "late-call", ProfileID: "a", ModemID: "a", Number: "10086",
		Route: "volte", Direction: "outgoing", State: "ended",
	})
	if err != nil || !saved || call.ProfileID != "a" || call.HistoryProfileID != "b" {
		t.Fatalf("late call snapshot = %+v, saved = %v, error = %v", call, saved, err)
	}
	history, _, err := s.CallHistory(t.Context(), HistoryFilter{ProfileID: "b"})
	if err != nil || len(history) != 1 || history[0] != call {
		t.Fatalf("late call history = %+v, error = %v", history, err)
	}
	stored, err := s.Call(t.Context(), call.ID)
	if err != nil || stored != call {
		t.Fatalf("stored late call = %+v, error = %v", stored, err)
	}
}

func TestSIMRegionUpdateOnlyRegroupsItsOwnHistory(t *testing.T) {
	s := testStore(t)
	for _, id := range []string{"a", "b"} {
		insertThreadMessage(t, s, id, "(415) 555-2671", id)
	}
	insertThreadMessage(t, s, "international", "+14155552671", "international")
	if err := s.RecordSIMProfile(t.Context(), SIMProfile{ProfileID: "a", ModemID: "modem-a", Region: "US"}); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		profileID   string
		participant string
	}{
		{profileID: "a", participant: "+14155552671"},
		{profileID: "b", participant: "(415) 555-2671"},
	} {
		t.Run(tt.profileID, func(t *testing.T) {
			conversationID := ConversationID(tt.profileID, "(415) 555-2671")
			thread, err := s.ThreadForConversation(t.Context(), conversationID)
			if err != nil || thread != ThreadID(tt.participant) {
				t.Fatalf("thread = %q, error = %v", thread, err)
			}
			messages, err := s.ConversationMessages(t.Context(), conversationID)
			if err != nil || len(messages) != 1 || messages[0].Sender != "(415) 555-2671" {
				t.Fatalf("original messages = %+v, error = %v", messages, err)
			}
		})
	}
	page, err := s.ThreadMessages(t.Context(), ThreadID("+14155552671"), HistoryFilter{})
	if err != nil || len(page.Items) != 2 {
		t.Fatalf("regrouped messages = %+v, error = %v", page, err)
	}
}
