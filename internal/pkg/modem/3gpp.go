package modem

import (
	"context"

	"github.com/godbus/dbus/v5"
)

// ModemManager spells the DBus interface as "Modem3gpp". Keep Go names as 3GPP,
// but never leak that Go casing into DBus calls.
const Modem3GPPInterface = ModemInterface + ".Modem3gpp"

type ThreeGPP struct {
	modem *Modem
}

func (m *Modem) ThreeGPP() *ThreeGPP {
	return &ThreeGPP{modem: m}
}

type ThreeGPPNetwork struct {
	Status            Modem3GPPNetworkAvailability
	OperatorName      string
	OperatorShortName string
	OperatorCode      string
	AccessTechnology  []ModemAccessTechnology
}

func (g *ThreeGPP) IMEI(ctx context.Context) (string, error) {
	variant, err := dbusProperty(ctx, g.modem.dbusObject, Modem3GPPInterface, "Imei")
	if err != nil {
		return "", err
	}
	return stringFromVariant(variant), nil
}

func (g *ThreeGPP) RegistrationState(ctx context.Context) (Modem3GPPRegistrationState, error) {
	variant, err := dbusProperty(ctx, g.modem.dbusObject, Modem3GPPInterface, "RegistrationState")
	if err != nil {
		return 0, err
	}
	return Modem3GPPRegistrationState(uintFromVariant[uint32](variant)), nil
}

func (g *ThreeGPP) OperatorCode(ctx context.Context) (string, error) {
	variant, err := dbusProperty(ctx, g.modem.dbusObject, Modem3GPPInterface, "OperatorCode")
	if err != nil {
		return "", err
	}
	return stringFromVariant(variant), nil
}

func (g *ThreeGPP) OperatorName(ctx context.Context) (string, error) {
	variant, err := dbusProperty(ctx, g.modem.dbusObject, Modem3GPPInterface, "OperatorName")
	if err != nil {
		return "", err
	}
	return stringFromVariant(variant), nil
}

func (g *ThreeGPP) ScanNetworks(ctx context.Context) ([]*ThreeGPPNetwork, error) {
	var results []map[string]dbus.Variant
	err := g.modem.dbusObject.CallWithContext(ctx, Modem3GPPInterface+".Scan", 0).Store(&results)
	if err != nil {
		return nil, err
	}
	networks := make([]*ThreeGPPNetwork, len(results))
	for i, result := range results {
		var accessTechnology ModemAccessTechnology
		n := ThreeGPPNetwork{
			Status:           Modem3GPPNetworkAvailability(variantUint[uint32](result, "status")),
			OperatorCode:     variantString(result, "operator-code"),
			AccessTechnology: accessTechnology.UnmarshalBitmask(variantUint[uint32](result, "access-technology")),
		}
		n.OperatorName = variantString(result, "operator-long")
		n.OperatorShortName = variantString(result, "operator-short")
		networks[i] = &n
	}
	return networks, nil
}

func (g *ThreeGPP) RegisterNetwork(ctx context.Context, operatorCode string) error {
	return g.modem.dbusObject.CallWithContext(ctx, Modem3GPPInterface+".Register", 0, operatorCode).Err
}
