package license

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
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
	licenseStateScope      = "license"
	sessionStateKey        = "device.session"
	legacyIdentityStateKey = "device.identity"
	legacyLeaseStateKey    = "lease"

	maxResponseSize        = 1 << 20
	metadataRequestTimeout = 15 * time.Second
	maxLeaseRefreshDelay   = 30 * time.Minute
	maxLeaseLifetime       = 6 * time.Hour
	leaseRefreshRetryDelay = 5 * time.Minute
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
	IdentityPath     string
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

	mu               sync.RWMutex
	session          *storedSession
	lease            *Lease
	leaseProof       *signedLease
	leaseRefreshAt   time.Time
	leaseDeadline    time.Time
	serverIssuedAt   time.Time
	serverReceivedAt time.Time
	pairings         map[string]pairingSession
	leaseChanged     chan struct{}
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
	identityPath := strings.TrimSpace(cfg.IdentityPath)
	if identityPath == "" {
		return nil, errors.New("device identity path is required")
	}
	identity, err := loadOrCreateIdentity(identityPath)
	if err != nil {
		return nil, err
	}
	if err := cfg.Storage.Delete(ctx, licenseStateScope, legacyIdentityStateKey); err != nil {
		return nil, fmt.Errorf("remove legacy device identity: %w", err)
	}
	if err := cfg.Storage.Delete(ctx, licenseStateScope, legacyLeaseStateKey); err != nil {
		return nil, fmt.Errorf("remove legacy license lease: %w", err)
	}
	var savedSession storedSession
	err = cfg.Storage.Get(ctx, licenseStateScope, sessionStateKey, &savedSession)
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return nil, fmt.Errorf("read device session: %w", err)
	}
	if err == nil && !validStoredSession(savedSession) {
		return nil, errors.New("read device session: invalid session state")
	}
	controller := &Controller{
		baseURL:          baseURL,
		licensePublicKey: publicKey,
		releasePublicKey: cfg.ReleasePublicKey,
		storage:          cfg.Storage,
		client:           client,
		restart:          cfg.Restart,
		identity:         identity,
		pairings:         make(map[string]pairingSession),
		leaseChanged:     make(chan struct{}, 1),
	}
	if savedSession.SessionID != "" {
		controller.session = cloneSession(&savedSession)
	}
	return controller, nil
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
	c.clearLease()
	if c.currentSession() == nil {
		return nil
	}
	return c.refresh(ctx)
}

func (c *Controller) Authorized() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lease != nil && c.lease.Status == "active" && time.Now().Before(c.leaseDeadline)
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
		_, refreshAt, deadline := c.currentLeaseState()
		if deadline.IsZero() {
			return nil
		}
		if !time.Now().Before(deadline) {
			c.clearLease()
			c.restartHealthy()
			return nil
		}
		next := refreshAt
		if !retryAt.IsZero() {
			next = retryAt
		}
		if next.After(deadline) {
			next = deadline
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
		if !time.Now().Before(deadline) {
			c.clearLease()
			c.restartHealthy()
			return nil
		}
		err := c.refresh(ctx)
		if err == nil {
			retryAt = time.Time{}
			continue
		}
		if errors.Is(err, errExplicitUnauthorized) || !c.Authorized() {
			c.clearLease()
			c.restartHealthy()
			return nil
		}
		_, _, deadline = c.currentLeaseState()
		slog.Warn("refresh product authorization", "error", err)
		retryAt = time.Now().Add(leaseRefreshRetryDelay)
		if retryAt.After(deadline) {
			retryAt = deadline
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
	session, err := c.prepareRotation(ctx)
	if err != nil {
		return err
	}
	var challengeResponse challenge
	_, err = c.doJSON(ctx, http.MethodPost, "/v1/license-challenges", leaseChallengeRequest{
		DeviceID:   c.identity.DeviceID,
		SessionID:  session.SessionID,
		Generation: session.Generation,
	}, "", &challengeResponse)
	if err != nil {
		return c.classifyAuthorizationError(err)
	}
	if _, err := decodeKey(challengeResponse.Challenge); err != nil {
		return fmt.Errorf("decode license challenge: %w", err)
	}
	nextHash := tokenHash(session.Pending.NextRefreshToken)
	rotation := leaseRotationRequest{
		DeviceID:             c.identity.DeviceID,
		SessionID:            session.SessionID,
		Generation:           session.Generation,
		Challenge:            challengeResponse.Challenge,
		RefreshToken:         session.RefreshToken,
		NextRefreshTokenHash: nextHash,
		RotationID:           session.Pending.ID,
		FingerprintHash:      c.identity.FingerprintHash,
	}
	rotation.Signature = base64.RawStdEncoding.EncodeToString(ed25519.Sign(c.identity.PrivateKey, []byte(rotationMessage(rotation))))
	var proof signedLease
	_, err = c.doJSON(ctx, http.MethodPost, "/v1/license-leases", rotation, "", &proof)
	if err != nil {
		return c.classifyAuthorizationError(err)
	}
	lease, err := c.verifyLease(proof, session.SessionID, session.Generation+1)
	if err != nil {
		return err
	}
	next := storedSession{
		SessionID:    session.SessionID,
		Generation:   session.Generation + 1,
		RefreshToken: session.Pending.NextRefreshToken,
	}
	if err := c.saveSession(ctx, next); err != nil {
		return err
	}
	c.mu.Lock()
	c.session = cloneSession(&next)
	c.mu.Unlock()
	c.setLease(lease, &proof)
	return nil
}

func (c *Controller) prepareRotation(ctx context.Context) (*storedSession, error) {
	c.mu.Lock()
	if c.session == nil {
		c.mu.Unlock()
		return nil, errors.New("device authorization session is unavailable")
	}
	if c.session.Pending == nil {
		rotationID, err := randomToken(24)
		if err != nil {
			c.mu.Unlock()
			return nil, err
		}
		nextToken, err := randomToken(32)
		if err != nil {
			c.mu.Unlock()
			return nil, err
		}
		c.session.Pending = &pendingRotation{ID: rotationID, NextRefreshToken: nextToken}
	}
	session := cloneSession(c.session)
	c.mu.Unlock()
	if err := c.saveSession(ctx, *session); err != nil {
		return nil, err
	}
	return session, nil
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
	case "license_device_unauthorized", "license_entitlement_inactive", "license_lease_expired", "license_session_superseded":
		return true
	default:
		return false
	}
}

