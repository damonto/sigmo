package license

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"time"
)

// Run refreshes service rights in the background. Authorization outages
// and expiry affect official catalogs, without restarting the application's base services.
func (c *Controller) Run(ctx context.Context) error {
	for {
		delay := 30 * time.Second
		lease, refreshAt, _ := c.currentLeaseState()
		if lease != nil && time.Now().Before(refreshAt) {
			delay = time.Until(refreshAt)
		} else if c.currentSession() != nil && c.baseURL != "" && len(c.licensePublicKey) != 0 {
			attemptSession := c.currentSession()
			requestCtx, cancel := context.WithTimeout(ctx, metadataRequestTimeout)
			err := c.refresh(requestCtx)
			cancel()
			if ctx.Err() != nil {
				return nil
			}
			if err == nil {
				_, refreshAt, _ := c.currentLeaseState()
				delay = max(time.Second, time.Until(refreshAt))
			} else if errors.Is(err, errExplicitUnauthorized) {
				c.revokeConfiguration(attemptSession)
			} else {
				slog.Warn("refresh service authorization", "error", err)
				var remote *serviceError
				if errors.As(err, &remote) {
					delay = max(delay, time.Until(remote.RetryAt))
				}
			}
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-c.leaseChanged:
			timer.Stop()
		case <-timer.C:
		}
	}
}

func (c *Controller) revokeConfiguration(attempt *storedSession) {
	c.rotationMu.Lock()
	defer c.rotationMu.Unlock()
	c.configuration.mu.Lock()
	defer c.configuration.mu.Unlock()
	current := c.currentSession()
	if attempt == nil || current == nil || (current.SessionID != attempt.SessionID || current.Generation != attempt.Generation) {
		return
	}
	c.clearLease()
	c.configuration.current = nil
	c.configuration.grant = configurationGrant{}
	c.configuration.denied = true
	removeErr := os.Remove(c.configuration.path)
	if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		slog.Warn("remove revoked configuration cache", "error", removeErr)
	}
}
