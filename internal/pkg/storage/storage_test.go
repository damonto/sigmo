package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenProtectsDatabaseFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sigmo.db")
	store, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("database mode = %o, want 600", got)
	}
}

func TestAppState(t *testing.T) {
	ctx := t.Context()
	store := testStore(t)

	tests := []struct {
		name  string
		scope string
		key   string
		value bool
	}{
		{name: "enabled", scope: "profile:891", key: "wifi_calling.enabled", value: true},
		{name: "disabled", scope: "profile:892", key: "wifi_calling.enabled", value: false},
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
	ctx := t.Context()
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
				ModemID:     "modem-a",
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
				ModemID:     "modem-a",
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
			name: "duplicate modem message with new profile and path",
			message: Message{
				ModemID:     "modem-a",
				ProfileID:   "892",
				Source:      MessageSourceModem,
				ExternalKey: "/sms/2",
				Sender:      "+100",
				Recipient:   "+999",
				Text:        "hello",
				Timestamp:   base,
				Status:      "received",
				Incoming:    true,
			},
			wantInsert: false,
		},
		{
			name: "same content on different modem",
			message: Message{
				ModemID:     "modem-b",
				ProfileID:   "893",
				Source:      MessageSourceModem,
				ExternalKey: "/sms/3",
				Sender:      "+100",
				Recipient:   "+999",
				Text:        "hello",
				Timestamp:   base,
				Status:      "received",
				Incoming:    true,
			},
			wantInsert: true,
		},
		{
			name: "wifi calling message",
			message: Message{
				ModemID:     "modem-a",
				ProfileID:   "891",
				Source:      MessageSourceRouted,
				ExternalKey: "sms-1",
				Sender:      "+200",
				Recipient:   "+100",
				Text:        "reply",
				Timestamp:   base.Add(time.Minute),
				Status:      "sent",
				Routed:      true,
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
		conversations, err := store.ListConversations(ctx, "891", "")
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

	t.Run("conversation search", func(t *testing.T) {
		inserted, err := store.InsertMessage(ctx, Message{
			ModemID:     "modem-a",
			ProfileID:   "891",
			Source:      MessageSourceModem,
			ExternalKey: "/sms/escaped",
			Sender:      "+300",
			Recipient:   "+200",
			Text:        "100% done",
			Timestamp:   base.Add(2 * time.Minute),
			Status:      "received",
			Incoming:    true,
		})
		if err != nil {
			t.Fatalf("InsertMessage(search) error = %v", err)
		}
		if !inserted {
			t.Fatal("InsertMessage(search) = false, want true")
		}

		tests := []struct {
			name     string
			query    string
			wantLen  int
			wantText string
		}{
			{name: "empty query keeps latest conversations", wantLen: 2, wantText: "100% done"},
			{name: "body text", query: "hello", wantLen: 1, wantText: "hello"},
			{name: "formatted number", query: "(300)", wantLen: 1, wantText: "100% done"},
			{name: "escaped percent", query: "%", wantLen: 1, wantText: "100% done"},
			{name: "digits from mixed text do not broaden body search", query: "hello 123", wantLen: 0},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				conversations, err := store.ListConversations(ctx, "891", tt.query)
				if err != nil {
					t.Fatalf("ListConversations() error = %v", err)
				}
				if len(conversations) != tt.wantLen {
					t.Fatalf("ListConversations() length = %d, want %d", len(conversations), tt.wantLen)
				}
				if tt.wantLen == 0 {
					return
				}
				if conversations[0].Text != tt.wantText {
					t.Fatalf("ListConversations()[0].Text = %q, want %q", conversations[0].Text, tt.wantText)
				}
			})
		}
	})
}

