package storage

import (
	"maps"
	"path/filepath"
	"testing"
	"time"
)

func TestSIMProfileMigrationPreservesHistory(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "history.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	msg := Message{ProfileID: "sim-a", ModemID: "modem", Source: MessageSourceModem, ExternalKey: "legacy", Sender: "10086", Recipient: "+8613800000000", Text: "hello", Incoming: true, Timestamp: time.Now()}
	if _, err := s.InsertMessage(ctx, msg); err != nil {
		t.Fatal(err)
	}
	const currentNumber = "+8613900000000"
	if err := s.RecordSIMProfile(ctx, SIMProfile{ProfileID: "sim-a", ModemID: "modem", Label: "Travel", Number: currentNumber}); err != nil {
		t.Fatal(err)
	}
	before, err := s.MessagesState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	removeCommunicationSchema(t, s)
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE sim_profiles RENAME TO message_lines`); err != nil {
		t.Fatal(err)
	}
	// Historical releases had no migration ledger.
	if _, err := s.db.ExecContext(ctx, `DROP TABLE goose_db_version`); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
		numbers, err := s.SIMProfileNumbers(ctx, []string{"sim-a", "missing"})
		if err != nil || !maps.Equal(numbers, map[string]string{"sim-a": currentNumber}) {
			t.Fatalf("migrated numbers = %v, error = %v", numbers, err)
		}
		conversation, err := s.Conversation(ctx, ConversationID("sim-a", msg.Sender))
		if err != nil || conversation.LineLabel != "Travel" || conversation.LineNumber != currentNumber || conversation.ModemID != "modem" {
			t.Fatalf("migrated conversation = %+v, error = %v", conversation, err)
		}
		after, err := s.MessagesState(ctx)
		if err != nil || after != before {
			t.Fatalf("message state = %+v, want %+v, error = %v", after, before, err)
		}
		var legacy bool
		if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name = 'message_lines')`).Scan(&legacy); err != nil || legacy {
			t.Fatalf("legacy table exists = %v, error = %v", legacy, err)
		}
	}
	if err := s.DeleteConversation(ctx, ConversationID("sim-a", msg.Sender)); err != nil {
		t.Fatal(err)
	}
	numbers, err := s.SIMProfileNumbers(ctx, []string{"sim-a"})
	if err != nil || numbers["sim-a"] != currentNumber {
		t.Fatalf("numbers after deleting messages = %v, error = %v", numbers, err)
	}
}

func TestRecordSIMProfile(t *testing.T) {
	tests := []struct {
		name   string
		update SIMProfile
		want   string
	}{
		{name: "replace number", update: SIMProfile{ProfileID: "sim-a", ModemID: "modem", Number: "+200"}, want: "+200"},
		{name: "empty retains number", update: SIMProfile{ProfileID: "sim-a", ModemID: "modem"}, want: "+100"},
		{name: "whitespace retains number", update: SIMProfile{ProfileID: "sim-a", ModemID: "modem", Number: " "}, want: "+100"},
		{name: "trim identity and number", update: SIMProfile{ProfileID: " sim-a ", ModemID: " modem ", Number: " +200 "}, want: "+200"},
		{name: "other SIM is independent", update: SIMProfile{ProfileID: "sim-b", ModemID: "modem", Number: "+200"}, want: "+100"},
		{name: "missing identity is ignored", update: SIMProfile{ModemID: "modem", Number: "+200"}, want: "+100"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := testStore(t)
			ctx := t.Context()
			if err := s.RecordSIMProfile(ctx, SIMProfile{ProfileID: "sim-a", ModemID: "modem", Label: "Travel", Number: "+100"}); err != nil {
				t.Fatal(err)
			}
			if err := s.RecordSIMProfile(ctx, tt.update); err != nil {
				t.Fatal(err)
			}
			numbers, err := s.SIMProfileNumbers(ctx, []string{"sim-a"})
			if err != nil || !maps.Equal(numbers, map[string]string{"sim-a": tt.want}) {
				t.Fatalf("numbers = %v, error = %v", numbers, err)
			}
			empty, err := s.SIMProfileNumbers(ctx, nil)
			if err != nil || len(empty) != 0 {
				t.Fatalf("empty query = %v, error = %v", empty, err)
			}
		})
	}
}

