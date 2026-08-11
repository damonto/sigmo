package modem

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/damonto/wwan-go/cdcwdm"
	wwanmodem "github.com/damonto/wwan-go/modem"
	"github.com/damonto/wwan-go/qcom"
	qmitransport "github.com/damonto/wwan-go/qcom/qmi"
)

var modemWatchRetryDelay = time.Second

func (m *Modem) applyStatus(status wwanmodem.Status) {
	if m == nil {
		return
	}
	status.OperatorName = strings.TrimSpace(status.OperatorName)
	status.OperatorID = strings.TrimSpace(status.OperatorID)
	m.runtimeMu.Lock()
	m.Status = status
	m.statusKnown = true
	m.runtimeMu.Unlock()
}

func (m *Modem) applyPowerState(state wwanmodem.PowerState) {
	if m == nil {
		return
	}
	m.runtimeMu.Lock()
	m.Status.Power = state
	m.statusKnown = true
	if state == wwanmodem.PowerStateOff || state == wwanmodem.PowerStateLow {
		m.Status.Registration = wwanmodem.RegistrationIdle
		m.Status.PacketService = wwanmodem.PacketServiceDetached
		m.Status.Technology = 0
		m.Status.OperatorID = ""
		m.Status.OperatorName = ""
		m.Status.SignalQuality = 0
	}
	m.runtimeMu.Unlock()
}

func (m *Modem) applySIMInfo(info wwanmodem.SIMInfo) {
	if m == nil {
		return
	}
	m.runtimeMu.Lock()
	// The process-owned QMI control client is created for slot 1, so some
	// firmware reports that configured slot in SIMInfo even when another
	// physical slot is active. SIMSlots is authoritative once discovered.
	if activeSlot := activeSIMSlot(m.slotSIMs); activeSlot != 0 {
		info.Slot = uint8(activeSlot)
	}
	previous := m.SIM
	m.PrimarySIMSlot = uint32(info.Slot)
	for _, sim := range m.slotSIMs {
		if sim != nil {
			sim.Active = false
		}
	}
	m.SIM = mergeSIMMetadata(previous, simFromInfo(m, info))
	if m.PrimarySIMSlot != 0 {
		if m.slotSIMs == nil {
			m.slotSIMs = make(map[uint32]*SIM)
		}
		m.slotSIMs[m.PrimarySIMSlot] = cloneSIM(m, m.SIM)
	}
	if len(info.OwnNumbers) > 0 {
		m.Number = strings.TrimSpace(info.OwnNumbers[0])
	} else if !sameSIMIdentity(previous, m.SIM) {
		m.Number = ""
	}
	m.Status.SIM = info.State
	if m.PrimarySIMSlot != 0 && !slices.Contains(m.SIMSlots, m.PrimarySIMSlot) {
		m.SIMSlots = append(m.SIMSlots, m.PrimarySIMSlot)
		slices.Sort(m.SIMSlots)
	}
	m.runtimeMu.Unlock()
}

func activeSIMSlot(slots map[uint32]*SIM) uint32 {
	var active uint32
	for slot, sim := range slots {
		if sim == nil || !sim.Active {
			continue
		}
		if active != 0 {
			return 0
		}
		active = slot
	}
	return active
}

func mergeSIMMetadata(previous, current *SIM) *SIM {
	if previous == nil || current == nil {
		return current
	}
	if previous.Slot == current.Slot && atrSupportsEUICC(previous.ATR) &&
		(strings.TrimSpace(current.Identifier) == "" || len(current.ATR) == 0) {
		// ICCID disappears briefly while one eUICC profile replaces another.
		// ATR and EID describe the card hardware, so keep them through that
		// transition and the first identity-only update for the new profile.
		current.ATR = slices.Clone(previous.ATR)
		if current.EID == "" {
			current.EID = previous.EID
		}
	}
	if !sameSIMIdentity(previous, current) {
		return current
	}
	if len(current.ATR) == 0 {
		current.ATR = slices.Clone(previous.ATR)
	}
	if current.EID == "" {
		current.EID = previous.EID
	}
	if current.IMSI == "" {
		current.IMSI = previous.IMSI
	}
	if current.OperatorIdentifier == "" {
		current.OperatorIdentifier = previous.OperatorIdentifier
	}
	if current.OperatorName == "" {
		current.OperatorName = previous.OperatorName
	}
	if current.GID1 == "" {
		current.GID1 = previous.GID1
	}
	if current.SPN == "" {
		current.SPN = previous.SPN
	}
	return current
}