func TestModemMessageRefsIncludeGenerationAndZeroID(t *testing.T) {
	ctx := t.Context()
	store := testStore(t)
	base := time.Date(2026, 7, 31, 10, 0, 0, 0, time.UTC)

	messages := []Message{
		{
			ModemID:     "modem-a",
			ProfileID:   "profile-a",
			Source:      MessageSourceModem,
			ExternalKey: "g1:0",
			Sender:      "+100",
			Recipient:   "+200",
			Text:        "first",
			Timestamp:   base,
			Status:      "received",
			Incoming:    true,
			ModemRefs: []ModemMessageRef{
				{Generation: 1, Storage: 2, ID: 0},
			},
		},
		{
			ModemID:     "modem-a",
			ProfileID:   "profile-a",
			Source:      MessageSourceModem,
			ExternalKey: "g2:0",
			Sender:      "+100",
			Recipient:   "+200",
			Text:        "second",
			Timestamp:   base.Add(time.Second),
			Status:      "received",
			Incoming:    true,
			ModemRefs: []ModemMessageRef{
				{Generation: 2, Storage: 2, ID: 0},
			},
		},
	}
	for _, msg := range messages {
		inserted, err := store.InsertMessage(ctx, msg)
		if err != nil {
			t.Fatalf("InsertMessage() error = %v", err)
		}
		if !inserted {
			t.Fatal("InsertMessage() = false, want true")
		}
	}

	got, err := store.ListByParticipant(ctx, "profile-a", "+100")
	if err != nil {
		t.Fatalf("ListByParticipant() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListByParticipant() length = %d, want 2", len(got))
	}
	for i, wantGeneration := range []uint64{1, 2} {
		if len(got[i].ModemRefs) != 1 {
			t.Fatalf("message %d refs = %v, want one ref", i, got[i].ModemRefs)
		}
		ref := got[i].ModemRefs[0]
		if ref.ModemID != "modem-a" || ref.Generation != wantGeneration || ref.Storage != 2 || ref.ID != 0 {
			t.Fatalf("message %d ref = %+v, want generation %d zero id", i, ref, wantGeneration)
		}
	}
}

func TestDeleteModemMessageRefsDeletesExactReferencesTransactionally(t *testing.T) {
	ctx := t.Context()
	store := testStore(t)
	base := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	targetRefs := []ModemMessageRef{
		{ModemID: "modem-a", Generation: 7, Storage: 2, ID: 0},
		{ModemID: "modem-a", Generation: 7, Storage: 1, ID: 9},
	}
	messages := []Message{
		{
			ModemID:     "modem-a",
			ProfileID:   "profile-a",
			Source:      MessageSourceModem,
			ExternalKey: "target",
			Sender:      "+100",
			Recipient:   "+200",
			Text:        "multipart",
			Timestamp:   base,
			Status:      "received",
			Incoming:    true,
			ModemRefs:   targetRefs,
		},
		{
			ModemID:     "modem-a",
			ProfileID:   "profile-a",
			Source:      MessageSourceModem,
			ExternalKey: "other-generation",
			Sender:      "+101",
			Recipient:   "+200",
			Text:        "new generation",
			Timestamp:   base.Add(time.Second),
			Status:      "received",
			Incoming:    true,
			ModemRefs: []ModemMessageRef{
				{ModemID: "modem-a", Generation: 8, Storage: 2, ID: 0},
			},
		},
		{
			ModemID:     "modem-b",
			ProfileID:   "profile-b",
			Source:      MessageSourceModem,
			ExternalKey: "other-modem",
			Sender:      "+102",
			Recipient:   "+200",
			Text:        "other modem",
			Timestamp:   base.Add(2 * time.Second),
			Status:      "received",
			Incoming:    true,
			ModemRefs: []ModemMessageRef{
				{ModemID: "modem-b", Generation: 7, Storage: 1, ID: 9},
			},
		},
	}
	for _, msg := range messages {
		inserted, err := store.InsertMessage(ctx, msg)
		if err != nil {
			t.Fatalf("InsertMessage() error = %v", err)
		}
		if !inserted {
			t.Fatal("InsertMessage() = false, want true")
		}
	}

	invalidRefs := []ModemMessageRef{
		targetRefs[0],
		{Generation: 7, Storage: 1, ID: 9},
	}
	if err := store.DeleteModemMessageRefs(ctx, invalidRefs); err == nil {
		t.Fatal("DeleteModemMessageRefs() error = nil, want missing modem id error")
	}
	target, err := store.ListByParticipant(ctx, "profile-a", "+100")
	if err != nil {
		t.Fatalf("ListByParticipant() after rollback error = %v", err)
	}
	if len(target) != 1 || len(target[0].ModemRefs) != 2 {
		t.Fatalf("target refs after rollback = %+v, want both refs", target)
	}

	if err := store.DeleteModemMessageRefs(ctx, targetRefs); err != nil {
		t.Fatalf("DeleteModemMessageRefs() error = %v", err)
	}
	target, err = store.ListByParticipant(ctx, "profile-a", "+100")
	if err != nil {
		t.Fatalf("ListByParticipant() target error = %v", err)
	}
	if len(target) != 1 || len(target[0].ModemRefs) != 0 {
		t.Fatalf("target refs = %+v, want none", target)
	}
	remainingMessages := []struct {
		profileID   string
		participant string
	}{
		{profileID: "profile-a", participant: "+101"},
		{profileID: "profile-b", participant: "+102"},
	}
	for _, remainingMessage := range remainingMessages {
		remaining, err := store.ListByParticipant(ctx, remainingMessage.profileID, remainingMessage.participant)
		if err != nil {
			t.Fatalf("ListByParticipant(%s) error = %v", remainingMessage.participant, err)
		}
		if len(remaining) != 1 || len(remaining[0].ModemRefs) != 1 {
			t.Fatalf("remaining refs for %s = %+v, want one", remainingMessage.participant, remaining)
		}
	}
}

