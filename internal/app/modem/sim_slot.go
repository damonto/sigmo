package modem

import (
	"context"
	"errors"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

var (
	errSIMSlotRequired      = errors.New("SIM slot is required")
	errSIMSlotsUnavailable  = errors.New("sim slots not available")
	errSIMSlotNotFound      = errors.New("sim slot not found")
	errSIMSlotAlreadyActive = errors.New("sim slot already active")
)

type simSlot struct {
	registry *mmodem.Registry
}

func newSIMSlot(registry *mmodem.Registry) *simSlot {
	return &simSlot{registry: registry}
}

func (s *simSlot) Switch(ctx context.Context, modem *mmodem.Modem, slotIndex uint32) error {
	_, err := s.registry.SwitchSIMSlot(ctx, modem, slotIndex)
	return err
}

func (s *simSlot) targetIndex(modem *mmodem.Modem, slotIndex uint32) (uint32, error) {
	if slotIndex == 0 {
		return 0, errSIMSlotRequired
	}
	snapshot := modem.Snapshot()
	if len(snapshot.Slots) == 0 {
		return 0, errSIMSlotsUnavailable
	}
	for _, sim := range snapshot.Slots {
		if sim == nil || sim.Slot != slotIndex {
			continue
		}
		if sim.Active {
			return 0, errSIMSlotAlreadyActive
		}
		return slotIndex, nil
	}
	return 0, errSIMSlotNotFound
}
