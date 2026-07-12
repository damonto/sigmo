package modem

import (
	"context"
	"errors"
	"fmt"
	"strings"

	wwan "github.com/damonto/sigmo/internal/pkg/modem/wwan"
)

const maxSIMSlot = wwan.MaxSIMSlot

type devicePort struct {
	portType wwan.PortType
	device   string
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

func OpenVoLTEStatusDevice(m *Modem) (*wwan.Device, error) {
	cfg, err := voLTEDeviceConfig(m)
	if err != nil {
		return nil, err
	}
	return wwan.Open(cfg)
}

func voLTEDeviceConfig(m *Modem) (wwan.Config, error) {
	if m == nil {
		return wwan.Config{}, errModemRequired
	}
	slot, err := deviceSlot(m)
	if err != nil {
		return wwan.Config{}, err
	}
	port, err := selectQMIDevicePort(m)
	if errors.Is(err, wwan.ErrUnsupported) {
		port, err = selectDevicePort(m)
	}
	if err != nil {
		return wwan.Config{}, err
	}
	return wwan.Config{
		PortType:        port.portType,
		Device:          port.device,
		Slot:            slot,
		IMEI:            m.EquipmentIdentifier,
		ReuseQMIClients: port.portType == wwan.PortTypeQMI,
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
	if m == nil {
		return wwan.Config{}, errModemRequired
	}
	slot, err := deviceSlot(m)
	if err != nil {
		return wwan.Config{}, err
	}
	return deviceConfigForSlot(m, slot)
}

func qmiDeviceConfig(m *Modem) (wwan.Config, error) {
	if m == nil {
		return wwan.Config{}, errModemRequired
	}
	slot, err := deviceSlot(m)
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
		PortType: port.portType,
		Device:   port.device,
		Slot:     slot,
		IMEI:     m.EquipmentIdentifier,
	}, nil
}

func deviceConfigForSlot(m *Modem, slot uint8) (wwan.Config, error) {
	if m == nil {
		return wwan.Config{}, errModemRequired
	}
	port, err := selectDevicePort(m)
	if err != nil {
		return wwan.Config{}, err
	}
	return wwan.Config{
		PortType: port.portType,
		Device:   port.device,
		Slot:     slot,
		IMEI:     m.EquipmentIdentifier,
	}, nil
}

func selectDevicePort(m *Modem) (devicePort, error) {
	primaryPort := strings.TrimSpace(m.PrimaryPort)
	if primaryPort != "" {
		for _, port := range m.Ports {
			portType, ok := devicePortType(port.PortType)
			if port.Device == primaryPort && ok {
				return devicePort{portType: portType, device: port.Device}, nil
			}
		}
	}

	for _, want := range []ModemPortType{ModemPortTypeQmi, ModemPortTypeMbim} {
		for _, port := range m.Ports {
			if port.PortType != want || strings.TrimSpace(port.Device) == "" {
				continue
			}
			portType, ok := devicePortType(port.PortType)
			if !ok {
				continue
			}
			return devicePort{portType: portType, device: port.Device}, nil
		}
	}
	return devicePort{}, wwan.ErrUnsupported
}

func selectQMIDevicePort(m *Modem) (devicePort, error) {
	for _, port := range m.Ports {
		if port.PortType != ModemPortTypeQmi || strings.TrimSpace(port.Device) == "" {
			continue
		}
		return devicePort{portType: wwan.PortTypeQMI, device: port.Device}, nil
	}
	return devicePort{}, wwan.ErrUnsupported
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

func deviceSlot(m *Modem) (uint8, error) {
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
	slot, err := deviceSlot(m)
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
