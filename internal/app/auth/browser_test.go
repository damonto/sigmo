package auth

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/damonto/sigmo/internal/pkg/settings"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

// A TLS provider exercises discovery, PKCE exchange, signed claims and JWKS together.
func TestOIDCBrowserFlow(t *testing.T) {
	for _, scenario := range []string{"web", "empty admission list", "alternate origin", "HTTPS port", "web prefix", "native", "native prefix", "wrong nonce", "wrong audience", "wrong issuer", "unlisted subject", "expired", "wrong binding", "wrong state", "wrong callback origin", "policy changed", "PKCE mismatch"} {
		t.Run(scenario, func(t *testing.T) {
			key, err := rsa.GenerateKey(rand.Reader, 2048)
			if err != nil {
				t.Fatal(err)
			}
			s := testAuthStore(t)
			origin := "https://sigmo.example"
			switch scenario {
			case "alternate origin":
				origin = "https://another-sigmo.example"
			case "HTTPS port":
				origin = "https://sigmo.example:8443"
			}
			basePath := ""
			if scenario == "web prefix" || scenario == "native prefix" {
				basePath = "/sigmo"
			}
			baseURL := origin + basePath
			var issuer, nonce, challenge string
			provider := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/.well-known/openid-configuration":
					_ = json.NewEncoder(w).Encode(map[string]any{"issuer": issuer, "authorization_endpoint": issuer + "/authorize", "token_endpoint": issuer + "/token", "jwks_uri": issuer + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"}})
				case "/keys":
					_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{"kty": "RSA", "kid": "key", "use": "sig", "alg": "RS256", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())}}})
				case "/token":
					if scenario == "wrong callback origin" {
						t.Error("origin mismatch reached the token endpoint")
					}
					if err := r.ParseForm(); err != nil {
						t.Error(err)
						w.WriteHeader(400)
						return
					}
					if pkce(r.Form.Get("code_verifier")) != challenge {
						t.Error("missing upstream PKCE")
						w.WriteHeader(400)
						return
					}
					if r.Form.Get("redirect_uri") != baseURL+"/api/v1/auth/oidc/callback" {
						t.Error("token exchange changed the initiating redirect URI")
						w.WriteHeader(400)
						return
					}
					claims := map[string]any{"iss": issuer, "sub": "owner", "aud": "client", "nonce": nonce, "iat": time.Now().Unix(), "exp": time.Now().Add(time.Minute).Unix()}
					switch scenario {
					case "wrong nonce":
						claims["nonce"] = "wrong"
					case "wrong audience":
						claims["aud"] = "other"
					case "wrong issuer":
						claims["iss"] = "https://other.example"
					case "unlisted subject":
						claims["sub"] = "stranger"
					case "expired":
						claims["exp"] = time.Now().Add(-time.Minute).Unix()
					}
					_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "upstream-secret", "token_type": "Bearer", "id_token": signedTestJWT(t, key, claims)})
				default:
					w.WriteHeader(404)
				}
			}))
			defer provider.Close()
			issuer = provider.URL
			s.client = provider.Client()
			cfg := otpPolicy()
			cfg.Mode = "oidc"
			cfg.OIDC = settings.OIDC{Issuer: issuer, ClientID: "client", ClientSecret: "secret", AllowedSubjects: []string{"owner"}}
			if scenario == "empty admission list" {
				cfg.OIDC.AllowedSubjects = nil
			}
			native := scenario == "native" || scenario == "native prefix" || scenario == "PKCE mismatch"
			verifier := strings.Repeat("a", 43)
			target, err := s.CreateBrowserLogin(BrowserRequest{Policy: cfg, Origin: origin, BasePath: basePath, Native: native, State: strings.Repeat("s", 32), CodeChallenge: pkce(verifier)})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(target, baseURL+"/api/v1/auth/browser-sessions/") {
				t.Fatal("browser entry point changed the initiating origin")
			}
			start, err := s.StartBrowserLogin(t.Context(), BrowserStartRequest{Policy: cfg, ID: target[strings.LastIndex(target, "/")+1:], Origin: origin})
			if err != nil {
				t.Fatal(err)
			}
			authorization, err := url.Parse(start.URL)
			if err != nil {
				t.Fatal(err)
			}
			nonce = authorization.Query().Get("nonce")
			if authorization.Query().Get("redirect_uri") != baseURL+"/api/v1/auth/oidc/callback" {
				t.Fatal("authorization did not use the initiating origin")
			}
			challenge = authorization.Query().Get("code_challenge")
			if nonce == "" || challenge == "" || authorization.Query().Get("code_challenge_method") != "S256" {
				t.Fatal("missing proof in authorization request")
			}
			callback := BrowserCallback{Policy: cfg, Origin: origin, State: start.State, Code: "code", Binding: start.Binding}
			switch scenario {
			case "wrong binding":
				callback.Binding = "wrong"
			case "wrong state":
				callback.State = "wrong"
			case "wrong callback origin":
				callback.Origin = "https://evil.example"
			case "policy changed":
				callback.Policy.SessionMaxDays++
			}
			result, err := s.CompleteBrowserLogin(t.Context(), callback)
			if scenario != "web" && scenario != "empty admission list" && scenario != "alternate origin" && scenario != "HTTPS port" && scenario != "web prefix" && !native {
				if err == nil {
					t.Fatal("accepted invalid login")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.CompleteBrowserLogin(t.Context(), callback); err == nil {
				t.Fatal("callback replay accepted")
			}
			if !native {
				if result.RedirectURL != baseURL+"/" {
					t.Fatal("web login did not return to the initiating origin")
				}
				if result.Identity.Subject != "owner" {
					t.Fatal("wrong identity")
				}
				return
			}
			redirect, err := url.Parse(result.RedirectURL)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(result.RedirectURL, NativeRedirectURL+"?") || strings.Contains(result.RedirectURL, "upstream-secret") {
				t.Fatal("unsafe native redirect")
			}
			if redirect.Query().Get("state") != strings.Repeat("s", 32) {
				t.Fatal("lost app state")
			}
			if scenario == "PKCE mismatch" {
				verifier = strings.Repeat("b", 43)
			}
			handoff := HandoffRequest{Policy: cfg, Code: redirect.Query().Get("code"), Verifier: verifier}
			identity, err := s.CompleteHandoff(handoff)
			if scenario == "PKCE mismatch" {
				if !errors.Is(err, ErrInvalidLogin) {
					t.Fatal("accepted wrong handoff proof")
				}
				return
			}
			if err != nil || identity.Subject != "owner" {
				t.Fatalf("handoff=%+v, %v", identity, err)
			}
			if _, err := s.CompleteHandoff(handoff); err == nil {
				t.Fatal("handoff replay accepted")
			}
		})
	}
}

