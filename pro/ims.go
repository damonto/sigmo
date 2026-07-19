//go:build ims

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/modemstatus"
	"github.com/damonto/sigmo/internal/app/router"
	pmessage "github.com/damonto/sigmo/internal/pkg/message"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/storage"
	pussd "github.com/damonto/sigmo/internal/pkg/ussd"
	procall "github.com/damonto/sigmo/pro/call"
	pims "github.com/damonto/sigmo/pro/ims"
)

var proIMS = func(app *proApp) error {
	runtime := app.runtime
	app.RegisterWebsheets()
	wifiCalling := pims.New(pims.Config{
		Store:  runtime.Storage,
		Access: pims.AccessWiFiCalling,
		OnIncoming: func(ctx context.Context, incoming pims.IncomingSMS) error {
			return runtime.Relay.ForwardRoutedSMS(ctx, incoming.ModemID, incoming.Message)
		},
		Websheets: app.Websheets(),
	})
	volte := pims.New(pims.Config{
		Store:    runtime.Storage,
		Access:   pims.AccessVoLTE,
		Internet: runtime.Internet,
		OnIncoming: func(ctx context.Context, incoming pims.IncomingSMS) error {
			return runtime.Relay.ForwardRoutedSMS(ctx, incoming.ModemID, incoming.Message)
		},
	})
	calls := procall.New(runtime.Storage, wifiCalling, procall.VoiceRoute{
		Route: procall.RouteVoLTE,
		Voice: volte,
	})
	media, err := procall.NewMedia(context.Background(), calls)
	if err != nil {
		return fmt.Errorf("configure call media: %w", err)
	}

	runtime.AddFeatures(pims.WiFiCallingFeatureName, pims.VoLTEFeatureName)
	runtime.AddMCPTools(registerIMSMCP(runtime.Registry, wifiCalling, volte, calls))
	runtime.SetMessageRoute(messageRoute{wifiCalling: wifiCalling, volte: volte})
	runtime.SetUSSDRoute(ussdRoute{wifiCalling: wifiCalling, volte: volte})
	runtime.AddModemOverview(wifiCallingOverview(wifiCalling.Status))
	runtime.AddRunner(func(ctx context.Context) error {
		return wifiCalling.Run(ctx, runtime.Registry)
	})
	runtime.AddRunner(func(ctx context.Context) error {
		return volte.Run(ctx, runtime.Registry)
	})
	runtime.AddRunner(calls.Run)
	runtime.AddRunner(media.Run)
	runtime.AddRunner(func(ctx context.Context) error {
		return forwardCalls(ctx, runtime.Relay, calls)
	})
	runtime.AddRoute(func(group *echo.Group, deps router.RegisterConfig) error {
		pims.RegisterRoutes(group, deps.Registry, wifiCalling, volte)
		procall.RegisterRoutes(group, deps.Registry, calls, media)
		return nil
	})
	return nil
}

type wifiCallingStatusFunc func(context.Context, *mmodem.Modem) (pims.Status, error)

func wifiCallingOverview(readStatus wifiCallingStatusFunc) modemstatus.Extension {
	return func(ctx context.Context, modem *mmodem.Modem, fields *modemstatus.Fields) error {
		status, err := readStatus(ctx, modem)
		if errors.Is(err, pims.ErrUnavailable) || errors.Is(err, mmodem.ErrProfileIDMissing) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("fetch Wi-Fi Calling status: %w", err)
		}
		fields.WiFiCallingEnabled = status.Enabled
		fields.WiFiCallingPreferred = status.Preferred
		fields.WiFiCallingConnected = status.Connected
		return nil
	}
}

type messageRoute struct {
	wifiCalling pims.Coordinator
	volte       pims.Coordinator
}

func (r messageRoute) Status(ctx context.Context, modem *mmodem.Modem) (pmessage.RouteStatus, error) {
	route, err := r.selectRoute(ctx, modem)
	if errors.Is(err, pims.ErrUnavailable) {
		return pmessage.RouteStatus{}, pmessage.ErrRouteUnavailable
	}
	if err != nil {
		return pmessage.RouteStatus{}, err
	}
	return pmessage.RouteStatus{
		Preferred: route != nil,
		Connected: route != nil,
	}, nil
}

