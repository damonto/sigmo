package modem

import (
	"errors"
	"strings"

	wwanmodem "github.com/damonto/wwan-go/modem"
)

// ErrSIMActivationChanged indicates that an observation no longer belongs to the active SIM.
var ErrSIMActivationChanged = errors.New("SIM activation changed")

// SIMIdentity binds an observation to one activation of a SIM on a modem.
// Its private version distinguishes successive activations of the same ICCID.
type SIMIdentity struct {
	ICCID string
	Slot  uint32

	modem    *Modem
	revision uint64
	absent   bool
}

// SIMIdentity returns the identity of the current SIM activation.
func (m *Modem) SIMIdentity() SIMIdentity {
	if m == nil {
		return SIMIdentity{}
	}
	m.runtimeMu.RLock()
	defer m.runtimeMu.RUnlock()
	return m.simIdentityLocked()
}

// MatchesSIMActivation reports whether identity still belongs to the active SIM
// on this modem generation. Reserve the SIM slot before checking and acting.
func (m *Modem) MatchesSIMActivation(identity SIMIdentity) bool {
	if identity.ICCID == "" || m.SIMIdentity() != identity || m.SIMReloadPending() {
		return false
	}
	select {
	case <-m.Done():
		return false
	default:
		return true
	}
}

// SetIMSNumber records a live fallback for the specified SIM. An empty number
// clears only the IMS observation. Saved numbers and card readings take precedence.
// It returns false if the SIM or modem activation has changed.
func (m *Modem) SetIMSNumber(target SIMIdentity, number string) bool {
	if m == nil || target.ICCID == "" {
		return false
	}
	m.runtimeMu.Lock()
	if target != m.simIdentityLocked() {
		m.runtimeMu.Unlock()
		return false
	}
	m.imsNumber = strings.TrimSpace(number)
	m.runtimeMu.Unlock()
	m.notifyNumberChanged()
	return true
}

// UpdateStoredNumber serializes an explicit edit or network observation with
// other number updates. An empty result leaves the number and revision unchanged.
func (m *Modem) UpdateStoredNumber(target SIMIdentity, update func(ModemSnapshot) (string, error)) error {
	return m.syncStoredNumber(target, update, true)
}

// RestoreStoredNumber loads cached data without invalidating a pending network lookup.
func (m *Modem) RestoreStoredNumber(target SIMIdentity, read func(ModemSnapshot) (string, error)) error {
	return m.syncStoredNumber(target, read, false)
}

// The callback may access storage, but must not call back into the modem.
func (m *Modem) syncStoredNumber(target SIMIdentity, update func(ModemSnapshot) (string, error), fresh bool) error {
	if m == nil || target.ICCID == "" || m.SIMReloadPending() {
		return ErrSIMActivationChanged
	}
	m.runtimeMu.Lock()
	defer m.runtimeMu.Unlock()
	if target != m.simIdentityLocked() {
		return ErrSIMActivationChanged
	}
	select {
	case <-m.Done():
		return ErrSIMActivationChanged
	default:
	}
	number, err := update(m.snapshotLocked())
	if err != nil {
		return err
	}
	if number = strings.TrimSpace(number); number != "" {
		m.storedNumber = number
		if fresh {
			m.numberRevision++
		}
	}
	return nil
}

func (m *Modem) notifyNumberChanged() {
	// Deduplicate observations, but never hold a lock across subscriber callbacks:
	// a subscriber may synchronously update the modem again.
	m.numberNotifyMu.Lock()
	snapshot := m.Snapshot()
	var operator string
	if snapshot.SIM != nil {
		operator = snapshot.SIM.OperatorIdentifier
	}
	m.runtimeMu.RLock()
	notify := m.onNumberChange
	m.runtimeMu.RUnlock()
	if notify == nil || (m.notifiedSIM == snapshot.SIMIdentity && m.notifiedNumber == snapshot.Number && m.notifiedPLMN == operator) {
		m.numberNotifyMu.Unlock()
		return
	}
	m.notifiedSIM, m.notifiedNumber = snapshot.SIMIdentity, snapshot.Number
	m.notifiedPLMN = operator
	m.numberNotifyMu.Unlock()
	if snapshot.SIMIdentity.ICCID != "" && strings.TrimSpace(snapshot.Number) != "" {
		notify()
	}
}

func (m *Modem) simIdentityLocked() SIMIdentity {
	identity := SIMIdentity{
		Slot:     m.PrimarySIMSlot,
		modem:    m,
		revision: m.simRevision,
		absent:   m.Status.SIM == wwanmodem.SIMStateAbsent,
	}
	if m.SIM != nil && !identity.absent {
		identity.ICCID = strings.TrimSpace(m.SIM.Identifier)
	}
	return identity
}

func (m *Modem) invalidateSIMIdentityLocked(previous SIMIdentity) {
	if previous == m.simIdentityLocked() {
		return
	}
	m.simRevision++
	m.imsNumber = ""
	m.storedNumber = ""
}

// InvalidateSIMActivation retires observations before a slot or profile refresh.
// Callers must hold ReserveSIMSlot. Even a timed-out switch may change the SIM
// before firmware exposes its new ICCID, so invalidate before sending it.
func (m *Modem) InvalidateSIMActivation() {
	if m == nil {
		return
	}
	m.runtimeMu.Lock()
	defer m.runtimeMu.Unlock()
	m.simRevision++
	m.imsNumber = ""
	m.storedNumber = ""
	if m.SIM != nil {
		m.SIM.PINStatus = wwanmodem.SIMPINStatus{Lock: wwanmodem.SIMLockUnknown}
	}
}
