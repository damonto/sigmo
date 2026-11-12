package esimtransfer

import (
	"context"

	"github.com/damonto/sigmo/internal/pkg/modem"
)

// Service owns transfer coordination, peer approvals, and their shutdown lifecycle.
type Service struct {
	registry *modem.Registry
	runner   *transferRunner
	peers    *peerBroker
}

// New creates a transfer service using the shared eSIM lifecycle operations.
func New(cfg Config) *Service {
	return &Service{
		registry: cfg.Registry,
		runner:   newTransferRunner(cfg),
		peers:    newPeerBroker(cfg.Allowed),
	}
}

// Close cancels remote transfers and subscriptions before releasing device pools.
func (s *Service) Close(ctx context.Context) error { return s.peers.close(ctx) }
