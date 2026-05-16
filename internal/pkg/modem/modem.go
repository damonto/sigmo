package modem

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/godbus/dbus/v5"
)

const ModemInterface = ModemManagerInterface + ".Modem"

type Modem struct {
	dbusConn            *dbus.Conn
	inhibitDevice       func(context.Context, string, bool) error
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

type dbusModePair struct {
	Allowed   uint32
	Preferred uint32
}

func (m *Modem) Enable(ctx context.Context) error {
	return m.dbusObject.CallWithContext(ctx, ModemInterface+".Enable", 0, true).Err
}

func (m *Modem) Disable(ctx context.Context) error {
	return m.dbusObject.CallWithContext(ctx, ModemInterface+".Enable", 0, false).Err
}

func (m *Modem) SetPrimarySimSlot(ctx context.Context, slot uint32) error {
	return m.dbusObject.CallWithContext(ctx, ModemInterface+".SetPrimarySimSlot", 0, slot).Err
}

func (m *Modem) SupportedModes(ctx context.Context) ([]ModemModePair, error) {
	variant, err := dbusProperty(ctx, m.dbusObject, ModemInterface, "SupportedModes")
	if err != nil {
		return nil, err
	}
	return modePairsFromVariant(variant)
}

func (m *Modem) CurrentModes(ctx context.Context) (ModemModePair, error) {
	variant, err := dbusProperty(ctx, m.dbusObject, ModemInterface, "CurrentModes")
	if err != nil {
		return ModemModePair{}, err
	}
	return modePairFromVariant(variant)
}

func (m *Modem) SetCurrentModes(ctx context.Context, modes ModemModePair) error {
	return m.dbusObject.CallWithContext(ctx, ModemInterface+".SetCurrentModes", 0, dbusModePair{
		Allowed:   uint32(modes.Allowed),
		Preferred: uint32(modes.Preferred),
	}).Err
}

func (m *Modem) SupportedBands(ctx context.Context) ([]ModemBand, error) {
	variant, err := dbusProperty(ctx, m.dbusObject, ModemInterface, "SupportedBands")
	if err != nil {
		return nil, err
	}
	return bandsFromVariant(variant)
}

func (m *Modem) CurrentBands(ctx context.Context) ([]ModemBand, error) {
	variant, err := dbusProperty(ctx, m.dbusObject, ModemInterface, "CurrentBands")
	if err != nil {
		return nil, err
	}
	return bandsFromVariant(variant)
}

func (m *Modem) SetCurrentBands(ctx context.Context, bands []ModemBand) error {
	values := make([]uint32, 0, len(bands))
	for _, band := range bands {
		values = append(values, uint32(band))
	}
	return m.dbusObject.CallWithContext(ctx, ModemInterface+".SetCurrentBands", 0, values).Err
}

func (m *Modem) AccessTechnologies(ctx context.Context) ([]ModemAccessTechnology, error) {
	variant, err := dbusProperty(ctx, m.dbusObject, ModemInterface, "AccessTechnologies")
	if err != nil {
		return nil, err
	}
	bitmask := uintFromVariant[uint32](variant)
	return ModemAccessTechnology(bitmask).UnmarshalBitmask(bitmask), nil
}

func (m *Modem) SignalQuality(ctx context.Context) (percent uint32, recent bool, err error) {
	variant, err := dbusProperty(ctx, m.dbusObject, ModemInterface, "SignalQuality")
	if err != nil {
		return 0, false, err
	}
	values := anySliceFromVariant(variant)
	if len(values) < 2 {
		return 0, false, nil
	}
	percent, _ = values[0].(uint32)
	recent, _ = values[1].(bool)
	return percent, recent, nil
}

func (m *Modem) Restart(ctx context.Context, compatible bool) error {
	var err error
	if m.PrimaryPortType() == ModemPortTypeQmi {
		err = errors.Join(err, qmicliRepowerSimCard(ctx, m))
		// Wait for the SIM card to be ready.
		if e := sleepContext(ctx, time.Second); e != nil {
			return errors.Join(err, e)
		}
	}

	// Try to activate provisioning session if SIM is missing.
	if compatible {
		err = errors.Join(err, qmicliActivateProvisioningIfSimMissing(ctx, m))
	}

	// Some legacy modems require the modem to be disabled and enabled to take effect.
	if e := m.simpleStatus(ctx); e == nil {
		slog.Info("try to disable and enable modem", "modem", m.EquipmentIdentifier)
		err = errors.Join(err, m.togglePower(ctx))
	} else if ctx.Err() != nil {
		return errors.Join(err, ctx.Err())
	}

	// This workaround is needed for some modems that don't properly reload.
	if compatible {
		// Inhibiting the device will cause the ModemManager to reload the device.
		if e := sleepContext(ctx, time.Second); e != nil {
			return errors.Join(err, e)
		}
		if e := m.simpleStatus(ctx); e == nil {
			if m.inhibitDevice == nil {
				return errors.Join(err, errors.New("modem inhibit function is required"))
			}
			slog.Info("try to inhibit and uninhibit modem", "modem", m.EquipmentIdentifier, "compatible", compatible)
			err = errors.Join(
				err,
				m.inhibitDevice(ctx, m.Device, true),
				m.inhibitDevice(ctx, m.Device, false),
			)
		} else if ctx.Err() != nil {
			return errors.Join(err, ctx.Err())
		}
		return err
	}
	return err
}

func (m *Modem) simpleStatus(ctx context.Context) error {
	return m.dbusObject.CallWithContext(ctx, ModemInterface+".Simple.GetStatus", 0).Err
}

func (m *Modem) togglePower(ctx context.Context) error {
	if err := m.Disable(ctx); err != nil {
		if !isTransientRestartError(err) {
			return err
		}
		// Some modems disappear from DBus or cancel in-flight calls while ModemManager is replacing the object.
		slog.Info("ignoring transient restart error", "modem", m.EquipmentIdentifier, "path", m.objectPath, "action", "disabling", "error", err)
	}
	if err := m.Enable(ctx); err != nil {
		if !isTransientRestartError(err) {
			return err
		}
		// Some modems disappear from DBus or cancel in-flight calls while ModemManager is replacing the object.
		slog.Info("ignoring transient restart error", "modem", m.EquipmentIdentifier, "path", m.objectPath, "action", "enabling", "error", err)
	}
	return nil
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
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
