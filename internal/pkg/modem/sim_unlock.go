package modem

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrSIMPinRequired           = errors.New("PIN is required")
	ErrSIMUnlockNotRequired     = errors.New("SIM PIN unlock is not required")
	ErrSIMUnlockUnsupportedLock = errors.New("modem lock is not supported")
	ErrSIMUnlockFailed          = errors.New("unlock SIM PIN")
	ErrEnableAfterSIMUnlock     = errors.New("enable modem after unlock")
	ErrPrimarySIMMissing        = errors.New("primary SIM is not available")
)

func (m *Modem) UnlockSIMPinAndEnable(ctx context.Context, pin string) error {
	if m == nil {
		return errModemRequired
	}
	pin = strings.TrimSpace(pin)
	if pin == "" {
		return ErrSIMPinRequired
	}
	snapshot := m.Snapshot()
	if snapshot.State != ModemStateLocked {
		return ErrSIMUnlockNotRequired
	}
	if snapshot.UnlockRequired != ModemLockSimPin {
		return fmt.Errorf("%w: %s", ErrSIMUnlockUnsupportedLock, snapshot.UnlockRequired)
	}
	if snapshot.SIM == nil {
		return ErrPrimarySIMMissing
	}
	if err := snapshot.SIM.SendPin(ctx, pin); err != nil {
		return fmt.Errorf("%w: %w", ErrSIMUnlockFailed, err)
	}
	if err := m.Enable(ctx); err != nil {
		return fmt.Errorf("%w: %w", ErrEnableAfterSIMUnlock, err)
	}
	return nil
}
