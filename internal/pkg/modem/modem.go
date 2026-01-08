package modem

import (
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"time"

	"github.com/godbus/dbus/v5"
)

const ModemInterface = ModemManagerInterface + ".Modem"

type Modem struct {
	mmgr                *Manager
	objectPath          dbus.ObjectPath
	dbusObject          dbus.BusObject
	Device              string
	Manufacturer        string
	EquipmentIdentifier string
	Driver              string
	Model               string
	FirmwareRevision    string
	HardwareRevision    string
	Number              string
	PrimaryPort         string
	Ports               []ModemPort
	SimSlots            []dbus.ObjectPath
	PrimarySimSlot      uint32
	Sim                 *SIM
	State               ModemState
}

type ModemPort struct {
	PortType ModemPortType
	Device   string
}

func (m *Modem) Enable() error {
	return m.dbusObject.Call(ModemInterface+".Enable", 0, true).Err
}

func (m *Modem) Disable() error {
	return m.dbusObject.Call(ModemInterface+".Enable", 0, false).Err
}

func (m *Modem) SetPrimarySimSlot(slot uint32) error {
	return m.dbusObject.Call(ModemInterface+".SetPrimarySimSlot", 0, slot).Err
}

func (m *Modem) AccessTechnologies() ([]ModemAccessTechnology, error) {
	variant, err := m.dbusObject.GetProperty(ModemInterface + ".AccessTechnologies")
	if err != nil {
		return nil, err
	}
	bitmask := variant.Value().(uint32)
	return ModemAccessTechnology(bitmask).UnmarshalBitmask(bitmask), nil
}

func (m *Modem) SignalQuality() (percent uint32, recent bool, err error) {
	variant, err := m.dbusObject.GetProperty(ModemInterface + ".SignalQuality")
	if err != nil {
		return 0, false, err
	}
	values := variant.Value().([]any)
	return values[0].(uint32), values[1].(bool), nil
}

func (m *Modem) Restart(compatible bool) error {
	var err error
	if m.PrimaryPortType() == ModemPortTypeQmi {
		err = errors.Join(err, qmicliRepowerSimCard(m))
		// Wait for the SIM card to be ready.
		time.Sleep(200 * time.Millisecond)
	}

	// Some legacy modems require the modem to be disabled and enabled to take effect.
	if e := m.dbusObject.Call(ModemInterface+".Simple.GetStatus", 0).Err; e == nil {
		slog.Info("try to disable and enable modem", "modem", m.EquipmentIdentifier)
		err = errors.Join(err, m.Disable(), m.Enable())
	}

	// Inhibiting the device will cause the ModemManager to reload the device.
	// This workaround is needed for some modems that don't properly reload.
	if compatible {
		slog.Info("try to inhibit and uninhibit modem", "modem", m.EquipmentIdentifier, "compatible", compatible)
		time.Sleep(200 * time.Millisecond)
		if e := m.dbusObject.Call(ModemInterface+".Simple.GetStatus", 0).Err; e == nil {
			err = errors.Join(err, m.mmgr.InhibitDevice(m.Device, true), m.mmgr.InhibitDevice(m.Device, false))
		}
		return err
	}
	return err
}

func qmicliRepowerSimCard(m *Modem) error {
	bin, err := exec.LookPath("qmicli")
	if err != nil {
		slog.Error("qmicli not found in PATH", "error", err)
		return err
	}
	// If multiple SIM slots aren't supported, this property will report value 0.
	// On QMI based modems the SIM slot is 1 based.
	var slot uint32 = 1
	if m.PrimarySimSlot > 0 {
		slot = m.PrimarySimSlot
	}
	if result, err := exec.Command(
		bin,
		"-d", m.PrimaryPort,
		"-p",
		fmt.Sprintf("--uim-sim-power-off=%d", slot),
	).Output(); err != nil {
		slog.Error("failed to power off sim", "error", err, "result", string(result))
		return err
	}
	slog.Info("sim powered off", "modem", m.EquipmentIdentifier, "slot", slot)
	if result, err := exec.Command(
		bin,
		"-d", m.PrimaryPort,
		"-p",
		fmt.Sprintf("--uim-sim-power-on=%d", slot),
	).Output(); err != nil {
		slog.Error("failed to power on sim", "error", err, "result", string(result))
		return err
	}
	slog.Info("sim powered on", "modem", m.EquipmentIdentifier, "slot", slot)
	return nil
}

func (m *Modem) PrimaryPortType() ModemPortType {
	for _, port := range m.Ports {
		if port.Device == m.PrimaryPort {
			return port.PortType
		}
	}
	return ModemPortTypeUnknown
}

func (m *Modem) Port(portType ModemPortType) (*ModemPort, error) {
	for i := range m.Ports {
		port := &m.Ports[i]
		if port.PortType == portType {
			return port, nil
		}
	}
	return nil, errors.New("port not found")
}
