// Package network coordinates network selection, radio preferences, and scan lifecycles.
package network

import (
	"errors"

	appconnectivity "github.com/damonto/sigmo/internal/app/connectivity"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/networkprefs"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

// Service owns shared network state used by HTTP and MCP.
type Service struct {
	registry *mmodem.Registry
	networks *network
}

// Config supplies network persistence and modem lifecycle dependencies.
type Config struct {
	Registry              *mmodem.Registry
	Preferences           *networkprefs.Store
	Store                 *storage.Store
	AirplaneModeLifecycle appconnectivity.AirplaneModeLifecycle
}

var errNetworkRegistryRequired = errors.New("modem registry is required")

// New creates the shared network service and its scan coordinator.
func New(cfg Config) (*Service, error) {
	if cfg.Registry == nil {
		return nil, errNetworkRegistryRequired
	}
	networks, err := newNetwork(cfg.Preferences, cfg.Store, cfg.AirplaneModeLifecycle)
	if err != nil {
		return nil, err
	}
	return &Service{
		registry: cfg.Registry,
		networks: networks,
	}, nil
}

// Close cancels scans and waits for their workers to stop.
func (s *Service) Close() {
	if s != nil && s.networks != nil {
		s.networks.Close()
	}
}
