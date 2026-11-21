package ims

import (
	"context"
	"errors"

	pmessage "github.com/damonto/sigmo/internal/pkg/message"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/storage"
	pussd "github.com/damonto/sigmo/internal/pkg/ussd"
)

type routedAccess interface {
	connected(context.Context, *mmodem.Modem) (bool, error)
	SendSMS(context.Context, *mmodem.Modem, string, string) (storage.Message, error)
	ApplyPendingSMSStatus(context.Context, storage.Message) error
	ExecuteUSSD(context.Context, *mmodem.Modem, string, string) (string, error)
}

func (c *Connectivity) MessageRoute() pmessage.Route {
	return connectivityMessageRoute{connectivity: c}
}

// SendSMSVia submits through one IMS access without consulting route preference.
func (c *Connectivity) SendSMSVia(ctx context.Context, modem *mmodem.Modem, access Access, to, text string) (storage.Message, error) {
	route := c.accessCoordinator(access)
	if route == nil {
		return storage.Message{}, pmessage.ErrRouteUnavailable
	}
	return sendSMSOnAccess(ctx, route, modem, to, text)
}

// AutomationAccessStatus reports saved enablement and the current SIM's session.
func (c *Connectivity) AutomationAccessStatus(ctx context.Context, modem *mmodem.Modem, access Access) (enabled, connected bool, err error) {
	route := c.accessCoordinator(access)
	if route == nil {
		return false, false, ErrUnavailable
	}
	if access == AccessWiFiCalling {
		settings, err := route.WiFiCallingSettings(ctx, modem)
		if err != nil {
			return false, false, err
		}
		enabled = settings.Enabled
	} else {
		// A modem without a supported VoLTE port can still have a saved setting.
		settings, err := route.volteStore().Get(ctx, modem.EquipmentIdentifier)
		if err != nil {
			return false, false, err
		}
		enabled = settings.Enabled
	}
	status, err := route.SessionStatus(ctx, modem, enabled)
	return enabled, status.Connected, err
}

func (c *Connectivity) accessCoordinator(access Access) *coordinator {
	if c != nil {
		switch access {
		case AccessWiFiCalling:
			return c.wifiCalling
		case AccessVoLTE:
			return c.volte
		}
	}
	return nil
}

func (c *Connectivity) USSDRoute() pussd.Route {
	return connectivityUSSDRoute{connectivity: c}
}

type connectivityMessageRoute struct {
	connectivity *Connectivity
}

func (r connectivityMessageRoute) Status(ctx context.Context, modem *mmodem.Modem) (pmessage.RouteStatus, error) {
	route, err := r.connectivity.preferredAccess(ctx, modem)
	if errors.Is(err, ErrUnavailable) {
		return pmessage.RouteStatus{}, pmessage.ErrRouteUnavailable
	}
	if err != nil {
		return pmessage.RouteStatus{}, err
	}
	return pmessage.RouteStatus{Preferred: route != nil, Connected: route != nil}, nil
}

func (r connectivityMessageRoute) SendSMS(ctx context.Context, modem *mmodem.Modem, to, text string) (storage.Message, error) {
	route, err := r.connectivity.preferredAccess(ctx, modem)
	if err != nil {
		return storage.Message{}, err
	}
	if route == nil {
		return storage.Message{}, pmessage.ErrRouteNotConnected
	}
	return sendSMSOnAccess(ctx, route, modem, to, text)
}

func sendSMSOnAccess(ctx context.Context, route routedAccess, modem *mmodem.Modem, to, text string) (storage.Message, error) {
	msg, err := route.SendSMS(ctx, modem, to, text)
	if errors.Is(err, ErrNotConnected) {
		return storage.Message{}, pmessage.ErrRouteNotConnected
	}
	return msg, err
}

func (r connectivityMessageRoute) ApplyPendingSMSStatus(ctx context.Context, msg storage.Message) error {
	if r.connectivity == nil || r.connectivity.wifiCalling == nil || r.connectivity.volte == nil {
		return ErrUnavailable
	}
	return errors.Join(
		r.connectivity.wifiCalling.ApplyPendingSMSStatus(ctx, msg),
		r.connectivity.volte.ApplyPendingSMSStatus(ctx, msg),
	)
}

type connectivityUSSDRoute struct {
	connectivity *Connectivity
}

func (r connectivityUSSDRoute) Status(ctx context.Context, modem *mmodem.Modem) (pussd.RouteStatus, error) {
	route, err := r.connectivity.preferredAccess(ctx, modem)
	if errors.Is(err, ErrUnavailable) {
		return pussd.RouteStatus{}, pussd.ErrRouteUnavailable
	}
	if err != nil {
		return pussd.RouteStatus{}, err
	}
	return pussd.RouteStatus{Preferred: route != nil, Connected: route != nil}, nil
}

func (r connectivityUSSDRoute) ExecuteUSSD(ctx context.Context, modem *mmodem.Modem, action, code string) (string, error) {
	route, err := r.connectivity.preferredAccess(ctx, modem)
	if err != nil {
		return "", err
	}
	if route == nil {
		return "", pussd.ErrRouteUnavailable
	}
	return route.ExecuteUSSD(ctx, modem, action, code)
}

func (c *Connectivity) preferredAccess(ctx context.Context, modem *mmodem.Modem) (routedAccess, error) {
	if c == nil || c.wifiCalling == nil || c.volte == nil {
		return nil, ErrUnavailable
	}
	return selectPreferredAccess(ctx, modem, c.wifiCalling, c.volte)
}

func selectPreferredAccess(ctx context.Context, modem *mmodem.Modem, wifiCalling, volte routedAccess) (routedAccess, error) {
	wifiCallingConnected, err := routedAccessStatus(ctx, wifiCalling, modem)
	if err != nil {
		return nil, err
	}
	if wifiCallingConnected {
		return wifiCalling, nil
	}
	volteConnected, err := routedAccessStatus(ctx, volte, modem)
	if err != nil {
		return nil, err
	}
	if volteConnected {
		return volte, nil
	}
	return nil, nil
}

func routedAccessStatus(ctx context.Context, access routedAccess, modem *mmodem.Modem) (bool, error) {
	connected, err := access.connected(ctx, modem)
	if errors.Is(err, ErrUnavailable) || errors.Is(err, mmodem.ErrProfileIDMissing) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return connected, nil
}

func (c *coordinator) connected(ctx context.Context, modem *mmodem.Modem) (bool, error) {
	var enabled bool
	switch c.access {
	case AccessWiFiCalling:
		settings, err := c.WiFiCallingSettings(ctx, modem)
		if err != nil {
			return false, err
		}
		enabled = settings.Enabled
	case AccessVoLTE:
		settings, err := c.VoLTESettings(ctx, modem)
		if err != nil {
			return false, err
		}
		enabled = settings.Enabled
	default:
		return false, ErrUnavailable
	}
	status, err := c.SessionStatus(ctx, modem, enabled)
	if err != nil {
		return false, err
	}
	return status.Connected, nil
}
