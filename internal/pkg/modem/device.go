package modem

import (
	"context"
	"errors"
	"fmt"
	"strings"

	wwan "github.com/damonto/sigmo/internal/pkg/modem/wwan"
)

const maxSIMSlot = wwan.MaxSIMSlot

// DeviceEndpoint identifies the protocol port and active SIM used for WWAN access.
type DeviceEndpoint struct {
	Port    ModemPort
	SIMSlot uint8
}

type deviceControl interface {
	AirplaneMode(ctx context.Context) (bool, error)
	SetAirplaneMode(ctx context.Context, enabled bool) error
	PowerCycleSIM(ctx context.Context) error
	ActivateProvisioningIfSIMMissing(ctx context.Context) error
	SIMState(ctx context.Context, target wwan.Target) (wwan.SIMState, error)
	MSISDN(ctx context.Context) (string, error)
	UpdateMSISDN(ctx context.Context, number string) error
}

type deviceControlOpener func(wwan.Config) (deviceControl, error)

func OpenDevice(m *Modem) (*wwan.Device, error) {
	cfg, err := deviceConfig(m)
	if err != nil {
		return nil, err
	}
	return wwan.Open(cfg)
}

func OpenVoLTESession(m *Modem) (*wwan.Session, error) {
	cfg, err := voLTEDeviceConfig(m)
	if err != nil {
		return nil, err
	}
	return wwan.OpenSession(cfg)
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

func openQMIDeviceForTarget(m *Modem, target SIMTarget, open deviceControlOpener) (deviceControl, error) {
	cfg, err := qmiDeviceConfigForTarget(m, target)
	if err != nil {
		return nil, err
	}
	return openDeviceWith(cfg, open)
}

func openDeviceForModem(m *Modem, open deviceControlOpener) (deviceControl, error) {
	cfg, err := deviceConfig(m)
	if err != nil {
		return nil, err
	}
	return openDeviceWith(cfg, open)
}

func openQMIDeviceForModem(m *Modem, open deviceControlOpener) (deviceControl, error) {
	cfg, err := qmiDeviceConfig(m)
	if err != nil {
		return nil, err
	}
	return openDeviceWith(cfg, open)
}

func openQMIDeviceForSlot(m *Modem, slot uint8, open deviceControlOpener) (deviceControl, error) {
	cfg, err := qmiDeviceConfigForSlot(m, slot)
	if err != nil {
		return nil, err
	}
	return openDeviceWith(cfg, open)
}

func openDeviceWith(cfg wwan.Config, open deviceControlOpener) (deviceControl, error) {
	if open == nil {
		return wwan.Open(cfg)
	}
	return open(cfg)
}

func readDeviceSIMState(ctx context.Context, m *Modem, target SIMTarget, open deviceControlOpener) (wwan.SIMState, error) {
	device, err := openQMIDeviceForModem(m, open)
	if errors.Is(err, wwan.ErrUnsupported) {
		return wwan.SIMState{}, nil
	}
	if err != nil {
		return wwan.SIMState{}, err
	}
	return device.SIMState(ctx, deviceTarget(target))
}

func deviceConfig(m *Modem) (wwan.Config, error) {
	endpoint, err := ResolveDeviceEndpoint(m)
	if err != nil {
		return wwan.Config{}, err
	}
	return deviceConfigForEndpoint(endpoint, m.EquipmentIdentifier)
}

func qmiDeviceConfig(m *Modem) (wwan.Config, error) {
	if m == nil {
		return wwan.Config{}, errModemRequired
	}
	slot, err := ActiveSIMSlot(m)
	if err != nil {
		return wwan.Config{}, err
	}
	return qmiDeviceConfigForSlot(m, slot)
}

func qmiDeviceConfigForTarget(m *Modem, target SIMTarget) (wwan.Config, error) {
	slot, err := deviceTargetSlot(m, target)
	if err != nil {
		return wwan.Config{}, err
	}
	return qmiDeviceConfigForSlot(m, slot)
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

	for _, want := range []ModemPortType{ModemPortTypeQmi, ModemPortTypeMbim} {
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
		if port.PortType != ModemPortTypeQmi || strings.TrimSpace(port.Device) == "" {
			continue
		}
		return port, nil
	}
	return ModemPort{}, wwan.ErrUnsupported
}

func isDevicePortType(portType ModemPortType) bool {
	return portType == ModemPortTypeQmi || portType == ModemPortTypeMbim
}

func devicePortType(portType ModemPortType) (wwan.PortType, bool) {
	switch portType {
	case ModemPortTypeQmi:
		return wwan.PortTypeQMI, true
	case ModemPortTypeMbim:
		return wwan.PortTypeMBIM, true
	default:
		return 0, false
	}
}

// ActiveSIMSlot returns the selected SIM slot, defaulting ModemManager's zero
// value to the first slot.
func ActiveSIMSlot(m *Modem) (uint8, error) {
	if m == nil {
		return 0, errModemRequired
	}
	if m.PrimarySimSlot == 0 {
		return 1, nil
	}
	if m.PrimarySimSlot > maxSIMSlot {
		return 0, fmt.Errorf("sim slot %d is out of range", m.PrimarySimSlot)
	}
	return uint8(m.PrimarySimSlot), nil
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

func deviceTarget(target SIMTarget) wwan.Target {
	return wwan.Target{
		Slot:  target.Slot,
		ICCID: strings.TrimSpace(target.ICCID),
	}
}