func sameSIMIdentity(a, b *SIM) bool {
	return a != nil && b != nil && a.Identifier != "" && a.Identifier == b.Identifier
}

func (m *Modem) applyActiveSIMIdentity(slot uint8, iccid string) {
	if m == nil || slot == 0 {
		return
	}
	iccid = strings.TrimSpace(iccid)
	index := uint32(slot)

	m.runtimeMu.Lock()
	defer m.runtimeMu.Unlock()

	previousIdentifier := ""
	if m.SIM != nil {
		previousIdentifier = strings.TrimSpace(m.SIM.Identifier)
	}
	for _, sim := range m.slotSIMs {
		if sim != nil {
			sim.Active = false
		}
	}
	cached := m.slotSIMs[index]
	if cached == nil || (iccid != "" && strings.TrimSpace(cached.Identifier) != iccid) {
		cached = &SIM{modem: m, Slot: index}
	} else {
		cached = cloneSIM(m, cached)
	}
	cached.Slot = index
	cached.Active = true
	if iccid != "" {
		cached.Identifier = iccid
	}
	if m.slotSIMs == nil {
		m.slotSIMs = make(map[uint32]*SIM)
	}
	m.slotSIMs[index] = cached
	m.PrimarySIMSlot = index
	m.SIM = cloneSIM(m, cached)
	if !slices.Contains(m.SIMSlots, index) {
		m.SIMSlots = append(m.SIMSlots, index)
		slices.Sort(m.SIMSlots)
	}
	if nextIdentifier := strings.TrimSpace(cached.Identifier); previousIdentifier != "" && nextIdentifier != "" && previousIdentifier != nextIdentifier {
		m.Number = ""
	}
}

func (m *Modem) applySIMSlots(slots []wwanmodem.SIMSlot) {
	if m == nil {
		return
	}
	m.runtimeMu.Lock()
	defer m.runtimeMu.Unlock()

	previousIdentifier := ""
	if m.SIM != nil {
		previousIdentifier = strings.TrimSpace(m.SIM.Identifier)
	}
	values := make([]uint32, 0, len(slots))
	known := make(map[uint32]*SIM, len(slots))
	var active uint32
	for _, slot := range slots {
		if slot.Index == 0 {
			continue
		}
		index := uint32(slot.Index)
		identifier := strings.TrimSpace(slot.ICCID)
		cached := m.slotSIMs[index]
		cachedIdentifier := cached != nil && strings.TrimSpace(cached.Identifier) != ""
		// Some firmware reports empty physical placeholders as SIM slots, but
		// Sigmo can only select an inactive SIM after it has a stable ICCID.
		if !slot.Active && (slot.State == wwanmodem.SIMStateAbsent || (identifier == "" && !cachedIdentifier)) {
			continue
		}
		values = append(values, index)
		if slot.Active {
			active = index
		}

		if slot.Active && m.SIM != nil &&
			(m.SIM.Slot == index || (m.SIM.Slot == 0 && m.PrimarySIMSlot == index) || (identifier != "" && m.SIM.Identifier == identifier)) {
			cached = m.SIM
		}
		if cached == nil || (identifier != "" && cached.Identifier != "" && cached.Identifier != identifier) {
			cached = &SIM{modem: m, Slot: index}
		} else {
			cached = cloneSIM(m, cached)
		}
		cached.Slot = index
		cached.Active = slot.Active
		if identifier != "" {
			cached.Identifier = identifier
		}
		if eid := strings.TrimSpace(slot.EID); eid != "" {
			cached.EID = eid
		}
		known[index] = cached
	}
	slices.Sort(values)
	values = slices.Compact(values)
	m.SIMSlots = values
	if active != 0 {
		m.PrimarySIMSlot = active
		m.SIM = cloneSIM(m, known[active])
		if m.SIM != nil && previousIdentifier != "" && m.SIM.Identifier != "" && m.SIM.Identifier != previousIdentifier {
			m.Number = ""
		}
	}
	if len(m.SIMSlots) == 0 && m.PrimarySIMSlot != 0 {
		m.SIMSlots = []uint32{m.PrimarySIMSlot}
		known[m.PrimarySIMSlot] = cloneSIM(m, m.SIM)
	}
	m.slotSIMs = known
}

