package modem

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

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
	closeErr       error
	watchOnce      sync.Once
	watchCancel    context.CancelFunc
	watchWG        sync.WaitGroup
	failureOnce    sync.Once
	onFailure      func(error)
	deviceSessions deviceSessionStore
	runtimeMu      sync.RWMutex
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
	PrimarySimSlot      uint32
	Sim                 *SIM
	SimSlots            []uint32
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
		PrimarySIMSlot: m.PrimarySimSlot,
		SIM:            cloneSIM(m, m.Sim),
		SIMSlots:       slices.Clone(m.SimSlots),
		Slots:          cloneSIMSlots(m),
		Number:         m.Number,
		StatusKnown:    m.statusKnown || m.Status != (wwanmodem.Status{}),
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
