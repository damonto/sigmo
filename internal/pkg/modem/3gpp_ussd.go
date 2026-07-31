package modem

import (
	"context"

	wwanmodem "github.com/damonto/wwan-go/modem"
)

type USSD struct{ modem *Modem }

func (g *ThreeGPP) USSD() *USSD { return &USSD{modem: g.modem} }

func (u *USSD) Initiate(ctx context.Context, command string) (string, error) {
	message, err := u.modem.core.InitiateUSSD(ctx, command)
	if err != nil {
		return "", err
	}
	u.modem.setUSSD(message)
	return message.Text, nil
}

func (u *USSD) Respond(ctx context.Context, response string) (string, error) {
	message, err := u.modem.core.RespondUSSD(ctx, response)
	if err != nil {
		return "", err
	}
	u.modem.setUSSD(message)
	return message.Text, nil
}

func (u *USSD) Cancel(ctx context.Context) error {
	if err := u.modem.core.CancelUSSD(ctx); err != nil {
		return err
	}
	u.modem.setUSSD(wwanmodem.USSDMessage{State: wwanmodem.USSDStateIdle})
	return nil
}

func (u *USSD) State(context.Context) (Modem3GPPUSSDSessionState, error) {
	return legacyUSSDState(u.modem.currentUSSD().State), nil
}

func (u *USSD) NetworkRequest(context.Context) (string, error) {
	return u.modem.currentUSSD().Text, nil
}

func legacyUSSDState(state wwanmodem.USSDState) Modem3GPPUSSDSessionState {
	switch state {
	case wwanmodem.USSDStateIdle, wwanmodem.USSDStateTerminated:
		return Modem3GPPUSSDSessionStateIdle
	case wwanmodem.USSDStateUserResponse:
		return Modem3GPPUSSDSessionStateUserResponse
	case wwanmodem.USSDStateActive, wwanmodem.USSDStateNetworkResponse:
		return Modem3GPPUSSDSessionStateActive
	default:
		return Modem3GPPUSSDSessionStateUnknown
	}
}
