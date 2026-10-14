package storage

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestAuthSessionPersistence(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "auth.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	session := AuthSession{ID: "device", Method: "otp", Issuer: "sigmo", Subject: "owner", PolicyID: "policy", CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}
	credentials := []AuthCredential{{Hash: "access-digest", Kind: "access", ExpiresAt: session.ExpiresAt}, {Hash: "refresh-digest", Kind: "refresh", ExpiresAt: session.ExpiresAt}}
	if err := s.CreateAuthSession(ctx, session, credentials); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	got, err := s.AuthSessionByCredential(ctx, "access-digest", "access")
	if err != nil || got.ID != session.ID {
		t.Fatalf("reopened session=%+v, %v", got, err)
	}
	if active, err := s.AuthSessionActive(ctx, session.ID, "wrong policy"); err != nil || active {
		t.Fatalf("policy mismatch=%v %v", active, err)
	}
	if err := s.RevokeAuthSession(ctx, session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AuthSessionByCredential(ctx, "access-digest", "access"); !errors.Is(err, ErrInvalidSession) {
		t.Fatal(err)
	}
}
func TestCredentialExpiryAndAtomicRotation(t *testing.T) {
	for _, tc := range []struct {
		name     string
		lifetime time.Duration
	}{{"live", time.Hour}, {"expired", -time.Second}} {
		t.Run(tc.name, func(t *testing.T) {
			s := testStore(t)
			ctx := t.Context()
			expiry := time.Now().Add(tc.lifetime)
			session := AuthSession{ID: "id", Method: "otp", ExpiresAt: expiry, CreatedAt: time.Now()}
			if err := s.CreateAuthSession(ctx, session, []AuthCredential{{Hash: "refresh", Kind: "refresh", ExpiresAt: expiry}}); err != nil {
				t.Fatal(err)
			}
			next := []AuthCredential{{Hash: "next", Kind: "access", ExpiresAt: time.Now().Add(2 * time.Hour)}}
			_, err := s.RotateAuthCredentials(ctx, "refresh", next)
			if tc.lifetime < 0 {
				if !errors.Is(err, ErrInvalidSession) {
					t.Fatal(err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var storedExpiry int64
			if err := s.db.QueryRowContext(ctx, `SELECT expires_at FROM auth_credentials WHERE token_hash='next'`).Scan(&storedExpiry); err != nil {
				t.Fatal(err)
			}
			if storedExpiry != expiry.UnixMilli() {
				t.Fatal("credential exceeds absolute session expiry")
			}
			if _, err := s.RotateAuthCredentials(ctx, "refresh", next); !errors.Is(err, ErrInvalidSession) {
				t.Fatal(err)
			}
			if _, err := s.AuthSessionByCredential(ctx, "next", "access"); !errors.Is(err, ErrInvalidSession) {
				t.Fatal("replay did not revoke new credential")
			}
		})
	}
}
