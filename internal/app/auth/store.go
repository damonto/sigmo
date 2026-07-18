package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"
)

const (
	otpMaxValue        = 1000000
	otpLength          = 6
	defaultOTPTTL      = 10 * time.Minute
	defaultOTPCooldown = 30 * time.Second
	defaultTokenTTL    = 7 * 24 * time.Hour
)

var ErrOTPCooldown = errors.New("otp requested too soon")

type Store struct {
	mu              sync.Mutex
	otps            map[string]otpEntry
	tokens          map[string]tokenEntry
	otpTTL          time.Duration
	otpCooldown     time.Duration
	tokenTTL        time.Duration
	lastOTPIssuedAt time.Time
}

type otpEntry struct {
	expiresAt time.Time
}

type tokenEntry struct {
	expiresAt time.Time
}

func NewStore() *Store {
	return &Store{
		otps:        make(map[string]otpEntry),
		tokens:      make(map[string]tokenEntry),
		otpTTL:      defaultOTPTTL,
		otpCooldown: defaultOTPCooldown,
		tokenTTL:    defaultTokenTTL,
	}
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

func (s *Store) IssueToken() (string, time.Time, error) {
	token, err := generateToken()
	if err != nil {
		return "", time.Time{}, err
	}
	expiresAt := time.Now().Add(s.tokenTTL)

	s.mu.Lock()
	s.tokens[token] = tokenEntry{expiresAt: expiresAt}
	s.mu.Unlock()

	return token, expiresAt, nil
}

func (s *Store) ValidateToken(token string) bool {
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}
	now := time.Now()

	s.mu.Lock()
	entry, ok := s.tokens[token]
	if !ok {
		s.mu.Unlock()
		return false
	}
	if now.After(entry.expiresAt) {
		delete(s.tokens, token)
		s.mu.Unlock()
		return false
	}
	s.mu.Unlock()
	return true
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
