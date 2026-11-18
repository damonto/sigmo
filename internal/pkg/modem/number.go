package modem

import (
	"strings"

	wwanmodem "github.com/damonto/wwan-go/modem"
)

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

// SetIMSNumber records an IMS-derived number for the specified SIM.
// An empty number clears only the IMS observation, preserving MDN/history.
// Modem-reported numbers always take precedence.
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

// SetKnownNumber fills an empty number from history or an MDN entitlement.
// It rejects stale activations and never replaces a number learned meanwhile.
// IMS can subsequently take precedence without erasing this observation.
func (m *Modem) SetKnownNumber(target SIMIdentity, number string) bool {
	if m == nil || target.ICCID == "" || strings.TrimSpace(number) == "" {
		return false
	}
	m.runtimeMu.Lock()
	if target != m.simIdentityLocked() || strings.TrimSpace(m.Number) != "" ||
		strings.TrimSpace(m.imsNumber) != "" || m.knownNumber != "" {
		m.runtimeMu.Unlock()
		return false
	}
	m.knownNumber = strings.TrimSpace(number)
	m.runtimeMu.Unlock()
	m.notifyNumberChanged()
	return true
}

// SetNumber records a successfully written MSISDN only while its SIM activation is current.
func (m *Modem) SetNumber(target SIMIdentity, number string) bool {
	if m == nil || target.ICCID == "" {
		return false
	}
	m.runtimeMu.Lock()
	if target != m.simIdentityLocked() {
		m.runtimeMu.Unlock()
		return false
	}
	m.Number = strings.TrimSpace(number)
	m.runtimeMu.Unlock()
	m.notifyNumberChanged()
	return true
}

func (m *Modem) notifyNumberChanged() {
	// Deduplicate observations, but never hold a lock across subscriber callbacks:
	// a subscriber may synchronously update the modem again.
	m.numberNotifyMu.Lock()
	snapshot := m.Snapshot()
	m.runtimeMu.RLock()
	notify := m.onNumberChange
	m.runtimeMu.RUnlock()
	if notify == nil || (m.notifiedSIM == snapshot.SIMIdentity && m.notifiedNumber == snapshot.Number) {
		m.numberNotifyMu.Unlock()
		return
	}
	m.notifiedSIM, m.notifiedNumber = snapshot.SIMIdentity, snapshot.Number
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
	m.knownNumber = ""
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
	m.knownNumber = ""
	if m.SIM != nil {
		m.SIM.PINStatus = wwanmodem.SIMPINStatus{Lock: wwanmodem.SIMLockUnknown}
	}
}
