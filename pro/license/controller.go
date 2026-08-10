package license

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	appupdate "github.com/damonto/sigmo/internal/app/update"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

const (
	leaseStateKey          = "lease"
	maxResponseSize        = 1 << 20
	metadataRequestTimeout = 15 * time.Second
	leaseClockSkew         = 5 * time.Minute
	maxLeaseRefreshDelay   = 24 * time.Hour
	maxLeaseLifetime       = 72 * time.Hour
)

var (
	// WorkerURL and LicensePublicKey are injected into Pro release builds. They
	// are intentionally not configurable at runtime.
	WorkerURL        string
	LicensePublicKey string

	errExplicitUnauthorized = errors.New("product authorization is revoked or expired")
)

type Config struct {
	BaseURL          string
	LicensePublicKey string
	ReleasePublicKey string
	Storage          *storage.Store
	Client           *http.Client
	Restart          func()
}

type Controller struct {
	baseURL          string
	licensePublicKey ed25519.PublicKey
	releasePublicKey string
	storage          *storage.Store
	client           *http.Client
	restart          func()
	identity         identity

	mu           sync.RWMutex
	lease        *Lease
	leaseProof   *signedLease
	pairings     map[string]pairingSession
	leaseChanged chan struct{}
}

func New(ctx context.Context, cfg Config) (*Controller, error) {
	if cfg.Storage == nil {
		return nil, errors.New("license storage is required")
	}
	client := authorizationHTTPClient(cfg.Client)
	baseURL, err := normalizeServiceURL(cfg.BaseURL)
	if err != nil {
		return nil, err
	}
	publicKeyValue := strings.TrimSpace(cfg.LicensePublicKey)
	var publicKey ed25519.PublicKey
	if publicKeyValue != "" {
		decoded, err := decodeKey(publicKeyValue)
		if err != nil || len(decoded) != ed25519.PublicKeySize {
			return nil, errors.New("decode license public key: invalid Ed25519 public key")
		}
		publicKey = ed25519.PublicKey(decoded)
	}
	identity, err := loadOrCreateIdentity(ctx, cfg.Storage)
	if err != nil {
		return nil, err
	}
	return &Controller{
		baseURL:          baseURL,
		licensePublicKey: publicKey,
		releasePublicKey: cfg.ReleasePublicKey,
		storage:          cfg.Storage,
		client:           client,
		restart:          cfg.Restart,
		identity:         identity,
		pairings:         make(map[string]pairingSession),
		leaseChanged:     make(chan struct{}, 1),
	}, nil
}

func authorizationHTTPClient(client *http.Client) *http.Client {
	if client == nil {
		client = &http.Client{}
	}
	cloned := *client
	cloned.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &cloned
}

func (c *Controller) Start(ctx context.Context) error {
	proof, err := c.readLease(ctx)
	if errors.Is(err, storage.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	lease, err := c.verifyLease(proof, time.Now())
	if err != nil {
		if removeErr := c.removeLease(ctx); removeErr != nil {
			return errors.Join(err, fmt.Errorf("remove invalid saved license lease: %w", removeErr))
		}
		return err
	}
	c.setLease(lease, &proof)
	if err := c.refresh(ctx); err != nil {
		if errors.Is(err, errExplicitUnauthorized) {
			c.clearLease(ctx)
		}
		return err
	}
	return nil
}

func (c *Controller) Authorized() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lease != nil && c.lease.Status == "active" && time.Now().Before(c.lease.ExpiresAt)
}

func (c *Controller) Licensee() *appupdate.Licensee {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.lease == nil {
		return nil
	}
	var entitlementExpiresAt *time.Time
	if c.lease.EntitlementExpiresAt != nil {
		value := *c.lease.EntitlementExpiresAt
		entitlementExpiresAt = &value
	}
	expiresAt := c.lease.ExpiresAt
	return &appupdate.Licensee{
		Status:       c.lease.Status,
		TelegramID:   c.lease.TelegramID,
		DisplayName:  c.lease.DisplayName,
		Username:     c.lease.Username,
		ExpiresAt:    entitlementExpiresAt,
		OfflineUntil: &expiresAt,
	}
}

