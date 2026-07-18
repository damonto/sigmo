package auth

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/damonto/sigmo/internal/pkg/storage"
)

func TestFormatOTP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		randomValue int64
		want        string
	}{
		{name: "lowest random value skips reserved code", randomValue: 0, want: "000001"},
		{name: "highest random value stays six digits", randomValue: otpMaxValue - 2, want: "999999"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := formatOTP(tt.randomValue); got != tt.want {
				t.Fatalf("formatOTP(%d) = %q, want %q", tt.randomValue, got, tt.want)
			}
		})
	}
}

func TestStoreRequiresStorage(t *testing.T) {
	t.Parallel()

	if _, err := NewStore(nil); err == nil {
		t.Fatal("NewStore() error = nil, want storage error")
	}
}

func TestTokenPersistence(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "sigmo.db")
	db, err := storage.Open(ctx, path)
	if err != nil {
		t.Fatalf("storage.Open() error = %v", err)
	}
	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	validity := 90 * 24 * time.Hour
	issuedAt := time.Now()
	token, expiresAt, err := store.IssueToken(ctx, validity)
	if err != nil {
		t.Fatalf("IssueToken() error = %v", err)
	}
	if expiresAt.Before(issuedAt.Add(validity)) || expiresAt.After(time.Now().Add(validity)) {
		t.Fatalf("expiresAt = %v, want about %v after issuance", expiresAt, validity)
	}
	valid, err := store.ValidateToken(ctx, token)
	if err != nil {
		t.Fatalf("ValidateToken() error = %v", err)
	}
	if !valid {
		t.Fatal("ValidateToken() = false, want true")
	}
	rawStored, err := db.AuthTokenValid(ctx, token, time.Now())
	if err != nil {
		t.Fatalf("AuthTokenValid() raw token error = %v", err)
	}
	if rawStored {
		t.Fatal("database contains the raw bearer token")
	}
	hashStored, err := db.AuthTokenValid(ctx, hashToken(token), time.Now())
	if err != nil {
		t.Fatalf("AuthTokenValid() token hash error = %v", err)
	}
	if !hashStored {
		t.Fatal("database does not contain the token hash")
	}
	valid, err = store.ValidateToken(ctx, "wrong-token")
	if err != nil {
		t.Fatalf("ValidateToken() wrong token error = %v", err)
	}
	if valid {
		t.Fatal("ValidateToken() wrong token = true, want false")
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	reopened, err := storage.Open(ctx, path)
	if err != nil {
		t.Fatalf("storage.Open() reopened error = %v", err)
	}
	t.Cleanup(func() {
		if err := reopened.Close(); err != nil {
			t.Fatalf("Close() reopened error = %v", err)
		}
	})
	restartedStore, err := NewStore(reopened)
	if err != nil {
		t.Fatalf("NewStore() reopened error = %v", err)
	}
	valid, err = restartedStore.ValidateToken(ctx, token)
	if err != nil {
		t.Fatalf("ValidateToken() reopened error = %v", err)
	}
	if !valid {
		t.Fatal("ValidateToken() after reopening database = false, want true")
	}
}

func TestValidateTokenRejectsExpiredToken(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "sigmo.db"))
	if err != nil {
		t.Fatalf("storage.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})
	if err := db.CreateAuthToken(ctx, hashToken("expired-token"), time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("CreateAuthToken() error = %v", err)
	}
	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	valid, err := store.ValidateToken(ctx, "expired-token")
	if err != nil {
		t.Fatalf("ValidateToken() error = %v", err)
	}
	if valid {
		t.Fatal("ValidateToken() expired token = true, want false")
	}
}
