package auth

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/damonto/sigmo/internal/pkg/settings"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

func testAuthStore(t *testing.T) *Store {
	t.Helper()
	db, err := storage.Open(t.Context(), filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	s, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func otpPolicy() settings.Auth {
	cfg := settings.Default().Auth
	cfg.Mode = "otp"

	cfg.OTP.Channels = []string{"http"}
	return cfg
}
func TestOTPChallenges(t *testing.T) {
	for _, tc := range []string{"success and replay", "attempt limit", "expiry", "policy change", "independent devices"} {
		t.Run(tc, func(t *testing.T) {
			s := testAuthStore(t)
			cfg := otpPolicy()
			id, code, err := s.IssueOTP(cfg.PolicyID())
			if err != nil {
				t.Fatal(err)
			}
			if len(code) != 6 {
				t.Fatalf("code length=%d", len(code))
			}
			attempt := OTPAttempt{Policy: cfg, ID: id, Code: code}
			switch tc {
			case "attempt limit":
				for range 5 {
					if err := s.VerifyOTP(OTPAttempt{Policy: cfg, ID: id, Code: "wrong"}); !errors.Is(err, ErrInvalidLogin) {
						t.Fatal(err)
					}
				}
			case "expiry":
				s.challenges[id].expiresAt = time.Now().Add(-time.Second)
			case "policy change":
				attempt.Policy.SessionMaxDays++
			case "independent devices":
				second, secondCode, err := s.IssueOTP(cfg.PolicyID())
				if err != nil {
					t.Fatal(err)
				}
				if second == id {
					t.Fatal("reused challenge id")
				}
				if err := s.VerifyOTP(OTPAttempt{Policy: cfg, ID: second, Code: secondCode}); err != nil {
					t.Fatal(err)
				}
			}
			err = s.VerifyOTP(attempt)
			success := tc == "success and replay" || tc == "independent devices"
			if success && err != nil {
				t.Fatal(err)
			}
			if !success && !errors.Is(err, ErrInvalidLogin) {
				t.Fatalf("verification=%v", err)
			}
			if err := s.VerifyOTP(attempt); !errors.Is(err, ErrInvalidLogin) {
				t.Fatalf("replay=%v", err)
			}
		})
	}
}
func TestSessionsRotateRevokeAndEnforcePolicy(t *testing.T) {
	s := testAuthStore(t)
	cfg := otpPolicy()
	ctx := t.Context()
	issue := SessionIssue{Native: true, Session: storage.AuthSession{Method: "otp", Issuer: "sigmo", Subject: "owner", PolicyID: cfg.PolicyID(), ExpiresAt: time.Now().Add(time.Hour)}}
	tokens, err := s.IssueSession(ctx, issue)
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.Authenticate(ctx, SessionRequest{Policy: cfg, Token: tokens.AccessToken, Kind: "access"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.AuthSessionByCredential(ctx, tokens.AccessToken, "access"); !errors.Is(err, storage.ErrInvalidSession) {
		t.Fatal("stored raw credential")
	}
	for _, req := range []SessionRequest{{Policy: cfg, Token: tokens.AccessToken, Kind: "web"}, {Policy: cfg, Token: tokens.RefreshToken, Kind: "access"}} {
		if _, err := s.Authenticate(ctx, req); !errors.Is(err, storage.ErrInvalidSession) {
			t.Fatalf("accepted wrong transport: %v", err)
		}
	}
	changed := cfg.Clone()
	changed.SessionMaxDays++
	if _, err := s.Authenticate(ctx, SessionRequest{Policy: changed, Token: tokens.AccessToken, Kind: "access"}); !errors.Is(err, storage.ErrInvalidSession) {
		t.Fatal("accepted changed policy")
	}
	rotated, err := s.Refresh(ctx, SessionRequest{Policy: cfg, Token: tokens.RefreshToken})
	if err != nil {
		t.Fatal(err)
	}
	after, err := s.Authenticate(ctx, SessionRequest{Policy: cfg, Token: rotated.AccessToken, Kind: "access"})
	if err != nil || after.ID != session.ID {
		t.Fatalf("rotation changed identity: %v", err)
	}
	if rotated.SessionExpiresAt.After(issue.Session.ExpiresAt) {
		t.Fatal("refresh extended absolute lifetime")
	}
	if _, err := s.Refresh(ctx, SessionRequest{Policy: cfg, Token: tokens.RefreshToken}); !errors.Is(err, storage.ErrInvalidSession) {
		t.Fatalf("refresh replay=%v", err)
	}
	if _, err := s.Authenticate(ctx, SessionRequest{Policy: cfg, Token: rotated.AccessToken, Kind: "access"}); !errors.Is(err, storage.ErrInvalidSession) {
		t.Fatal("refresh reuse did not revoke session")
	}
	if s.SessionActive(ctx, session) {
		t.Fatal("revoked session remains active")
	}
}
func TestLoginRateLimit(t *testing.T) {
	s := testAuthStore(t)
	for range 10 {
		if err := s.AllowLogin("source"); err != nil {
			t.Fatal(err)
		}
	}
	if !errors.Is(s.AllowLogin("source"), ErrRateLimited) {
		t.Fatal("unlimited attempts")
	}
	if err := s.AllowLogin("other"); err != nil {
		t.Fatal(err)
	}
}