func (c *Controller) Run(ctx context.Context) error {
	var retryAt time.Time
	for {
		lease := c.currentLease()
		if lease == nil {
			return nil
		}
		now := time.Now()
		if !now.Before(lease.ExpiresAt) {
			c.clearLease(ctx)
			c.restartHealthy()
			return nil
		}
		next := lease.RefreshAfter
		if !retryAt.IsZero() {
			next = retryAt
		}
		if next.After(lease.ExpiresAt) {
			next = lease.ExpiresAt
		}
		wait := time.Until(next)
		if wait < 0 {
			wait = 0
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-c.leaseChanged:
			timer.Stop()
			retryAt = time.Time{}
			continue
		case <-timer.C:
		}
		if !time.Now().Before(lease.ExpiresAt) {
			c.clearLease(ctx)
			c.restartHealthy()
			return nil
		}
		err := c.refresh(ctx)
		if err == nil {
			retryAt = time.Time{}
			continue
		}
		if errors.Is(err, errExplicitUnauthorized) || !c.Authorized() {
			c.clearLease(ctx)
			c.restartHealthy()
			return nil
		}
		lease = c.currentLease()
		if lease == nil {
			return nil
		}
		slog.Warn("refresh product authorization", "error", err, "offline_until", lease.ExpiresAt)
		retryAt = time.Now().Add(time.Hour)
		if retryAt.After(lease.ExpiresAt) {
			retryAt = lease.ExpiresAt
		}
	}
}

func (c *Controller) restartHealthy() {
	if c.restart == nil {
		return
	}
	executable, err := os.Executable()
	if err != nil {
		slog.Warn("resolve executable before authorization restart", "error", err)
	} else if err := appupdate.MarkHealthy(executable); err != nil {
		slog.Warn("mark updated binary healthy before authorization restart", "error", err)
	}
	c.restart()
}

func (c *Controller) refresh(ctx context.Context) error {
	if c.baseURL == "" || len(c.licensePublicKey) == 0 {
		return errors.New("Pro authorization service is not configured")
	}
	var challengeResponse challenge
	_, err := c.doJSON(ctx, http.MethodPost, "/v1/license-challenges", map[string]string{
		"deviceId": c.identity.DeviceID,
	}, "", &challengeResponse)
	if err != nil {
		return c.classifyAuthorizationError(err)
	}
	challengeBytes, err := decodeKey(challengeResponse.Challenge)
	if err != nil {
		return fmt.Errorf("decode license challenge: %w", err)
	}
	signature := ed25519.Sign(c.identity.PrivateKey, challengeBytes)
	var proof signedLease
	_, err = c.doJSON(ctx, http.MethodPost, "/v1/license-leases", map[string]string{
		"deviceId":  c.identity.DeviceID,
		"challenge": challengeResponse.Challenge,
		"signature": base64.RawStdEncoding.EncodeToString(signature),
	}, "", &proof)
	if err != nil {
		return c.classifyAuthorizationError(err)
	}
	lease, err := c.verifyLease(proof, time.Now())
	if err != nil {
		return err
	}
	if err := c.saveLease(ctx, proof); err != nil {
		return err
	}
	c.setLease(lease, &proof)
	return nil
}

func (c *Controller) classifyAuthorizationError(err error) error {
	var remote *serviceError
	if errors.As(err, &remote) && isExplicitAuthorizationError(remote.ErrorCode) {
		return fmt.Errorf("%w: %w", errExplicitUnauthorized, err)
	}
	return err
}

func isExplicitAuthorizationError(code string) bool {
	switch code {
	case "license_device_unauthorized", "license_entitlement_inactive", "license_lease_expired":
		return true
	default:
		return false
	}
}

