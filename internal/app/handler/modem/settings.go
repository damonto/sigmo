package modem

import (
	"errors"
	"fmt"
	"strings"

	"github.com/damonto/sigmo/internal/pkg/config"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

var errCompatibleRequired = errors.New("compatible is required")

type settings struct {
	store *config.Store
}

func newSettings(store *config.Store) *settings {
	return &settings{store: store}
}

func (s *settings) Update(modem *mmodem.Modem, req UpdateModemSettingsRequest) error {
	if req.Compatible == nil {
		return errCompatibleRequired
	}
	modemID := modem.EquipmentIdentifier
	cfg := s.store.FindModem(modemID)
	cfg.Alias = strings.TrimSpace(req.Alias)
	cfg.Compatible = *req.Compatible
	cfg.MSS = req.MSS
	if err := s.store.UpdateModem(modemID, cfg); err != nil {
		return fmt.Errorf("save modem config: %w", err)
	}
	return nil
}

func (s *settings) Get(modem *mmodem.Modem) *ModemSettingsResponse {
	cfg := s.store.FindModem(modem.EquipmentIdentifier)
	return &ModemSettingsResponse{
		Alias:      cfg.Alias,
		Compatible: cfg.Compatible,
		MSS:        cfg.MSS,
	}
}
