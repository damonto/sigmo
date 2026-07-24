//go:build ims

package main

import (
	"context"
	"errors"
	"testing"

	"github.com/damonto/sigmo/internal/app/modemstatus"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	pims "github.com/damonto/sigmo/pro/ims"
)

func TestWiFiCallingOverview(t *testing.T) {
	errStatus := errors.New("status read")
	tests := []struct {
		name              string
		status            pims.Status
		err               error
		wantWiFiEnabled   bool
		wantWiFiConnected bool
		wantErr           error
	}{
		{
			name: "fills connected status",
			status: pims.Status{
				Settings: pims.Settings{
					Enabled: true,
				},
				Connected: true,
			},
			wantWiFiEnabled:   true,
			wantWiFiConnected: true,
		},
		{
			name: "ignores unavailable route",
			err:  pims.ErrUnavailable,
		},
		{
			name: "ignores missing profile id",
			err:  mmodem.ErrProfileIDMissing,
		},
		{
			name:    "wraps status error",
			err:     errStatus,
			wantErr: errStatus,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			extension := wifiCallingOverview(func(ctx context.Context, modem *mmodem.Modem) (pims.Status, error) {
				return tt.status, tt.err
			})
			fields := &modemstatus.Fields{}

			err := extension(context.Background(), &mmodem.Modem{}, fields)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("wifiCallingOverview() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("wifiCallingOverview() error = %v", err)
			}
			if fields.WiFiCallingEnabled != tt.wantWiFiEnabled {
				t.Fatalf("WiFiCallingEnabled = %v, want %v", fields.WiFiCallingEnabled, tt.wantWiFiEnabled)
			}
			if fields.WiFiCallingConnected != tt.wantWiFiConnected {
				t.Fatalf("WiFiCallingConnected = %v, want %v", fields.WiFiCallingConnected, tt.wantWiFiConnected)
			}
		})
	}
}

type routeCoordinator struct {
	pims.Coordinator
	status      pims.Status
	err         error
	statusCalls int
}

func (c *routeCoordinator) Status(context.Context, *mmodem.Modem) (pims.Status, error) {
	c.statusCalls++
	return c.status, c.err
}

func TestMessageRoutePrefersWiFiCalling(t *testing.T) {
	tests := []struct {
		name           string
		wifiConnected  bool
		volteConnected bool
		wantWiFi       bool
		wantVoLTE      bool
	}{
		{name: "wifi calling wins when both are connected", wifiConnected: true, volteConnected: true, wantWiFi: true},
		{name: "volte is the fallback", volteConnected: true, wantVoLTE: true},
		{name: "no connected route"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wifiCalling := &routeCoordinator{status: pims.Status{Connected: tt.wifiConnected}}
			volte := &routeCoordinator{status: pims.Status{Connected: tt.volteConnected}}
			got, err := (messageRoute{wifiCalling: wifiCalling, volte: volte}).selectRoute(context.Background(), &mmodem.Modem{})
			if err != nil {
				t.Fatalf("selectRoute() error = %v", err)
			}
			switch {
			case tt.wantWiFi && got != wifiCalling:
				t.Fatalf("selectRoute() = %T, want Wi-Fi Calling", got)
			case tt.wantVoLTE && got != volte:
				t.Fatalf("selectRoute() = %T, want VoLTE", got)
			case !tt.wantWiFi && !tt.wantVoLTE && got != nil:
				t.Fatalf("selectRoute() = %T, want nil", got)
			}
		})
	}
}

func TestMessageRouteSkipsVoLTEWhenWiFiCallingConnected(t *testing.T) {
	statusErr := errors.New("VoLTE status")
	wifiCalling := &routeCoordinator{status: pims.Status{Connected: true}}
	volte := &routeCoordinator{err: statusErr}

	got, err := (messageRoute{wifiCalling: wifiCalling, volte: volte}).selectRoute(context.Background(), &mmodem.Modem{})
	if err != nil {
		t.Fatalf("selectRoute() error = %v", err)
	}
	if got != wifiCalling {
		t.Fatalf("selectRoute() = %T, want Wi-Fi Calling", got)
	}
	if volte.statusCalls != 0 {
		t.Fatalf("VoLTE Status() calls = %d, want 0", volte.statusCalls)
	}
}
