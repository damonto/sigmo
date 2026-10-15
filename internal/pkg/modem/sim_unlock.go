package modem

import (
	"context"
	"errors"
	"strings"
)

var (
	ErrSIMPINRequired       = errors.New("PIN is required")
	ErrSIMUnlockNotRequired = errors.New("SIM PIN unlock is not required")
	ErrSIMUnlockFailed      = errors.New("unlock SIM PIN")
	ErrEnableAfterSIMUnlock = errors.New("enable modem after unlock")
	ErrPrimarySIMMissing    = errors.New("primary SIM is not available")
)

// UnlockSIMPINAndEnable preserves the historical PIN-only entry point.
func (m *Modem) UnlockSIMPINAndEnable(ctx context.Context, pin string) error {
	return m.UnlockSIM(ctx, SIMUnlock{PIN: strings.TrimSpace(pin)})
}
