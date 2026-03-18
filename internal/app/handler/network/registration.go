package network

import (
	"errors"
	"log/slog"
	"strings"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

var errOperatorCodeRequired = errors.New("operator code is required")

func (s *Service) Register(modem *mmodem.Modem, operatorCode string) error {
	operatorCode = strings.TrimSpace(operatorCode)
	if operatorCode == "" {
		return errOperatorCodeRequired
	}
	if err := modem.ThreeGPP().RegisterNetwork(operatorCode); err != nil {
		slog.Error("failed to register network", "modem", modem.EquipmentIdentifier, "operator", operatorCode, "error", err)
		return err
	}
	return nil
}
