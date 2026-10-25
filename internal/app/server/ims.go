package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/labstack/echo/v5"

	appcall "github.com/damonto/sigmo/internal/app/call"
	"github.com/damonto/sigmo/internal/app/feature"
	pims "github.com/damonto/sigmo/internal/app/ims"
	appmiddleware "github.com/damonto/sigmo/internal/app/middleware"
	"github.com/damonto/sigmo/internal/app/modemstatus"
	"github.com/damonto/sigmo/internal/app/router"
	"github.com/damonto/sigmo/internal/app/websheet"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

func configureIMS(ctx context.Context, runtime *Runtime, websheets *websheet.Broker) error {
	connectivity := pims.NewConnectivity(pims.ConnectivityConfig{
		Store:              runtime.Storage,
		CarrierConfig:      runtime.License.CarrierConfig,
		Registry:           runtime.Registry,
		Internet:           runtime.InternetConnector,
		NetworkPreferences: runtime.NetworkPreferences,
		OnIncoming: func(ctx context.Context, incoming pims.IncomingSMS) error {
			return runtime.Relay.ForwardRoutedSMS(ctx, incoming.ModemID, incoming.Message)
		},
		Websheets: websheets,
	})
	runtime.SetInternetConnections(connectivity)
	runtime.SetAirplaneModeLifecycle(connectivity)
	if err := runtime.Storage.EndInterruptedCalls(ctx); err != nil {
		return fmt.Errorf("reconcile interrupted calls: %w", err)
	}
	calls := appcall.New(runtime.Storage, connectivity.WiFiCalling(), appcall.VoiceRoute{
		Route: appcall.RouteVoLTE,
		Voice: connectivity.VoLTE(),
	})
	media, err := appcall.NewMedia(ctx, calls)
	if err != nil {
		return fmt.Errorf("configure call media: %w", err)
	}

	runtime.AddFeatures(feature.WiFiCalling, feature.VoLTE, feature.NativeCalls, feature.CallHistory)
	runtime.AddMCPTools(registerIMSMCP(runtime.Registry, connectivity, calls))
	runtime.SetMessageRoute(connectivity.MessageRoute())
	runtime.SetUSSDRoute(connectivity.USSDRoute())
	runtime.AddModemOverview(wifiCallingOverview(connectivity.WiFiCallingStatus))
	runtime.AddModemOverview(voLTEOverview(connectivity.VoLTEStatus))
	runtime.AddRunner("IMS connectivity", connectivity.Run)
	runtime.AddRunner("calls", calls.Run)
	runtime.AddCleanup(media.Shutdown)
	runtime.AddRunner("call forwarding", func(ctx context.Context) error {
		return forwardCalls(ctx, runtime.Relay, calls)
	})
	runtime.AddRoute(func(group *echo.Group, deps router.RegisterConfig) error {
		pims.RegisterRoutes(group.Group("", appmiddleware.RequireFeature(runtime.Policy, feature.WiFiCalling)), deps.Registry, connectivity)
		appcall.RegisterRoutes(group, deps.Registry, calls, media)
		return nil
	})
	return nil
}

type wifiCallingStatusFunc func(context.Context, *mmodem.Modem) (pims.WiFiCallingStatus, error)

type callForwarder interface {
	ForwardCall(context.Context, storage.Call) error
}

type callEventSource interface {
	Subscribe(appcall.SubscriptionConfig) (<-chan appcall.Event, func())
}

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

type voLTEStatusFunc func(context.Context, *mmodem.Modem) (pims.VoLTEStatus, error)

func voLTEOverview(readStatus voLTEStatusFunc) modemstatus.Extension {
	return func(ctx context.Context, modem *mmodem.Modem, fields *modemstatus.Fields) error {
		status, err := readStatus(ctx, modem)
		if errors.Is(err, pims.ErrUnavailable) || errors.Is(err, mmodem.ErrProfileIDMissing) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("fetch VoLTE status: %w", err)
		}
		fields.VoLTEEnabled = status.Enabled
		fields.VoLTEConnected = status.Connected
		return nil
	}
}

func forwardCalls(ctx context.Context, relay callForwarder, calls callEventSource) error {
	events, unsubscribe := calls.Subscribe(appcall.SubscriptionConfig{Kind: appcall.EventKindCall, Buffer: 16})
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
