package modem

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	wwan "github.com/damonto/sigmo/internal/pkg/modem/wwan"
	wwanmodem "github.com/damonto/wwan-go/modem"
	usimcard "github.com/damonto/wwan-go/sim/card"
)

const maxSIMSlot = wwan.MaxSIMSlot

// DeviceEndpoint identifies the protocol port and active SIM used for WWAN access.
type DeviceEndpoint struct {
	Port    ModemPort
	SIMSlot uint8
}

type deviceControl interface {
	PowerCycleSIM(ctx context.Context) error
	ActivateProvisioningIfSIMMissing(ctx context.Context) error
	SIMState(ctx context.Context, target wwan.Target) (wwan.SIMState, error)
	MSISDN(ctx context.Context) (string, error)
	UpdateMSISDN(ctx context.Context, number string) error
}

// Device exposes modem operations without transferring ownership of the
// generation-scoped protocol session. Readers returned by USIM methods own
// their independent protocol clients and must still be closed by callers.
type Device interface {
	deviceControl
	USIM(ctx context.Context) (usimcard.Reader, error)
	USIMWithCAT(ctx context.Context, profile wwan.CATProfile) (usimcard.Reader, error)
	WatchSIMRefresh(ctx context.Context) (<-chan wwan.SIMRefreshEvent, error)
	VoLTEStatus(ctx context.Context) (wwan.VoLTEStatus, error)
	PacketServiceStatus(ctx context.Context) (wwan.PacketServiceStatus, error)
	IMSProfile(ctx context.Context) (wwan.IMSProfile, error)
	IMSSTestMode(ctx context.Context) (bool, error)
	SetIMSSTestMode(ctx context.Context, enabled bool) error
}

type deviceControlOpener func(wwan.Config) (deviceControl, error)

type deviceSessionStore struct {
	mu       sync.Mutex
	closed   bool
	sessions map[wwan.Config]*wwan.Session
}

func (s *deviceSessionStore) open(m *Modem, cfg wwan.Config) (Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil, wwanmodem.ErrClosed
	}
	if cfg.PortType != wwan.PortTypeQMI {
		return wwan.Open(cfg)
	}
	if session := s.sessions[cfg]; session != nil {
		return session, nil
	}
	session, err := openDeviceSession(m, cfg)
	if err != nil {
		return nil, err
	}
	if s.sessions == nil {
		s.sessions = make(map[wwan.Config]*wwan.Session)
	}
	s.sessions[cfg] = session
	return session, nil
}

func openDeviceSession(m *Modem, cfg wwan.Config) (*wwan.Session, error) {
	if m == nil || m.core == nil || m.core.Protocol() != wwanmodem.ProtocolQMI {
		return wwan.OpenSession(cfg)
	}
	if strings.TrimSpace(m.core.Port().Path) != strings.TrimSpace(cfg.Device) {
		return wwan.OpenSession(cfg)
	}
	return wwan.OpenQMISession(cfg, m.core)
}

func (s *deviceSessionStore) close() error {
	s.mu.Lock()
	s.closed = true
	sessions := make([]*wwan.Session, 0, len(s.sessions))
	for _, session := range s.sessions {
		sessions = append(sessions, session)
	}
	s.sessions = nil
	s.mu.Unlock()

	var result error
	for _, session := range sessions {
		result = errors.Join(result, session.Close())
	}
	return result
}

// OpenDevice returns a non-owning device facade. QMI control clients are
// reused until the modem generation closes.
func OpenDevice(m *Modem) (Device, error) {
	cfg, err := deviceConfig(m)
	if err != nil {
		return nil, err
	}
	return m.deviceSessions.open(m, cfg)
}

// OpenVoLTEDevice returns the generation-scoped device selected for managed
// VoLTE control. QMI clients are shared with other device operations using the
// same control port and SIM slot.
func OpenVoLTEDevice(m *Modem) (Device, error) {
	cfg, err := voLTEDeviceConfig(m)
	if err != nil {
		return nil, err
	}
	return m.deviceSessions.open(m, cfg)
}

func voLTEDeviceConfig(m *Modem) (wwan.Config, error) {
	endpoint, err := ResolveVoLTEEndpoint(m)
	if err != nil {
		return wwan.Config{}, err
	}
	portType, ok := devicePortType(endpoint.Port.PortType)
	if !ok {
		return wwan.Config{}, wwan.ErrUnsupported
	}
	return wwan.Config{
		PortType: portType,
		Device:   endpoint.Port.Device,
		Slot:     endpoint.SIMSlot,
		IMEI:     m.EquipmentIdentifier,
	}, nil
}

func openQMIDeviceForSlot(m *Modem, slot uint8, open deviceControlOpener) (deviceControl, error) {
	cfg, err := qmiDeviceConfigForSlot(m, slot)
	if err != nil {
		return nil, err
	}
	return openDeviceWith(m, cfg, open)
}

func openDeviceForSlot(m *Modem, slot uint8, open deviceControlOpener) (deviceControl, error) {
	cfg, err := deviceConfigForSlot(m, slot)
	if err != nil {
		return nil, err
	}
	return openDeviceWith(m, cfg, open)
}

func openDeviceWith(m *Modem, cfg wwan.Config, open deviceControlOpener) (deviceControl, error) {
	if open == nil {
		return m.deviceSessions.open(m, cfg)
	}
	return open(cfg)
}

func deviceConfig(m *Modem) (wwan.Config, error) {
	endpoint, err := ResolveDeviceEndpoint(m)
	if err != nil {
		return wwan.Config{}, err
	}
	return deviceConfigForEndpoint(endpoint, m.EquipmentIdentifier)
}

