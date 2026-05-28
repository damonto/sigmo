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

	_ "github.com/ncruces/go-sqlite3/driver"
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
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(1)
	store := &Store{db: db}
	if err := store.Migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
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
		`CREATE TABLE IF NOT EXISTS calls (
			id TEXT PRIMARY KEY,
			profile_id TEXT NOT NULL,
			modem_id TEXT NOT NULL,
			route TEXT NOT NULL,
			direction TEXT NOT NULL,
			number TEXT NOT NULL,
			state TEXT NOT NULL,
			reason TEXT NOT NULL,
			started_at TEXT NOT NULL,
			answered_at TEXT NOT NULL,
			ended_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_calls_profile_modem_updated ON calls(profile_id, modem_id, updated_at)`,
	}
	for _, stmt := range statements {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migrate database: %w", err)
		}
	}
	if err := s.migrateMessageFingerprints(ctx); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS idx_messages_fingerprint ON messages(fingerprint) WHERE fingerprint <> ''`); err != nil {
		return fmt.Errorf("migrate message fingerprint index: %w", err)
	}
	return nil
}

func (s *Store) migrateMessageFingerprints(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("start message fingerprint migration: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	hasFingerprint, err := messageColumnExists(ctx, tx, "fingerprint")
	if err != nil {
		return err
	}
	if !hasFingerprint {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE messages ADD COLUMN fingerprint TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("add message fingerprint column: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit message fingerprint migration: %w", err)
	}
	committed = true
	return nil
}

func messageColumnExists(ctx context.Context, tx *sql.Tx, name string) (bool, error) {
	rows, err := tx.QueryContext(ctx, `PRAGMA table_info(messages)`)
	if err != nil {
		return false, fmt.Errorf("read message columns: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var columnName, columnType string
		var notNull, primaryKey int
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &columnName, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return false, fmt.Errorf("scan message column: %w", err)
		}
		if columnName == name {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("read message columns: %w", err)
	}
	return false, nil
}

func nowText() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}
