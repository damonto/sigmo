package modem

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/damonto/wwan-go/cdcwdm"
	wwanmodem "github.com/damonto/wwan-go/modem"
	"github.com/damonto/wwan-go/qcom"
	qmitransport "github.com/damonto/wwan-go/qcom/qmi"
)

var ErrProfileIDMissing = errors.New("profile id is missing")

// Modem is Sigmo's API adapter around one process-owned wwan-go modem.
type Modem struct {
	core         *wwanmodem.Modem
	deviceInfo   wwanmodem.Device
	deviceKey    string
	generation   uint64
	closeOnce    sync.Once
	closeErr     error
	watchOnce    sync.Once
	watchCancel  context.CancelFunc
	watchWG      sync.WaitGroup
	failureOnce  sync.Once
	onFailure    func(error)
	runtimeMu    sync.RWMutex
	simSlotOnce  sync.Once
	simSlotToken chan struct{}
	ussdMu       sync.RWMutex
	ussd         wwanmodem.USSDMessage
	smsMu        sync.RWMutex
	smsStorage   wwanmodem.MessageStorage
	bearerMu     sync.Mutex
	bearers      map[uint64]*Bearer
	slotSIMs     map[uint32]*SIM
	airplaneMode bool
	registration Modem3GPPRegistrationState
	access       []ModemAccessTechnology
	operatorName string
	operatorCode string
	signal       uint32
	statusKnown  bool

	Device              string
	Manufacturer        string
	EquipmentIdentifier string
	Driver              string
	Model               string
	FirmwareRevision    string
	HardwareRevision    string
	State               ModemState
	UnlockRequired      ModemLock
	PrimaryPort         string
	PrimarySimSlot      uint32
	Sim                 *SIM
	SimSlots            []uint32
	Ports               []ModemPort
	Number              string
}

var modemWatchRetryDelay = time.Second

// ModemSnapshot is a point-in-time copy of state which may change while the
// process owns the modem. Device identity and port topology remain immutable
// for one generation and stay on Modem itself.
type ModemSnapshot struct {
	State          ModemState
	UnlockRequired ModemLock
	PrimarySIMSlot uint32
	SIM            *SIM
	SIMSlots       []uint32
	Slots          []*SIM
	Number         string
	AirplaneMode   bool
	Registration   Modem3GPPRegistrationState
	Access         []ModemAccessTechnology
	OperatorName   string
	OperatorCode   string
	SignalQuality  uint32
	StatusKnown    bool
}

func (m *Modem) Snapshot() ModemSnapshot {
	if m == nil {
		return ModemSnapshot{}
	}
	m.runtimeMu.RLock()
	defer m.runtimeMu.RUnlock()
	return ModemSnapshot{
		State:          m.State,
		UnlockRequired: m.UnlockRequired,
		PrimarySIMSlot: m.PrimarySimSlot,
		SIM:            cloneSIM(m, m.Sim),
		SIMSlots:       slices.Clone(m.SimSlots),
		Slots:          cloneSIMSlots(m),
		Number:         m.Number,
		AirplaneMode:   m.airplaneMode,
		Registration:   m.registration,
		Access:         slices.Clone(m.access),
		OperatorName:   m.operatorName,
		OperatorCode:   m.operatorCode,
		SignalQuality:  m.signal,
		StatusKnown:    m.statusKnown,
	}
}

func cloneSIMSlots(m *Modem) []*SIM {
	if len(m.SimSlots) == 0 {
		if m.Sim == nil {
			return nil
		}
		return []*SIM{cloneSIM(m, m.Sim)}
	}
	slots := make([]*SIM, 0, len(m.SimSlots))
	for _, slot := range m.SimSlots {
		sim := cloneSIM(m, m.slotSIMs[slot])
		if sim == nil && m.Sim != nil && (m.Sim.Slot == slot || (m.Sim.Slot == 0 && m.PrimarySimSlot == slot)) {
			sim = cloneSIM(m, m.Sim)
		}
		if sim == nil {
			sim = &SIM{modem: m, Slot: slot, Active: slot == m.PrimarySimSlot}
		}
		slots = append(slots, sim)
	}
	return slots
}

