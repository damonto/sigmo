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
