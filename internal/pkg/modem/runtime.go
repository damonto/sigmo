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

func (m *Modem) applySIMInfo(info wwanmodem.SIMInfo) {
	if m == nil {
		return
	}
	m.runtimeMu.Lock()
	m.PrimarySimSlot = uint32(info.Slot)
	for _, sim := range m.slotSIMs {
		if sim != nil {
			sim.Active = false
		}
	}
	m.Sim = simFromInfo(m, info)
	if m.PrimarySimSlot != 0 {
		if m.slotSIMs == nil {
			m.slotSIMs = make(map[uint32]*SIM)
		}
		m.slotSIMs[m.PrimarySimSlot] = cloneSIM(m, m.Sim)
	}
	m.Number = ""
	if len(info.OwnNumbers) > 0 {
		if number := strings.TrimSpace(info.OwnNumbers[0]); number != "" {
			m.Number = number
		}
	}
	m.Status.SIM = info.State
	if m.PrimarySimSlot != 0 && !slices.Contains(m.SimSlots, m.PrimarySimSlot) {
		m.SimSlots = append(m.SimSlots, m.PrimarySimSlot)
		slices.Sort(m.SimSlots)
	}
	m.runtimeMu.Unlock()
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
	if m.Sim != nil {
		previousIdentifier = strings.TrimSpace(m.Sim.Identifier)
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
	m.PrimarySimSlot = index
	m.Sim = cloneSIM(m, cached)
	if !slices.Contains(m.SimSlots, index) {
		m.SimSlots = append(m.SimSlots, index)
		slices.Sort(m.SimSlots)
	}
	if previousIdentifier != "" && iccid != "" && previousIdentifier != iccid {
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
	if m.Sim != nil {
		previousIdentifier = strings.TrimSpace(m.Sim.Identifier)
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

		if slot.Active && m.Sim != nil &&
			(m.Sim.Slot == index || (m.Sim.Slot == 0 && m.PrimarySimSlot == index) || (identifier != "" && m.Sim.Identifier == identifier)) {
			cached = m.Sim
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
			cached.Eid = eid
		}
		known[index] = cached
	}
	slices.Sort(values)
	values = slices.Compact(values)
	m.SimSlots = values
	if active != 0 {
		m.PrimarySimSlot = active
		m.Sim = cloneSIM(m, known[active])
		if m.Sim != nil && previousIdentifier != "" && m.Sim.Identifier != "" && m.Sim.Identifier != previousIdentifier {
			m.Number = ""
		}
	}
	if len(m.SimSlots) == 0 && m.PrimarySimSlot != 0 {
		m.SimSlots = []uint32{m.PrimarySimSlot}
		known[m.PrimarySimSlot] = cloneSIM(m, m.Sim)
	}
	m.slotSIMs = known
}

func (m *Modem) startRuntimeWatchers(parent context.Context, onFailure func(error)) {
	if m == nil || m.core == nil {
		return
	}
	m.watchOnce.Do(func() {
		ctx, cancel := context.WithCancel(parent)
		m.watchCancel = cancel
		m.onFailure = onFailure
		m.watchWG.Add(2)
		go m.watchStatus(ctx)
		go m.watchSIM(ctx)
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
			err = consumeModemStream(ctx, stream, m.applySIMInfo)
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
	if errors.Is(err, qcom.QMIErrorClientIdsExhausted) {
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