func (m *Modem) startRuntimeWatchers(parent context.Context, onFailure func(error), onSIMChange func(uint32, string)) {
	if m == nil || m.core == nil {
		return
	}
	m.watchOnce.Do(func() {
		ctx, cancel := context.WithCancel(parent)
		m.watchCancel = cancel
		m.onFailure = onFailure
		m.onSIMChange = onSIMChange
		m.watchWG.Add(2)
		go m.watchStatus(ctx)
		go m.watchSIM(ctx)
		m.startSIMRefreshWatcher(ctx)
	})
}

func (m *Modem) watchStatus(ctx context.Context) {
	defer m.watchWG.Done()
	for ctx.Err() == nil {
		stream, err := m.core.WatchStatus(ctx)
		if err == nil {
			err = consumeModemStream(ctx, stream, m.applyStatus)
		}
		if ctx.Err() != nil {
			return
		}
		if m.reportTerminalRuntimeError(err) {
			return
		}
		slog.Warn("modem status watcher stopped", "imei", m.EquipmentIdentifier, "generation", m.Generation(), "error", err)
		if err := sleepContext(ctx, modemWatchRetryDelay); err != nil {
			return
		}
	}
}

func (m *Modem) watchSIM(ctx context.Context) {
	defer m.watchWG.Done()
	for ctx.Err() == nil {
		stream, err := m.core.WatchSIM(ctx)
		if err == nil {
			err = consumeModemStream(ctx, stream, func(info wwanmodem.SIMInfo) {
				m.applySIMRuntimeUpdate(ctx, info)
			})
		}
		if ctx.Err() != nil {
			return
		}
		if m.reportTerminalRuntimeError(err) {
			return
		}
		slog.Warn("modem SIM watcher stopped", "imei", m.EquipmentIdentifier, "generation", m.Generation(), "error", err)
		if err := sleepContext(ctx, modemWatchRetryDelay); err != nil {
			return
		}
	}
}

func (m *Modem) applySIMRuntimeUpdate(ctx context.Context, info wwanmodem.SIMInfo) {
	previousSlot, previousIdentifier := activeSIMIdentity(m)
	slots, err := m.core.SIMSlots(ctx)
	if err != nil {
		slog.Debug("refresh physical SIM slots", "imei", m.EquipmentIdentifier, "error", err)
	} else {
		m.applySIMSlots(slots)
	}
	m.applySIMInfo(info)
	m.notifySIMChanged(previousSlot, previousIdentifier)
}

func (m *Modem) notifySIMChanged(previousSlot uint32, previousIdentifier string) {
	nextSlot, nextIdentifier := activeSIMIdentity(m)
	if nextSlot == previousSlot && nextIdentifier == strings.TrimSpace(previousIdentifier) {
		return
	}
	if m.onSIMChange != nil {
		m.onSIMChange(previousSlot, previousIdentifier)
	}
}

func (m *Modem) reportTerminalRuntimeError(err error) bool {
	if !isTerminalRuntimeError(err) {
		return false
	}
	m.failureOnce.Do(func() {
		if m.onFailure != nil {
			m.onFailure(err)
		}
	})
	return true
}

func isTerminalRuntimeError(err error) bool {
	if errors.Is(err, cdcwdm.ErrDisconnected) {
		return true
	}
	if errors.Is(err, qcom.QMIErrorClientIDsExhausted) {
		return true
	}
	var transportErr *qmitransport.TransportError
	return errors.As(err, &transportErr)
}

func consumeModemStream[T any](ctx context.Context, stream <-chan wwanmodem.Result[T], apply func(T)) error {
	if stream == nil {
		return errors.New("modem watcher returned a nil stream")
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case result, ok := <-stream:
			if !ok {
				return errors.New("modem stream closed")
			}
			if result.Err != nil {
				return result.Err
			}
			apply(result.Value)
		}
	}
}

func (m *Modem) setUSSD(message wwanmodem.USSDMessage) {
	m.ussdMu.Lock()
	m.ussd = message
	m.ussdMu.Unlock()
}

func (m *Modem) currentUSSD() wwanmodem.USSDMessage {
	m.ussdMu.RLock()
	defer m.ussdMu.RUnlock()
	return m.ussd
}
