package mcpauth

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/damonto/sigmo/internal/pkg/storage"
)

func TestStoreIssueValidityAndHashing(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 19, 8, 30, 0, 0, time.UTC)
	tests := []struct {
		name         string
		validityDays int
		wantDays     int
		wantErr      bool
	}{
		{name: "zero", wantErr: true},
		{name: "minimum", validityDays: 1, wantDays: 1},
		{name: "maximum", validityDays: MaxValidityDays, wantDays: MaxValidityDays},
		{name: "below minimum", validityDays: -1, wantErr: true},
		{name: "above maximum", validityDays: MaxValidityDays + 1, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := openTestStorage(t)
			keys, err := NewStore(db)
			if err != nil {
				t.Fatalf("NewStore() error = %v", err)
			}
			keys.now = func() time.Time { return now }

			grant, token, err := keys.Issue(ctx, IssueRequest{
				Name:         "Automation",
				ValidityDays: tt.validityDays,
				AllModems:    true,
				Permissions:  []string{"sms.read", "modem.read", "sms.read"},
			})
			if tt.wantErr {
				if err == nil {
					t.Fatal("Issue() error = nil, want validation error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Issue() error = %v", err)
			}
			if !strings.HasPrefix(token, tokenPrefix) {
				t.Fatalf("Issue() token = %q, want %q prefix", token, tokenPrefix)
			}
			if got, want := grant.ExpiresAt, now.AddDate(0, 0, tt.wantDays); !got.Equal(want) {
				t.Fatalf("Issue() ExpiresAt = %v, want %v", got, want)
			}
			if want := []string{"modem.read", "sms.read"}; !slices.Equal(grant.Permissions, want) {
				t.Fatalf("Issue() permissions = %v, want %v", grant.Permissions, want)
			}

			stored, err := db.MCPAPIKeyByHash(ctx, hashToken(token))
			if err != nil {
				t.Fatalf("MCPAPIKeyByHash() error = %v", err)
			}
			if stored.TokenHash == token || stored.TokenHint == token {
				t.Fatal("stored API key contains the plaintext token")
			}
			if got, want := stored.TokenHash, hashToken(token); got != want {
				t.Fatalf("stored token hash = %q, want %q", got, want)
			}
		})
	}
}

func TestStoreValidateRevocationAndExpiry(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 19, 8, 30, 0, 0, time.UTC)
	db := openTestStorage(t)
	keys, err := NewStore(db)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	keys.now = func() time.Time { return now }

	grant, token, err := keys.Issue(ctx, IssueRequest{Name: "Agent", ValidityDays: 1, AllModems: true, Permissions: []string{"modem.read"}})
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	if _, err := keys.Validate(ctx, token); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if _, err := keys.Validate(ctx, "not-a-key"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("Validate(invalid) error = %v, want ErrInvalidToken", err)
	}

	keys.now = func() time.Time { return grant.ExpiresAt }
	if _, err := keys.Validate(ctx, token); !errors.Is(err, ErrExpired) {
		t.Fatalf("Validate(expired) error = %v, want ErrExpired", err)
	}

	keys.now = func() time.Time { return now }
	revoked, err := keys.Revoke(ctx, grant.ID)
	if err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}
	if !revoked {
		t.Fatal("Revoke() = false, want true")
	}
	if _, err := keys.Validate(ctx, token); !errors.Is(err, ErrRevoked) {
		t.Fatalf("Validate(revoked) error = %v, want ErrRevoked", err)
	}
}

func TestGrantAllowsModem(t *testing.T) {
	tests := []struct {
		name  string
		grant Grant
		id    string
		want  bool
	}{
		{name: "all modems includes future modem", grant: Grant{AllModems: true}, id: "future-imei", want: true},
		{name: "fixed modem matches", grant: Grant{ModemIDs: []string{"imei-a"}}, id: "imei-a", want: true},
		{name: "fixed modem rejects other", grant: Grant{ModemIDs: []string{"imei-a"}}, id: "imei-b", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.grant.AllowsModem(tt.id); got != tt.want {
				t.Fatalf("AllowsModem(%q) = %v, want %v", tt.id, got, tt.want)
			}
		})
	}
}

func TestStoreRejectsInvalidGrant(t *testing.T) {
	ctx := context.Background()
	db := openTestStorage(t)
	keys, err := NewStore(db)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	tests := []struct {
		name string
		req  IssueRequest
	}{
		{name: "missing name", req: IssueRequest{ValidityDays: DefaultValidityDays, AllModems: true, Permissions: []string{"modem.read"}}},
		{name: "all and fixed modems", req: IssueRequest{Name: "key", ValidityDays: DefaultValidityDays, AllModems: true, ModemIDs: []string{"imei"}, Permissions: []string{"modem.read"}}},
		{name: "missing fixed modem", req: IssueRequest{Name: "key", ValidityDays: DefaultValidityDays, Permissions: []string{"modem.read"}}},
		{name: "missing permission", req: IssueRequest{Name: "key", ValidityDays: DefaultValidityDays, AllModems: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := keys.Issue(ctx, tt.req); err == nil {
				t.Fatal("Issue() error = nil, want validation error")
			}
		})
	}
}

func openTestStorage(t *testing.T) *storage.Store {
	t.Helper()
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "sigmo.db"))
	if err != nil {
		t.Fatalf("storage.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})
	return db
}
