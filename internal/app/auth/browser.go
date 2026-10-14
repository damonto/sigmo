package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/url"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/damonto/sigmo/internal/pkg/settings"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

type login struct {
	kind, policyID, nativeState, challenge, verifier, nonce, binding string
	origin, basePath                                                 string
	native                                                           bool
	expiresAt                                                        time.Time
	identity                                                         storage.AuthSession
}

// BrowserRequest binds the callback to the request origin and the native handoff to PKCE.
// Origin comes from the HTTP request; BasePath comes from the client's API URL so
// proxies that strip a path prefix do not require a second instance URL setting.
type BrowserRequest struct {
	Policy        settings.Auth
	Origin        string
	BasePath      string
	Native        bool
	State         string
	CodeChallenge string
}

// CreateBrowserLogin returns a same-backend browser entry point, never credentials.
func (s *Store) CreateBrowserLogin(req BrowserRequest) (string, error) {
	if !req.Policy.HasMethod("oidc") || !validBrowserOrigin(req.Origin) || !validBrowserBasePath(req.BasePath) {
		return "", ErrInvalidLogin
	}
	if req.Native && (len(req.State) < 32 || len(req.State) > 128 || !validChallenge(req.CodeChallenge)) {
		return "", ErrInvalidLogin
	}
	id := rand.Text()
	err := s.saveLogin(id, login{kind: "start", policyID: req.Policy.PolicyID(), origin: req.Origin, basePath: req.BasePath, native: req.Native, nativeState: req.State, challenge: req.CodeChallenge, expiresAt: time.Now().Add(transactionTTL)})
	if err != nil {
		return "", err
	}
	return req.Origin + req.BasePath + "/api/v1/auth/browser-sessions/" + id, nil
}

// BrowserStartRequest keeps the browser entry point on the initiating origin.
type BrowserStartRequest struct {
	Policy     settings.Auth
	ID, Origin string
}

// BrowserStart contains a provider URL and a separate browser-binding cookie.
type BrowserStart struct{ URL, State, Binding string }

// StartBrowserLogin binds the provider callback to the browser that opened the login.
func (s *Store) StartBrowserLogin(ctx context.Context, req BrowserStartRequest) (BrowserStart, error) {
	transaction, err := s.takeLogin(req.ID, "start", req.Policy.PolicyID())
	if err != nil {
		return BrowserStart{}, err
	}
	if req.Origin != transaction.origin {
		return BrowserStart{}, ErrInvalidLogin
	}
	provider, err := s.provider(ctx, req.Policy.OIDC.Issuer)
	if err != nil {
		return BrowserStart{}, err
	}
	state, binding := rand.Text(), rand.Text()
	transaction.kind, transaction.verifier, transaction.nonce, transaction.binding = "callback", oauth2.GenerateVerifier(), rand.Text(), Hash(binding)
	if err := s.saveLogin(state, transaction); err != nil {
		return BrowserStart{}, err
	}
	config := oauthConfig(req.Policy, provider, transaction.origin+transaction.basePath)
	target := config.AuthCodeURL(state, oidc.Nonce(transaction.nonce), oauth2.S256ChallengeOption(transaction.verifier))
	return BrowserStart{URL: target, State: state, Binding: binding}, nil
}

// BrowserCallback contains only values from the fixed provider callback endpoint.
type BrowserCallback struct {
	Policy               settings.Auth
	Origin               string
	State, Code, Binding string
}

// BrowserResult either establishes a web identity or returns a native one-time code.
type BrowserResult struct {
	Identity    storage.AuthSession
	RedirectURL string
	Native      bool
}

