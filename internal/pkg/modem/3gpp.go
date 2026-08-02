package modem

import (
	"context"
	"strings"

	wwanmodem "github.com/damonto/wwan-go/modem"
)

type ThreeGPP struct{ modem *Modem }

func (m *Modem) ThreeGPP() *ThreeGPP { return &ThreeGPP{modem: m} }

func (g *ThreeGPP) IMEI(context.Context) (string, error) {
	if g == nil || g.modem == nil {
		return "", errModemRequired
	}
	return g.modem.EquipmentIdentifier, nil
}

func (g *ThreeGPP) RegistrationState(ctx context.Context) (wwanmodem.RegistrationState, error) {
	status, err := g.modem.core.NetworkStatus(ctx)
	if err != nil {
		return wwanmodem.RegistrationUnknown, err
	}
	return status.Registration, nil
}

func (g *ThreeGPP) OperatorCode(ctx context.Context) (string, error) {
	status, err := g.modem.core.NetworkStatus(ctx)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(status.OperatorID), nil
}

func (g *ThreeGPP) OperatorName(ctx context.Context) (string, error) {
	status, err := g.modem.core.NetworkStatus(ctx)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(status.OperatorName), nil
}

func (g *ThreeGPP) ScanNetworks(ctx context.Context) ([]wwanmodem.Operator, error) {
	return g.modem.core.ScanNetworks(ctx)
}

func (g *ThreeGPP) RegisterNetwork(ctx context.Context, operatorCode string) error {
	return g.modem.core.Register(ctx, wwanmodem.RegisterConfig{OperatorID: strings.TrimSpace(operatorCode)})
}
