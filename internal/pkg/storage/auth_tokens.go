package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

func (s *Store) CreateAuthToken(ctx context.Context, tokenHash string, expiresAt time.Time) error {
	tokenHash = strings.TrimSpace(tokenHash)
	if tokenHash == "" {
		return errors.New("token hash is required")
	}
	if expiresAt.IsZero() {
		return errors.New("token expiration is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("start auth token transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if _, err := tx.ExecContext(ctx, `DELETE FROM auth_tokens WHERE expires_at <= ?`, time.Now().UnixMilli()); err != nil {
		return fmt.Errorf("delete expired auth tokens: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO auth_tokens (token_hash, expires_at) VALUES (?, ?)`, tokenHash, expiresAt.UnixMilli()); err != nil {
		return fmt.Errorf("create auth token: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit auth token transaction: %w", err)
	}
	committed = true
	return nil
}

func (s *Store) AuthTokenValid(ctx context.Context, tokenHash string, now time.Time) (bool, error) {
	tokenHash = strings.TrimSpace(tokenHash)
	if tokenHash == "" {
		return false, nil
	}

	var valid bool
	if err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM auth_tokens WHERE token_hash = ? AND expires_at > ?
		)
	`, tokenHash, now.UnixMilli()).Scan(&valid); err != nil {
		return false, fmt.Errorf("validate auth token: %w", err)
	}
	return valid, nil
}
