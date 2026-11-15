package storage

import (
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	sqlite3driver "github.com/ncruces/go-sqlite3/driver"
)

func TestRemoveMessageDeduplicationPreservesDataAndRetries(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "messages.db")
	db, err := sqlite3driver.Open("file:" + path + "?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	// Apply the published migrations through version 11 to exercise real upgrades.
	provider, err := newMigrationProvider(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 11); err != nil {
		t.Fatal(err)
	}
	msg := Message{
		ProfileID: "sim-a", ModemID: "modem-a", Source: MessageSourceModem,
		ExternalKey: "sms-1", Sender: "10086", Recipient: "+100", Text: "hello",
		Incoming: true, Status: "received", Timestamp: time.Now(),
		ModemRefs: []ModemMessageRef{{ModemID: "modem-a", Generation: 7, Storage: 1, ID: 2}},
	}
	id, err := s.InsertMessage(ctx, msg)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateConversationRead(ctx, ReadUpdate{ConversationID: ConversationID(msg.ProfileID, msg.Sender), ReadThroughMessageID: &id}); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`UPDATE messages SET fingerprint = 'old-fingerprint', history_profile_id = 'sim-b'`,
		`INSERT INTO deleted_messages (fingerprint) VALUES ('deleted-fingerprint')`,
		// A deleted high ID must not be reused after rebuilding the table.
		`INSERT INTO messages (id, profile_id, source, external_key, sender, recipient, text, timestamp, status, incoming, wifi_calling, created_at, updated_at)
		 VALUES (80, 'sim-a', 'modem', 'deleted', '', '', '', '', '', 0, 0, '', '')`,
		`DELETE FROM messages WHERE id = 80`,
		`CREATE TRIGGER reject_message_migration BEFORE INSERT ON goose_db_version
		 WHEN NEW.version_id = 12 BEGIN SELECT RAISE(ABORT, 'injected migration failure'); END`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	beforeSchema := databaseSchema(t, s)
	beforeMessages, err := s.ListByParticipant(ctx, msg.ProfileID, msg.Sender)
	if err != nil {
		t.Fatal(err)
	}
	beforeThread, err := s.ThreadMessages(ctx, ThreadID(msg.Sender), HistoryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	beforeState, err := s.MessagesState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx); err == nil {
		t.Fatal("migration succeeded with a rejected version record")
	}
	if got := databaseSchema(t, s); !slices.Equal(got, beforeSchema) {
		t.Fatalf("failed migration changed schema: %v", got)
	}
	var applied int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM goose_db_version WHERE version_id = 12 AND is_applied`).Scan(&applied); err != nil || applied != 0 {
		t.Fatalf("failed migration version = %d, %v", applied, err)
	}
	if _, err := db.ExecContext(ctx, `DROP TRIGGER reject_message_migration`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		s, err = Open(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		messages, err := s.ListByParticipant(ctx, msg.ProfileID, msg.Sender)
		if err != nil || !reflect.DeepEqual(messages, beforeMessages) {
			t.Fatalf("migration changed messages or modem refs: %+v, %v", messages, err)
		}
		thread, err := s.ThreadMessages(ctx, ThreadID(msg.Sender), HistoryFilter{})
		if err != nil || !reflect.DeepEqual(thread, beforeThread) {
			t.Fatalf("migration changed history ownership or read state: %+v, %v", thread, err)
		}
		state, err := s.MessagesState(ctx)
		if err != nil || state != beforeState {
			t.Fatalf("migration changed message state: %+v, %v", state, err)
		}
	}
	fresh := testStore(t)
	if got, want := databaseSchema(t, s), databaseSchema(t, fresh); !slices.Equal(got, want) {
		t.Fatalf("upgraded schema differs from fresh schema:\ngot %v\nwant %v", got, want)
	}
	var violations int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&violations); err != nil || violations != 0 {
		t.Fatalf("foreign key violations after migration = %d, %v", violations, err)
	}
	var obsolete int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_schema WHERE name IN ('deleted_messages', 'idx_messages_fingerprint')`).Scan(&obsolete); err != nil || obsolete != 0 {
		t.Fatalf("obsolete deduplication objects = %d, %v", obsolete, err)
	}
	previousID := int64(80)
	for range 2 {
		id, err := s.InsertMessage(ctx, msg)
		if err != nil || id <= previousID {
			t.Fatalf("repeated SMS insert = %d, %v, want ID above %d", id, err, previousID)
		}
		previousID = id
	}
	messages, err := s.ListByParticipant(ctx, msg.ProfileID, msg.Sender)
	if err != nil || len(messages) != 3 {
		t.Fatalf("repeated SMS messages = %+v, %v", messages, err)
	}
}
