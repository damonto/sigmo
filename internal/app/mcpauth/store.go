package mcpauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/damonto/sigmo/internal/pkg/storage"
)

const (
	DefaultValidityDays = 30
	MaxValidityDays     = 180
	tokenPrefix         = "sigmo_mcp_"
)

var (
	ErrInvalidToken = errors.New("invalid MCP API key")
	ErrRevoked      = errors.New("MCP API key is revoked")
	ErrExpired      = errors.New("MCP API key is expired")
)

type Grant struct {
	ID          string
	Name        string
	TokenHint   string
	AllModems   bool
	ModemIDs    []string
	Permissions []string
	CreatedAt   time.Time
	ExpiresAt   time.Time
	RevokedAt   *time.Time
}

type IssueRequest struct {
	Name         string
	ValidityDays int
	AllModems    bool
	ModemIDs     []string
	Permissions  []string
}

type Store struct {
	db  *storage.Store
	now func() time.Time
}

func NewStore(db *storage.Store) (*Store, error) {
	if db == nil {
		return nil, errors.New("MCP API key storage is required")
	}
	return &Store{db: db, now: time.Now}, nil
}

func (s *Store) Issue(ctx context.Context, req IssueRequest) (Grant, string, error) {
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return Grant{}, "", errors.New("API key name is required")
	}
	if len(req.Name) > 64 {
		return Grant{}, "", errors.New("API key name must not exceed 64 characters")
	}
	if req.ValidityDays < 1 || req.ValidityDays > MaxValidityDays {
		return Grant{}, "", fmt.Errorf("API key validity must be between 1 and %d days", MaxValidityDays)
	}
	req.ModemIDs = normalize(req.ModemIDs)
	req.Permissions = normalize(req.Permissions)
	if req.AllModems && len(req.ModemIDs) > 0 {
		return Grant{}, "", errors.New("all modems and specific modems are mutually exclusive")
	}
	if !req.AllModems && len(req.ModemIDs) == 0 {
		return Grant{}, "", errors.New("at least one modem is required")
	}
	if len(req.Permissions) == 0 {
		return Grant{}, "", errors.New("at least one permission is required")
	}

	id, err := randomText(16)
	if err != nil {
		return Grant{}, "", fmt.Errorf("generate API key id: %w", err)
	}
	secret, err := randomText(32)
	if err != nil {
		return Grant{}, "", fmt.Errorf("generate API key secret: %w", err)
	}
	token := tokenPrefix + secret
	now := s.now().UTC()
	key := storage.MCPAPIKey{
		ID:          id,
		Name:        req.Name,
		TokenHash:   hashToken(token),
		TokenHint:   tokenHint(token),
		AllModems:   req.AllModems,
		ModemIDs:    req.ModemIDs,
		Permissions: req.Permissions,
		CreatedAt:   now,
		ExpiresAt:   now.AddDate(0, 0, req.ValidityDays),
	}
	if err := s.db.CreateMCPAPIKey(ctx, key); err != nil {
		return Grant{}, "", fmt.Errorf("store MCP API key: %w", err)
	}
	return grantFromStorage(key), token, nil
}

func (s *Store) Validate(ctx context.Context, token string) (Grant, error) {
	token = strings.TrimSpace(token)
	if !strings.HasPrefix(token, tokenPrefix) {
		return Grant{}, ErrInvalidToken
	}
	key, err := s.db.MCPAPIKeyByHash(ctx, hashToken(token))
	if errors.Is(err, storage.ErrNotFound) {
		return Grant{}, ErrInvalidToken
	}
	if err != nil {
		return Grant{}, fmt.Errorf("read MCP API key: %w", err)
	}
	if key.RevokedAt != nil {
		return Grant{}, ErrRevoked
	}
	if !s.now().Before(key.ExpiresAt) {
		return Grant{}, ErrExpired
	}
	return grantFromStorage(key), nil
}

func (s *Store) List(ctx context.Context) ([]Grant, error) {
	keys, err := s.db.ListMCPAPIKeys(ctx)
	if err != nil {
		return nil, err
	}
	grants := make([]Grant, 0, len(keys))
	for _, key := range keys {
		grants = append(grants, grantFromStorage(key))
	}
	return grants, nil
}

func (s *Store) Revoke(ctx context.Context, id string) (bool, error) {
	return s.db.RevokeMCPAPIKey(ctx, strings.TrimSpace(id), s.now().UTC())
}

func (g Grant) HasPermission(permission string) bool {
	return slices.Contains(g.Permissions, permission)
}

func (g Grant) AllowsModem(id string) bool {
	return g.AllModems || slices.Contains(g.ModemIDs, strings.TrimSpace(id))
}

func (g Grant) Status(now time.Time) string {
	if g.RevokedAt != nil {
		return "revoked"
	}
	if !now.Before(g.ExpiresAt) {
		return "expired"
	}
	return "active"
}

func grantFromStorage(key storage.MCPAPIKey) Grant {
	return Grant{
		ID:          key.ID,
		Name:        key.Name,
		TokenHint:   key.TokenHint,
		AllModems:   key.AllModems,
		ModemIDs:    slices.Clone(key.ModemIDs),
		Permissions: slices.Clone(key.Permissions),
		CreatedAt:   key.CreatedAt,
		ExpiresAt:   key.ExpiresAt,
		RevokedAt:   key.RevokedAt,
	}
}

func normalize(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	slices.Sort(normalized)
	return normalized
}

func randomText(size int) (string, error) {
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func tokenHint(token string) string {
	const suffixLength = 6
	if len(token) <= suffixLength {
		return token
	}
	return tokenPrefix + "…" + token[len(token)-suffixLength:]
}