func (c *Controller) verifyLease(proof signedLease, now time.Time) (*Lease, error) {
	if len(c.licensePublicKey) == 0 {
		return nil, errors.New("verify license lease: public key is unavailable")
	}
	signature, err := decodeKey(proof.Signature)
	if err != nil || !ed25519.Verify(c.licensePublicKey, proof.Lease, signature) {
		return nil, errors.New("verify license lease: invalid signature")
	}
	var lease Lease
	if err := json.Unmarshal(proof.Lease, &lease); err != nil {
		return nil, fmt.Errorf("decode license lease: %w", err)
	}
	if lease.SchemaVersion != leaseSchemaVersion || lease.DeviceID != c.identity.DeviceID || lease.TelegramID <= 0 || lease.Status != "active" {
		return nil, errors.New("verify license lease: metadata mismatch")
	}
	if lease.IssuedAt.After(now.Add(leaseClockSkew)) ||
		!lease.RefreshAfter.After(lease.IssuedAt) ||
		lease.RefreshAfter.Sub(lease.IssuedAt) > maxLeaseRefreshDelay ||
		!lease.ExpiresAt.After(lease.RefreshAfter) ||
		lease.ExpiresAt.Sub(lease.IssuedAt) > maxLeaseLifetime {
		return nil, errors.New("verify license lease: invalid validity period")
	}
	if lease.EntitlementExpiresAt != nil && lease.EntitlementExpiresAt.Before(lease.ExpiresAt) {
		return nil, errors.New("verify license lease: entitlement expires before lease")
	}
	if !now.Before(lease.ExpiresAt) {
		return nil, errExplicitUnauthorized
	}
	return &lease, nil
}

func (c *Controller) setLease(lease *Lease, proof *signedLease) {
	c.mu.Lock()
	c.lease = cloneLease(lease)
	if proof == nil {
		c.leaseProof = nil
	} else {
		cloned := *proof
		cloned.Lease = bytes.Clone(proof.Lease)
		c.leaseProof = &cloned
	}
	c.mu.Unlock()
	c.notifyLeaseChanged()
}

func (c *Controller) clearLease(ctx context.Context) {
	c.mu.Lock()
	c.lease = nil
	c.leaseProof = nil
	c.mu.Unlock()
	c.notifyLeaseChanged()
	if err := c.removeLease(ctx); err != nil {
		slog.Warn("remove saved license lease", "error", err)
	}
}

func (c *Controller) currentLease() *Lease {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.lease == nil {
		return nil
	}
	return cloneLease(c.lease)
}

func cloneLease(lease *Lease) *Lease {
	if lease == nil {
		return nil
	}
	cloned := *lease
	if lease.EntitlementExpiresAt != nil {
		value := *lease.EntitlementExpiresAt
		cloned.EntitlementExpiresAt = &value
	}
	return &cloned
}

func (c *Controller) notifyLeaseChanged() {
	select {
	case c.leaseChanged <- struct{}{}:
	default:
	}
}

func (c *Controller) readLease(ctx context.Context) (signedLease, error) {
	var proof signedLease
	if err := c.storage.Get(ctx, licenseStateScope, leaseStateKey, &proof); err != nil {
		return signedLease{}, err
	}
	return proof, nil
}

func (c *Controller) saveLease(ctx context.Context, proof signedLease) error {
	if err := c.storage.Put(ctx, licenseStateScope, leaseStateKey, proof); err != nil {
		return fmt.Errorf("save license lease: %w", err)
	}
	return nil
}

func (c *Controller) removeLease(ctx context.Context) error {
	return c.storage.Delete(ctx, licenseStateScope, leaseStateKey)
}

