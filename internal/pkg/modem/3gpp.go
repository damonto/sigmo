package modem

import (
	"context"
	"strings"

	wwanmodem "github.com/damonto/wwan-go/modem"
)

type ThreeGPP struct{ modem *Modem }

func (m *Modem) ThreeGPP() *ThreeGPP { return &ThreeGPP{modem: m} }

type ThreeGPPNetwork struct {
	Status            Modem3GPPNetworkAvailability
	OperatorName      string
	OperatorShortName string
	OperatorCode      string
	AccessTechnology  []ModemAccessTechnology
}

func (g *ThreeGPP) IMEI(context.Context) (string, error) {
	if g == nil || g.modem == nil {
		return "", errModemRequired
	}
	return g.modem.EquipmentIdentifier, nil
}

func (g *ThreeGPP) RegistrationState(ctx context.Context) (Modem3GPPRegistrationState, error) {
	status, err := g.modem.core.NetworkStatus(ctx)
	if err != nil {
		return Modem3GPPRegistrationStateUnknown, err
	}
	return legacyRegistration(status.Registration), nil
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

func (g *ThreeGPP) ScanNetworks(ctx context.Context) ([]*ThreeGPPNetwork, error) {
	operators, err := g.modem.core.ScanNetworks(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]*ThreeGPPNetwork, 0, len(operators))
	for _, operator := range operators {
		availability := Modem3GPPNetworkAvailabilityUnknown
		switch {
		case operator.Current:
			availability = Modem3GPPNetworkAvailabilityCurrent
		case operator.Forbidden:
			availability = Modem3GPPNetworkAvailabilityForbidden
		case operator.Available:
			availability = Modem3GPPNetworkAvailabilityAvailable
		}
		result = append(result, &ThreeGPPNetwork{
			Status: availability, OperatorName: operator.Name, OperatorShortName: operator.Name,
			OperatorCode: operator.ID, AccessTechnology: accessTechnologies(operator.Technology),
		})
	}
	return result, nil
}

func (g *ThreeGPP) RegisterNetwork(ctx context.Context, operatorCode string) error {
	return g.modem.core.Register(ctx, wwanmodem.RegisterConfig{OperatorID: strings.TrimSpace(operatorCode)})
}

func legacyRegistration(state wwanmodem.RegistrationState) Modem3GPPRegistrationState {
	switch state {
	case wwanmodem.RegistrationHome:
		return Modem3GPPRegistrationStateHome
	case wwanmodem.RegistrationSearching:
		return Modem3GPPRegistrationStateSearching
	case wwanmodem.RegistrationDenied:
		return Modem3GPPRegistrationStateDenied
	case wwanmodem.RegistrationRoaming:
		return Modem3GPPRegistrationStateRoaming
	case wwanmodem.RegistrationIdle:
		return Modem3GPPRegistrationStateIdle
	default:
		return Modem3GPPRegistrationStateUnknown
	}
}