func cloneSIM(m *Modem, sim *SIM) *SIM {
	if sim == nil {
		return nil
	}
	cloned := *sim
	cloned.modem = m
	cloned.ATR = slices.Clone(sim.ATR)
	return &cloned
}

func (m *Modem) applyStatus(status wwanmodem.Status) {
	if m == nil {
		return
	}
	m.runtimeMu.Lock()
	m.State = legacyModemState(status)
	m.airplaneMode = status.Power == wwanmodem.PowerStateOff || status.Power == wwanmodem.PowerStateLow
	m.registration = legacyRegistration(status.Registration)
	m.access = accessTechnologies(status.Technology)
	m.operatorName = strings.TrimSpace(status.OperatorName)
	m.operatorCode = strings.TrimSpace(status.OperatorID)
	m.signal = uint32(status.SignalQuality)
	m.statusKnown = true
	if status.SIM == wwanmodem.SIMStateLocked {
		m.UnlockRequired = ModemLockSimPin
	} else if status.SIM != wwanmodem.SIMStateUnknown {
		m.UnlockRequired = ModemLockNone
	}
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
	if info.State == wwanmodem.SIMStateLocked {
		m.UnlockRequired = ModemLockSimPin
	} else {
		m.UnlockRequired = ModemLockNone
	}
	if m.PrimarySimSlot != 0 && !slices.Contains(m.SimSlots, m.PrimarySimSlot) {
		m.SimSlots = append(m.SimSlots, m.PrimarySimSlot)
		slices.Sort(m.SimSlots)
	}
	m.runtimeMu.Unlock()
}