func TestBrowserLoginOrigin(t *testing.T) {
	for _, tt := range []struct {
		name, origin string
		valid        bool
	}{
		{name: "HTTPS", origin: "https://sigmo.example", valid: true},
		{name: "HTTPS port", origin: "https://sigmo.example:8443", valid: true},
		{name: "IPv6", origin: "https://[::1]:8443", valid: true},
		{name: "empty"},
		{name: "HTTP", origin: "http://sigmo.example"},
		{name: "missing hostname", origin: "https://:8443"},
		{name: "credentials", origin: "https://user@sigmo.example"},
		{name: "path", origin: "https://sigmo.example/unexpected"},
		{name: "query", origin: "https://sigmo.example?next=elsewhere"},
		{name: "empty query", origin: "https://sigmo.example?"},
		{name: "fragment", origin: "https://sigmo.example#fragment"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := testAuthStore(t)
			cfg := otpPolicy()
			cfg.Mode = "oidc"
			target, err := s.CreateBrowserLogin(BrowserRequest{Policy: cfg, Origin: tt.origin})
			if !tt.valid {
				if !errors.Is(err, ErrInvalidLogin) {
					t.Fatalf("CreateBrowserLogin() error = %v, want ErrInvalidLogin", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			_, err = s.StartBrowserLogin(t.Context(), BrowserStartRequest{
				Policy: cfg, ID: target[strings.LastIndex(target, "/")+1:], Origin: "https://evil.example",
			})
			if !errors.Is(err, ErrInvalidLogin) {
				t.Fatalf("StartBrowserLogin() on another origin error = %v, want ErrInvalidLogin", err)
			}
		})
	}
}

func TestBrowserLoginBasePath(t *testing.T) {
	for _, tt := range []struct {
		basePath string
		valid    bool
	}{
		{"", true}, {"/sigmo", true}, {"/apps/sigmo", true}, {"/my%20sigmo", true},
		{"https://evil.example", false}, {"//evil.example", false}, {"/", false},
		{"/a/../b", false}, {"/%2e%2e/b", false}, {"/a//b", false}, {"/a\\b", false},
		{"/sigmo?next=elsewhere", false}, {"/sigmo?", false}, {"/sigmo#fragment", false},
	} {
		t.Run(tt.basePath, func(t *testing.T) {
			s := testAuthStore(t)
			cfg := otpPolicy()
			cfg.Mode = "oidc"
			_, err := s.CreateBrowserLogin(BrowserRequest{Policy: cfg, Origin: "https://sigmo.example", BasePath: tt.basePath})
			if (err == nil) != tt.valid {
				t.Fatalf("CreateBrowserLogin() error = %v, want valid %v", err, tt.valid)
			}
		})
	}
}

func TestAccessAssertion(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cdn-cgi/access/certs" {
			t.Error("wrong keys endpoint")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{"kty": "RSA", "kid": "key", "alg": "RS256", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": "AQAB"}}})
	}))
	defer server.Close()
	s := testAuthStore(t)
	s.client = server.Client()
	cfg := settings.Auth{Mode: "cloudflare_access", CloudflareAccess: settings.CloudflareAccess{TeamDomain: server.URL, Audience: "app-aud", AllowedSubjects: []string{"owner"}}}
	for _, scenario := range []string{"valid", "wrong aud", "wrong issuer", "unlisted", "expired", "future", "unsigned"} {
		t.Run(scenario, func(t *testing.T) {
			claims := map[string]any{"iss": server.URL, "aud": []string{"app-aud"}, "sub": "owner", "exp": time.Now().Add(time.Minute).Unix(), "iat": time.Now().Unix()}
			switch scenario {
			case "wrong aud":
				claims["aud"] = "other"
			case "wrong issuer":
				claims["iss"] = "https://other.example"
			case "unlisted":
				claims["sub"] = "other"
			case "expired":
				claims["exp"] = time.Now().Add(-time.Minute).Unix()
			case "future":
				claims["nbf"] = time.Now().Add(time.Minute).Unix()
			}
			assertion := signedTestJWT(t, key, claims)
			if scenario == "unsigned" {
				assertion = "owner@example.com"
			}
			_, err := s.VerifyAccess(t.Context(), AccessRequest{Policy: cfg, Assertion: assertion})
			if scenario == "valid" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if !errors.Is(err, storage.ErrInvalidSession) {
				t.Fatalf("invalid assertion accepted: %v", err)
			}
		})
	}
}

