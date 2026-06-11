package ussd

import (
	"context"
	"errors"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

type Service struct {
	session *session
	route   Route
}

type RouteStatus struct {
	Preferred bool
	Connected bool
}

type Route interface {
	Status(context.Context, *mmodem.Modem) (RouteStatus, error)
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

var ErrRouteUnavailable = errors.New("ussd route is unavailable")

func New(route Route) *Service {
	return &Service{
		session: newSession(),
		route:   route,
	}
}

func (s *Service) Execute(ctx context.Context, modem *mmodem.Modem, action string, code string) (string, error) {
	return s.execute(ctx, realModemDevice{modemRef: modem, session: s.session}, action, code)
}

func (s *Service) execute(ctx context.Context, device modemDevice, action string, code string) (string, error) {
	status, err := s.routeStatus(ctx, device.modem())
	if err != nil {
		return "", err
	}
	if status.Preferred && status.Connected {
		return s.route.ExecuteUSSD(ctx, device.modem(), action, code)
	}
	reply, err := device.executeUSSD(ctx, action, code)
	if err == nil {
		return reply, nil
	}
	if status.Connected && s.route != nil {
		reply, routeErr := s.route.ExecuteUSSD(ctx, device.modem(), action, code)
		if routeErr == nil {
			return reply, nil
		}
	}
	return "", err
}

func (s *Service) routeStatus(ctx context.Context, modem *mmodem.Modem) (RouteStatus, error) {
	if s.route == nil {
		return RouteStatus{}, nil
	}
	status, err := s.route.Status(ctx, modem)
	if err != nil && !errors.Is(err, ErrRouteUnavailable) {
		return RouteStatus{}, err
	}
	return status, nil
}

func (d realModemDevice) modem() *mmodem.Modem {
	return d.modemRef
}

func (d realModemDevice) executeUSSD(ctx context.Context, action string, code string) (string, error) {
	return d.session.Execute(ctx, d.modemRef, action, code)
}
