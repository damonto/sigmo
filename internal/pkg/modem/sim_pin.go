package modem

import (
	"context"
	"crypto/rand"
	"errors"
	"strconv"
	"time"

	wwanmodem "github.com/damonto/wwan-go/modem"
)

var (
	ErrSIMPINInvalid         = errors.New("invalid SIM PIN request")
	ErrSIMPINUnsupported     = errors.New("SIM PIN management is not supported")
	ErrSIMPINUnavailable     = errors.New("SIM PIN status is unavailable")
	ErrSIMChanged            = errors.New("the active SIM changed; refresh before continuing")
	ErrSIMIncorrectPIN       = errors.New("incorrect SIM PIN")
	ErrSIMIncorrectPUK       = errors.New("incorrect SIM PUK")
	ErrSIMPUKRequired        = errors.New("SIM PUK is required")
	ErrSIMPermanentlyBlocked = errors.New("SIM is permanently blocked; contact the carrier")
	ErrSIMPINState           = errors.New("the SIM PIN operation is not available in the current state")
	ErrSIMPINResultUnknown   = errors.New("SIM PIN result is unknown; refresh the status before continuing")
)

// SIMPINStatus is a credential-free observation of the active SIM's protection.
// Nil values mean the device has not supplied that information.
type SIMPINStatus struct {
	Supported  bool
	Enabled    *bool
	Lock       wwanmodem.SIMLock
	PINRetries *uint32
	PUKRetries *uint32
	SIMToken   string
}

// SIMPINProtection requests a protection change after verifying the current PIN.
type SIMPINProtection struct {
	Enabled       bool
	PIN, SIMToken string
}

// SIMPINChange replaces the current PIN on the observed SIM.
type SIMPINChange struct{ OldPIN, NewPIN, SIMToken string }

// SIMUnlock verifies either PIN, or PUK together with a replacement PIN.
type SIMUnlock struct{ PIN, PUK, NewPIN, SIMToken string }

type simPINDevice interface {
	SIMInfo(context.Context) (wwanmodem.SIMInfo, error)
	SIMPINStatus(context.Context) (wwanmodem.SIMPINStatus, error)
	SendPIN(context.Context, string) error
	SendPUK(context.Context, string, string) error
	EnablePIN(context.Context, string, bool) error
	ChangePIN(context.Context, string, string) error
	SetPowerState(context.Context, wwanmodem.PowerState) error
}

func (m *Modem) pinDevice() simPINDevice {
	if m.simPINDevice != nil {
		return m.simPINDevice
	}
	if m.core != nil {
		return m.core
	}
	return nil
}

// ValidSIMPIN reports whether value is a 4–8 digit PIN, preserving leading zeros.
func ValidSIMPIN(value string) bool { return simDigits(value, 4, 8) }

// ValidSIMPUK reports whether value is an eight digit PUK.
func ValidSIMPUK(value string) bool { return simDigits(value, 8, 8) }