func (m *Modem) applySIMSlots(slots []wwanmodem.SIMSlot) {
	if m == nil {
		return
	}
	m.runtimeMu.Lock()
	defer m.runtimeMu.Unlock()

	values := make([]uint32, 0, len(slots))
	known := make(map[uint32]*SIM, len(slots))
	var active uint32
	for _, slot := range slots {
		if slot.Index == 0 {
			continue
		}
		index := uint32(slot.Index)
		values = append(values, index)
		if slot.Active {
			active = index
		}

		identifier := strings.TrimSpace(slot.ICCID)
		cached := m.slotSIMs[index]
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

type ModemPort struct {
	PortType ModemPortType
	Device   string
}

func (m *Modem) Path() string {
	if m == nil {
		return ""
	}
	return m.deviceKey
}

func (m *Modem) Generation() uint64 {
	if m == nil {
		return 0
	}
	return m.generation
}

func (m *Modem) WWAN() *wwanmodem.Modem {
	if m == nil {
		return nil
	}
	return m.core
}

// ReserveSIMSlot prevents the active SIM slot from changing until the
// returned release function is called.
func (m *Modem) ReserveSIMSlot(ctx context.Context) (func(), error) {
	if m == nil {
		return nil, errModemRequired
	}
	m.simSlotOnce.Do(func() {
		m.simSlotToken = make(chan struct{}, 1)
		m.simSlotToken <- struct{}{}
	})

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-m.simSlotToken:
	}

	var once sync.Once
	return func() {
		once.Do(func() { m.simSlotToken <- struct{}{} })
	}, nil
}

func (m *Modem) Close() error {
	if m == nil {
		return nil
	}
	m.closeOnce.Do(func() {
		if m.watchCancel != nil {
			m.watchCancel()
		}
		m.watchWG.Wait()
		m.closeBearerAdapters()
		if m.core != nil {
			m.closeErr = m.core.Close()
		}
	})
	return m.closeErr
}

func (m *Modem) ProfileID(ctx context.Context) (string, error) {
	if m == nil {
		return "", errModemRequired
	}
	snapshot := m.Snapshot()
	if snapshot.SIM != nil {
		if id := strings.TrimSpace(snapshot.SIM.Identifier); id != "" {
			return id, nil
		}
		if id := strings.TrimSpace(snapshot.SIM.Eid); id != "" {
			return id, nil
		}
	}
	if m.core == nil {
		return "", ErrProfileIDMissing
	}
	info, err := m.core.SIMInfo(ctx)
	if err != nil {
		return "", fmt.Errorf("read SIM profile identifier: %w", err)
	}
	if id := strings.TrimSpace(info.ICCID); id != "" {
		return id, nil
	}
	if id := strings.TrimSpace(info.EID); id != "" {
		return id, nil
	}
	return "", ErrProfileIDMissing
}

func (m *Modem) AirplaneMode(ctx context.Context) (bool, error) {
	if m == nil {
		return false, errModemRequired
	}
	if m.core == nil {
		return false, wwanmodem.ErrNotSupported
	}
	state, err := m.core.PowerState(ctx)
	if err != nil {
		return false, fmt.Errorf("read modem power state: %w", err)
	}
	return airplaneModeEnabled(state), nil
}

func (m *Modem) SetAirplaneMode(ctx context.Context, enabled bool) error {
	if m == nil {
		return errModemRequired
	}
	if m.core == nil {
		return wwanmodem.ErrNotSupported
	}
	if enabled {
		return m.Disable(ctx)
	}
	return m.Enable(ctx)
}

func airplaneModeEnabled(state wwanmodem.PowerState) bool {
	return state == wwanmodem.PowerStateOff || state == wwanmodem.PowerStateLow
}

func (m *Modem) Enable(ctx context.Context) error {
	if m == nil || m.core == nil {
		return errModemRequired
	}
	return m.core.SetPowerState(ctx, wwanmodem.PowerStateOn)
}

func (m *Modem) Reset(ctx context.Context) error {
	if m == nil || m.core == nil {
		return errModemRequired
	}
	return m.core.Reset(ctx)
}

func (m *Modem) Disable(ctx context.Context) error {
	if m == nil || m.core == nil {
		return errModemRequired
	}
	return m.core.SetPowerState(ctx, wwanmodem.PowerStateLow)
}

func (m *Modem) SetPrimarySimSlot(ctx context.Context, slot uint32) error {
	if m == nil || m.core == nil {
		return errModemRequired
	}
	if slot == 0 || slot > 255 {
		return fmt.Errorf("set primary SIM slot: slot %d is outside 1..255", slot)
	}
	release, err := m.ReserveSIMSlot(ctx)
	if err != nil {
		return fmt.Errorf("reserve primary SIM slot: %w", err)
	}
	defer release()
	return m.core.SetPrimarySIMSlot(ctx, uint8(slot))
}

func (m *Modem) SupportedModes(ctx context.Context) ([]ModemModePair, error) {
	if m == nil || m.core == nil {
		return nil, errModemRequired
	}
	modes, _, err := m.core.Modes(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]ModemModePair, 0, len(modes))
	for _, mode := range modes {
		result = append(result, modemModePair(mode))
	}
	return result, nil
}

func (m *Modem) CurrentModes(ctx context.Context) (ModemModePair, error) {
	if m == nil || m.core == nil {
		return ModemModePair{}, errModemRequired
	}
	_, current, err := m.core.Modes(ctx)
	return modemModePair(current), err
}

func (m *Modem) SetCurrentModes(ctx context.Context, mode ModemModePair) error {
	if m == nil || m.core == nil {
		return errModemRequired
	}
	return m.core.SetModes(ctx, wwanmodem.Mode{
		Allowed:   modemTechnology(mode.Allowed),
		Preferred: modemTechnology(mode.Preferred),
	})
}

func (m *Modem) SupportedBands(ctx context.Context) ([]ModemBand, error) {
	if m == nil || m.core == nil {
		return nil, errModemRequired
	}
	bands, err := m.core.SupportedBands(ctx)
	if err != nil {
		return nil, err
	}
	return legacyBands(bands, true), nil
}

