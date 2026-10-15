package storage

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	sqlite3driver "github.com/ncruces/go-sqlite3/driver"
)

func TestMigrationsAdoptLegacyDatabase(t *testing.T) {
	ctx := t.Context()
	db, err := sqlite3driver.Open(filepath.Join(t.TempDir(), "legacy.db"))
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
	schema, err := os.ReadFile("testdata/legacy.sql")
	if err != nil {
		t.Fatal(err)
	}
	// Execute separately, just as the historical startup code did.
	for _, statement := range strings.Split(string(schema), ";") {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	for _, statement := range []string{
		`INSERT INTO messages (id, profile_id, source, external_key, fingerprint, sender, recipient, text, timestamp, status, incoming, wifi_calling, created_at, updated_at)
		 VALUES (42, 'sim-a', 'modem', 'sms-1', 'fingerprint', '10086', '+8613800000000', 'old message', '2026-01-01T00:00:00Z', 'received', 1, 0, '', '')`,
		`INSERT INTO modem_message_refs (message_id, modem_id, storage, ref_id) VALUES (42, 'modem-a', 1, 2)`,
		`INSERT INTO reminders (profile_type, profile_id, modem_id, se_id, profile_name, next_at, repeat_days, content, created_at, updated_at, revision)
		 VALUES ('esim', 'sim-a', 'modem-a', 'se0', 'Travel', '2026-01-02T00:00:00.000000000Z', 7, 'renew', '', '', 4)`,
		`INSERT INTO push_subscriptions (id, endpoint, p256dh, auth, label, user_agent, platform, created_at, updated_at)
		 VALUES ('browser', 'https://push.example/device', 'key', 'auth', 'Phone', '', '', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	// Pin the historical identity independently of the current ConversationID helper.
	const conversationID = "1116e2736dc356acd250a3254717b3ed"
	conversation, err := s.Conversation(ctx, conversationID)
	if err != nil {
		t.Fatal(err)
	}
	if conversation.ReadThroughMessageID != 42 || conversation.UnreadCount != 0 || conversation.ModemID != "modem-a" || conversation.LineNumber != "+8613800000000" {
		t.Fatalf("legacy conversation = %+v", conversation)
	}
	reminder, found, err := s.Reminder(ctx, "esim", "sim-a")
	if err != nil || !found || reminder.Revision != 4 || reminder.RepeatDays == nil || *reminder.RepeatDays != 7 {
		t.Fatalf("legacy reminder = %+v, found = %v, error = %v", reminder, found, err)
	}
	subscriptions, err := s.ListPushSubscriptions(ctx)
	if err != nil || len(subscriptions) != 1 || subscriptions[0].ID != "browser" || subscriptions[0].P256DH != "key" {
		t.Fatalf("legacy subscriptions = %+v, error = %v", subscriptions, err)
	}
	fresh := testStore(t)
	if got, want := databaseSchema(t, s), databaseSchema(t, fresh); !slices.Equal(got, want) {
		t.Fatalf("upgraded schema differs from fresh schema:\ngot %v\nwant %v", got, want)
	}
}

func TestMigrationsRunOnce(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	before, err := s.MessagesState(ctx)
	if err != nil || before.BackendID == "" {
		t.Fatalf("initial state = %+v, error = %v", before, err)
	}
	// Re-running either Go migration would fire this trigger, even with INSERT OR IGNORE.
	if _, err := s.db.ExecContext(ctx, `CREATE TRIGGER reject_reinitialization BEFORE INSERT ON message_state
		BEGIN SELECT RAISE(ABORT, 'backend identity must not be reinitialized'); END`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
	}
	after, err := s.MessagesState(ctx)
	if err != nil || after != before {
		t.Fatalf("state after migration = %+v, want %+v, error = %v", after, before, err)
	}
	var versions int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM goose_db_version WHERE is_applied AND version_id > 0`).Scan(&versions); err != nil || versions != 10 {
		t.Fatalf("applied migrations = %d, want 10, error = %v", versions, err)
	}
}

func TestMigrationsRejectNewerDatabase(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	before, err := s.MessagesState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// A rolled-back binary must not open a schema written by a newer release.
	if _, err := s.db.ExecContext(ctx, `INSERT INTO goose_db_version (version_id, is_applied) VALUES (99999, 1)`); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx); err == nil {
		t.Fatal("migration accepted a database newer than the embedded migrations")
	}
	after, err := s.MessagesState(ctx)
	if err != nil || after != before {
		t.Fatalf("state after rejected migration = %+v, want %+v, error = %v", after, before, err)
	}
}

