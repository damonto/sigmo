package license

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	jose "github.com/go-jose/go-jose/v4"

	"github.com/damonto/sigmo/internal/pkg/apierror"
	"github.com/damonto/sigmo/internal/pkg/carrierconfig"
)

const maxConfigurationResponse = 12 << 20

// State changes never wait for a configuration HTTP request. refreshMu only
// serializes downloads; mu protects publication, revocation and cache writes.
type configurationState struct {
	refreshMu sync.Mutex
	mu        sync.Mutex
	path      string
	key       *ecdsa.PrivateKey
	current   *carrierconfig.Config
	grant     configurationGrant
	denied    bool
	retryAt   time.Time
}

type configurationGrant struct {
	SchemaVersion int       `json:"schemaVersion"`
	DeviceID      string    `json:"deviceId"`
	SessionID     string    `json:"sessionId"`
	Revision      string    `json:"revision"`
	IssuedAt      time.Time `json:"issuedAt"`
	RefreshAfter  time.Time `json:"refreshAfter"`
	ExpiresAt     time.Time `json:"expiresAt"`
	Bundle        string    `json:"bundle"`
}

// CarrierConfig returns the current official catalogs. It serializes refreshes,
// keeps only ciphertext on disk, and never extends a signed offline deadline.
func (c *Controller) CarrierConfig(ctx context.Context) (*carrierconfig.Config, error) {
	s := &c.configuration
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	s.mu.Lock()
	if err := ctx.Err(); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	if s.denied || !c.Authorized() {
		s.mu.Unlock()
		return nil, errExplicitUnauthorized
	}
	now := time.Now()
	if config := s.cached(now); config != nil && (now.Before(s.grant.RefreshAfter) || now.Before(s.retryAt)) {
		s.mu.Unlock()
		return config, nil
	}
	lease, _, deadline := c.currentLeaseState()
	if lease == nil || !now.Before(deadline) {
		config := s.cached(now)
		s.mu.Unlock()
		if config != nil {
			return config, nil
		}
		return nil, errExplicitUnauthorized
	}
	if s.key == nil {
		key, err := loadConfigurationKey(s.path + ".key")
		if err != nil {
			s.mu.Unlock()
			return nil, err
		}
		s.key = key
	}
	key := s.key
	s.mu.Unlock()

	compact, attempt, err := c.downloadConfiguration(ctx, &key.PublicKey)

	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if s.denied || !c.Authorized() {
		return nil, errExplicitUnauthorized
	}
	if err != nil {
		var remote *apierror.Error
		if errors.As(err, &remote) && explicitAuthorizationError(remote) {
			c.mu.Lock()
			sameSession := attempt != nil && c.session != nil && c.session.SessionID == attempt.SessionID && c.session.Generation == attempt.Generation
			if sameSession {
				c.clearLeaseLocked()
			}
			c.mu.Unlock()
			if !sameSession {
				return nil, err
			}
			s.current = nil
			s.grant = configurationGrant{}
			s.denied = true
			c.notifyLeaseChanged()
			if removeErr := os.Remove(s.path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				return nil, errors.Join(err, fmt.Errorf("remove revoked configuration: %w", removeErr))
			}
			return nil, errors.Join(errExplicitUnauthorized, err)
		}
		var requestErr *url.Error
		temporary := errors.As(err, &requestErr) || (remote != nil && (remote.StatusCode >= 500 || remote.StatusCode == http.StatusTooManyRequests))
		if config := s.cached(time.Now()); config != nil && temporary {
			s.retryAt = time.Now().Add(time.Minute)
			if remote != nil && remote.RetryAt.After(s.retryAt) {
				s.retryAt = remote.RetryAt
			}
			return config, nil
		}
		return nil, err
	}
	if err := c.acceptConfiguration(compact); err != nil {
		return nil, err
	}
	if err := writeIdentity(s.path, []byte(compact)); err != nil {
		return nil, fmt.Errorf("save encrypted carrier configuration: %w", err)
	}
	return s.current, nil
}

// cached runs with mu held and returns only an unexpired, verified snapshot.
func (s *configurationState) cached(now time.Time) *carrierconfig.Config {
	if s.current != nil && now.Before(s.grant.ExpiresAt) {
		return s.current
	}
	return nil
}

