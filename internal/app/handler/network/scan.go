package network

import (
	"context"
	"errors"
	"fmt"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/networkprefs"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

type network struct {
	preferences *networkprefs.Store
	store       *storage.Store
}

var errNetworkPreferencesRequired = errors.New("network preferences are required")

func newNetwork(preferences *networkprefs.Store, store *storage.Store) (*network, error) {
	if preferences == nil {
		return nil, errNetworkPreferencesRequired
	}
	if store == nil {
		return nil, errNetworkRegistrationStorageRequired
	}
	return &network{preferences: preferences, store: store}, nil
}

func (n *network) List(ctx context.Context, modem *mmodem.Modem) ([]NetworkResponse, error) {
	networks, err := modem.ThreeGPP().ScanNetworks(ctx)
	if err != nil {
		return nil, fmt.Errorf("scan networks: %w", err)
	}

	response := make([]NetworkResponse, 0, len(networks))
	for _, network := range networks {
		response = append(response, NetworkResponse{
			Status:             networkAvailabilityName(network),
			OperatorName:       network.Name,
			OperatorShortName:  network.Name,
			OperatorCode:       network.ID,
			AccessTechnologies: accessTechnologyStrings(network.Technology),
		})
	}
	return response, nil
}
