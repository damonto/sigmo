// Package euicc reads secure-element inventory through the shared LPA pool.
package euicc

import (
	"github.com/damonto/sigmo/internal/pkg/lpa"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

// Service reads secure-element inventory independently of the transport.
type Service struct {
	registry *mmodem.Registry
	euicc    *euicc
}

// New creates the secure-element inventory service.
func New(registry *mmodem.Registry, clients *lpa.Pool) *Service {
	return &Service{
		registry: registry,
		euicc:    newEUICC(clients),
	}
}
