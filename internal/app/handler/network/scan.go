package network

import (
	"log/slog"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

type Service struct{}

func NewService() *Service {
	return &Service{}
}

func (s *Service) List(modem *mmodem.Modem) ([]NetworkResponse, error) {
	networks, err := modem.ThreeGPP().ScanNetworks()
	if err != nil {
		slog.Error("failed to scan networks", "modem", modem.EquipmentIdentifier, "error", err)
		return nil, err
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