// CompleteBrowserLogin validates both protocol claims and explicit local admission.
func (s *Store) CompleteBrowserLogin(ctx context.Context, req BrowserCallback) (BrowserResult, error) {
	transaction, err := s.takeLogin(req.State, "callback", req.Policy.PolicyID())
	if err != nil {
		return BrowserResult{}, err
	}
	if !req.Policy.HasMethod("oidc") || req.Origin != transaction.origin || req.Code == "" || !equal(transaction.binding, Hash(req.Binding)) {
		return BrowserResult{}, ErrInvalidLogin
	}
	provider, err := s.provider(ctx, req.Policy.OIDC.Issuer)
	if err != nil {
		return BrowserResult{}, err
	}
	config := oauthConfig(req.Policy, provider, transaction.origin+transaction.basePath)
	token, err := config.Exchange(oidc.ClientContext(ctx, s.client), req.Code, oauth2.VerifierOption(transaction.verifier))
	if err != nil {
		return BrowserResult{}, fmt.Errorf("exchange OIDC code: %w", err)
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok {
		return BrowserResult{}, ErrInvalidLogin
	}
	verified, err := provider.Verifier(&oidc.Config{ClientID: req.Policy.OIDC.ClientID}).Verify(ctx, raw)
	if err != nil {
		return BrowserResult{}, fmt.Errorf("verify ID token: %w", err)
	}
	if !equal(verified.Nonce, transaction.nonce) || verified.Subject == "" || !slices.Contains(req.Policy.OIDC.AllowedSubjects, verified.Subject) {
		return BrowserResult{}, ErrInvalidLogin
	}
	var claims struct {
		AuthorizedParty string `json:"azp"`
	}
	if err := verified.Claims(&claims); err != nil {
		return BrowserResult{}, fmt.Errorf("read ID claims: %w", err)
	}
	if (len(verified.Audience) > 1 || claims.AuthorizedParty != "") && claims.AuthorizedParty != req.Policy.OIDC.ClientID {
		return BrowserResult{}, ErrInvalidLogin
	}
	// Reauthentication is bounded even when an administrator selects long OTP sessions.
	expires := time.Now().Add(min(time.Duration(req.Policy.SessionMaxDays)*24*time.Hour, 24*time.Hour))
	identity := storage.AuthSession{Method: "oidc", Issuer: verified.Issuer, Subject: verified.Subject, PolicyID: req.Policy.PolicyID(), ExpiresAt: expires}
	if !transaction.native {
		return BrowserResult{Identity: identity, RedirectURL: transaction.origin + transaction.basePath + "/"}, nil
	}
	code := rand.Text()
	transaction.kind, transaction.identity, transaction.expiresAt = "handoff", identity, time.Now().Add(time.Minute)
	if err := s.saveLogin(code, transaction); err != nil {
		return BrowserResult{}, err
	}
	target := NativeRedirectURL + "?" + url.Values{"code": {code}, "state": {transaction.nativeState}}.Encode()
	return BrowserResult{Native: true, RedirectURL: target}, nil
}

// HandoffRequest proves that the same app that started login is redeeming its result.
type HandoffRequest struct {
	Policy         settings.Auth
	Code, Verifier string
}

// CompleteHandoff atomically consumes an app-specific, short-lived authorization result.
func (s *Store) CompleteHandoff(req HandoffRequest) (storage.AuthSession, error) {
	transaction, err := s.takeLogin(req.Code, "handoff", req.Policy.PolicyID())
	if err != nil {
		return storage.AuthSession{}, err
	}
	if !req.Policy.HasMethod("oidc") || len(req.Verifier) < 43 || len(req.Verifier) > 128 || !equal(pkce(req.Verifier), transaction.challenge) {
		return storage.AuthSession{}, ErrInvalidLogin
	}
	return transaction.identity, nil
}

// AccessRequest pins verification to administrator-supplied issuer and application audience.
type AccessRequest struct {
	Policy    settings.Auth
	Assertion string
}

// VerifyAccess authenticates a signed edge assertion, never an unsigned identity header.
func (s *Store) VerifyAccess(ctx context.Context, req AccessRequest) (storage.AuthSession, error) {
	cfg := req.Policy.CloudflareAccess
	if req.Policy.Mode != "cloudflare_access" || req.Assertion == "" {
		return storage.AuthSession{}, storage.ErrInvalidSession
	}
	s.mu.Lock()
	keys := s.keys[cfg.TeamDomain]
	if keys == nil {
		keys = oidc.NewRemoteKeySet(oidc.ClientContext(context.Background(), s.client), cfg.TeamDomain+"/cdn-cgi/access/certs")
		s.keys = map[string]*oidc.RemoteKeySet{cfg.TeamDomain: keys}
	}
	s.mu.Unlock()
	token, err := oidc.NewVerifier(cfg.TeamDomain, keys, &oidc.Config{ClientID: cfg.Audience, SupportedSigningAlgs: []string{"RS256"}}).Verify(ctx, req.Assertion)
	if err != nil {
		return storage.AuthSession{}, storage.ErrInvalidSession
	}
	var claims struct {
		NotBefore int64 `json:"nbf"`
	}
	if err := token.Claims(&claims); err != nil {
		return storage.AuthSession{}, storage.ErrInvalidSession
	}
	if token.Subject == "" || claims.NotBefore > time.Now().Unix() || !slices.Contains(cfg.AllowedSubjects, token.Subject) {
		return storage.AuthSession{}, storage.ErrInvalidSession
	}
	return storage.AuthSession{Method: "cloudflare_access", Issuer: token.Issuer, Subject: token.Subject, PolicyID: req.Policy.PolicyID(), ExpiresAt: token.Expiry}, nil
}

func (s *Store) provider(ctx context.Context, issuer string) (*oidc.Provider, error) {
	s.mu.Lock()
	provider := s.providers[issuer]
	s.mu.Unlock()
	if provider != nil {
		return provider, nil
	}
	provider, err := oidc.NewProvider(oidc.ClientContext(ctx, s.client), issuer)
	if err != nil {
		return nil, fmt.Errorf("discover OIDC provider: %w", err)
	}
	var metadata struct {
		JWKSURI string `json:"jwks_uri"`
	}
	if err := provider.Claims(&metadata); err != nil {
		return nil, fmt.Errorf("read OIDC metadata: %w", err)
	}
	for _, endpoint := range []string{provider.Endpoint().AuthURL, provider.Endpoint().TokenURL, metadata.JWKSURI} {
		u, err := url.Parse(endpoint)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
			return nil, errors.New("OIDC endpoints must use HTTPS")
		}
	}
	s.mu.Lock()
	s.providers = map[string]*oidc.Provider{issuer: provider}
	s.mu.Unlock()
	return provider, nil
}