func simDigits(value string, minLength, maxLength int) bool {
	if len(value) < minLength || len(value) > maxLength {
		return false
	}
	for i := range len(value) {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return true
}

// SIMPINStatus reads live status while excluding SIM slot and profile changes.
func (m *Modem) SIMPINStatus(ctx context.Context) (SIMPINStatus, error) {
	if m == nil || m.pinDevice() == nil {
		return SIMPINStatus{}, ErrSIMPINUnsupported
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	var status SIMPINStatus
	err := m.withReservedSIMSlot(ctx, func() error {
		var err error
		status, err = m.readSIMPINStatus(ctx)
		return err
	})
	return status, err
}

func (m *Modem) readSIMPINStatus(ctx context.Context) (SIMPINStatus, error) {
	device := m.pinDevice()
	info, err := device.SIMInfo(ctx)
	if err != nil {
		return SIMPINStatus{}, ErrSIMPINUnavailable
	}
	m.applySIMInfo(info)
	identity := m.Snapshot().SIMIdentity
	status, err := device.SIMPINStatus(ctx)
	if errors.Is(err, wwanmodem.ErrNotSupported) {
		status, err = wwanmodem.SIMPINStatus{Lock: wwanmodem.SIMLockUnknown}, nil
	}
	if err != nil {
		return SIMPINStatus{}, ErrSIMPINUnavailable
	}
	if status.Lock == "" {
		status.Lock = wwanmodem.SIMLockUnknown
	}
	m.runtimeMu.Lock()
	defer m.runtimeMu.Unlock()
	if identity != m.simIdentityLocked() {
		return SIMPINStatus{}, ErrSIMChanged
	}
	if m.simPINToken == "" {
		m.simPINToken = rand.Text()
	}
	if m.SIM != nil {
		m.SIM.PINStatus = status
	}
	return pinStatusResponse(status, m.simPINToken+"."+strconv.FormatUint(m.simRevision, 10)), nil
}

func pinStatusResponse(status wwanmodem.SIMPINStatus, token string) SIMPINStatus {
	result := SIMPINStatus{Supported: status.Supported, Lock: status.Lock, SIMToken: token}
	if status.EnabledKnown {
		result.Enabled = new(status.Enabled)
	}
	if status.PINRetriesKnown {
		result.PINRetries = new(status.PINRetries)
	}
	if status.PUKRetriesKnown {
		result.PUKRetries = new(status.PUKRetries)
	}
	return result
}

// SetSIMPINProtection changes protection without changing modem radio power.
func (m *Modem) SetSIMPINProtection(ctx context.Context, req SIMPINProtection) error {
	if !ValidSIMPIN(req.PIN) || req.SIMToken == "" {
		return ErrSIMPINInvalid
	}
	return m.performSIMPIN(ctx, simPINOperation{kind: pinProtection, pin: req.PIN, enabled: req.Enabled, token: req.SIMToken})
}

// ChangeSIMPIN changes an enabled, verified PIN without cycling the modem.
func (m *Modem) ChangeSIMPIN(ctx context.Context, req SIMPINChange) error {
	if !ValidSIMPIN(req.OldPIN) || !ValidSIMPIN(req.NewPIN) || req.SIMToken == "" {
		return ErrSIMPINInvalid
	}
	return m.performSIMPIN(ctx, simPINOperation{kind: pinChange, pin: req.OldPIN, newPIN: req.NewPIN, token: req.SIMToken})
}

// UnlockSIM verifies PIN or PUK exactly once, then enables the modem radio.
// An empty token is accepted only for the historical PIN-only API.
func (m *Modem) UnlockSIM(ctx context.Context, req SIMUnlock) error {
	op := simPINOperation{kind: pinUnlock, pin: req.PIN, token: req.SIMToken}
	if req.PUK != "" {
		if req.PIN != "" || !ValidSIMPUK(req.PUK) || !ValidSIMPIN(req.NewPIN) || req.SIMToken == "" {
			return ErrSIMPINInvalid
		}
		op.kind, op.pin, op.newPIN = pinUnblock, req.PUK, req.NewPIN
	} else {
		if req.PIN == "" && req.NewPIN == "" {
			return ErrSIMPINRequired
		}
		if !ValidSIMPIN(req.PIN) || req.NewPIN != "" {
			return ErrSIMPINInvalid
		}
	}
	return m.performSIMPIN(ctx, op)
}

type simPINAction uint8

const (
	pinProtection simPINAction = iota
	pinChange
	pinUnlock
	pinUnblock
)

type simPINOperation struct {
	kind               simPINAction
	pin, newPIN, token string
	enabled            bool
}

func (m *Modem) performSIMPIN(ctx context.Context, op simPINOperation) error {
	if m == nil || m.pinDevice() == nil {
		return ErrSIMPINUnsupported
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	return m.withReservedSIMSlot(ctx, func() error {
		before, err := m.readSIMPINStatus(ctx)
		if err != nil {
			return err
		}
		if op.token != "" && op.token != before.SIMToken {
			return ErrSIMChanged
		}
		if !before.Supported {
			return ErrSIMPINUnsupported
		}
		if before.Lock == wwanmodem.SIMLockBlocked {
			return ErrSIMPermanentlyBlocked
		}
		if before.Lock == wwanmodem.SIMLockPUK && op.kind != pinUnblock {
			return ErrSIMPUKRequired
		}
		switch op.kind {
		case pinUnlock:
			if before.Lock != wwanmodem.SIMLockPIN {
				return ErrSIMUnlockNotRequired
			}
		case pinUnblock:
			if before.Lock != wwanmodem.SIMLockPUK {
				return ErrSIMPINState
			}
		default:
			if before.Lock != wwanmodem.SIMLockNone || before.Enabled == nil {
				return ErrSIMPINState
			}
			if op.kind == pinChange && !*before.Enabled {
				return ErrSIMPINState
			}
			if op.kind == pinProtection && *before.Enabled == op.enabled {
				return nil
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		identity := m.Snapshot().SIMIdentity
		m.runtimeMu.RLock()
		currentToken := m.simPINToken + "." + strconv.FormatUint(m.simRevision, 10)
		m.runtimeMu.RUnlock()
		if currentToken != before.SIMToken {
			return ErrSIMChanged
		}
		device := m.pinDevice()
		switch op.kind {
		case pinProtection:
			err = device.EnablePIN(ctx, op.pin, op.enabled)
		case pinChange:
			err = device.ChangePIN(ctx, op.pin, op.newPIN)
		case pinUnlock:
			err = device.SendPIN(ctx, op.pin)
		case pinUnblock:
			err = device.SendPUK(ctx, op.pin, op.newPIN)
		}
		// A status read may fail after an acknowledged write. Never replay the
		// credential or turn that acknowledgement into a credential failure.
		after, refreshErr := m.readSIMPINStatus(ctx)
		if err != nil {
			if refreshErr != nil {
				var operation *wwanmodem.PINOperationError
				if errors.As(err, &operation) && identity == m.Snapshot().SIMIdentity {
					after = pinStatusResponse(operation.Status, before.SIMToken)
				}
			}
			return classifySIMPINError(op.kind, before, after, err)
		}
		m.markNetworkStateChanged()
		if op.kind == pinUnlock || op.kind == pinUnblock {
			current := m.Snapshot().SIMIdentity
			// Unlocking can reveal an ICCID that was unreadable while locked.
			revealed := identity.ICCID == "" && current.ICCID != "" && identity.Slot == current.Slot && current.revision == identity.revision+1
			if current != identity && !revealed {
				return ErrEnableAfterSIMUnlock
			}
			if err := device.SetPowerState(ctx, wwanmodem.PowerStateOn); err != nil {
				return ErrEnableAfterSIMUnlock
			}
			m.applyPowerState(wwanmodem.PowerStateOn)
		}
		return nil
	})
}

func classifySIMPINError(kind simPINAction, before, after SIMPINStatus, err error) error {
	if after.Lock == wwanmodem.SIMLockBlocked || errors.Is(err, wwanmodem.ErrPINPermanentlyBlocked) {
		return ErrSIMPermanentlyBlocked
	}
	if kind != pinUnblock && (after.Lock == wwanmodem.SIMLockPUK || errors.Is(err, wwanmodem.ErrPUKRequired)) {
		return ErrSIMPUKRequired
	}
	incorrect := errors.Is(err, wwanmodem.ErrIncorrectPIN)
	if kind == pinUnblock {
		incorrect = incorrect || (before.PUKRetries != nil && after.PUKRetries != nil && *after.PUKRetries < *before.PUKRetries)
		if incorrect {
			return ErrSIMIncorrectPUK
		}
	} else {
		incorrect = incorrect || (before.PINRetries != nil && after.PINRetries != nil && *after.PINRetries < *before.PINRetries)
		if incorrect {
			return ErrSIMIncorrectPIN
		}
	}
	if errors.Is(err, wwanmodem.ErrNotSupported) {
		return ErrSIMPINUnsupported
	}
	// Transport and unclassified failures may have occurred after card execution.
	// Do not expose arbitrary device text or suggest an automatic retry.
	return ErrSIMPINResultUnknown
}
