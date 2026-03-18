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

func (s *Service) SwitchSimSlot(ctx context.Context, modem *mmodem.Modem, identifier string) error {
	if identifier == "" {
		return errSimIdentifierRequired
	}
	slotIndex, err := s.findSimSlotIndex(modem, identifier)
	if err != nil {
		return err
	}
	if err := modem.SetPrimarySimSlot(slotIndex); err != nil {
		slog.Error("failed to set primary SIM slot", "modem", modem.EquipmentIdentifier, "error", err)
		return err
	}
	_, err = s.manager.WaitForModem(ctx, modem.EquipmentIdentifier)
	if err != nil {
		slog.Error("failed to wait for modem", "modem", modem.EquipmentIdentifier, "error", err)
	}
	return err
}

func (s *Service) findSimSlotIndex(modem *mmodem.Modem, identifier string) (uint32, error) {
	if len(modem.SimSlots) == 0 {
		return 0, errSimSlotsUnavailable
	}
	for index, slotPath := range modem.SimSlots {
		sim, err := modem.SIMs().Get(slotPath)
		if err != nil {
			slog.Error("failed to fetch SIM for slot", "modem", modem.EquipmentIdentifier, "slot", slotPath, "error", err)
			return 0, err
		}
		if sim.Identifier == identifier && !sim.Active {
			return uint32(index + 1), nil
		}
	}
	return 0, errSimSlotNotFound
}
