package modem

import (
	"errors"
	"log/slog"
	"strings"

	"github.com/damonto/sigmo/internal/pkg/config"
)

var errCompatibleRequired = errors.New("compatible is required")

func (s *Service) UpdateSettings(modemID string, req UpdateModemSettingsRequest) error {
	if req.Compatible == nil {
		return errCompatibleRequired
	}
	modem := s.cfg.FindModem(modemID)
	modem.Alias = strings.TrimSpace(req.Alias)
	modem.Compatible = *req.Compatible
	modem.MSS = req.MSS
	if s.cfg.Modems == nil {
		s.cfg.Modems = make(map[string]config.Modem)
	}
	s.cfg.Modems[modemID] = modem
	if err := s.cfg.Save(); err != nil {
		slog.Error("failed to save config", "modem", modemID, "error", err)
		return err
	}
	return nil
}

func (s *Service) GetSettings(modemID string) *ModemSettingsResponse {
	modem := s.cfg.FindModem(modemID)
	return &ModemSettingsResponse{
		Alias:      modem.Alias,
		Compatible: modem.Compatible,
		MSS:        modem.MSS,
	}
}