func (c *Controller) downloadConfiguration(ctx context.Context, key *ecdsa.PublicKey) (string, *Lease, error) {
	der, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		return "", nil, fmt.Errorf("encode configuration recipient: %w", err)
	}
	query := url.Values{"recipient": {base64.RawURLEncoding.EncodeToString(der)}}
	requestCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, attempt, err := c.signedRequest(requestCtx, http.MethodGet, "/v1/configurations/current?"+query.Encode())
	if err != nil {
		return "", nil, err
	}
	response, err := c.client.Do(req)
	if err != nil {
		return "", attempt, fmt.Errorf("download carrier configuration: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", attempt, apierror.Read(response)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxConfigurationResponse+1))
	if err != nil {
		return "", attempt, fmt.Errorf("read carrier configuration: %w", err)
	}
	if len(data) > maxConfigurationResponse {
		return "", attempt, errors.New("carrier configuration exceeds size limit")
	}
	var result struct {
		Configuration string `json:"configuration"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", attempt, fmt.Errorf("decode carrier configuration response: %w", err)
	}
	return result.Configuration, attempt, nil
}

// acceptConfiguration runs with configuration.mu held. No state changes until
// authentication, metadata checks, decompression and all three parsers succeed.
func (c *Controller) acceptConfiguration(compact string) error {
	if len(compact) > maxConfigurationResponse {
		return errors.New("encrypted carrier configuration exceeds size limit")
	}
	encrypted, err := jose.ParseEncrypted(compact, []jose.KeyAlgorithm{jose.ECDH_ES}, []jose.ContentEncryption{jose.A256GCM})
	if err != nil {
		return fmt.Errorf("parse encrypted carrier configuration: %w", err)
	}
	plaintext, err := encrypted.Decrypt(c.configuration.key)
	if err != nil {
		return fmt.Errorf("decrypt carrier configuration: %w", err)
	}
	var signed struct {
		Payload   string `json:"payload"`
		Signature string `json:"signature"`
	}
	if err := json.Unmarshal(plaintext, &signed); err != nil {
		return fmt.Errorf("decode signed carrier configuration: %w", err)
	}
	payload, err := base64.RawURLEncoding.DecodeString(signed.Payload)
	if err != nil {
		return errors.New("invalid carrier configuration payload")
	}
	signature, err := decodeKey(signed.Signature)
	message := append([]byte(carrierconfig.SignatureContext), payload...)
	if err != nil || len(c.licensePublicKey) != ed25519.PublicKeySize || !ed25519.Verify(c.licensePublicKey, message, signature) {
		return errors.New("invalid carrier configuration signature")
	}
	var grant configurationGrant
	if err := json.Unmarshal(payload, &grant); err != nil {
		return fmt.Errorf("decode carrier configuration metadata: %w", err)
	}
	session := c.currentSession()
	now := time.Now()
	if grant.SchemaVersion != 1 || grant.DeviceID != c.identity.DeviceID || session == nil ||
		grant.SessionID != session.SessionID || grant.IssuedAt.After(now.Add(5*time.Minute)) ||
		!grant.ExpiresAt.After(now) || !grant.ExpiresAt.After(grant.IssuedAt) ||
		grant.ExpiresAt.Sub(grant.IssuedAt) > maxLeaseLifetime ||
		grant.RefreshAfter.After(grant.ExpiresAt) {
		return errors.New("invalid carrier configuration metadata")
	}
	compressed, err := base64.RawURLEncoding.DecodeString(grant.Bundle)
	if err != nil {
		return errors.New("invalid compressed carrier configuration")
	}
	config, err := carrierconfig.Unpack(compressed, grant.Revision)
	if err != nil {
		return err
	}
	c.mu.Lock()
	if c.session == nil || c.session.SessionID != grant.SessionID {
		c.mu.Unlock()
		return errors.New("configuration session changed during verification")
	}
	c.configurationDeadline = now.Add(grant.ExpiresAt.Sub(now))
	c.mu.Unlock()
	grant.Bundle = ""
	c.configuration.current, c.configuration.grant = config, grant
	c.configuration.retryAt = time.Time{}
	return nil
}

// loadCachedConfiguration restores only a device-bound, signed, unexpired grant.
// Invalid caches never prevent the base application from starting.
func (c *Controller) loadCachedConfiguration() {
	s := &c.configuration
	s.mu.Lock()
	defer s.mu.Unlock()
	cached, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	key, err := loadConfigurationKey(s.path + ".key")
	if err != nil {
		return
	}
	s.key = key
	_ = c.acceptConfiguration(string(cached)) // An online response replaces invalid caches.
}

func loadConfigurationKey(path string) (*ecdsa.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		key, err := x509.ParsePKCS8PrivateKey(data)
		if err != nil {
			return nil, fmt.Errorf("parse configuration private key: %w", err)
		}
		privateKey, ok := key.(*ecdsa.PrivateKey)
		if !ok || privateKey.Curve != elliptic.P256() {
			return nil, errors.New("invalid configuration private key")
		}
		if err := os.Chmod(path, 0o600); err != nil {
			return nil, err
		}
		return privateKey, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read configuration private key: %w", err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate configuration private key: %w", err)
	}
	data, err = x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	if err := writeIdentity(path, data); err != nil {
		return nil, err
	}
	return key, nil
}