func deviceConfigForSlot(m *Modem, slot uint8) (wwan.Config, error) {
	if m == nil {
		return wwan.Config{}, errModemRequired
	}
	if slot == 0 || slot > maxSIMSlot {
		return wwan.Config{}, fmt.Errorf("sim slot %d is out of range", slot)
	}
	port, err := selectDevicePort(m)
	if err != nil {
		return wwan.Config{}, err
	}
	return deviceConfigForEndpoint(DeviceEndpoint{Port: port, SIMSlot: slot}, m.EquipmentIdentifier)
}

func qmiDeviceConfigForSlot(m *Modem, slot uint8) (wwan.Config, error) {
	if m == nil {
		return wwan.Config{}, errModemRequired
	}
	port, err := selectQMIDevicePort(m)
	if err != nil {
		return wwan.Config{}, err
	}
	return wwan.Config{
		PortType: wwan.PortTypeQMI,
		Device:   port.Device,
		Slot:     slot,
		IMEI:     m.EquipmentIdentifier,
	}, nil
}

func deviceConfigForEndpoint(endpoint DeviceEndpoint, imei string) (wwan.Config, error) {
	portType, ok := devicePortType(endpoint.Port.PortType)
	if !ok {
		return wwan.Config{}, wwan.ErrUnsupported
	}
	return wwan.Config{
		PortType: portType,
		Device:   endpoint.Port.Device,
		Slot:     endpoint.SIMSlot,
		IMEI:     imei,
	}, nil
}

// ResolveDeviceEndpoint selects the primary QMI or MBIM port, then falls back
// to QMI and MBIM in that order.
func ResolveDeviceEndpoint(m *Modem) (DeviceEndpoint, error) {
	if m == nil {
		return DeviceEndpoint{}, errModemRequired
	}
	slot, err := ActiveSIMSlot(m)
	if err != nil {
		return DeviceEndpoint{}, err
	}
	port, err := selectDevicePort(m)
	if err != nil {
		return DeviceEndpoint{}, err
	}
	return DeviceEndpoint{Port: port, SIMSlot: slot}, nil
}

// ResolveVoLTEPort prefers QMI because modem IMS takeover requires QMI
// services, then falls back to the regular MBIM device port.
func ResolveVoLTEPort(m *Modem) (ModemPort, error) {
	if m == nil {
		return ModemPort{}, errModemRequired
	}
	port, err := selectQMIDevicePort(m)
	if errors.Is(err, wwan.ErrUnsupported) {
		port, err = selectDevicePort(m)
	}
	return port, err
}

// ResolveVoLTEEndpoint prefers QMI because modem IMS takeover requires QMI
// services, then falls back to the regular MBIM device endpoint.
func ResolveVoLTEEndpoint(m *Modem) (DeviceEndpoint, error) {
	if m == nil {
		return DeviceEndpoint{}, errModemRequired
	}
	slot, err := ActiveSIMSlot(m)
	if err != nil {
		return DeviceEndpoint{}, err
	}
	port, err := ResolveVoLTEPort(m)
	if err != nil {
		return DeviceEndpoint{}, err
	}
	return DeviceEndpoint{Port: port, SIMSlot: slot}, nil
}

func selectDevicePort(m *Modem) (ModemPort, error) {
	primaryPort := strings.TrimSpace(m.PrimaryPort)
	if primaryPort != "" {
		for _, port := range m.Ports {
			if port.Device == primaryPort && isDevicePortType(port.PortType) {
				return port, nil
			}
		}
	}

	for _, want := range []wwanmodem.PortType{wwanmodem.PortQMI, wwanmodem.PortMBIM} {
		for _, port := range m.Ports {
			if port.PortType != want || strings.TrimSpace(port.Device) == "" {
				continue
			}
			return port, nil
		}
	}
	return ModemPort{}, wwan.ErrUnsupported
}

func selectQMIDevicePort(m *Modem) (ModemPort, error) {
	for _, port := range m.Ports {
		if port.PortType != wwanmodem.PortQMI || strings.TrimSpace(port.Device) == "" {
			continue
		}
		return port, nil
	}
	return ModemPort{}, wwan.ErrUnsupported
}

func isDevicePortType(portType wwanmodem.PortType) bool {
	return portType == wwanmodem.PortQMI || portType == wwanmodem.PortMBIM
}

func devicePortType(portType wwanmodem.PortType) (wwan.PortType, bool) {
	switch portType {
	case wwanmodem.PortQMI:
		return wwan.PortTypeQMI, true
	case wwanmodem.PortMBIM:
		return wwan.PortTypeMBIM, true
	default:
		return 0, false
	}
}

// ActiveSIMSlot returns the selected SIM slot, defaulting an unknown slot to
// the first slot.
func ActiveSIMSlot(m *Modem) (uint8, error) {
	if m == nil {
		return 0, errModemRequired
	}
	slot := m.Snapshot().PrimarySIMSlot
	if slot == 0 {
		return 1, nil
	}
	if slot > maxSIMSlot {
		return 0, fmt.Errorf("sim slot %d is out of range", slot)
	}
	return uint8(slot), nil
}

func deviceTargetSlot(m *Modem, target SIMTarget) (uint8, error) {
	if m == nil {
		return 0, errModemRequired
	}
	slot, err := ActiveSIMSlot(m)
	if err != nil {
		return 0, err
	}
	if target.Slot != 0 {
		if target.Slot > maxSIMSlot {
			return 0, fmt.Errorf("sim slot %d is out of range", target.Slot)
		}
		return uint8(target.Slot), nil
	}
	return slot, nil
}
