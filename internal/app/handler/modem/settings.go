package modem

import (
	"context"
	"fmt"
	"strings"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	appsettings "github.com/damonto/sigmo/internal/pkg/settings"
)

type modemSettings struct {
	store *appsettings.Store
}

func newSettings(store *appsettings.Store) *modemSettings {
	return &modemSettings{store: store}
}

func (s *modemSettings) Update(ctx context.Context, modem *mmodem.Modem, req UpdateModemSettingsRequest) error {
	modemID := modem.EquipmentIdentifier
	current := s.store.FindModem(modemID)
	current.Alias = strings.TrimSpace(req.Alias)
	current.MSS = req.MSS
	if err := s.store.UpdateModem(ctx, modemID, current); err != nil {
		return fmt.Errorf("save modem settings: %w", err)
	}
	return nil
}

func (s *modemSettings) Get(modem *mmodem.Modem) *ModemSettingsResponse {
	current := s.store.FindModem(modem.EquipmentIdentifier)
	return &ModemSettingsResponse{
		Alias: current.Alias,
		MSS:   current.MSS,
	}
}
