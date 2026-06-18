//go:build wifi_calling

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
	"github.com/damonto/sigmo/pro/wificalling"
)

var proWiFiCalling = func(app *proApp) error {
	runtime := app.runtime
	app.RegisterWebsheets()
	wifiCalling := wificalling.New(wificalling.Config{
		Store: runtime.Storage,
		OnIncoming: func(ctx context.Context, incoming wificalling.IncomingSMS) error {
			return runtime.Relay.ForwardRoutedSMS(ctx, incoming.ModemID, incoming.Message)
		},
		Websheets: app.Websheets(),
	})
	calls := procall.New(runtime.Storage, wifiCalling)
	media := procall.NewMedia(calls)

	runtime.AddFeatures(wificalling.FeatureName)
	runtime.SetMessageRoute(messageRoute{wifiCalling: wifiCalling})
	runtime.SetUSSDRoute(ussdRoute{wifiCalling: wifiCalling})
	runtime.AddModemOverview(wifiCallingOverview(wifiCalling.Status))
	runtime.AddRunner(func(ctx context.Context) error {
		return wifiCalling.Run(ctx, runtime.Registry)
	})
	runtime.AddRunner(calls.Run)
	runtime.AddRunner(media.Run)
	runtime.AddRunner(func(ctx context.Context) error {
		return forwardCalls(ctx, runtime.Relay, calls)
	})
	runtime.AddRoute(func(group *echo.Group, deps router.RegisterConfig) error {
		wificalling.RegisterRoutes(group, deps.Registry, wifiCalling)
		procall.RegisterRoutes(group, deps.Registry, calls, media)
		return nil
	})
	return nil
}

type wifiCallingStatusFunc func(context.Context, *mmodem.Modem) (wificalling.Status, error)

func wifiCallingOverview(readStatus wifiCallingStatusFunc) modemstatus.Extension {
	return func(ctx context.Context, modem *mmodem.Modem, fields *modemstatus.Fields) error {
		status, err := readStatus(ctx, modem)
		if errors.Is(err, wificalling.ErrUnavailable) || errors.Is(err, mmodem.ErrProfileIDMissing) {
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
	wifiCalling wificalling.Coordinator
}

func (r messageRoute) Status(ctx context.Context, modem *mmodem.Modem) (pmessage.RouteStatus, error) {
	status, err := r.wifiCalling.Status(ctx, modem)
	if errors.Is(err, wificalling.ErrUnavailable) {
		return pmessage.RouteStatus{}, pmessage.ErrRouteUnavailable
	}
	if err != nil {
		return pmessage.RouteStatus{}, err
	}
	return pmessage.RouteStatus{
		Preferred: status.Preferred,
		Connected: status.Connected,
	}, nil
}

func (r messageRoute) SendSMS(ctx context.Context, modem *mmodem.Modem, to string, text string) (storage.Message, error) {
	msg, err := r.wifiCalling.SendSMS(ctx, modem, to, text)
	if errors.Is(err, wificalling.ErrNotConnected) {
		return storage.Message{}, pmessage.ErrRouteNotConnected
	}
	return msg, err
}

func (r messageRoute) ApplyPendingSMSStatus(ctx context.Context, msg storage.Message) error {
	return r.wifiCalling.ApplyPendingSMSStatus(ctx, msg)
}

type ussdRoute struct {
	wifiCalling wificalling.Coordinator
}

func (r ussdRoute) Status(ctx context.Context, modem *mmodem.Modem) (pussd.RouteStatus, error) {
	status, err := r.wifiCalling.Status(ctx, modem)
	if errors.Is(err, wificalling.ErrUnavailable) {
		return pussd.RouteStatus{}, pussd.ErrRouteUnavailable
	}
	if err != nil {
		return pussd.RouteStatus{}, err
	}
	return pussd.RouteStatus{
		Preferred: status.Preferred,
		Connected: status.Connected,
	}, nil
}

func (r ussdRoute) ExecuteUSSD(ctx context.Context, modem *mmodem.Modem, action string, code string) (string, error) {
	return r.wifiCalling.ExecuteUSSD(ctx, modem, action, code)
}

func forwardCalls(ctx context.Context, relay interface {
	ForwardCall(context.Context, storage.Call) error
}, calls *procall.Service) error {
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