func (c *Controller) doJSON(ctx context.Context, method string, path string, body any, bearer string, dst any) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, metadataRequestTimeout)
	defer cancel()
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return 0, fmt.Errorf("encode authorization request: %w", err)
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return 0, fmt.Errorf("create authorization request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("contact authorization service: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize+1))
	if err != nil {
		return resp.StatusCode, fmt.Errorf("read authorization response: %w", err)
	}
	if len(data) > maxResponseSize {
		return resp.StatusCode, errors.New("authorization response exceeds size limit")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var response errorResponse
		if err := json.Unmarshal(data, &response); err != nil {
			response = errorResponse{}
		}
		if response.ErrorCode == "" {
			response.ErrorCode = "authorization_service_error"
		}
		if response.Message == "" {
			response.Message = http.StatusText(resp.StatusCode)
		}
		return resp.StatusCode, &serviceError{
			StatusCode: resp.StatusCode,
			ErrorCode:  response.ErrorCode,
			Message:    response.Message,
		}
	}
	if dst != nil {
		if err := json.Unmarshal(data, dst); err != nil {
			return resp.StatusCode, fmt.Errorf("decode authorization response: %w", err)
		}
	}
	return resp.StatusCode, nil
}

func (c *Controller) signedRequest(ctx context.Context, method string, rawURL string) (*http.Request, error) {
	c.mu.RLock()
	proof := c.leaseProof
	c.mu.RUnlock()
	if proof == nil || !c.Authorized() {
		return nil, errExplicitUnauthorized
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse update URL: %w", err)
	}
	if parsed.IsAbs() {
		base, err := url.Parse(c.baseURL)
		if err != nil {
			return nil, fmt.Errorf("parse authorization service URL: %w", err)
		}
		if !sameOrigin(parsed, base) {
			return nil, errors.New("reject update URL: origin does not match authorization service")
		}
	} else {
		parsed, err = url.Parse(c.baseURL + rawURL)
		if err != nil {
			return nil, fmt.Errorf("resolve update URL: %w", err)
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, parsed.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create update request: %w", err)
	}
	proofData, err := json.Marshal(proof)
	if err != nil {
		return nil, fmt.Errorf("encode update authorization: %w", err)
	}
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	message := method + "\n" + req.URL.RequestURI() + "\n" + timestamp
	req.Header.Set("X-Sigmo-Device-ID", c.identity.DeviceID)
	req.Header.Set("X-Sigmo-Lease", base64.RawURLEncoding.EncodeToString(proofData))
	req.Header.Set("X-Sigmo-Timestamp", timestamp)
	req.Header.Set("X-Sigmo-Signature", base64.RawStdEncoding.EncodeToString(ed25519.Sign(c.identity.PrivateKey, []byte(message))))
	return req, nil
}

func sameOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) &&
		strings.EqualFold(a.Hostname(), b.Hostname()) &&
		originPort(a) == originPort(b)
}

func originPort(value *url.URL) string {
	if port := value.Port(); port != "" {
		return port
	}
	switch strings.ToLower(value.Scheme) {
	case "http":
		return "80"
	case "https":
		return "443"
	default:
		return ""
	}
}

func normalizeServiceURL(value string) (string, error) {
	value = strings.TrimRight(strings.TrimSpace(value), "/")
	if value == "" {
		return "", nil
	}
	parsed, err := url.Parse(value)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" {
		return "", errors.New("authorization service URL must be absolute")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("authorization service URL must not contain credentials, a query, or a fragment")
	}
	switch strings.ToLower(parsed.Scheme) {
	case "https":
		return value, nil
	case "http":
		host := parsed.Hostname()
		ip := net.ParseIP(host)
		if strings.EqualFold(host, "localhost") || ip != nil && ip.IsLoopback() {
			return value, nil
		}
	}
	return "", errors.New("authorization service URL must use HTTPS")
}

func decodeKey(value string) ([]byte, error) {
	for _, encoding := range []*base64.Encoding{base64.RawStdEncoding, base64.StdEncoding, base64.RawURLEncoding, base64.URLEncoding} {
		decoded, err := encoding.DecodeString(strings.TrimSpace(value))
		if err == nil {
			return decoded, nil
		}
	}
	return nil, errors.New("invalid base64")
}
