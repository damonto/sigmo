// Package auth owns login transactions and revocable application sessions.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"

	"github.com/damonto/sigmo/internal/pkg/settings"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

// NativeRedirectURL is the app-associated callback hosted by pro-api.
const NativeRedirectURL = "https://sigmo-pro.952007.xyz/auth/callback"
const accessTTL = 15 * time.Minute
const transactionTTL = 5 * time.Minute

// ErrInvalidLogin deliberately does not distinguish wrong, expired, and replayed codes.
var ErrInvalidLogin = errors.New("invalid or expired login")

// ErrRateLimited indicates a bounded login queue or an exhausted attempt allowance.
var ErrRateLimited = errors.New("too many login attempts")

// Store keeps short-lived login state in memory; sessions survive restarts in SQLite.
type Store struct {
	mu         sync.Mutex
	db         *storage.Store
	challenges map[string]*challenge
	logins     map[string]login
	limits     map[string]rateLimit
	providers  map[string]*oidc.Provider
	keys       map[string]*oidc.RemoteKeySet
	client     *http.Client
}
type challenge struct {
	digest, policyID string
	expiresAt        time.Time
	attempts         int
}
type rateLimit struct {
	count     int
	expiresAt time.Time
}

// Tokens contains credentials only at issuance; callers must not log it.
type Tokens struct {
	AccessToken      string    `json:"accessToken"`
	RefreshToken     string    `json:"refreshToken,omitempty"`
	ExpiresAt        time.Time `json:"expiresAt"`
	SessionExpiresAt time.Time `json:"sessionExpiresAt"`
}

// NewStore attaches authentication to the existing application database.
func NewStore(db *storage.Store) (*Store, error) {
	if db == nil {
		return nil, errors.New("auth storage is required")
	}
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	return &Store{db: db, challenges: map[string]*challenge{}, logins: map[string]login{}, limits: map[string]rateLimit{}, providers: map[string]*oidc.Provider{}, keys: map[string]*oidc.RemoteKeySet{}, client: client}, nil
}

// AllowLogin bounds both memory and unauthenticated work per source and operation.
func (s *Store) AllowLogin(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for key, limit := range s.limits {
		if !now.Before(limit.expiresAt) {
			delete(s.limits, key)
		}
	}
	limit := s.limits[key]
	if limit.expiresAt.IsZero() {
		limit.expiresAt = now.Add(time.Minute)
	}
	if limit.count >= 10 || len(s.limits) >= 2048 {
		return ErrRateLimited
	}
	limit.count++
	s.limits[key] = limit
	return nil
}

// IssueOTP creates an independent challenge so another device cannot replace its code.
func (s *Store) IssueOTP(policyID string) (string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for id, c := range s.challenges {
		if !now.Before(c.expiresAt) {
			delete(s.challenges, id)
		}
	}
	if len(s.challenges) >= 128 {
		return "", "", ErrRateLimited
	}
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", "", fmt.Errorf("generate OTP: %w", err)
	}
	id, code := rand.Text(), fmt.Sprintf("%06d", n.Int64())
	s.challenges[id] = &challenge{digest: Hash(id + code), policyID: policyID, expiresAt: now.Add(transactionTTL)}
	return id, code, nil
}

// DiscardOTP removes codes that could not be delivered.
func (s *Store) DiscardOTP(id string) { s.mu.Lock(); defer s.mu.Unlock(); delete(s.challenges, id) }

// OTPAttempt binds a verification attempt to its challenge and current policy.
type OTPAttempt struct {
	Policy   settings.Auth
	ID, Code string
}

// VerifyOTP consumes successful challenges and limits unsuccessful guesses.
func (s *Store) VerifyOTP(attempt OTPAttempt) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.challenges[attempt.ID]
	if !ok {
		return ErrInvalidLogin
	}
	c.attempts++
	valid := attempt.Policy.HasMethod("otp") && c.policyID == attempt.Policy.PolicyID() && time.Now().Before(c.expiresAt) && c.attempts <= 5 && equal(c.digest, Hash(attempt.ID+attempt.Code))
	if valid || c.attempts >= 5 || !time.Now().Before(c.expiresAt) {
		delete(s.challenges, attempt.ID)
	}
	if !valid {
		return ErrInvalidLogin
	}
	return nil
}