func TestUpdateMessageStatus(t *testing.T) {
	ctx := t.Context()
	store := testStore(t)
	base := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	messages := []Message{
		{
			ModemID:     "modem-a",
			ProfileID:   "profile-a",
			Source:      MessageSourceRouted,
			ExternalKey: "outgoing-1",
			Sender:      "+100",
			Recipient:   "+200",
			Text:        "hello",
			Timestamp:   base,
			Status:      "sent",
			Routed:      true,
		},
		{
			ModemID:     "modem-a",
			ProfileID:   "profile-a",
			Source:      MessageSourceModem,
			ExternalKey: "outgoing-1",
			Sender:      "+100",
			Recipient:   "+200",
			Text:        "hello",
			Timestamp:   base,
			Status:      "sent",
		},
		{
			ModemID:     "modem-a",
			ProfileID:   "profile-b",
			Source:      MessageSourceRouted,
			ExternalKey: "outgoing-1",
			Sender:      "+100",
			Recipient:   "+200",
			Text:        "hello",
			Timestamp:   base.Add(time.Second),
			Status:      "sent",
			Routed:      true,
		},
	}
	for _, msg := range messages {
		if _, err := store.InsertMessage(ctx, msg); err != nil {
			t.Fatalf("InsertMessage() error = %v", err)
		}
	}

	tests := []struct {
		name        string
		profileID   string
		source      string
		externalKey string
		status      string
		wantUpdated bool
		wantStatus  string
		wantErr     bool
	}{
		{
			name:        "updates matching message",
			profileID:   "profile-a",
			source:      MessageSourceRouted,
			externalKey: "outgoing-1",
			status:      "DELIVERED",
			wantUpdated: true,
			wantStatus:  "delivered",
		},
		{
			name:        "unknown message is ignored",
			profileID:   "profile-a",
			source:      MessageSourceRouted,
			externalKey: "missing",
			status:      "failed",
			wantStatus:  "delivered",
		},
		{
			name:        "empty status is rejected",
			profileID:   "profile-a",
			source:      MessageSourceRouted,
			externalKey: "outgoing-1",
			status:      " ",
			wantStatus:  "delivered",
			wantErr:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updated, err := store.UpdateMessageStatus(ctx, MessageStatusUpdate{
				ProfileID:   tt.profileID,
				Source:      tt.source,
				ExternalKey: tt.externalKey,
				Status:      tt.status,
			})
			if tt.wantErr {
				if err == nil {
					t.Fatal("UpdateMessageStatus() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("UpdateMessageStatus() error = %v", err)
			}
			if updated != tt.wantUpdated {
				t.Fatalf("UpdateMessageStatus() = %v, want %v", updated, tt.wantUpdated)
			}
			got, err := store.ListByParticipant(ctx, "profile-a", "+200")
			if err != nil {
				t.Fatalf("ListByParticipant() error = %v", err)
			}
			statuses := make(map[string]string)
			for _, msg := range got {
				statuses[msg.Source] = msg.Status
			}
			if statuses[MessageSourceRouted] != tt.wantStatus {
				t.Fatalf("routed message status = %q, want %q", statuses[MessageSourceRouted], tt.wantStatus)
			}
			if statuses[MessageSourceModem] != "sent" {
				t.Fatalf("modem status = %q, want sent", statuses[MessageSourceModem])
			}
			other, err := store.ListByParticipant(ctx, "profile-b", "+200")
			if err != nil {
				t.Fatalf("ListByParticipant(other) error = %v", err)
			}
			if len(other) != 1 || other[0].Status != "sent" {
				t.Fatalf("other profile messages = %+v, want untouched sent", other)
			}
		})
	}
}

func TestCallsPersistAndListByModem(t *testing.T) {
	ctx := t.Context()
	store := testStore(t)
	base := time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC)

	calls := []Call{
		{
			ID:        "call-old",
			ProfileID: "profile-a",
			ModemID:   "modem-1",
			Route:     "wifi_calling",
			Direction: "outgoing",
			Number:    "+12242255559",
			State:     "dialing",
			Hold:      "local",
			StartedAt: base,
			UpdatedAt: base,
		},
		{
			ID:         "call-new",
			ProfileID:  "profile-a",
			ModemID:    "modem-1",
			Route:      "modem",
			Direction:  "incoming",
			Number:     "+15551234567",
			State:      "ended",
			Reason:     "Busy Here",
			StartedAt:  base.Add(time.Minute),
			AnsweredAt: base.Add(2 * time.Minute),
			EndedAt:    base.Add(3 * time.Minute),
			UpdatedAt:  base.Add(3 * time.Minute),
		},
		{
			ID:        "call-other-profile",
			ProfileID: "profile-b",
			ModemID:   "modem-2",
			Route:     "wifi_calling",
			Direction: "outgoing",
			Number:    "+100",
			State:     "ended",
			StartedAt: base.Add(4 * time.Minute),
			UpdatedAt: base.Add(4 * time.Minute),
		},
		{
			ID:        "call-other-modem",
			ProfileID: "profile-a",
			ModemID:   "modem-2",
			Route:     "wifi_calling",
			Direction: "outgoing",
			Number:    "+101",
			State:     "ended",
			StartedAt: base.Add(5 * time.Minute),
			UpdatedAt: base.Add(5 * time.Minute),
		},
	}
	for _, call := range calls {
		if err := store.SaveCall(ctx, call); err != nil {
			t.Fatalf("SaveCall(%s) error = %v", call.ID, err)
		}
	}

	got, err := store.ListCalls(ctx, "profile-a", "modem-1", 10, "")
	if err != nil {
		t.Fatalf("ListCalls() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListCalls() len = %d, want 2", len(got))
	}
	if got[0].ID != "call-new" || got[1].ID != "call-old" {
		t.Fatalf("ListCalls() order = [%s %s], want [call-new call-old]", got[0].ID, got[1].ID)
	}
	if got[0].Route != "modem" || got[1].Route != "wifi_calling" {
		t.Fatalf("ListCalls() routes = [%s %s], want [modem wifi_calling]", got[0].Route, got[1].Route)
	}
	if got[1].Hold != "local" {
		t.Fatalf("ListCalls() hold = %q, want local", got[1].Hold)
	}

	calls[0].State = "active"
	calls[0].AnsweredAt = base.Add(30 * time.Second)
	calls[0].UpdatedAt = base.Add(5 * time.Minute)
	if err := store.SaveCall(ctx, calls[0]); err != nil {
		t.Fatalf("SaveCall(update) error = %v", err)
	}

	updated, err := store.Call(ctx, "call-old")
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if updated.State != "active" || updated.AnsweredAt.IsZero() {
		t.Fatalf("Call() = %+v, want active with answered_at", updated)
	}
	if updated.Hold != "local" {
		t.Fatalf("Call() hold = %q, want local", updated.Hold)
	}

	got, err = store.ListCalls(ctx, "profile-a", "modem-1", 1, "")
	if err != nil {
		t.Fatalf("ListCalls(limit) error = %v", err)
	}
	if len(got) != 1 || got[0].ID != "call-old" {
		t.Fatalf("ListCalls(limit) = %+v, want updated call-old first", got)
	}

	t.Run("call search", func(t *testing.T) {
		tests := []struct {
			name    string
			query   string
			wantIDs []string
		}{
			{name: "empty query", wantIDs: []string{"call-old", "call-new"}},
			{name: "formatted number", query: "(224) 225", wantIDs: []string{"call-old"}},
			{name: "plain digits", query: "555123", wantIDs: []string{"call-new"}},
			{name: "no match", query: "999", wantIDs: []string{}},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				calls, err := store.ListCalls(ctx, "profile-a", "modem-1", 10, tt.query)
				if err != nil {
					t.Fatalf("ListCalls() error = %v", err)
				}
				if len(calls) != len(tt.wantIDs) {
					t.Fatalf("ListCalls() length = %d, want %d", len(calls), len(tt.wantIDs))
				}
				for i, wantID := range tt.wantIDs {
					if calls[i].ID != wantID {
						t.Fatalf("ListCalls()[%d].ID = %q, want %q", i, calls[i].ID, wantID)
					}
				}
			})
		}
	})

	if err := store.DeleteCall(ctx, "profile-a", "modem-1", "call-old"); err != nil {
		t.Fatalf("DeleteCall() error = %v", err)
	}
	if _, err := store.Call(ctx, "call-old"); err == nil {
		t.Fatal("Call(deleted) error = nil, want not found")
	}
	if err := store.DeleteCall(ctx, "profile-a", "modem-1", "call-other-profile"); err == nil {
		t.Fatal("DeleteCall(other profile) error = nil, want not found")
	}
}

