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
	"sync"
	"time"

	sqlite3driver "github.com/ncruces/go-sqlite3/driver"
)

// Store owns the local database and coordinates storage transactions.
type Store struct {
	db *sql.DB

	// Card reads outlive database transactions. The revision prevents a read
	// started before a nickname change from restoring invalidated branding.
	simDisplayMu       sync.RWMutex
	simDisplayRevision uint64
}

// Open upgrades the database before returning a usable store.
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

// Close releases the database connection.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func nowText() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}
