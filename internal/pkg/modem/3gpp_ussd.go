package modem

import "context"

type USSD struct {
	modem *Modem
}

func (g *ThreeGPP) USSD() *USSD {
	return &USSD{modem: g.modem}
}

func (u *USSD) Initiate(ctx context.Context, command string) (string, error) {
	var reply string
	err := u.modem.dbusObject.CallWithContext(ctx, Modem3GPPInterface+".Ussd.Initiate", 0, command).Store(&reply)
	return reply, err
}

func (u *USSD) Respond(ctx context.Context, response string) (string, error) {
	var reply string
	err := u.modem.dbusObject.CallWithContext(ctx, Modem3GPPInterface+".Ussd.Respond", 0, response).Store(&reply)
	return reply, err
}

func (u *USSD) Cancel() error {
	return u.modem.dbusObject.Call(Modem3GPPInterface+".Ussd.Cancel", 0).Err
}

func (u *USSD) State() (Modem3gppUssdSessionState, error) {
	variant, err := u.modem.dbusObject.GetProperty(Modem3GPPInterface + ".Ussd.State")
	if err != nil {
		return 0, err
	}
	return Modem3gppUssdSessionState(uintFromVariant[uint32](variant)), nil
}

func (u *USSD) NetworkRequest() (string, error) {
	variant, err := u.modem.dbusObject.GetProperty(Modem3GPPInterface + ".Ussd.NetworkRequest")
	if err != nil {
		return "", err
	}
	return stringFromVariant(variant), nil
}
