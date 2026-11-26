package internet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"golang.org/x/sync/singleflight"
)

const (
	publicIPLookupURL     = "https://api.ip.sb/geoip"
	publicIPLookupTimeout = 15 * time.Second
	publicIPLockTimeout   = 3 * time.Second
	publicIPCacheTTL      = 5 * time.Minute
	publicIPFailureTTL    = 30 * time.Second
)

// PublicIPInfo describes the public IP address and its associated network.
type PublicIPInfo struct {
	IP           string
	Country      string
	Organization string
	ISP          string
}

// Public returns the cached or newly observed public IP for the active modem connection.
func (c *Connector) Public(ctx context.Context, modem *mmodem.Modem) (PublicIPInfo, error) {
	if modem == nil {
		return PublicIPInfo{}, ErrModemRequired
	}
	access := modemAccess{modem: modem}
	identity := modem.SIMIdentity()
	lockCtx, cancel := context.WithTimeout(ctx, publicIPLockTimeout)
	unlock, err := c.acquireModem(lockCtx, access.id())
	cancel()
	if err != nil {
		if ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded) {
			return PublicIPInfo{}, ErrOperationInProgress
		}
		return PublicIPInfo{}, err
	}
	connection, session, err := c.diagnosticsLocked(ctx, access)
	unlock()
	if errors.Is(err, ErrNotConnected) {
		return PublicIPInfo{}, nil
	}
	if err != nil {
		return PublicIPInfo{}, err
	}
	if modem.SIMIdentity() != identity || modem.SIMReloadPending() {
		return PublicIPInfo{}, nil
	}
	info, err := session.publicIP.get(ctx, session.lifetime, time.Now, func(ctx context.Context) (PublicIPInfo, error) {
		client := &http.Client{
			Transport: boundTransportWithTimeout(strings.TrimSpace(connection.InterfaceName), connection.DNS, publicIPLookupTimeout),
		}
		defer client.CloseIdleConnections()
		return LookupPublicIP(ctx, client)
	})
	if session.lifetime.Err() != nil || modem.SIMIdentity() != identity || modem.SIMReloadPending() {
		return PublicIPInfo{}, nil
	}
	return info, err
}

type publicIPResult struct {
	info      PublicIPInfo
	err       error
	expiresAt time.Time
}

type publicIPCache struct {
	mu    sync.Mutex
	entry publicIPResult
	group singleflight.Group
}

func (c *publicIPCache) cached(now time.Time) (publicIPResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.entry, now.Before(c.entry.expiresAt)
}

func (c *publicIPCache) get(ctx, lifetime context.Context, now func() time.Time, lookup func(context.Context) (PublicIPInfo, error)) (PublicIPInfo, error) {
	if err := ctx.Err(); err != nil {
		return PublicIPInfo{}, err
	}
	if err := lifetime.Err(); err != nil {
		return PublicIPInfo{}, err
	}
	if entry, ok := c.cached(now()); ok {
		return entry.info, entry.err
	}
	// One caller closing its page should not cancel the other callers' lookup.
	result := c.group.DoChan("public-ip", func() (any, error) {
		if entry, ok := c.cached(now()); ok {
			return entry, nil
		}
		lookupCtx, cancel := context.WithTimeout(lifetime, publicIPLookupTimeout)
		defer cancel()
		info, err := lookup(lookupCtx)
		ttl := publicIPCacheTTL
		if err != nil {
			ttl = publicIPFailureTTL
		}
		entry := publicIPResult{info: info, err: err, expiresAt: now().Add(ttl)}
		c.mu.Lock()
		c.entry = entry
		c.mu.Unlock()
		return entry, nil
	})
	select {
	case <-ctx.Done():
		return PublicIPInfo{}, ctx.Err()
	case <-lifetime.Done():
		return PublicIPInfo{}, lifetime.Err()
	case value := <-result:
		entry := value.Val.(publicIPResult)
		return entry.info, entry.err
	}
}

// LookupPublicIP finds the public IP address used by the supplied HTTP client.
func LookupPublicIP(ctx context.Context, client *http.Client) (PublicIPInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, publicIPLookupURL, nil)
	if err != nil {
		return PublicIPInfo{}, fmt.Errorf("create public IP lookup request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	// IP.SB may reject the default Go HTTP user agent.
	req.Header.Set("User-Agent", "Sigmo")

	resp, err := client.Do(req)
	if err != nil {
		return PublicIPInfo{}, fmt.Errorf("look up public IP: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return PublicIPInfo{}, fmt.Errorf("public IP lookup returned HTTP status %d", resp.StatusCode)
	}

	var payload struct {
		IP           string `json:"ip"`
		CountryCode  string `json:"country_code"`
		Organization string `json:"organization"`
		ISP          string `json:"isp"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&payload); err != nil {
		return PublicIPInfo{}, fmt.Errorf("decode public IP lookup response: %w", err)
	}
	ip := strings.TrimSpace(payload.IP)
	if _, err := netip.ParseAddr(ip); err != nil {
		return PublicIPInfo{}, errors.New("public IP lookup returned an invalid address")
	}
	return PublicIPInfo{
		IP:           ip,
		Country:      strings.ToUpper(strings.TrimSpace(payload.CountryCode)),
		Organization: strings.TrimSpace(payload.Organization),
		ISP:          strings.TrimSpace(payload.ISP),
	}, nil
}
