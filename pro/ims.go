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
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/storage"
	procall "github.com/damonto/sigmo/pro/call"
	pims "github.com/damonto/sigmo/pro/ims"
)

var proIMS = func(app *proApp) error {
	runtime := app.runtime
	app.RegisterWebsheets()
	connectivity := pims.NewConnectivity(pims.ConnectivityConfig{
		Store:              runtime.Storage,
		Registry:           runtime.Registry,
		Internet:           runtime.InternetConnector,
		NetworkPreferences: runtime.NetworkPreferences,
		OnIncoming: func(ctx context.Context, incoming pims.IncomingSMS) error {
			return runtime.Relay.ForwardRoutedSMS(ctx, incoming.ModemID, incoming.Message)
		},
		Websheets: app.Websheets(),
	})
	runtime.SetInternetConnections(connectivity)
	runtime.SetAirplaneModeLifecycle(connectivity)
	calls := procall.New(runtime.Storage, connectivity.WiFiCalling(), procall.VoiceRoute{
		Route: procall.RouteVoLTE,
		Voice: connectivity.VoLTE(),
	})
	media, err := procall.NewMedia(context.Background(), calls)
	if err != nil {
		return fmt.Errorf("configure call media: %w", err)
	}

	runtime.AddFeatures(pims.WiFiCallingFeatureName, pims.VoLTEFeatureName)
	runtime.AddMCPTools(registerIMSMCP(runtime.Registry, connectivity, calls))
	runtime.SetMessageRoute(connectivity.MessageRoute())
	runtime.SetUSSDRoute(connectivity.USSDRoute())
	runtime.AddModemOverview(wifiCallingOverview(connectivity.WiFiCallingStatus))
	runtime.AddRunner(connectivity.Run)
	runtime.AddRunner(calls.Run)
	runtime.AddCleanup(media.Shutdown)
	runtime.AddRunner(func(ctx context.Context) error {
		return forwardCalls(ctx, runtime.Relay, calls)
	})
	runtime.AddRoute(func(group *echo.Group, deps router.RegisterConfig) error {
		pims.RegisterRoutes(group, deps.Registry, connectivity)
		procall.RegisterRoutes(group, deps.Registry, calls, media)
		return nil
	})
	return nil
}

type wifiCallingStatusFunc func(context.Context, *mmodem.Modem) (pims.WiFiCallingStatus, error)

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
		fields.WiFiCallingConnected = status.Connected
		return nil
	}
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
