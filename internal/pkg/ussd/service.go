package ussd

import (
	"context"
	"errors"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/wificalling"
)

type Service struct {
	session     *session
	wifiCalling wifiCallingUSSD
}

type wifiCallingUSSD interface {
	Status(context.Context, *mmodem.Modem) (wificalling.Status, error)
	ExecuteUSSD(context.Context, *mmodem.Modem, string, string) (string, error)
}

type modemDevice interface {
	modem() *mmodem.Modem
	executeUSSD(context.Context, string, string) (string, error)
}

type realModemDevice struct {
	modemRef *mmodem.Modem
	session  *session
}

func New(wifiCalling wifiCallingUSSD) *Service {
	return &Service{
		session:     newSession(),
		wifiCalling: wifiCalling,
	}
}

func (s *Service) Execute(ctx context.Context, modem *mmodem.Modem, action string, code string) (string, error) {
	return s.execute(ctx, realModemDevice{modemRef: modem, session: s.session}, action, code)
}

func (s *Service) execute(ctx context.Context, device modemDevice, action string, code string) (string, error) {
	status, err := s.wifiCalling.Status(ctx, device.modem())
	if err != nil && !errors.Is(err, wificalling.ErrUnavailable) {
		return "", err
	}
	if status.Preferred && status.Connected {
		return s.wifiCalling.ExecuteUSSD(ctx, device.modem(), action, code)
	}
	reply, err := device.executeUSSD(ctx, action, code)
	if err == nil {
		return reply, nil
	}
	if status.Connected {
		reply, werr := s.wifiCalling.ExecuteUSSD(ctx, device.modem(), action, code)
		if werr == nil {
			return reply, nil
		}
	}
	return "", err
}

func (d realModemDevice) modem() *mmodem.Modem {
	return d.modemRef
}

func (d realModemDevice) executeUSSD(ctx context.Context, action string, code string) (string, error) {
	return d.session.Execute(ctx, d.modemRef, action, code)
}
