package modem

import (
	"context"
	"errors"
	"log/slog"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

var (
	errSimIdentifierRequired = errors.New("identifier is required")
	errSimSlotsUnavailable   = errors.New("sim slots not available")
	errSimSlotNotFound       = errors.New("sim slot not found")
	errSimSlotAlreadyActive  = errors.New("sim slot already active")
)

type simSlot struct {
	manager *mmodem.Manager
}

func newSIMSlot(manager *mmodem.Manager) *simSlot {
	return &simSlot{manager: manager}
}

func (s *simSlot) Switch(ctx context.Context, modem *mmodem.Modem, slotIndex uint32) error {
	if err := modem.SetPrimarySimSlot(slotIndex); err != nil {
		slog.Error("failed to set primary SIM slot", "modem", modem.EquipmentIdentifier, "error", err)
		return err
	}
	_, err := s.manager.WaitForModem(ctx, modem)
	if err != nil {
		slog.Error("failed to wait for modem", "modem", modem.EquipmentIdentifier, "error", err)
	}
	return err
}

func (s *simSlot) targetIndex(modem *mmodem.Modem, identifier string) (uint32, error) {
	if identifier == "" {
		return 0, errSimIdentifierRequired
	}
	if len(modem.SimSlots) == 0 {
		return 0, errSimSlotsUnavailable
	}
	for index, slotPath := range modem.SimSlots {
		sim, err := modem.SIMs().Get(slotPath)
		if err != nil {
			slog.Error("failed to fetch SIM for slot", "modem", modem.EquipmentIdentifier, "slot", slotPath, "error", err)
			return 0, err
		}
		if sim.Identifier != identifier {
			continue
		}
		if sim.Active {
			return 0, errSimSlotAlreadyActive
		}
		return uint32(index + 1), nil
	}
	return 0, errSimSlotNotFound
}
