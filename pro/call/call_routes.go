//go:build ims

package call

import (
	"context"
	"errors"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	pims "github.com/damonto/sigmo/pro/ims"
)

type callRoutes struct {
	voices map[string]imsVoice
}

func newCallRoutes(voices []VoiceRoute) *callRoutes {
	routes := &callRoutes{voices: make(map[string]imsVoice)}
	for _, voice := range voices {
		if voice.Route == "" || voice.Voice == nil {
			continue
		}
		if _, ok := routes.voices[voice.Route]; ok {
			continue
		}
		routes.voices[voice.Route] = voice.Voice
	}
	return routes
}

func (r *callRoutes) selectRoute(ctx context.Context, modem *mmodem.Modem, requested string) (string, error) {
	switch requested {
	case RouteWiFiCalling:
		return r.requireConnected(ctx, modem, RouteWiFiCalling)
	case RouteVoLTE:
		return r.requireConnected(ctx, modem, RouteVoLTE)
	case RouteModem:
		return RouteModem, nil
	}

	wifiCalling, wifiCallingOK, err := r.routeStatus(ctx, modem, RouteWiFiCalling)
	if err != nil {
		return "", err
	}
	if wifiCallingOK && wifiCalling.Connected && wifiCalling.Preferred {
		return RouteWiFiCalling, nil
	}
	volte, volteOK, err := r.routeStatus(ctx, modem, RouteVoLTE)
	if err != nil {
		return "", err
	}
	if volteOK && volte.Connected {
		return RouteVoLTE, nil
	}
	if wifiCallingOK && wifiCalling.Connected {
		return RouteWiFiCalling, nil
	}
	return "", ErrNoRouteAvailable
}

func (r *callRoutes) requireConnected(ctx context.Context, modem *mmodem.Modem, route string) (string, error) {
	status, ok, err := r.routeStatus(ctx, modem, route)
	if err != nil {
		return "", err
	}
	if !ok || !status.Connected {
		if route == RouteVoLTE {
			return "", ErrVoLTENotConnected
		}
		return "", ErrWiFiCallingNotConnected
	}
	return route, nil
}

func (r *callRoutes) routeStatus(ctx context.Context, modem *mmodem.Modem, route string) (routeStatus, bool, error) {
	voice := r.voice(route)
	if voice == nil {
		return routeStatus{}, false, nil
	}
	got, err := voice.Status(ctx, modem)
	if err != nil {
		if errors.Is(err, pims.ErrUnavailable) || errors.Is(err, mmodem.ErrProfileIDMissing) {
			return routeStatus{}, false, nil
		}
		return routeStatus{}, false, err
	}
	return routeStatus{Connected: got.Connected, Preferred: got.Preferred}, true, nil
}

func (r *callRoutes) voice(route string) imsVoice {
	if r == nil {
		return nil
	}
	return r.voices[route]
}

type routeStatus struct {
	Preferred bool
	Connected bool
}