func TestSaveCallPreservesAnsweredAtOnSparseUpdates(t *testing.T) {
	ctx := t.Context()
	base := time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		update Call
		want   time.Time
	}{
		{
			name: "ended event keeps previous answer time",
			update: Call{
				ID:        "call-1",
				ProfileID: "profile-a",
				ModemID:   "modem-1",
				Route:     "wifi_calling",
				Direction: "outgoing",
				Number:    "+12242255559",
				State:     "ended",
				StartedAt: base,
				EndedAt:   base.Add(2 * time.Minute),
				UpdatedAt: base.Add(2 * time.Minute),
			},
			want: base.Add(30 * time.Second),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := testStore(t)
			answered := Call{
				ID:         "call-1",
				ProfileID:  "profile-a",
				ModemID:    "modem-1",
				Route:      "wifi_calling",
				Direction:  "outgoing",
				Number:     "+12242255559",
				State:      "active",
				StartedAt:  base,
				AnsweredAt: base.Add(30 * time.Second),
				UpdatedAt:  base.Add(30 * time.Second),
			}
			if err := store.SaveCall(ctx, answered); err != nil {
				t.Fatalf("SaveCall(answered) error = %v", err)
			}
			if err := store.SaveCall(ctx, tt.update); err != nil {
				t.Fatalf("SaveCall(update) error = %v", err)
			}

			got, err := store.Call(ctx, "call-1")
			if err != nil {
				t.Fatalf("Call() error = %v", err)
			}
			if !got.AnsweredAt.Equal(tt.want) {
				t.Fatalf("AnsweredAt = %v, want %v", got.AnsweredAt, tt.want)
			}
		})
	}
}

