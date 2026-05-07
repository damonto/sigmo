package modem

import (
	"errors"
	"log/slog"
	"strings"

	"github.com/damonto/sigmo/internal/pkg/config"
)

var errCompatibleRequired = errors.New("compatible is required")

type settings struct {
	store *config.Store
}

func newSettings(store *config.Store) *settings {
	return &settings{store: store}
}

func (s *settings) Update(modemID string, req UpdateModemSettingsRequest) error {
	if req.Compatible == nil {
		return errCompatibleRequired
	}
	modem := s.store.FindModem(modemID)
	modem.Alias = strings.TrimSpace(req.Alias)
	modem.Compatible = *req.Compatible
	modem.MSS = req.MSS
	if err := s.store.UpdateModem(modemID, modem); err != nil {
		slog.Error("failed to save config", "modem", modemID, "error", err)
		return err
	}
	return nil
}

func (s *settings) Get(modemID string) *ModemSettingsResponse {
	modem := s.store.FindModem(modemID)
	return &ModemSettingsResponse{
		Alias:      modem.Alias,
		Compatible: modem.Compatible,
		MSS:        modem.MSS,
	}
}
