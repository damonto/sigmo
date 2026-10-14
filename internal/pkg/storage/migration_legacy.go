package storage

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
)

// migrateLegacy upgrades unversioned installations before the SQL baseline is applied.
// Keep this historical schema and its backfills frozen; add new SQL migrations instead.
func migrateLegacy(ctx context.Context, tx *sql.Tx) error {
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE type = 'table' AND name = 'messages')`).Scan(&exists); err != nil {
		return fmt.Errorf("inspect legacy database: %w", err)
	}
	if !exists {
		return nil
	}
	statements := []string{
		`CREATE TABLE IF NOT EXISTS app_state (
			scope TEXT NOT NULL,
			key TEXT NOT NULL,
			value_json TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (scope, key)
		)`,
		`CREATE TABLE IF NOT EXISTS messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			profile_id TEXT NOT NULL,
			source TEXT NOT NULL,
			external_key TEXT NOT NULL,
			fingerprint TEXT NOT NULL DEFAULT '',
			sender TEXT NOT NULL,
			recipient TEXT NOT NULL,
			text TEXT NOT NULL,
			timestamp TEXT NOT NULL,
			status TEXT NOT NULL,
			incoming INTEGER NOT NULL,
			wifi_calling INTEGER NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE (profile_id, source, external_key)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_profile_timestamp ON messages(profile_id, timestamp)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_profile_participants ON messages(profile_id, sender, recipient)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_messages_fingerprint ON messages(fingerprint) WHERE fingerprint <> ''`,
		`CREATE TABLE IF NOT EXISTS modem_message_refs (
			message_id INTEGER NOT NULL,
			modem_id TEXT NOT NULL,
			generation INTEGER NOT NULL DEFAULT 0,
			storage INTEGER NOT NULL,
			ref_id INTEGER NOT NULL,
			PRIMARY KEY (message_id, modem_id, generation, storage, ref_id),
			FOREIGN KEY (message_id) REFERENCES messages(id) ON DELETE CASCADE
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_modem_message_ref ON modem_message_refs(modem_id, generation, storage, ref_id)`,
		`CREATE TABLE IF NOT EXISTS calls (
			id TEXT PRIMARY KEY,
			profile_id TEXT NOT NULL,
			modem_id TEXT NOT NULL,
			route TEXT NOT NULL,
			direction TEXT NOT NULL,
			number TEXT NOT NULL,
			state TEXT NOT NULL,
			hold_state TEXT NOT NULL DEFAULT 'none',
			reason TEXT NOT NULL,
			started_at TEXT NOT NULL,
			answered_at TEXT NOT NULL,
			ended_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_calls_profile_modem_updated ON calls(profile_id, modem_id, updated_at)`,
		`CREATE TABLE IF NOT EXISTS reminders (
			profile_type TEXT NOT NULL,
			profile_id TEXT NOT NULL,
			modem_id TEXT NOT NULL,
			se_id TEXT NOT NULL,
			profile_name TEXT NOT NULL,
			next_at TEXT NOT NULL,
			repeat_days INTEGER,
			content TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			revision INTEGER NOT NULL DEFAULT 1,
			PRIMARY KEY (profile_type, profile_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_reminders_next_at ON reminders(next_at)`,
		`CREATE TABLE IF NOT EXISTS push_subscriptions (
			id TEXT PRIMARY KEY,
			endpoint TEXT NOT NULL UNIQUE,
			p256dh TEXT NOT NULL,
			auth TEXT NOT NULL,
			label TEXT NOT NULL,
			user_agent TEXT NOT NULL,
			platform TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_push_subscriptions_updated_at ON push_subscriptions(updated_at)`,
		`CREATE TABLE IF NOT EXISTS auth_tokens (
			token_hash TEXT PRIMARY KEY,
			expires_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_auth_tokens_expires_at ON auth_tokens(expires_at)`,
		`CREATE TABLE IF NOT EXISTS mcp_api_keys (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			token_hash TEXT NOT NULL UNIQUE,
			token_hint TEXT NOT NULL,
			all_modems INTEGER NOT NULL,
			created_at INTEGER NOT NULL,
			expires_at INTEGER NOT NULL,
			revoked_at INTEGER
		)`,
		`CREATE INDEX IF NOT EXISTS idx_mcp_api_keys_expires_at ON mcp_api_keys(expires_at)`,
		`CREATE TABLE IF NOT EXISTS mcp_api_key_modems (
			key_id TEXT NOT NULL REFERENCES mcp_api_keys(id) ON DELETE CASCADE,
			modem_id TEXT NOT NULL,
			PRIMARY KEY (key_id, modem_id)
		)`,
		`CREATE TABLE IF NOT EXISTS mcp_api_key_permissions (
			key_id TEXT NOT NULL REFERENCES mcp_api_keys(id) ON DELETE CASCADE,
			permission TEXT NOT NULL,
			PRIMARY KEY (key_id, permission)
		)`,
		`CREATE TABLE IF NOT EXISTS mcp_audit_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			key_id TEXT NOT NULL,
			key_name TEXT NOT NULL,
			tool TEXT NOT NULL,
			modem_ids_json TEXT NOT NULL,
			outcome TEXT NOT NULL,
			error_code TEXT NOT NULL,
			duration_ms INTEGER NOT NULL,
			created_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_mcp_audit_events_created_at ON mcp_audit_events(created_at)`,
	}
	for _, stmt := range statements {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migrate database: %w", err)
		}
	}
	return migrateLegacyConversations(ctx, tx)
}

func migrateLegacyConversations(ctx context.Context, tx *sql.Tx) error {
	if err := migrateSIMProfiles(ctx, tx); err != nil {
		return err
	}
	statements := []string{
		`CREATE TABLE IF NOT EXISTS message_state (
			id INTEGER PRIMARY KEY CHECK(id = 1),
			backend_id TEXT NOT NULL,
			revision INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS conversations (
			id TEXT PRIMARY KEY,
			profile_id TEXT NOT NULL,
			participant TEXT NOT NULL,
			read_through_id INTEGER NOT NULL DEFAULT 0,
			marked_unread INTEGER NOT NULL DEFAULT 0,
			UNIQUE(profile_id, participant)
		)`,
		`CREATE TABLE IF NOT EXISTS deleted_messages (fingerprint TEXT PRIMARY KEY)`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("create conversation schema: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO message_state (id, backend_id) VALUES (1, ?)`, rand.Text()); err != nil {
		return fmt.Errorf("create backend identity: %w", err)
	}
	var migrated bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pragma_table_info('messages') WHERE name = 'conversation_id')`).Scan(&migrated); err != nil {
		return fmt.Errorf("inspect message schema: %w", err)
	}
	if !migrated {
		for _, statement := range []string{
			`ALTER TABLE messages ADD COLUMN conversation_id TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE messages ADD COLUMN modem_id TEXT NOT NULL DEFAULT ''`,
		} {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("extend message schema: %w", err)
			}
		}
		history, err := readHistoricalConversations(ctx, tx)
		if err != nil {
			return err
		}
		for _, c := range history {
			if _, err := tx.ExecContext(ctx, `INSERT INTO conversations (id, profile_id, participant, read_through_id) VALUES (?, ?, ?, ?)`, c.id, c.profileID, c.participant, c.readThroughID); err != nil {
				return fmt.Errorf("backfill conversation: %w", err)
			}
			if _, err := tx.ExecContext(ctx, `
				UPDATE messages
				SET conversation_id = ?, modem_id = COALESCE((
					SELECT CASE WHEN COUNT(DISTINCT modem_id) = 1 THEN MAX(modem_id) ELSE '' END
					FROM modem_message_refs WHERE message_id = messages.id
				), '')
				WHERE profile_id = ? AND CASE WHEN incoming = 1 THEN sender ELSE recipient END = ?
			`, c.id, c.profileID, c.participant); err != nil {
				return fmt.Errorf("backfill message identity: %w", err)
			}
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO sim_profiles (profile_id, modem_id)
			SELECT profile_id, CASE WHEN COUNT(DISTINCT NULLIF(modem_id, '')) = 1 THEN MAX(modem_id) ELSE '' END
			FROM messages GROUP BY profile_id
		`); err != nil {
			return fmt.Errorf("backfill SIM profiles: %w", err)
		}
		if err := backfillSIMProfileNumbers(ctx, tx); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO sim_profiles (profile_id, modem_id, label)
		SELECT profile_id, modem_id, profile_name FROM reminders WHERE modem_id <> ''
		ON CONFLICT(profile_id) DO UPDATE SET modem_id = excluded.modem_id, label = excluded.label WHERE sim_profiles.modem_id = ''`); err != nil {
		return fmt.Errorf("backfill reminder line ownership: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_messages_conversation ON messages(conversation_id, id)`); err != nil {
		return fmt.Errorf("index conversations: %w", err)
	}
	if err := migrateNativePush(ctx, tx); err != nil {
		return err
	}
	if err := migrateNativeCalls(ctx, tx); err != nil {
		return err
	}
	return nil
}

// legacyConversation freezes the fields consumed by the historical backfill.
type legacyConversation struct {
	id            string
	profileID     string
	participant   string
	readThroughID int64
}

// readHistoricalConversations closes the cursor before migration writes reuse
// the transaction's connection. Existing messages establish the read watermark.
func readHistoricalConversations(ctx context.Context, tx *sql.Tx) ([]legacyConversation, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT profile_id, CASE WHEN incoming = 1 THEN sender ELSE recipient END, MAX(id)
		FROM messages
		GROUP BY profile_id, CASE WHEN incoming = 1 THEN sender ELSE recipient END
	`)
	if err != nil {
		return nil, fmt.Errorf("read historical conversations: %w", err)
	}
	defer func() { _ = rows.Close() }() // Rows.Err reports iteration errors; preserve earlier scan errors.
	var history []legacyConversation
	for rows.Next() {
		var conversation legacyConversation
		if err := rows.Scan(&conversation.profileID, &conversation.participant, &conversation.readThroughID); err != nil {
			return nil, fmt.Errorf("scan historical conversation: %w", err)
		}
		// Keep the historical identity algorithm independent of future business code.
		sum := sha256.Sum256([]byte(strings.TrimSpace(conversation.profileID) + "\x00" + strings.TrimSpace(conversation.participant)))
		conversation.id = hex.EncodeToString(sum[:16])
		history = append(history, conversation)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read historical conversations: %w", err)
	}
	return history, nil
}

func migrateSIMProfiles(ctx context.Context, tx *sql.Tx) error {
	var legacy bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE type = 'table' AND name = 'message_lines')`).Scan(&legacy); err != nil {
		return fmt.Errorf("inspect SIM profile schema: %w", err)
	}
	if legacy {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE message_lines RENAME TO sim_profiles`); err != nil {
			return fmt.Errorf("rename SIM profile table: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS sim_profiles (
		profile_id TEXT PRIMARY KEY,
		modem_id TEXT NOT NULL DEFAULT '',
		label TEXT NOT NULL DEFAULT '',
		number TEXT NOT NULL DEFAULT ''
	)`); err != nil {
		return fmt.Errorf("create SIM profile schema: %w", err)
	}
	if legacy {
		return backfillSIMProfileNumbers(ctx, tx)
	}
	return nil
}

// backfillSIMProfileNumbers runs only when upgrading a legacy schema. Existing
// observations take precedence over SMS, which may predate a number change.
func backfillSIMProfileNumbers(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `UPDATE sim_profiles SET number = COALESCE((
		SELECT TRIM(CASE WHEN incoming = 1 THEN recipient ELSE sender END)
		FROM messages WHERE profile_id = sim_profiles.profile_id
			AND TRIM(CASE WHEN incoming = 1 THEN recipient ELSE sender END) <> ''
		ORDER BY timestamp DESC, id DESC LIMIT 1
	), '') WHERE number = ''`)
	if err != nil {
		return fmt.Errorf("backfill SIM profile numbers: %w", err)
	}
	return nil
}