func TestSaveCallPreservingTerminal(t *testing.T) {
	ctx := t.Context()
	base := time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC)
	baseCall := Call{
		ID:        "call-1",
		ProfileID: "profile-a",
		ModemID:   "modem-1",
		Route:     "wifi_calling",
		Direction: "outgoing",
		Number:    "+12242255559",
		State:     "active",
		StartedAt: base,
		UpdatedAt: base,
	}
	tests := []struct {
		name        string
		existing    *Call
		update      Call
		wantState   string
		wantSaved   bool
		wantUpdated time.Time
	}{
		{
			name:        "inserts new call",
			update:      baseCall,
			wantState:   "active",
			wantSaved:   true,
			wantUpdated: base,
		},
		{
			name:     "active accepts ended",
			existing: callPtr(baseCall),
			update: callWithState(baseCall, "ended", func(call *Call) {
				call.EndedAt = base.Add(time.Minute)
				call.UpdatedAt = base.Add(time.Minute)
			}),
			wantState:   "ended",
			wantSaved:   true,
			wantUpdated: base.Add(time.Minute),
		},
		{
			name: "ended ignores active",
			existing: callPtr(callWithState(baseCall, "ended", func(call *Call) {
				call.EndedAt = base.Add(time.Minute)
				call.UpdatedAt = base.Add(time.Minute)
			})),
			update: callWithState(baseCall, "active", func(call *Call) {
				call.UpdatedAt = base.Add(2 * time.Minute)
			}),
			wantState:   "ended",
			wantSaved:   false,
			wantUpdated: base.Add(time.Minute),
		},
		{
			name: "failed accepts ended",
			existing: callPtr(callWithState(baseCall, "failed", func(call *Call) {
				call.EndedAt = base.Add(time.Minute)
				call.UpdatedAt = base.Add(time.Minute)
			})),
			update: callWithState(baseCall, "ended", func(call *Call) {
				call.EndedAt = base.Add(2 * time.Minute)
				call.UpdatedAt = base.Add(2 * time.Minute)
			}),
			wantState:   "ended",
			wantSaved:   true,
			wantUpdated: base.Add(2 * time.Minute),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := testStore(t)
			if tt.existing != nil {
				if err := store.SaveCall(ctx, *tt.existing); err != nil {
					t.Fatalf("SaveCall(existing) error = %v", err)
				}
			}

			got, saved, err := store.SaveCallPreservingTerminal(ctx, tt.update)
			if err != nil {
				t.Fatalf("SaveCallPreservingTerminal() error = %v", err)
			}
			if saved != tt.wantSaved {
				t.Fatalf("SaveCallPreservingTerminal() saved = %v, want %v", saved, tt.wantSaved)
			}
			if got.State != tt.wantState || !got.UpdatedAt.Equal(tt.wantUpdated) {
				t.Fatalf("SaveCallPreservingTerminal() = %+v, want state %q updated %v", got, tt.wantState, tt.wantUpdated)
			}
			stored, err := store.Call(ctx, tt.update.ID)
			if err != nil {
				t.Fatalf("Call() error = %v", err)
			}
			if stored.State != tt.wantState || !stored.UpdatedAt.Equal(tt.wantUpdated) {
				t.Fatalf("stored call = %+v, want state %q updated %v", stored, tt.wantState, tt.wantUpdated)
			}
		})
	}
}