func TestSIMDisplayRejectsReadsStartedBeforeInvalidation(t *testing.T) {
	for _, refreshFirst := range []bool{false, true} {
		name := "cache remains invalid"
		if refreshFirst {
			name = "new branding is preserved"
		}
		t.Run(name, func(t *testing.T) {
			s := testStore(t)
			ctx := t.Context()
			stale := SIMProfile{
				ProfileID: "sim-a", ModemID: "modem", Label: "Old", Icon: "old-icon",
				DisplayKnown: true, DisplayRevision: s.SIMDisplayRevision(),
			}
			if err := s.RecordSIMProfile(ctx, stale); err != nil {
				t.Fatal(err)
			}
			if err := s.InvalidateSIMDisplay(ctx, "sim-a"); err != nil {
				t.Fatal(err)
			}
			if refreshFirst {
				fresh := stale
				fresh.DisplayRevision = s.SIMDisplayRevision()
				fresh.Label, fresh.Icon = "New", "new-icon"
				if err := s.RecordSIMProfile(ctx, fresh); err != nil {
					t.Fatal(err)
				}
			}
			stale.Number = "+100"
			if err := s.RecordSIMProfile(ctx, stale); err != nil {
				t.Fatal(err)
			}
			display, found, err := s.ReadSIMDisplay(ctx, "sim-a")
			if err != nil || found != refreshFirst {
				t.Fatalf("ReadSIMDisplay() = %+v, %v, %v", display, found, err)
			}
			if found && (display.Label != "New" || display.Icon != "new-icon") {
				t.Fatalf("stale read replaced branding: %+v", display)
			}
			numbers, err := s.SIMProfileNumbers(ctx, []string{"sim-a"})
			if err != nil || numbers["sim-a"] != "+100" {
				t.Fatalf("identity metadata was lost: %v, %v", numbers, err)
			}
		})
	}
}

func TestSIMProfileMigrationBackfillsMissingNumbers(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	now := time.Now()
	for _, msg := range []Message{
		{ProfileID: "incoming", ExternalKey: "new", Sender: "+999", Recipient: "+100", Incoming: true, Timestamp: now},
		{ProfileID: "incoming", ExternalKey: "old", Sender: "+999", Recipient: "+101", Incoming: true, Timestamp: now.Add(-time.Hour)},
		{ProfileID: "outgoing", ExternalKey: "old", Sender: "+201", Recipient: "+999", Timestamp: now.Add(-time.Hour)},
		{ProfileID: "outgoing", ExternalKey: "new", Sender: "+200", Recipient: "+999", Timestamp: now},
		{ProfileID: "outgoing", ExternalKey: "empty", Recipient: "+999", Timestamp: now.Add(time.Hour)},
	} {
		msg.Source, msg.ModemID, msg.Text = MessageSourceModem, "modem", msg.ExternalKey
		if _, err := s.InsertMessage(ctx, msg); err != nil {
			t.Fatal(err)
		}
	}
	removeCommunicationSchema(t, s)
	for _, statement := range []string{
		`DROP TABLE goose_db_version`, // Historical releases had no migration ledger.
		`UPDATE sim_profiles SET number = ''`,
		`ALTER TABLE sim_profiles RENAME TO message_lines`,
	} {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	numbers, err := s.SIMProfileNumbers(ctx, []string{"incoming", "outgoing"})
	if err != nil || !maps.Equal(numbers, map[string]string{"incoming": "+100", "outgoing": "+200"}) {
		t.Fatalf("backfilled numbers = %v, error = %v", numbers, err)
	}
}

func TestHistoricalMessageNumbersOnlyFillMissingProfiles(t *testing.T) {
	for _, incoming := range []bool{true, false} {
		name := "outgoing"
		if incoming {
			name = "incoming"
		}
		t.Run(name, func(t *testing.T) {
			s := testStore(t)
			ctx := t.Context()
			if err := s.RecordSIMProfile(ctx, SIMProfile{ProfileID: "known", ModemID: "modem", Number: "+200"}); err != nil {
				t.Fatal(err)
			}
			for _, profileID := range []string{"known", "unknown"} {
				msg := Message{ProfileID: profileID, ModemID: "modem", Source: MessageSourceModem, ExternalKey: profileID, Sender: "+100", Recipient: "+300", Incoming: incoming, Text: "old message", Timestamp: time.Now().Add(-time.Hour)}
				if incoming {
					msg.Sender, msg.Recipient = msg.Recipient, msg.Sender
				}
				if _, err := s.InsertMessage(ctx, msg); err != nil {
					t.Fatal(err)
				}
			}
			numbers, err := s.SIMProfileNumbers(ctx, []string{"known", "unknown"})
			if err != nil || !maps.Equal(numbers, map[string]string{"known": "+200", "unknown": "+100"}) {
				t.Fatalf("numbers = %v, error = %v", numbers, err)
			}
		})
	}
}
