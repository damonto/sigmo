package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestAuthTokenStorage(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := testStore(t)
	now := time.Now().UTC().Truncate(time.Millisecond)
	expiresAt := now.Add(time.Hour)

	if err := store.CreateAuthToken(ctx, "valid-token-hash", expiresAt); err != nil {
		t.Fatalf("CreateAuthToken() error = %v", err)
	}

	tests := []struct {
		name      string
		tokenHash string
		now       time.Time
		want      bool
	}{
		{name: "valid before expiration", tokenHash: "valid-token-hash", now: expiresAt.Add(-time.Millisecond), want: true},
		{name: "invalid at expiration", tokenHash: "valid-token-hash", now: expiresAt},
		{name: "invalid after expiration", tokenHash: "valid-token-hash", now: expiresAt.Add(time.Millisecond)},
		{name: "unknown token", tokenHash: "unknown", now: now},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			valid, err := store.AuthTokenValid(ctx, tt.tokenHash, tt.now)
			if err != nil {
				t.Fatalf("AuthTokenValid() error = %v", err)
			}
			if valid != tt.want {
				t.Fatalf("AuthTokenValid() = %v, want %v", valid, tt.want)
			}
		})
	}
}

func TestCreateAuthTokenDeletesExpiredTokens(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := testStore(t)
	if err := store.CreateAuthToken(ctx, "expired", time.Now().Add(-time.Hour)); err != nil {
		t.Fatalf("CreateAuthToken() expired error = %v", err)
	}
	if err := store.CreateAuthToken(ctx, "valid", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("CreateAuthToken() valid error = %v", err)
	}

	var expiredCount int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM auth_tokens WHERE token_hash = 'expired'`).Scan(&expiredCount); err != nil {
		t.Fatalf("count expired tokens: %v", err)
	}
	if expiredCount != 0 {
		t.Fatalf("expired token count = %d, want 0", expiredCount)
	}
}

func TestAuthTokenPersistsAcrossReopen(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "sigmo.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	expiresAt := time.Now().Add(time.Hour)
	if err := store.CreateAuthToken(ctx, "persistent", expiresAt); err != nil {
		t.Fatalf("CreateAuthToken() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open() reopened error = %v", err)
	}
	t.Cleanup(func() {
		if err := reopened.Close(); err != nil {
			t.Fatalf("Close() reopened error = %v", err)
		}
	})
	valid, err := reopened.AuthTokenValid(ctx, "persistent", time.Now())
	if err != nil {
		t.Fatalf("AuthTokenValid() error = %v", err)
	}
	if !valid {
		t.Fatal("AuthTokenValid() = false after reopening database, want true")
	}
}
