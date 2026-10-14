package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/damonto/sigmo/internal/pkg/storage/query"
)

// ErrInvalidSession covers expired, revoked, consumed, and unknown credentials.
var ErrInvalidSession = errors.New("invalid session")

// AuthSession is a revocable identity bound to the policy that admitted it.
type AuthSession struct {
	ID        string
	Method    string
	Issuer    string
	Subject   string
	PolicyID  string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// AuthCredential contains a token digest; raw bearer credentials never enter storage.
type AuthCredential struct {
	Hash      string
	Kind      string
	ExpiresAt time.Time
}

// CreateAuthSession persists a session and its initial credentials atomically.
func (s *Store) CreateAuthSession(ctx context.Context, session AuthSession, credentials []AuthCredential) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin session: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // Commit makes this cleanup a no-op.
	if err := query.New(tx).DeleteExpiredAuthSessions(ctx, time.Now().UnixMilli()); err != nil {
		return fmt.Errorf("expire sessions: %w", err)
	}
	if err := query.New(tx).CreateAuthSession(ctx, query.CreateAuthSessionParams{ID: session.ID, Method: session.Method, Issuer: session.Issuer, Subject: session.Subject, PolicyID: session.PolicyID, CreatedAt: session.CreatedAt.UnixMilli(), ExpiresAt: session.ExpiresAt.UnixMilli()}); err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	if err := insertCredentials(ctx, tx, session.ID, credentials); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit session: %w", err)
	}
	return nil
}

// AuthSessionByCredential verifies a credential and its parent session together.
func (s *Store) AuthSessionByCredential(ctx context.Context, hash, kind string) (AuthSession, error) {
	row, err := query.New(s.db).AuthSessionByCredential(ctx, query.AuthSessionByCredentialParams{TokenHash: hash, Kind: kind, Now: time.Now().UnixMilli()})
	if errors.Is(err, sql.ErrNoRows) {
		return AuthSession{}, ErrInvalidSession
	}
	if err != nil {
		return AuthSession{}, fmt.Errorf("read session: %w", err)
	}
	return AuthSession{ID: row.ID, Method: row.Method, Issuer: row.Issuer, Subject: row.Subject, PolicyID: row.PolicyID, CreatedAt: time.UnixMilli(row.CreatedAt), ExpiresAt: time.UnixMilli(row.ExpiresAt)}, nil
}

// RotateAuthCredentials consumes a refresh token once. Reuse revokes its whole session.
func (s *Store) RotateAuthCredentials(ctx context.Context, hash string, credentials []AuthCredential) (AuthSession, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AuthSession{}, fmt.Errorf("begin refresh: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // All early returns roll back partially issued credentials.
	row, err := query.New(tx).AuthRefreshCredential(ctx, hash)
	session := AuthSession{ID: row.ID, Method: row.Method, Issuer: row.Issuer, Subject: row.Subject, PolicyID: row.PolicyID, CreatedAt: time.UnixMilli(row.CreatedAt), ExpiresAt: time.UnixMilli(row.ExpiresAt)}
	if errors.Is(err, sql.ErrNoRows) {
		return AuthSession{}, ErrInvalidSession
	}
	if err != nil {
		return AuthSession{}, fmt.Errorf("read refresh credential: %w", err)
	}
	now := time.Now().UnixMilli()
	if row.ConsumedAt != 0 {
		if err := query.New(tx).RevokeAuthSession(ctx, query.RevokeAuthSessionParams{ID: session.ID, RevokedAt: now}); err != nil {
			return AuthSession{}, fmt.Errorf("revoke reused refresh token: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return AuthSession{}, fmt.Errorf("commit revocation: %w", err)
		}
		return AuthSession{}, ErrInvalidSession
	}
	if row.RevokedAt != 0 || row.ExpiresAt <= now || row.CredentialExpiresAt <= now {
		return AuthSession{}, ErrInvalidSession
	}
	if err := query.New(tx).ConsumeAuthCredential(ctx, query.ConsumeAuthCredentialParams{TokenHash: hash, ConsumedAt: now}); err != nil {
		return AuthSession{}, fmt.Errorf("consume refresh credential: %w", err)
	}
	for i := range credentials {
		if credentials[i].ExpiresAt.UnixMilli() > row.ExpiresAt {
			credentials[i].ExpiresAt = time.UnixMilli(row.ExpiresAt)
		}
	}
	if err := insertCredentials(ctx, tx, session.ID, credentials); err != nil {
		return AuthSession{}, err
	}
	if err := tx.Commit(); err != nil {
		return AuthSession{}, fmt.Errorf("commit refresh: %w", err)
	}
	return session, nil
}

// RevokeAuthSession also invalidates all of the session's access and refresh tokens.
func (s *Store) RevokeAuthSession(ctx context.Context, id string) error {
	if err := query.New(s.db).RevokeAuthSession(ctx, query.RevokeAuthSessionParams{ID: id, RevokedAt: time.Now().UnixMilli()}); err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}

// AuthSessionActive checks a subscription's authorization independently of token rotation.
func (s *Store) AuthSessionActive(ctx context.Context, id, policyID string) (bool, error) {
	valid, err := query.New(s.db).AuthSessionActive(ctx, query.AuthSessionActiveParams{ID: id, PolicyID: policyID, ExpiresAt: time.Now().UnixMilli()})
	if err != nil {
		return false, fmt.Errorf("check session authorization: %w", err)
	}
	return valid, nil
}

func insertCredentials(ctx context.Context, tx *sql.Tx, sessionID string, credentials []AuthCredential) error {
	for _, credential := range credentials {
		if err := query.New(tx).CreateAuthCredential(ctx, query.CreateAuthCredentialParams{TokenHash: credential.Hash, SessionID: sessionID, Kind: credential.Kind, ExpiresAt: credential.ExpiresAt.UnixMilli()}); err != nil {
			return fmt.Errorf("store session credential: %w", err)
		}
	}
	return nil
}
