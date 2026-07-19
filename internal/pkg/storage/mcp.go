package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

type MCPAPIKey struct {
	ID          string
	Name        string
	TokenHash   string
	TokenHint   string
	AllModems   bool
	ModemIDs    []string
	Permissions []string
	CreatedAt   time.Time
	ExpiresAt   time.Time
	RevokedAt   *time.Time
}

type MCPAuditEvent struct {
	ID        int64
	KeyID     string
	KeyName   string
	Tool      string
	ModemIDs  []string
	Outcome   string
	ErrorCode string
	Duration  time.Duration
	CreatedAt time.Time
}

func (s *Store) CreateMCPAPIKey(ctx context.Context, key MCPAPIKey) error {
	if strings.TrimSpace(key.ID) == "" || strings.TrimSpace(key.TokenHash) == "" {
		return errors.New("MCP API key id and token hash are required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("start MCP API key transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO mcp_api_keys (id, name, token_hash, token_hint, all_modems, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, key.ID, key.Name, key.TokenHash, key.TokenHint, key.AllModems, key.CreatedAt.UnixMilli(), key.ExpiresAt.UnixMilli()); err != nil {
		return fmt.Errorf("create MCP API key: %w", err)
	}
	for _, modemID := range key.ModemIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO mcp_api_key_modems (key_id, modem_id) VALUES (?, ?)`, key.ID, modemID); err != nil {
			return fmt.Errorf("grant MCP API key modem: %w", err)
		}
	}
	for _, permission := range key.Permissions {
		if _, err := tx.ExecContext(ctx, `INSERT INTO mcp_api_key_permissions (key_id, permission) VALUES (?, ?)`, key.ID, permission); err != nil {
			return fmt.Errorf("grant MCP API key permission: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit MCP API key transaction: %w", err)
	}
	committed = true
	return nil
}

func (s *Store) ListMCPAPIKeys(ctx context.Context) ([]MCPAPIKey, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, token_hash, token_hint, all_modems, created_at, expires_at, revoked_at
		FROM mcp_api_keys ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("list MCP API keys: %w", err)
	}
	var keys []MCPAPIKey
	for rows.Next() {
		key, err := scanMCPAPIKey(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("list MCP API keys: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close MCP API key rows: %w", err)
	}
	for i := range keys {
		if err := s.loadMCPAPIKeyGrants(ctx, &keys[i]); err != nil {
			return nil, err
		}
	}
	return keys, nil
}

func (s *Store) MCPAPIKeyByHash(ctx context.Context, tokenHash string) (MCPAPIKey, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, token_hash, token_hint, all_modems, created_at, expires_at, revoked_at
		FROM mcp_api_keys WHERE token_hash = ?
	`, strings.TrimSpace(tokenHash))
	key, err := scanMCPAPIKey(row)
	if errors.Is(err, sql.ErrNoRows) {
		return MCPAPIKey{}, ErrNotFound
	}
	if err != nil {
		return MCPAPIKey{}, err
	}
	if err := s.loadMCPAPIKeyGrants(ctx, &key); err != nil {
		return MCPAPIKey{}, err
	}
	return key, nil
}

func (s *Store) RevokeMCPAPIKey(ctx context.Context, id string, at time.Time) (bool, error) {
	result, err := s.db.ExecContext(ctx, `
		UPDATE mcp_api_keys SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL
	`, at.UnixMilli(), strings.TrimSpace(id))
	if err != nil {
		return false, fmt.Errorf("revoke MCP API key: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read revoked MCP API key count: %w", err)
	}
	return rows > 0, nil
}

func (s *Store) loadMCPAPIKeyGrants(ctx context.Context, key *MCPAPIKey) error {
	modemIDs, err := s.listMCPGrantValues(ctx, `SELECT modem_id FROM mcp_api_key_modems WHERE key_id = ? ORDER BY modem_id`, key.ID)
	if err != nil {
		return fmt.Errorf("list MCP API key modems: %w", err)
	}
	permissions, err := s.listMCPGrantValues(ctx, `SELECT permission FROM mcp_api_key_permissions WHERE key_id = ? ORDER BY permission`, key.ID)
	if err != nil {
		return fmt.Errorf("list MCP API key permissions: %w", err)
	}
	key.ModemIDs = modemIDs
	key.Permissions = permissions
	return nil
}

func (s *Store) listMCPGrantValues(ctx context.Context, query string, keyID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, query, keyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]string, 0)
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

type mcpAPIKeyScanner interface {
	Scan(...any) error
}

func scanMCPAPIKey(row mcpAPIKeyScanner) (MCPAPIKey, error) {
	var key MCPAPIKey
	var allModems bool
	var createdAt, expiresAt int64
	var revokedAt sql.NullInt64
	if err := row.Scan(&key.ID, &key.Name, &key.TokenHash, &key.TokenHint, &allModems, &createdAt, &expiresAt, &revokedAt); err != nil {
		return MCPAPIKey{}, err
	}
	key.AllModems = allModems
	key.CreatedAt = time.UnixMilli(createdAt).UTC()
	key.ExpiresAt = time.UnixMilli(expiresAt).UTC()
	if revokedAt.Valid {
		at := time.UnixMilli(revokedAt.Int64).UTC()
		key.RevokedAt = &at
	}
	return key, nil
}

func (s *Store) CreateMCPAuditEvent(ctx context.Context, event MCPAuditEvent, cutoff time.Time) error {
	modemIDs := slices.Clone(event.ModemIDs)
	slices.Sort(modemIDs)
	data, err := json.Marshal(modemIDs)
	if err != nil {
		return fmt.Errorf("encode MCP audit modem ids: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("start MCP audit transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if _, err := tx.ExecContext(ctx, `DELETE FROM mcp_audit_events WHERE created_at < ?`, cutoff.UnixMilli()); err != nil {
		return fmt.Errorf("prune MCP audit events: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO mcp_audit_events (key_id, key_name, tool, modem_ids_json, outcome, error_code, duration_ms, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, event.KeyID, event.KeyName, event.Tool, string(data), event.Outcome, event.ErrorCode, event.Duration.Milliseconds(), event.CreatedAt.UnixMilli()); err != nil {
		return fmt.Errorf("create MCP audit event: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit MCP audit transaction: %w", err)
	}
	committed = true
	return nil
}

func (s *Store) ListMCPAuditEvents(ctx context.Context, before int64, limit int) ([]MCPAuditEvent, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	query := `
		SELECT id, key_id, key_name, tool, modem_ids_json, outcome, error_code, duration_ms, created_at
		FROM mcp_audit_events
	`
	args := []any{}
	if before > 0 {
		query += ` WHERE id < ?`
		args = append(args, before)
	}
	query += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list MCP audit events: %w", err)
	}
	defer rows.Close()
	var events []MCPAuditEvent
	for rows.Next() {
		var event MCPAuditEvent
		var modemIDs string
		var durationMS, createdAt int64
		if err := rows.Scan(&event.ID, &event.KeyID, &event.KeyName, &event.Tool, &modemIDs, &event.Outcome, &event.ErrorCode, &durationMS, &createdAt); err != nil {
			return nil, fmt.Errorf("scan MCP audit event: %w", err)
		}
		if err := json.Unmarshal([]byte(modemIDs), &event.ModemIDs); err != nil {
			return nil, fmt.Errorf("decode MCP audit modem ids: %w", err)
		}
		event.Duration = time.Duration(durationMS) * time.Millisecond
		event.CreatedAt = time.UnixMilli(createdAt).UTC()
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list MCP audit events: %w", err)
	}
	return events, nil
}
