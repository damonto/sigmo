package modem

import (
	"github.com/godbus/dbus/v5"
)

const Modem3GPPInterface = ModemInterface + ".Modem3gpp"

type ThreeGPP struct {
	modem *Modem
}

func (m *Modem) ThreeGPP() *ThreeGPP {
	return &ThreeGPP{modem: m}
}

type ThreeGPPNetwork struct {
	Status            Modem3gppNetworkAvailability
	OperatorName      string
	OperatorShortName string
	OperatorCode      string
	AccessTechnology  []ModemAccessTechnology
}

func (g *ThreeGPP) IMEI() (string, error) {
	variant, err := g.modem.dbusObject.GetProperty(Modem3GPPInterface + ".Imei")
	if err != nil {
		return "", err
	}
	return stringFromVariant(variant), nil
}

func (g *ThreeGPP) RegistrationState() (Modem3gppRegistrationState, error) {
	variant, err := g.modem.dbusObject.GetProperty(Modem3GPPInterface + ".RegistrationState")
	if err != nil {
		return 0, err
	}
	return Modem3gppRegistrationState(uintFromVariant[uint32](variant)), nil
}

func (g *ThreeGPP) OperatorCode() (string, error) {
	variant, err := g.modem.dbusObject.GetProperty(Modem3GPPInterface + ".OperatorCode")
	if err != nil {
		return "", err
	}
	return stringFromVariant(variant), nil
}

func (g *ThreeGPP) OperatorName() (string, error) {
	variant, err := g.modem.dbusObject.GetProperty(Modem3GPPInterface + ".OperatorName")
	if err != nil {
		return "", err
	}
	return stringFromVariant(variant), nil
}

func (g *ThreeGPP) ScanNetworks() ([]*ThreeGPPNetwork, error) {
	var results []map[string]dbus.Variant
	err := g.modem.dbusObject.Call(Modem3GPPInterface+".Scan", 0).Store(&results)
	if err != nil {
		return nil, err
	}
	networks := make([]*ThreeGPPNetwork, len(results))
	for i, result := range results {
		var accessTechnology ModemAccessTechnology
		n := ThreeGPPNetwork{
			Status:           Modem3gppNetworkAvailability(variantUint[uint32](result, "status")),
			OperatorCode:     variantString(result, "operator-code"),
			AccessTechnology: accessTechnology.UnmarshalBitmask(variantUint[uint32](result, "access-technology")),
		}
		n.OperatorName = variantString(result, "operator-long")
		n.OperatorShortName = variantString(result, "operator-short")
		networks[i] = &n
	}
	return networks, nil
}

func (g *ThreeGPP) RegisterNetwork(operatorCode string) error {
	return g.modem.dbusObject.Call(Modem3GPPInterface+".Register", 0, operatorCode).Err
}
