package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var ErrNotFound = errors.New("storage key not found")

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
	now := nowText()
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO app_state (scope, key, value_json, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(scope, key) DO UPDATE SET
			value_json = excluded.value_json,
			updated_at = excluded.updated_at
	`, scope, key, string(data), now)
	if err != nil {
		return fmt.Errorf("save value: %w", err)
	}
	return nil
}

func (s *Store) Get(ctx context.Context, scope string, key string, dst any) error {
	scope = strings.TrimSpace(scope)
	key = strings.TrimSpace(key)
	if scope == "" {
		return errors.New("scope is required")
	}
	if key == "" {
		return errors.New("key is required")
	}
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT value_json FROM app_state WHERE scope = ? AND key = ?`, scope, key).Scan(&raw)
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

func (s *Store) Delete(ctx context.Context, scope string, key string) error {
	scope = strings.TrimSpace(scope)
	key = strings.TrimSpace(key)
	if scope == "" {
		return nil
	}
	if key == "" {
		_, err := s.db.ExecContext(ctx, `DELETE FROM app_state WHERE scope = ?`, scope)
		if err != nil {
			return fmt.Errorf("delete scope: %w", err)
		}
		return nil
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM app_state WHERE scope = ? AND key = ?`, scope, key)
	if err != nil {
		return fmt.Errorf("delete value: %w", err)
	}
	return nil
}

func (s *Store) ListRaw(ctx context.Context, scopePrefix string, key string) (map[string]string, error) {
	scopePrefix = strings.TrimSpace(scopePrefix)
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, errors.New("key is required")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT scope, value_json
		FROM app_state
		WHERE scope LIKE ? AND key = ?
	`, scopePrefix+"%", key)
	if err != nil {
		return nil, fmt.Errorf("list values: %w", err)
	}
	defer rows.Close()

	values := make(map[string]string)
	for rows.Next() {
		var scope, value string
		if err := rows.Scan(&scope, &value); err != nil {
			return nil, fmt.Errorf("scan value: %w", err)
		}
		values[scope] = value
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list values: %w", err)
	}
	return values, nil
}