func TestOIDCTestFlow(t *testing.T) {
	for _, scenario := range []string{"success", "subject not allowed", "empty admission list", "wrong nonce", "token exchange failed"} {
		t.Run(scenario, func(t *testing.T) {
			key, err := rsa.GenerateKey(rand.Reader, 2048)
			if err != nil {
				t.Fatal(err)
			}
			s := testAuthStore(t)
			var issuer, nonce string
			provider := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/.well-known/openid-configuration":
					_ = json.NewEncoder(w).Encode(map[string]any{
						"issuer": issuer, "authorization_endpoint": issuer + "/authorize",
						"token_endpoint": issuer + "/token", "jwks_uri": issuer + "/keys",
						"id_token_signing_alg_values_supported": []string{"RS256"},
					})
				case "/keys":
					_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
						"kty": "RSA", "kid": "key", "use": "sig", "alg": "RS256",
						"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
						"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
					}}})
				case "/token":
					if scenario == "token exchange failed" {
						w.WriteHeader(http.StatusUnauthorized)
						_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_client"})
						return
					}
					if err := r.ParseForm(); err != nil {
						t.Error(err)
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					if len(r.Form.Get("code_verifier")) < 43 {
						t.Error("OIDC test used an invalid PKCE verifier")
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					tokenNonce := nonce
					if scenario == "wrong nonce" {
						tokenNonce = "wrong"
					}
					subject := "owner"
					if scenario == "subject not allowed" {
						subject = "stranger"
					}
					claims := map[string]any{
						"iss": issuer, "sub": subject, "aud": "client", "nonce": tokenNonce,
						"iat": time.Now().Unix(), "exp": time.Now().Add(time.Minute).Unix(),
					}
					_ = json.NewEncoder(w).Encode(map[string]string{
						"access_token": "upstream-secret", "token_type": "Bearer", "id_token": signedTestJWT(t, key, claims),
					})
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer provider.Close()
			issuer = provider.URL
			s.client = provider.Client()

			cfg := otpPolicy()
			cfg.Mode = "oidc"
			cfg.OIDC = settings.OIDC{
				Issuer: issuer, ClientID: "client", ClientSecret: "secret", AllowedSubjects: []string{"owner"},
			}
			if scenario == "empty admission list" {
				cfg.OIDC.AllowedSubjects = nil
			}
			origin := "https://sigmo.example"
			id, expiresAt, err := s.CreateOIDCTest(OIDCTestRequest{
				Policy: cfg, Origin: origin, BasePath: "/sigmo",
			})
			if err != nil {
				t.Fatal(err)
			}
			if !expiresAt.After(time.Now()) {
				t.Fatal("test did not receive a deadline")
			}
			pending, ok := s.OIDCTestResult(id)
			if !ok || pending.Status != AuthTestStatusPending {
				t.Fatalf("pending result = %+v, %v", pending, ok)
			}
			start, err := s.StartOIDCTest(t.Context(), OIDCTestStartRequest{ID: id, Origin: origin})
			if err != nil {
				t.Fatal(err)
			}
			authorization, err := url.Parse(start.URL)
			if err != nil {
				t.Fatal(err)
			}
			nonce = authorization.Query().Get("nonce")
			if nonce == "" || authorization.Query().Get("code_challenge_method") != "S256" {
				t.Fatal("test login did not include PKCE and nonce")
			}
			completion := s.CompleteOIDCTest(t.Context(), OIDCTestCallback{
				Origin: origin, State: start.State, Code: "code", Binding: start.Binding,
			})
			result := completion.Result
			switch scenario {
			case "success":
				if result.Status != AuthTestStatusSucceeded || result.Subject != "owner" {
					t.Fatalf("result = %+v", result)
				}
			case "subject not allowed":
				if result.Status != AuthTestStatusFailed || result.Reason != AuthTestReasonSubjectNotAllowed || result.Subject != "stranger" {
					t.Fatalf("result = %+v", result)
				}
			case "empty admission list":
				if result.Status != AuthTestStatusSucceeded || result.Subject != "owner" {
					t.Fatalf("result = %+v", result)
				}
			case "wrong nonce":
				if result.Status != AuthTestStatusFailed || result.Reason != AuthTestReasonTokenInvalid {
					t.Fatalf("result = %+v", result)
				}
			case "token exchange failed":
				if result.Status != AuthTestStatusFailed || result.Reason != AuthTestReasonTokenExchangeFailed {
					t.Fatalf("result = %+v", result)
				}
			}
			if _, ok := s.OIDCTestResult(id); !ok {
				t.Fatal("terminal test result disappeared")
			}
			if s.IsOIDCTest(start.State) {
				t.Fatal("test callback remained replayable")
			}
		})
	}
}

