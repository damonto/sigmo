package modem

import (
	"context"
	"errors"
	"fmt"

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
	_, err := s.manager.WaitForModemAfter(ctx, modem, func() error {
		if err := modem.SetPrimarySimSlot(slotIndex); err != nil {
			err = fmt.Errorf("set primary SIM slot: %w", err)
			if mmodem.IsTransientRestartError(err) {
				return mmodem.ReloadStarted(err)
			}
			return err
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("wait for modem: %w", err)
		}
		return err
	}
	return nil
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
			return 0, fmt.Errorf("fetch SIM for slot %s: %w", slotPath, err)
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
