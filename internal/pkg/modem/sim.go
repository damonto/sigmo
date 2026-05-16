package modem

import (
	"context"
	"errors"

	"github.com/godbus/dbus/v5"
)

const ModemSimInterface = ModemManagerInterface + ".Sim"

type SIMs struct {
	modem *Modem
}

func (m *Modem) SIMs() *SIMs {
	return &SIMs{modem: m}
}

type SIM struct {
	Path               dbus.ObjectPath
	Active             bool
	Identifier         string
	Eid                string
	Imsi               string
	OperatorIdentifier string
	OperatorName       string
}

func (s *SIMs) Primary(ctx context.Context) (*SIM, error) {
	if s.modem.Sim == nil {
		return nil, errors.New("primary SIM not available")
	}
	return s.Get(ctx, s.modem.Sim.Path)
}

func (sims *SIMs) Get(ctx context.Context, path dbus.ObjectPath) (*SIM, error) {
	if path == "" || path == "/" {
		return nil, errors.New("SIM path is required")
	}
	var variant dbus.Variant
	var err error
	sim := &SIM{Path: path}
	var dbusObject dbus.BusObject
	if sims.modem.dbusConn != nil {
		dbusObject = sims.modem.dbusConn.Object(ModemManagerInterface, path)
	} else {
		dbusObject, err = systemBusObject(path)
		if err != nil {
			return nil, err
		}
	}

	variant, err = dbusProperty(ctx, dbusObject, ModemSimInterface, "Active")
	if err != nil {
		return nil, err
	}
	sim.Active = boolFromVariant(variant)

	variant, err = dbusProperty(ctx, dbusObject, ModemSimInterface, "SimIdentifier")
	if err != nil {
		return nil, err
	}
	sim.Identifier = stringFromVariant(variant)

	variant, err = dbusProperty(ctx, dbusObject, ModemSimInterface, "Eid")
	if err != nil {
		return nil, err
	}
	sim.Eid = stringFromVariant(variant)

	variant, err = dbusProperty(ctx, dbusObject, ModemSimInterface, "Imsi")
	if err != nil {
		return nil, err
	}
	sim.Imsi = stringFromVariant(variant)

	variant, err = dbusProperty(ctx, dbusObject, ModemSimInterface, "OperatorIdentifier")
	if err != nil {
		return nil, err
	}
	sim.OperatorIdentifier = stringFromVariant(variant)

	variant, err = dbusProperty(ctx, dbusObject, ModemSimInterface, "OperatorName")
	if err != nil {
		return nil, err
	}
	sim.OperatorName = stringFromVariant(variant)
	return sim, nil
}
