package modem

import (
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

func (s *SIMs) Primary() (*SIM, error) {
	if s.modem.Sim == nil {
		return nil, errors.New("primary SIM not available")
	}
	return s.Get(s.modem.Sim.Path)
}

func (sims *SIMs) Get(path dbus.ObjectPath) (*SIM, error) {
	var variant dbus.Variant
	var err error
	sim := &SIM{Path: path}
	dbusObject, err := systemBusObject(path)
	if err != nil {
		return nil, err
	}

	variant, err = dbusObject.GetProperty(ModemSimInterface + ".Active")
	if err != nil {
		return nil, err
	}
	sim.Active = boolFromVariant(variant)

	variant, err = dbusObject.GetProperty(ModemSimInterface + ".SimIdentifier")
	if err != nil {
		return nil, err
	}
	sim.Identifier = stringFromVariant(variant)

	variant, err = dbusObject.GetProperty(ModemSimInterface + ".Eid")
	if err != nil {
		return nil, err
	}
	sim.Eid = stringFromVariant(variant)

	variant, err = dbusObject.GetProperty(ModemSimInterface + ".Imsi")
	if err != nil {
		return nil, err
	}
	sim.Imsi = stringFromVariant(variant)

	variant, err = dbusObject.GetProperty(ModemSimInterface + ".OperatorIdentifier")
	if err != nil {
		return nil, err
	}
	sim.OperatorIdentifier = stringFromVariant(variant)

	variant, err = dbusObject.GetProperty(ModemSimInterface + ".OperatorName")
	if err != nil {
		return nil, err
	}
	sim.OperatorName = stringFromVariant(variant)
	return sim, nil
}
