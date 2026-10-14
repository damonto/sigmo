package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/damonto/sigmo/internal/pkg/storage/query"
)

// ErrNotFound indicates that a requested storage record does not exist.
var ErrNotFound = errors.New("storage key not found")

// PutBatch commits related JSON settings together so readers never load a partial policy.
func (s *Store) PutBatch(ctx context.Context, scope string, values map[string]json.RawMessage) error {
	if strings.TrimSpace(scope) == "" {
		return errors.New("scope is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin settings transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // A committed transaction makes rollback a no-op.
	queries := query.New(tx)
	for key, value := range values {
		if key == "" || !json.Valid(value) {
			return errors.New("invalid settings entry")
		}
		if err := queries.SaveAppState(ctx, query.SaveAppStateParams{Scope: scope, Key: key, ValueJSON: string(value), UpdatedAt: nowText()}); err != nil {
			return fmt.Errorf("save setting %s: %w", key, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit settings: %w", err)
	}
	return nil
}

// Put stores a JSON value under the given scope and key.
func (s *Store) Put(ctx context.Context, scope string, key string, value any) error {
	scope = strings.TrimSpace(scope)
	key = strings.TrimSpace(key)
	if scope == "" {
		return errors.New("scope is required")
	}
	if key == "" {
		return errors.New("key is required")
	}
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode value: %w", err)
	}
	err = query.New(s.db).SaveAppState(ctx, query.SaveAppStateParams{
		Scope: scope, Key: key, ValueJSON: string(data), UpdatedAt: nowText(),
	})
	if err != nil {
		return fmt.Errorf("save value: %w", err)
	}
	return nil
}

// Get decodes a stored JSON value into dst, returning ErrNotFound for an absent key.
func (s *Store) Get(ctx context.Context, scope string, key string, dst any) error {
	scope = strings.TrimSpace(scope)
	key = strings.TrimSpace(key)
	if scope == "" {
		return errors.New("scope is required")
	}
	if key == "" {
		return errors.New("key is required")
	}
	raw, err := query.New(s.db).AppStateValue(ctx, query.AppStateValueParams{Scope: scope, Key: key})
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("read value: %w", err)
	}
	if err := json.Unmarshal([]byte(raw), dst); err != nil {
		return fmt.Errorf("decode value: %w", err)
	}
	return nil
}

// Delete removes a key, or its whole scope when key is empty.
func (s *Store) Delete(ctx context.Context, scope string, key string) error {
	scope = strings.TrimSpace(scope)
	key = strings.TrimSpace(key)
	if scope == "" {
		return nil
	}
	if key == "" {
		err := query.New(s.db).DeleteAppStateScope(ctx, scope)
		if err != nil {
			return fmt.Errorf("delete scope: %w", err)
		}
		return nil
	}
	err := query.New(s.db).DeleteAppStateValue(ctx, query.DeleteAppStateValueParams{Scope: scope, Key: key})
	if err != nil {
		return fmt.Errorf("delete value: %w", err)
	}
	return nil
}

// ListRaw returns JSON values for key in scopes matching the given LIKE prefix.
func (s *Store) ListRaw(ctx context.Context, scopePrefix string, key string) (map[string]string, error) {
	scopePrefix = strings.TrimSpace(scopePrefix)
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, errors.New("key is required")
	}
	rows, err := query.New(s.db).ListAppStateValues(ctx, query.ListAppStateValuesParams{
		Scope: scopePrefix + "%", Key: key,
	})
	if err != nil {
		return nil, fmt.Errorf("list values: %w", err)
	}
	values := make(map[string]string, len(rows))
	for _, row := range rows {
		values[row.Scope] = row.ValueJSON
	}
	return values, nil
}