// SessionIssue selects the credential transport for a newly authenticated identity.
type SessionIssue struct {
	Session storage.AuthSession
	Native  bool
}

// IssueSession stores a new identity and issues either a browser cookie or native credentials.
func (s *Store) IssueSession(ctx context.Context, issue SessionIssue) (Tokens, error) {
	session := issue.Session
	session.ID, session.CreatedAt = rand.Text(), time.Now()
	tokens, credentials := credentialsFor(session.ExpiresAt, issue.Native)
	if err := s.db.CreateAuthSession(ctx, session, credentials); err != nil {
		return Tokens{}, err
	}
	return tokens, nil
}

// SessionRequest identifies the token transport and the current trust policy.
type SessionRequest struct {
	Policy      settings.Auth
	Token, Kind string
}

// Authenticate checks both the stored session and the current authentication policy.
func (s *Store) Authenticate(ctx context.Context, req SessionRequest) (storage.AuthSession, error) {
	session, err := s.db.AuthSessionByCredential(ctx, Hash(req.Token), req.Kind)
	if err != nil {
		return storage.AuthSession{}, err
	}
	if session.PolicyID != req.Policy.PolicyID() || !req.Policy.HasMethod(session.Method) {
		return storage.AuthSession{}, storage.ErrInvalidSession
	}
	return session, nil
}

// Refresh rotates refresh credentials; replay revocation is enforced in the transaction.
func (s *Store) Refresh(ctx context.Context, req SessionRequest) (Tokens, error) {
	tokens, credentials := credentialsFor(time.Now().Add(time.Duration(req.Policy.SessionMaxDays)*24*time.Hour), true)
	session, err := s.db.RotateAuthCredentials(ctx, Hash(req.Token), credentials)
	if err != nil {
		return Tokens{}, err
	}
	if session.PolicyID != req.Policy.PolicyID() || !req.Policy.HasMethod(session.Method) {
		if err := s.db.RevokeAuthSession(ctx, session.ID); err != nil {
			return Tokens{}, err
		}
		return Tokens{}, storage.ErrInvalidSession
	}
	tokens.SessionExpiresAt = session.ExpiresAt
	if tokens.ExpiresAt.After(session.ExpiresAt) {
		tokens.ExpiresAt = session.ExpiresAt
	}
	return tokens, nil
}

// Revoke terminates a device or browser session and all its credentials.
func (s *Store) Revoke(ctx context.Context, id string) error { return s.db.RevokeAuthSession(ctx, id) }

// SessionActive bounds live operations to their original authorization lease.
func (s *Store) SessionActive(ctx context.Context, session storage.AuthSession) bool {
	if !time.Now().Before(session.ExpiresAt) {
		return false
	}
	if session.Method == "cloudflare_access" {
		return true // The edge assertion is valid only until its signed expiration.
	}
	active, err := s.db.AuthSessionActive(ctx, session.ID, session.PolicyID)
	return err == nil && active
}

func credentialsFor(expiresAt time.Time, native bool) (Tokens, []storage.AuthCredential) {
	tokens := Tokens{AccessToken: rand.Text(), ExpiresAt: expiresAt, SessionExpiresAt: expiresAt}
	kind := "web"
	if native {
		kind = "access"
		tokens.RefreshToken = rand.Text()
		if expiry := time.Now().Add(accessTTL); expiry.Before(expiresAt) {
			tokens.ExpiresAt = expiry
		}
	}
	credentials := []storage.AuthCredential{{Hash: Hash(tokens.AccessToken), Kind: kind, ExpiresAt: tokens.ExpiresAt}}
	if native {
		credentials = append(credentials, storage.AuthCredential{Hash: Hash(tokens.RefreshToken), Kind: "refresh", ExpiresAt: expiresAt})
	}
	return tokens, credentials
}

// Hash returns a stable token digest for persistence, never a password hash.
func Hash(raw string) string { hash := sha256.Sum256([]byte(raw)); return hex.EncodeToString(hash[:]) }
func equal(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }
func pkce(verifier string) string {
	hash := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}