func TestOIDCTestDiscoveryFailureAndExpiry(t *testing.T) {
	s := testAuthStore(t)
	cfg := otpPolicy()
	cfg.Mode = "oidc"
	cfg.OIDC = settings.OIDC{
		Issuer: "https://127.0.0.1:1", ClientID: "client", ClientSecret: "secret", AllowedSubjects: []string{"owner"},
	}
	id, _, err := s.CreateOIDCTest(OIDCTestRequest{Policy: cfg, Origin: "https://sigmo.example"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartOIDCTest(t.Context(), OIDCTestStartRequest{ID: id, Origin: "https://sigmo.example"}); err == nil {
		t.Fatal("unreachable issuer was accepted")
	}
	result, ok := s.OIDCTestResult(id)
	if !ok || result.Reason != AuthTestReasonDiscoveryFailed {
		t.Fatalf("result = %+v, %v", result, ok)
	}

	expiredID, _, err := s.CreateOIDCTest(OIDCTestRequest{Policy: cfg, Origin: "https://sigmo.example"})
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.tests[expiredID].expiresAt = time.Now().Add(-time.Second)
	s.mu.Unlock()
	if _, ok := s.OIDCTestResult(expiredID); ok {
		t.Fatal("expired test remained available")
	}
}

func TestOIDCLoginRejectsMismatchedTransactionPolicy(t *testing.T) {
	s := testAuthStore(t)
	cfg := otpPolicy()
	cfg.Mode = "oidc"
	_, err := s.beginOIDCLogin(t.Context(), cfg, login{policyID: "another-policy"})
	var failure *oidcStartFailure
	if !errors.As(err, &failure) || failure.kind != oidcStartAuthorization || !errors.Is(err, ErrInvalidLogin) {
		t.Fatalf("error = %v, want authorization failure", err)
	}
}

func TestCloudflareAccessTestResult(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "kid": "key", "alg": "RS256",
			"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": "AQAB",
		}}})
	}))
	defer server.Close()
	s := testAuthStore(t)
	s.client = server.Client()
	cfg := settings.Auth{
		Mode: "cloudflare_access",
		CloudflareAccess: settings.CloudflareAccess{
			TeamDomain: server.URL, Audience: "app-aud", AllowedSubjects: []string{"owner"},
		},
	}
	claims := map[string]any{
		"iss": server.URL, "aud": []string{"app-aud"}, "sub": "owner",
		"exp": time.Now().Add(time.Minute).Unix(), "iat": time.Now().Unix(),
	}
	assertion := signedTestJWT(t, key, claims)
	if result := s.TestCloudflareAccess(t.Context(), cfg, assertion); result.Status != AuthTestStatusSucceeded || result.Subject != "owner" {
		t.Fatalf("valid result = %+v", result)
	}
	if result := s.TestCloudflareAccess(t.Context(), cfg, ""); result.Reason != AuthTestReasonAccessAssertionMissing {
		t.Fatalf("missing result = %+v", result)
	}
	claims["sub"] = "stranger"
	if result := s.TestCloudflareAccess(t.Context(), cfg, signedTestJWT(t, key, claims)); result.Reason != AuthTestReasonSubjectNotAllowed || result.Subject != "stranger" {
		t.Fatalf("subject result = %+v", result)
	}
	if result := s.TestCloudflareAccess(t.Context(), cfg, "not-a-jwt"); result.Reason != AuthTestReasonAccessAssertionInvalid {
		t.Fatalf("invalid result = %+v", result)
	}
}

func signedTestJWT(t *testing.T, key *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	body, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	raw := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"key"}`)) + "." + base64.RawURLEncoding.EncodeToString(body)
	digest := sha256.Sum256([]byte(raw))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return raw + "." + base64.RawURLEncoding.EncodeToString(signature)
}