func (m *Modem) CurrentBands(ctx context.Context) ([]ModemBand, error) {
	if m == nil || m.core == nil {
		return nil, errModemRequired
	}
	bands, err := m.core.Bands(ctx)
	if err != nil {
		return nil, err
	}
	return legacyBands(bands, false), nil
}

func (m *Modem) SetCurrentBands(ctx context.Context, bands []ModemBand) error {
	if m == nil || m.core == nil {
		return errModemRequired
	}
	if slices.Equal(bands, []ModemBand{ModemBandAny}) {
		return m.core.SetBands(ctx, nil)
	}
	converted := make([]wwanmodem.Band, 0, len(bands))
	for _, band := range bands {
		value, ok := semanticBand(band)
		if !ok {
			return fmt.Errorf("set current bands: unsupported legacy band %d", band)
		}
		converted = append(converted, value)
	}
	return m.core.SetBands(ctx, converted)
}

func (m *Modem) AccessTechnologies(ctx context.Context) ([]ModemAccessTechnology, error) {
	if m == nil || m.core == nil {
		return nil, errModemRequired
	}
	status, err := m.core.NetworkStatus(ctx)
	if err != nil {
		return nil, err
	}
	return accessTechnologies(status.Technology), nil
}

func (m *Modem) SignalQuality(ctx context.Context) (percent uint32, recent bool, err error) {
	if m == nil || m.core == nil {
		return 0, false, errModemRequired
	}
	signal, err := m.core.Signal(ctx)
	if err != nil {
		return 0, false, err
	}
	return uint32(signal.Quality), true, nil
}

func (m *Modem) PrimaryPortType() ModemPortType {
	if m == nil {
		return ModemPortTypeUnknown
	}
	for _, port := range m.Ports {
		if port.Device == m.PrimaryPort {
			return port.PortType
		}
	}
	return ModemPortTypeUnknown
}

func (m *Modem) Port(portType ModemPortType) (*ModemPort, error) {
	if m == nil {
		return nil, errModemRequired
	}
	for i := range m.Ports {
		if m.Ports[i].PortType == portType {
			return &m.Ports[i], nil
		}
	}
	return nil, fmt.Errorf("modem port type %d is unavailable", portType)
}

func modemModePair(mode wwanmodem.Mode) ModemModePair {
	return ModemModePair{Allowed: legacyMode(mode.Allowed), Preferred: legacyMode(mode.Preferred)}
}

func legacyMode(technology wwanmodem.Technology) ModemMode {
	if technology == wwanmodem.TechnologyAny {
		return ModemModeAny
	}
	var mode ModemMode
	if technology&wwanmodem.TechnologyGSM != 0 {
		mode |= ModemMode2G
	}
	if technology&wwanmodem.TechnologyUMTS != 0 {
		mode |= ModemMode3G
	}
	if technology&(wwanmodem.TechnologyLTE|wwanmodem.TechnologyLTECatM|wwanmodem.TechnologyLTENB) != 0 {
		mode |= ModemMode4G
	}
	if technology&(wwanmodem.TechnologyNR5GNSA|wwanmodem.TechnologyNR5GSA) != 0 {
		mode |= ModemMode5G
	}
	return mode
}

func modemTechnology(mode ModemMode) wwanmodem.Technology {
	if mode == ModemModeAny {
		return wwanmodem.TechnologyAny
	}
	var technology wwanmodem.Technology
	if mode&ModemMode2G != 0 {
		technology |= wwanmodem.TechnologyGSM
	}
	if mode&ModemMode3G != 0 {
		technology |= wwanmodem.TechnologyUMTS
	}
	if mode&ModemMode4G != 0 {
		technology |= wwanmodem.TechnologyLTE | wwanmodem.TechnologyLTECatM | wwanmodem.TechnologyLTENB
	}
	if mode&ModemMode5G != 0 {
		technology |= wwanmodem.TechnologyNR5GNSA | wwanmodem.TechnologyNR5GSA
	}
	return technology
}

