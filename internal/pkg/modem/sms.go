package modem

import (
	"context"
	"time"

	"github.com/godbus/dbus/v5"
)

const ModemSMSInterface = ModemManagerInterface + ".Sms"

type SMS struct {
	objectPath dbus.ObjectPath
	State      SMSState
	Number     string
	Text       string
	Timestamp  time.Time
}

func (sms *SMS) Path() dbus.ObjectPath {
	return sms.objectPath
}

func (msg *Messaging) Retrieve(ctx context.Context, objectPath dbus.ObjectPath) (*SMS, error) {
	dbusObject, err := systemBusObject(objectPath)
	if err != nil {
		return nil, err
	}
	sms := SMS{objectPath: objectPath}
	variant, err := dbusProperty(ctx, dbusObject, ModemSMSInterface, "State")
	if err != nil {
		return nil, err
	}
	sms.State = SMSState(uintFromVariant[uint32](variant))

	variant, err = dbusProperty(ctx, dbusObject, ModemSMSInterface, "Number")
	if err != nil {
		return nil, err
	}
	sms.Number = stringFromVariant(variant)

	variant, err = dbusProperty(ctx, dbusObject, ModemSMSInterface, "Text")
	if err != nil {
		return nil, err
	}
	sms.Text = stringFromVariant(variant)

	variant, err = dbusProperty(ctx, dbusObject, ModemSMSInterface, "Timestamp")
	if err != nil {
		return nil, err
	}
	if t := stringFromVariant(variant); t != "" {
		if len(t) >= 3 && (t[len(t)-3] == '+' || t[len(t)-3] == '-') {
			t = t + ":00"
		}
		sms.Timestamp, err = time.Parse(time.RFC3339, t)
		if err != nil {
			return nil, err
		}
	}
	return &sms, nil
}

func (msg *Messaging) Send(ctx context.Context, to string, text string) (*SMS, error) {
	path, err := msg.Create(ctx, to, text)
	if err != nil {
		return nil, err
	}
	dbusObject, err := systemBusObject(path)
	if err != nil {
		return nil, err
	}
	if err := dbusObject.CallWithContext(ctx, ModemSMSInterface+".Send", 0).Err; err != nil {
		return nil, err
	}
	return msg.Retrieve(ctx, path)
}
