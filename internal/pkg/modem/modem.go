package modem

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	wwanmodem "github.com/damonto/wwan-go/modem"
)

var ErrProfileIDMissing = errors.New("profile id is missing")

// Modem is Sigmo's API adapter around one process-owned wwan-go modem.
type Modem struct {
	core           *wwanmodem.Modem
	deviceInfo     wwanmodem.Device
	deviceKey      string
	generation     uint64
	closeOnce      sync.Once
	doneOnce       sync.Once
	done           chan struct{}
	closeErr       error
	watchOnce      sync.Once
	watchCancel    context.CancelFunc
	watchWG        sync.WaitGroup
	failureOnce    sync.Once
	networkState   atomic.Uint64
	onFailure      func(error)
	onSIMChange    func(uint32, string)
	deviceSessions deviceSessionStore
	runtimeMu      sync.RWMutex
	simRefreshMu   sync.Mutex
	simRefresh     simRefreshState
	simSlotOnce    sync.Once
	simSlotToken   chan struct{}
	ussdMu         sync.RWMutex
	ussd           wwanmodem.USSDMessage
	smsMu          sync.RWMutex
	smsStorage     wwanmodem.MessageStorage
	bearerMu       sync.Mutex
	bearers        map[uint64]*Bearer
	slotSIMs       map[uint32]*SIM
	statusKnown    bool

	Device              string
	Manufacturer        string
	EquipmentIdentifier string
	Driver              string
	Model               string
	FirmwareRevision    string
	HardwareRevision    string
	Status              wwanmodem.Status
	PrimaryPort         string
	PrimarySIMSlot      uint32
	SIM                 *SIM
	SIMSlots            []uint32
	Ports               []ModemPort
	Number              string
}

// ModemSnapshot is a point-in-time copy of state which may change while the
// process owns the modem. Device identity and port topology remain immutable
// for one generation and stay on Modem itself.
type ModemSnapshot struct {
	Status         wwanmodem.Status
	PrimarySIMSlot uint32
	SIM            *SIM
	SIMSlots       []uint32
	Slots          []*SIM
	Number         string
	StatusKnown    bool
}

func (s ModemSnapshot) AirplaneMode() bool {
	return airplaneModeEnabled(s.Status.Power)
}

func (s ModemSnapshot) Locked() bool {
	return s.Status.SIM == wwanmodem.SIMStateLocked
}

func (m *Modem) Snapshot() ModemSnapshot {
	if m == nil {
		return ModemSnapshot{}
	}
	m.runtimeMu.RLock()
	defer m.runtimeMu.RUnlock()
	return ModemSnapshot{
		Status:         m.Status,
		PrimarySIMSlot: m.PrimarySIMSlot,
		SIM:            cloneSIM(m, m.SIM),
		SIMSlots:       slices.Clone(m.SIMSlots),
		Slots:          cloneSIMSlots(m),
		Number:         m.Number,
		StatusKnown:    m.statusKnown || m.Status != (wwanmodem.Status{}),
	}
}

func cloneSIMSlots(m *Modem) []*SIM {
	if len(m.SIMSlots) == 0 {
		if m.SIM == nil {
			return nil
		}
		return []*SIM{cloneSIM(m, m.SIM)}
	}
	slots := make([]*SIM, 0, len(m.SIMSlots))
	for _, slot := range m.SIMSlots {
		sim := cloneSIM(m, m.slotSIMs[slot])
		if sim == nil && m.SIM != nil && (m.SIM.Slot == slot || (m.SIM.Slot == 0 && m.PrimarySIMSlot == slot)) {
			sim = cloneSIM(m, m.SIM)
		}
		if sim == nil {
			sim = &SIM{modem: m, Slot: slot, Active: slot == m.PrimarySIMSlot}
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

type ModemPort struct {
	PortType wwanmodem.PortType
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

// Done is closed when this modem generation is no longer usable.
//
// A modem can be replaced while a long-running operation is still in flight.
// Exposing the generation lifecycle lets those operations stop without tying
// their context to an individual HTTP request.
func (m *Modem) Done() <-chan struct{} {
	if m == nil {
		return nil
	}
	m.doneOnce.Do(func() {
		m.done = make(chan struct{})
	})
	return m.done
}

// NetworkStateVersion changes after a successful radio, SIM, or registration
// mutation. Consumers can bind short-lived cached observations to the modem
// state which produced them without reading radio settings on every request.
func (m *Modem) NetworkStateVersion() uint64 {
	if m == nil {
		return 0
	}
	return m.networkState.Load()
}

func (m *Modem) markNetworkStateChanged() {
	if m != nil {
		m.networkState.Add(1)
	}
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

func (m *Modem) withReservedSIMSlot(ctx context.Context, run func() error) error {
	release, err := m.ReserveSIMSlot(ctx)
	if err != nil {
		return fmt.Errorf("reserve SIM slot: %w", err)
	}
	defer release()
	return run()
}

func (m *Modem) Close() error {
	if m == nil {
		return nil
	}
	m.doneOnce.Do(func() {
		m.done = make(chan struct{})
	})
	m.closeOnce.Do(func() {
		close(m.done)
		if m.watchCancel != nil {
			m.watchCancel()
		}
		m.closeErr = errors.Join(m.closeErr, m.deviceSessions.close())
		m.watchWG.Wait()
		m.closeBearerAdapters()
		if m.core != nil {
			m.closeErr = errors.Join(m.closeErr, m.core.Close())
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
		if id := strings.TrimSpace(snapshot.SIM.EID); id != "" {
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

func (m *Modem) PrimaryPortType() wwanmodem.PortType {
	if m == nil {
		return wwanmodem.PortUnknown
	}
	for _, port := range m.Ports {
		if port.Device == m.PrimaryPort {
			return port.PortType
		}
	}
	return wwanmodem.PortUnknown
}

func (m *Modem) Port(portType wwanmodem.PortType) (*ModemPort, error) {
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
