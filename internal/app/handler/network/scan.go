package network

import (
	"fmt"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

type network struct{}

func newNetwork() *network {
	return &network{}
}

func (n *network) List(modem *mmodem.Modem) ([]NetworkResponse, error) {
	networks, err := modem.ThreeGPP().ScanNetworks()
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