func TestMigrationsRollbackAndRetry(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	if _, err := s.InsertMessage(ctx, Message{
		ProfileID: "sim-a", ModemID: "modem-a", Source: MessageSourceModem,
		ExternalKey: "sms-1", Sender: "10086", Recipient: "+100", Text: "hello", Incoming: true, Timestamp: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	before, err := s.MessagesState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	removeCommunicationSchema(t, s)
	for _, statement := range []string{
		`DROP TABLE goose_db_version`,
		`ALTER TABLE sim_profiles RENAME TO message_lines`,
		`UPDATE message_lines SET number = ''`,
		`CREATE TRIGGER reject_migration BEFORE INSERT ON message_state
		 BEGIN SELECT RAISE(ABORT, 'injected migration failure'); END`,
	} {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Migrate(ctx); err == nil {
		t.Fatal("migration succeeded with an injected failure")
	}
	var versions int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM goose_db_version WHERE is_applied AND version_id > 0`).Scan(&versions); err != nil || versions != 0 {
		t.Fatalf("applied migrations after failure = %d, want 0, error = %v", versions, err)
	}
	var number string
	if err := s.db.QueryRowContext(ctx, `SELECT number FROM message_lines WHERE profile_id = 'sim-a'`).Scan(&number); err != nil || number != "" {
		t.Fatalf("rolled back table/number = %q, error = %v", number, err)
	}
	if _, err := s.db.ExecContext(ctx, `DROP TRIGGER reject_migration`); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	numbers, err := s.SIMProfileNumbers(ctx, []string{"sim-a"})
	if err != nil || numbers["sim-a"] != "+100" {
		t.Fatalf("retried backfill = %v, error = %v", numbers, err)
	}
	after, err := s.MessagesState(ctx)
	if err != nil || after != before {
		t.Fatalf("state after retry = %+v, want %+v, error = %v", after, before, err)
	}
}

// databaseSchema compares column order, foreign keys and indexes without depending
// on SQLite's formatting of CREATE TABLE statements rewritten by ALTER TABLE.
func databaseSchema(t *testing.T, s *Store) []string {
	t.Helper()
	rows, err := s.db.QueryContext(t.Context(), `
		SELECT json_array('column', m.name, p.cid, p.name, p.type, p."notnull", p.dflt_value, p.pk)
		FROM sqlite_schema m, pragma_table_info(m.name) p WHERE m.type = 'table'
		UNION ALL
		SELECT json_array('foreign_key', m.name, p.id, p.seq, p."table", p."from", p."to", p.on_update, p.on_delete)
		FROM sqlite_schema m, pragma_foreign_key_list(m.name) p WHERE m.type = 'table'
		UNION ALL
		SELECT json_array('index', m.name, p.name, p."unique", p.origin, p.partial, i.seqno, i.cid, i.name)
		FROM sqlite_schema m, pragma_index_list(m.name) p, pragma_index_info(p.name) i WHERE m.type = 'table'
		ORDER BY 1
	`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }() // Rows.Err reports cursor errors below.
	var schema []string
	for rows.Next() {
		var entry string
		if err := rows.Scan(&entry); err != nil {
			t.Fatal(err)
		}
		schema = append(schema, entry)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return schema
}

// removeCommunicationSchema recreates the pre-contacts schema used by legacy fixtures.
func removeCommunicationSchema(t *testing.T, s *Store) {
	t.Helper()
	// These tests simulate a complete historical schema, before authentication v2.
	for _, statement := range []string{
		`DROP INDEX idx_messages_conversation_time`,
		`ALTER TABLE sim_profiles DROP COLUMN icon`, `ALTER TABLE sim_profiles DROP COLUMN display_known`,
		`DROP TABLE auth_credentials`, `DROP TABLE auth_sessions`,
		`ALTER TABLE native_push_subscriptions RENAME COLUMN auth_session_id TO auth_token_hash`,
		`DELETE FROM app_state WHERE scope='global' AND key IN ('auth.settings','delivery.channels','notification.settings')`,
	} {
		if _, err := s.db.ExecContext(t.Context(), statement); err != nil {
			t.Fatal(err)
		}
	}
	for _, statement := range []string{
		`DROP TRIGGER messages_history_defaults`, `DROP TRIGGER calls_history_defaults`,
		`DROP INDEX idx_messages_history`, `DROP INDEX idx_calls_history`,
		`DROP INDEX idx_conversations_participant_key`, `ALTER TABLE conversations DROP COLUMN participant_key`,
		`ALTER TABLE messages DROP COLUMN history_profile_id`, `ALTER TABLE messages DROP COLUMN is_read`,
		`ALTER TABLE calls DROP COLUMN history_profile_id`,
		`ALTER TABLE sim_profiles DROP COLUMN region`, `ALTER TABLE sim_profiles DROP COLUMN kind`, `ALTER TABLE sim_profiles DROP COLUMN se_id`,
		`ALTER TABLE sim_profiles DROP COLUMN deletion_state`, `ALTER TABLE sim_profiles DROP COLUMN successor_id`,
		`DROP TABLE contact_numbers`, `DROP TABLE contacts`, `DROP TABLE communication_preferences`,
	} {
		if _, err := s.db.ExecContext(t.Context(), statement); err != nil {
			t.Fatal(err)
		}
	}
}