func legacyBands(bands []wwanmodem.Band, includeAny bool) []ModemBand {
	result := make([]ModemBand, 0, len(bands)+1)
	if includeAny {
		result = append(result, ModemBandAny)
	}
	for _, band := range bands {
		if value, ok := legacyBand(band); ok && !slices.Contains(result, value) {
			result = append(result, value)
		}
	}
	slices.Sort(result)
	return result
}

func legacyBand(band wwanmodem.Band) (ModemBand, bool) {
	switch band.Technology {
	case wwanmodem.TechnologyLTE, wwanmodem.TechnologyLTECatM, wwanmodem.TechnologyLTENB:
		if band.Number >= 1 && band.Number <= 85 {
			return ModemBand(band.Number + 30), true
		}
	case wwanmodem.TechnologyNR5GNSA, wwanmodem.TechnologyNR5GSA:
		if band.Number > 0 && band.Number < 300 {
			return ModemBand(band.Number + 300), true
		}
	case wwanmodem.TechnologyUMTS:
		value, ok := umtsLegacyBands[band.Number]
		return value, ok
	case wwanmodem.TechnologyGSM:
		value, ok := gsmLegacyBands[band.Number]
		return value, ok
	}
	return ModemBandUnknown, false
}

func semanticBand(band ModemBand) (wwanmodem.Band, bool) {
	if band >= 31 && band <= 115 {
		return wwanmodem.Band{Technology: wwanmodem.TechnologyLTE, Number: uint16(band - 30)}, true
	}
	if band >= 301 && band < 600 {
		return wwanmodem.Band{Technology: wwanmodem.TechnologyNR5GSA, Number: uint16(band - 300)}, true
	}
	for number, value := range umtsLegacyBands {
		if value == band {
			return wwanmodem.Band{Technology: wwanmodem.TechnologyUMTS, Number: number}, true
		}
	}
	for number, value := range gsmLegacyBands {
		if value == band {
			return wwanmodem.Band{Technology: wwanmodem.TechnologyGSM, Number: number}, true
		}
	}
	return wwanmodem.Band{}, false
}

var umtsLegacyBands = map[uint16]ModemBand{
	1: 5, 2: 12, 3: 6, 4: 7, 5: 9, 6: 8, 7: 13, 8: 10, 9: 11,
	10: 210, 11: 211, 12: 212, 13: 213, 14: 214, 19: 219, 20: 220,
	21: 221, 22: 222, 25: 225, 26: 226, 32: 232,
}

// GSM has no standardized numeric band number. These values preserve Sigmo's
// long-standing public API encoding.
var gsmLegacyBands = map[uint16]ModemBand{
	900: 1, 1800: 2, 1900: 3, 850: 4, 450: 14, 480: 15, 750: 16,
	380: 17, 410: 18, 710: 19, 810: 20,
}

func accessTechnologies(technology wwanmodem.Technology) []ModemAccessTechnology {
	var result []ModemAccessTechnology
	if technology&wwanmodem.TechnologyGSM != 0 {
		result = append(result, ModemAccessTechnologyGsm)
	}
	if technology&wwanmodem.TechnologyUMTS != 0 {
		result = append(result, ModemAccessTechnologyUmts)
	}
	if technology&wwanmodem.TechnologyLTE != 0 {
		result = append(result, ModemAccessTechnologyLte)
	}
	if technology&wwanmodem.TechnologyLTECatM != 0 {
		result = append(result, ModemAccessTechnologyLteCatM)
	}
	if technology&wwanmodem.TechnologyLTENB != 0 {
		result = append(result, ModemAccessTechnologyLteNBIot)
	}
	if technology&(wwanmodem.TechnologyNR5GNSA|wwanmodem.TechnologyNR5GSA) != 0 {
		result = append(result, ModemAccessTechnology5GNR)
	}
	return result
}