func TestSaveCallPreservingTerminalPreservesAnsweredAtOnSparseUpdates(t *testing.T) {
	ctx := t.Context()
	store := testStore(t)
	base := time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC)
	answered := Call{
		ID:         "call-1",
		ProfileID:  "profile-a",
		ModemID:    "modem-1",
		Route:      "wifi_calling",
		Direction:  "outgoing",
		Number:     "+12242255559",
		State:      "active",
		StartedAt:  base,
		AnsweredAt: base.Add(30 * time.Second),
		UpdatedAt:  base.Add(30 * time.Second),
	}
	if err := store.SaveCall(ctx, answered); err != nil {
		t.Fatalf("SaveCall(answered) error = %v", err)
	}
	update := answered
	update.State = "ended"
	update.AnsweredAt = time.Time{}
	update.EndedAt = base.Add(2 * time.Minute)
	update.UpdatedAt = base.Add(2 * time.Minute)

	got, saved, err := store.SaveCallPreservingTerminal(ctx, update)
	if err != nil {
		t.Fatalf("SaveCallPreservingTerminal() error = %v", err)
	}
	if !saved {
		t.Fatal("SaveCallPreservingTerminal() saved = false, want true")
	}
	if !got.AnsweredAt.Equal(answered.AnsweredAt) {
		t.Fatalf("AnsweredAt = %v, want %v", got.AnsweredAt, answered.AnsweredAt)
	}
}

func callPtr(call Call) *Call {
	return &call
}

func callWithState(call Call, state string, update func(*Call)) Call {
	call.State = state
	if update != nil {
		update(&call)
	}
	return call
}

func testStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(t.Context(), filepath.Join(t.TempDir(), "sigmo.db"))
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