func migrateNativePush(ctx context.Context, tx *sql.Tx) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS native_push_subscriptions (
			id TEXT PRIMARY KEY,
			installation_id TEXT NOT NULL UNIQUE,
			label TEXT NOT NULL,
			backend_name TEXT NOT NULL,
			send_token TEXT NOT NULL,
			encryption_key BLOB NOT NULL,
			auth_token_hash TEXT NOT NULL,
			sms INTEGER NOT NULL,
			reminders INTEGER NOT NULL,
			enabled INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS native_push_outbox (
			subscription_id TEXT NOT NULL REFERENCES native_push_subscriptions(id) ON DELETE CASCADE,
			event_id TEXT NOT NULL,
			revision INTEGER NOT NULL,
			content_json TEXT NOT NULL,
			attempts INTEGER NOT NULL DEFAULT 0,
			next_at INTEGER NOT NULL,
			expires_at INTEGER NOT NULL,
			PRIMARY KEY(subscription_id, event_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_native_push_due ON native_push_outbox(next_at)`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("create native push schema: %w", err)
		}
	}
	return nil
}

func migrateNativeCalls(ctx context.Context, tx *sql.Tx) error {
	for _, column := range []struct{ table, name, definition string }{
		{"calls", "owner_client_id", "TEXT NOT NULL DEFAULT ''"},
		{"native_push_subscriptions", "calls", "INTEGER NOT NULL DEFAULT 0"},
	} {
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pragma_table_info(?) WHERE name = ?)`, column.table, column.name).Scan(&exists); err != nil {
			return fmt.Errorf("inspect call schema: %w", err)
		}
		if !exists {
			// All identifiers and definitions are fixed above, never request data.
			if _, err := tx.ExecContext(ctx, "ALTER TABLE "+column.table+" ADD COLUMN "+column.name+" "+column.definition); err != nil {
				return fmt.Errorf("extend call schema: %w", err)
			}
		}
	}
	_, err := tx.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS native_call_outbox (
			subscription_id TEXT NOT NULL REFERENCES native_push_subscriptions(id) ON DELETE CASCADE,
			call_id TEXT NOT NULL REFERENCES calls(id) ON DELETE CASCADE,
			delivered INTEGER NOT NULL DEFAULT 0,
			next_at INTEGER NOT NULL,
			expires_at INTEGER NOT NULL,
			PRIMARY KEY(subscription_id, call_id)
		)
	`)
	if err != nil {
		return fmt.Errorf("create call outbox: %w", err)
	}
	return nil
}
