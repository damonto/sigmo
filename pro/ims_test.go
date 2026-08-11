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
		status            pims.WiFiCallingStatus
		err               error
		wantWiFiEnabled   bool
		wantWiFiConnected bool
		wantErr           error
	}{
		{
			name: "fills connected status",
			status: pims.WiFiCallingStatus{
				WiFiCallingSettings: pims.WiFiCallingSettings{
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
			extension := wifiCallingOverview(func(ctx context.Context, modem *mmodem.Modem) (pims.WiFiCallingStatus, error) {
				return tt.status, tt.err
			})
			fields := &modemstatus.Fields{}

			err := extension(t.Context(), &mmodem.Modem{}, fields)
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