func oauthConfig(cfg settings.Auth, provider *oidc.Provider, baseURL string) oauth2.Config {
	return oauth2.Config{ClientID: cfg.OIDC.ClientID, ClientSecret: cfg.OIDC.ClientSecret, Endpoint: provider.Endpoint(), RedirectURL: baseURL + "/api/v1/auth/oidc/callback", Scopes: []string{oidc.ScopeOpenID}}
}

func validBrowserOrigin(origin string) bool {
	u, err := url.Parse(origin)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil &&
		u.Path == "" && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" && u.String() == origin
}

func validBrowserBasePath(basePath string) bool {
	if basePath == "" {
		return true
	}
	u, err := url.ParseRequestURI(basePath)
	return err == nil && u.Scheme == "" && u.Host == "" && strings.HasPrefix(u.Path, "/") &&
		u.Path != "/" && path.Clean(u.Path) == u.Path && !strings.Contains(u.Path, "\\") &&
		u.RawQuery == "" && !u.ForceQuery && u.EscapedPath() == basePath
}

func (s *Store) saveLogin(id string, transaction login) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, value := range s.logins {
		if !time.Now().Before(value.expiresAt) {
			delete(s.logins, key)
		}
	}
	if len(s.logins) >= 256 {
		return ErrRateLimited
	}
	s.logins[id] = transaction
	return nil
}

func (s *Store) takeLogin(id, kind, policyID string) (login, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.logins[id]
	delete(s.logins, id)
	if !ok || value.kind != kind || value.policyID != policyID || !time.Now().Before(value.expiresAt) {
		return login{}, ErrInvalidLogin
	}
	return value, nil
}

func validChallenge(value string) bool {
	if len(value) != 43 {
		return false
	}
	for _, r := range value {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}
