package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	sqlite3driver "github.com/ncruces/go-sqlite3/driver"
)

type Store struct {
	db *sql.DB
}

func Open(ctx context.Context, path string) (*Store, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("database path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}

	dsn := (&url.URL{Scheme: "file", Path: path}).String() + "?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	db, err := sqlite3driver.Open(dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(1)
	store := &Store{db: db}
	if err := store.Migrate(ctx); err != nil {
		_ = db.Close() // The migration error is the useful startup failure.
		return nil, err
	}
	if err := protectDatabaseFiles(path); err != nil {
		_ = db.Close() // The permission error is the useful startup failure.
		return nil, err
	}
	return store, nil
}

func protectDatabaseFiles(path string) error {
	for _, candidate := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Chmod(candidate, 0o600); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("protect database file %s: %w", filepath.Base(candidate), err)
		}
	}
	return nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) Migrate(ctx context.Context) error {
	if s == nil || s.db == nil {
		return errors.New("storage is nil")
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
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migrate database: %w", err)
		}
	}
	return nil
}

func nowText() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}
