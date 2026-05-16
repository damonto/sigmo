package network

import (
	"context"
	"fmt"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

type network struct {
	preferences *mmodem.NetworkPreferences
}

func newNetwork(preferences *mmodem.NetworkPreferences) *network {
	return &network{preferences: preferences}
}

func (n *network) List(ctx context.Context, modem *mmodem.Modem) ([]NetworkResponse, error) {
	networks, err := modem.ThreeGPP().ScanNetworks(ctx)
	if err != nil {
		return nil, fmt.Errorf("scan networks: %w", err)
	}

	response := make([]NetworkResponse, 0, len(networks))
	for _, network := range networks {
		response = append(response, NetworkResponse{
			Status:             network.Status.String(),
			OperatorName:       network.OperatorName,
			OperatorShortName:  network.OperatorShortName,
			OperatorCode:       network.OperatorCode,
			AccessTechnologies: accessTechnologyStrings(network.AccessTechnology),
		})
	}
	return response, nil
}

func accessTechnologyStrings(access []mmodem.ModemAccessTechnology) []string {
	names := make([]string, 0, len(access))
	for _, tech := range access {
		names = append(names, tech.String())
	}
	return names
}
