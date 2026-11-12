package esimtransfer

import (
	"errors"
	"net/http"
	"time"

	"github.com/damonto/sigmo/internal/app/httpapi"
	"github.com/labstack/echo/v5"
)

// PeerDecision atomically accepts one decision for the displayed request version.
func (h *Handler) PeerDecision(c *echo.Context) error {
	var decision peerDecision
	if err := httpapi.BindJSON(c, &decision, "transfer_invalid_request"); err != nil {
		return err
	}
	if err := h.peers.decide(c.Param("id"), decision); err != nil {
		if errors.Is(err, ErrProfileUnsupported) {
			return httpapi.BadRequest(c, "transfer_invalid_request", err)
		}
		if errors.Is(err, ErrSourceIMEIRequired) {
			return httpapi.BadRequest(c, "transfer_source_imei_required", err)
		}
		return httpapi.Error(c, http.StatusConflict, "transfer_request_expired", err.Error())
	}
	return c.NoContent(http.StatusNoContent)
}

// PeerEvents sends an initial snapshot and subsequent changes on every page.
func (h *Handler) PeerEvents(c *echo.Context) error {
	conn, err := transferWSUpgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}
	ctx, release, err := h.peers.begin(c.Request().Context())
	if err != nil {
		_ = conn.Close()
		return nil
	}
	defer release()
	wire := newPeerWire(ctx, conn, h.peers.permits)
	defer wire.close()
	changed, unsubscribe := h.peers.subscribe()
	defer unsubscribe()
	// Policy changes invalidate an existing subscription as well as new requests.
	policyID := h.runner.store.Snapshot().Auth.PolicyID()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		if err := wire.send(peerMessage{Type: "requests", Requests: h.peers.snapshot()}); err != nil {
			return nil
		}
		select {
		case <-wire.ctx.Done():
			return nil
		case <-changed:
		case <-ticker.C:
			if h.runner.store.Snapshot().Auth.PolicyID() != policyID {
				return nil
			}
		}
	}
}