func (c *Controller) verifyLease(proof signedLease, sessionID string, generation int64) (*Lease, error) {
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
	if lease.SchemaVersion != leaseSchemaVersion || lease.DeviceID != c.identity.DeviceID || lease.SessionID != sessionID || lease.Generation != generation || lease.TelegramID <= 0 || lease.Status != "active" {
		return nil, errors.New("verify license lease: metadata mismatch")
	}
	if !lease.RefreshAfter.After(lease.IssuedAt) ||
		lease.RefreshAfter.Sub(lease.IssuedAt) > maxLeaseRefreshDelay ||
		!lease.ExpiresAt.After(lease.RefreshAfter) ||
		lease.ExpiresAt.Sub(lease.IssuedAt) > maxLeaseLifetime {
		return nil, errors.New("verify license lease: invalid validity period")
	}
	if lease.EntitlementExpiresAt != nil && lease.EntitlementExpiresAt.Before(lease.ExpiresAt) {
		return nil, errors.New("verify license lease: entitlement expires before lease")
	}
	return &lease, nil
}

func (c *Controller) setLease(lease *Lease, proof *signedLease) {
	receivedAt := time.Now()
	refreshAt := receivedAt.Add(lease.RefreshAfter.Sub(lease.IssuedAt))
	deadline := receivedAt.Add(lease.ExpiresAt.Sub(lease.IssuedAt))
	c.mu.Lock()
	c.lease = cloneLease(lease)
	if proof == nil {
		c.leaseProof = nil
	} else {
		cloned := *proof
		cloned.Lease = bytes.Clone(proof.Lease)
		c.leaseProof = &cloned
	}
	c.leaseRefreshAt = refreshAt
	c.leaseDeadline = deadline
	c.serverIssuedAt = lease.IssuedAt
	c.serverReceivedAt = receivedAt
	c.mu.Unlock()
	c.notifyLeaseChanged()
}

func (c *Controller) clearLease() {
	c.mu.Lock()
	c.lease = nil
	c.leaseProof = nil
	c.leaseRefreshAt = time.Time{}
	c.leaseDeadline = time.Time{}
	c.serverIssuedAt = time.Time{}
	c.serverReceivedAt = time.Time{}
	c.mu.Unlock()
	c.notifyLeaseChanged()
}

func (c *Controller) currentLeaseState() (*Lease, time.Time, time.Time) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return cloneLease(c.lease), c.leaseRefreshAt, c.leaseDeadline
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

func cloneSession(session *storedSession) *storedSession {
	if session == nil {
		return nil
	}
	cloned := *session
	if session.Pending != nil {
		pending := *session.Pending
		cloned.Pending = &pending
	}
	return &cloned
}

func (c *Controller) currentSession() *storedSession {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return cloneSession(c.session)
}

func (c *Controller) saveSession(ctx context.Context, session storedSession) error {
	if err := c.storage.Put(ctx, licenseStateScope, sessionStateKey, session); err != nil {
		return fmt.Errorf("save device session: %w", err)
	}
	return nil
}

func validStoredSession(session storedSession) bool {
	if strings.TrimSpace(session.SessionID) == "" || session.Generation < 1 || !validToken(session.RefreshToken, 32) {
		return false
	}
	return session.Pending == nil || strings.TrimSpace(session.Pending.ID) != "" && validToken(session.Pending.NextRefreshToken, 32)
}

func validToken(value string, size int) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil && len(decoded) == size
}

func randomToken(size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate authorization token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func tokenHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func rotationMessage(request leaseRotationRequest) string {
	return strings.Join([]string{
		"sigmo-license-v1",
		request.DeviceID,
		request.SessionID,
		fmt.Sprintf("%d", request.Generation),
		request.Challenge,
		request.RotationID,
		request.NextRefreshTokenHash,
		request.FingerprintHash,
	}, "\n")
}

func (c *Controller) notifyLeaseChanged() {
	select {
	case c.leaseChanged <- struct{}{}:
	default:
	}
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
	deadline := c.leaseDeadline
	serverIssuedAt := c.serverIssuedAt
	serverReceivedAt := c.serverReceivedAt
	c.mu.RUnlock()
	if proof == nil || deadline.IsZero() || !time.Now().Before(deadline) {
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
	timestamp := fmt.Sprintf("%d", serverIssuedAt.Add(time.Since(serverReceivedAt)).Unix())
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