func (r messageRoute) SendSMS(ctx context.Context, modem *mmodem.Modem, to string, text string) (storage.Message, error) {
	route, err := r.selectRoute(ctx, modem)
	if err != nil {
		return storage.Message{}, err
	}
	if route == nil {
		return storage.Message{}, pmessage.ErrRouteNotConnected
	}
	msg, err := route.SendSMS(ctx, modem, to, text)
	if errors.Is(err, pims.ErrNotConnected) {
		return storage.Message{}, pmessage.ErrRouteNotConnected
	}
	return msg, err
}

func (r messageRoute) ApplyPendingSMSStatus(ctx context.Context, msg storage.Message) error {
	err := r.wifiCalling.ApplyPendingSMSStatus(ctx, msg)
	if r.volte == nil {
		return err
	}
	return errors.Join(err, r.volte.ApplyPendingSMSStatus(ctx, msg))
}

type ussdRoute struct {
	wifiCalling pims.Coordinator
	volte       pims.Coordinator
}

func (r ussdRoute) Status(ctx context.Context, modem *mmodem.Modem) (pussd.RouteStatus, error) {
	route, err := messageRoute{wifiCalling: r.wifiCalling, volte: r.volte}.selectRoute(ctx, modem)
	if errors.Is(err, pims.ErrUnavailable) {
		return pussd.RouteStatus{}, pussd.ErrRouteUnavailable
	}
	if err != nil {
		return pussd.RouteStatus{}, err
	}
	return pussd.RouteStatus{
		Preferred: route != nil,
		Connected: route != nil,
	}, nil
}

func (r ussdRoute) ExecuteUSSD(ctx context.Context, modem *mmodem.Modem, action string, code string) (string, error) {
	route, err := messageRoute{wifiCalling: r.wifiCalling, volte: r.volte}.selectRoute(ctx, modem)
	if err != nil {
		return "", err
	}
	if route == nil {
		return "", pussd.ErrRouteUnavailable
	}
	return route.ExecuteUSSD(ctx, modem, action, code)
}

func (r messageRoute) selectRoute(ctx context.Context, modem *mmodem.Modem) (pims.Coordinator, error) {
	wifiCallingStatus, wifiCallingOK, err := routeStatus(ctx, r.wifiCalling, modem)
	if err != nil {
		return nil, err
	}
	volteStatus, volteOK, err := routeStatus(ctx, r.volte, modem)
	if err != nil {
		return nil, err
	}
	if wifiCallingOK && wifiCallingStatus.Connected && wifiCallingStatus.Preferred {
		return r.wifiCalling, nil
	}
	if volteOK && volteStatus.Connected {
		return r.volte, nil
	}
	if wifiCallingOK && wifiCallingStatus.Connected {
		return r.wifiCalling, nil
	}
	return nil, nil
}

func routeStatus(ctx context.Context, coordinator pims.Coordinator, modem *mmodem.Modem) (pims.Status, bool, error) {
	if coordinator == nil {
		return pims.Status{}, false, nil
	}
	status, err := coordinator.Status(ctx, modem)
	if errors.Is(err, pims.ErrUnavailable) || errors.Is(err, mmodem.ErrProfileIDMissing) {
		return pims.Status{}, false, nil
	}
	if err != nil {
		return pims.Status{}, false, err
	}
	return status, true, nil
}

func forwardCalls(ctx context.Context, relay interface {
	ForwardCall(context.Context, storage.Call) error
}, calls *procall.Calls) error {
	events, unsubscribe := calls.Subscribe(16)
	defer unsubscribe()

	for {
		select {
		case <-ctx.Done():
			return nil
		case event := <-events:
			if err := relay.ForwardCall(ctx, event.Call); err != nil {
				slog.Warn("forward call notification", "call_id", event.Call.ID, "imei", event.Call.ModemID, "error", err)
			}
		}
	}
}
