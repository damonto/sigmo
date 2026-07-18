package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/damonto/sigmo/internal/pkg/storage"
)

const (
	otpMaxValue        = 1000000
	otpLength          = 6
	defaultOTPTTL      = 10 * time.Minute
	defaultOTPCooldown = 30 * time.Second
)

var (
	ErrOTPCooldown     = errors.New("otp requested too soon")
	errStorageRequired = errors.New("auth token storage is required")
)

type Store struct {
	mu              sync.Mutex
	db              *storage.Store
	otps            map[string]otpEntry
	otpTTL          time.Duration
	otpCooldown     time.Duration
	lastOTPIssuedAt time.Time
}

type otpEntry struct {
	expiresAt time.Time
}

func NewStore(db *storage.Store) (*Store, error) {
	if db == nil {
		return nil, errStorageRequired
	}
	return &Store{
		db:          db,
		otps:        make(map[string]otpEntry),
		otpTTL:      defaultOTPTTL,
		otpCooldown: defaultOTPCooldown,
	}, nil
}

func (s *Store) IssueOTP() (string, time.Time, error) {
	code, err := generateOTP()
	if err != nil {
		return "", time.Time{}, err
	}
	now := time.Now()
	expiresAt := now.Add(s.otpTTL)

	s.mu.Lock()
	if !s.lastOTPIssuedAt.IsZero() && now.Sub(s.lastOTPIssuedAt) < s.otpCooldown {
		s.mu.Unlock()
		return "", time.Time{}, ErrOTPCooldown
	}
	s.lastOTPIssuedAt = now
	s.otps = map[string]otpEntry{
		code: {expiresAt: expiresAt},
	}
	s.mu.Unlock()

	return code, expiresAt, nil
}

func (s *Store) VerifyOTP(code string) bool {
	code = strings.TrimSpace(code)
	if code == "" {
		return false
	}
	now := time.Now()

	s.mu.Lock()
	entry, ok := s.otps[code]
	if !ok {
		s.mu.Unlock()
		return false
	}
	delete(s.otps, code)
	s.mu.Unlock()

	return now.Before(entry.expiresAt)
}

func (s *Store) IssueToken(ctx context.Context, validity time.Duration) (string, time.Time, error) {
	if validity <= 0 {
		return "", time.Time{}, errors.New("token validity must be positive")
	}
	token, err := generateToken()
	if err != nil {
		return "", time.Time{}, err
	}
	expiresAt := time.Now().Add(validity)

	if err := s.db.CreateAuthToken(ctx, hashToken(token), expiresAt); err != nil {
		return "", time.Time{}, fmt.Errorf("store token: %w", err)
	}

	return token, expiresAt, nil
}

func (s *Store) ValidateToken(ctx context.Context, token string) (bool, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return false, nil
	}
	valid, err := s.db.AuthTokenValid(ctx, hashToken(token), time.Now())
	if err != nil {
		return false, fmt.Errorf("validate token: %w", err)
	}
	return valid, nil
}

func generateOTP() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(otpMaxValue-1))
	if err != nil {
		return "", fmt.Errorf("generating otp: %w", err)
	}
	return formatOTP(n.Int64()), nil
}

func formatOTP(randomValue int64) string {
	return fmt.Sprintf("%0*d", otpLength, randomValue+1)
}

func generateToken() (string, error) {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", fmt.Errorf("generating token: %w", err)
	}
	return hex.EncodeToString(tokenBytes), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
