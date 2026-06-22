package modem

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/damonto/sigmo/internal/app/modemstatus"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/settings"
)

func TestCatalogBuildResponseLockedModem(t *testing.T) {
	tests := []struct {
		name            string
		lock            mmodem.ModemLock
		wantSupported   bool
		wantUnlockLabel string
	}{
		{
			name:            "supports sim pin unlock",
			lock:            mmodem.ModemLockSimPin,
			wantSupported:   true,
			wantUnlockLabel: "sim-pin",
		},
		{
			name:            "reports unsupported puk lock",
			lock:            mmodem.ModemLockSimPuk,
			wantUnlockLabel: "sim-puk",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			catalog := newCatalog(settings.NewMemoryStore(settings.Default()), nil)
			device := &mmodem.Modem{
				EquipmentIdentifier: "860588043408833",
				Manufacturer:        "Quectel",
				Model:               "RM520N",
				State:               mmodem.ModemStateLocked,
				UnlockRequired:      tt.lock,
			}

			got, err := catalog.buildResponse(context.Background(), device)
			if err != nil {
				t.Fatalf("buildResponse() error = %v", err)
			}
			if got.State != "locked" {
				t.Fatalf("state = %q, want locked", got.State)
			}
			if got.UnlockRequired != tt.wantUnlockLabel {
				t.Fatalf("unlockRequired = %q, want %q", got.UnlockRequired, tt.wantUnlockLabel)
			}
			if got.UnlockSupported != tt.wantSupported {
				t.Fatalf("unlockSupported = %v, want %v", got.UnlockSupported, tt.wantSupported)
			}
		})
	}
}

func TestCatalogApplyOverviewExtensions(t *testing.T) {
	errStatus := errors.New("status source")
	tests := []struct {
		name              string
		extensions        []modemstatus.Extension
		wantWiFiEnabled   bool
		wantWiFiPreferred bool
		wantWiFiConnected bool
		wantErr           error
	}{
		{
			name: "fills wifi calling fields",
			extensions: []modemstatus.Extension{
				func(ctx context.Context, modem *mmodem.Modem, fields *modemstatus.Fields) error {
					fields.WiFiCallingEnabled = true
					fields.WiFiCallingPreferred = true
					fields.WiFiCallingConnected = true
					return nil
				},
			},
			wantWiFiEnabled:   true,
			wantWiFiPreferred: true,
			wantWiFiConnected: true,
		},
		{
			name: "skips nil extension",
			extensions: []modemstatus.Extension{
				nil,
			},
		},
		{
			name: "wraps extension error",
			extensions: []modemstatus.Extension{
				func(ctx context.Context, modem *mmodem.Modem, fields *modemstatus.Fields) error {
					return errStatus
				},
			},
			wantErr: errStatus,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			catalog := newCatalog(settings.NewMemoryStore(settings.Default()), nil, tt.extensions...)
			resp := &ModemResponse{}

			err := catalog.applyOverviewExtensions(context.Background(), &mmodem.Modem{}, resp)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("applyOverviewExtensions() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("applyOverviewExtensions() error = %v", err)
			}
			if resp.WiFiCallingEnabled != tt.wantWiFiEnabled {
				t.Fatalf("WiFiCallingEnabled = %v, want %v", resp.WiFiCallingEnabled, tt.wantWiFiEnabled)
			}
			if resp.WiFiCallingPreferred != tt.wantWiFiPreferred {
				t.Fatalf("WiFiCallingPreferred = %v, want %v", resp.WiFiCallingPreferred, tt.wantWiFiPreferred)
			}
			if resp.WiFiCallingConnected != tt.wantWiFiConnected {
				t.Fatalf("WiFiCallingConnected = %v, want %v", resp.WiFiCallingConnected, tt.wantWiFiConnected)
			}
		})
	}
}

func TestSupportsEsimUsesModemMetadata(t *testing.T) {
	tests := []struct {
		name  string
		modem *mmodem.Modem
		want  bool
	}{
		{
			name: "unsupported AT modem",
			modem: &mmodem.Modem{
				EquipmentIdentifier: "imei-1",
				PrimaryPort:         "/dev/ttyUSB2",
				Ports: []mmodem.ModemPort{
					{PortType: mmodem.ModemPortTypeAt, Device: "/dev/ttyUSB2"},
				},
			},
		},
		{
			name: "unknown primary port",
			modem: &mmodem.Modem{
				EquipmentIdentifier: "imei-2",
				PrimaryPort:         "/dev/ttyUSB2",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			currentSettings := settings.Default()
			got, err := supportsEsim(context.Background(), tt.modem, currentSettings)
			if err != nil {
				t.Fatalf("supportsEsim() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("supportsEsim() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestModemResponseJSONIncludesOverviewFields(t *testing.T) {
	tests := []struct {
		name string
		resp ModemResponse
		want string
	}{
		{
			name: "wifi calling connected",
			resp: ModemResponse{
				Fields: modemstatus.Fields{
					WiFiCallingEnabled:   true,
					WiFiCallingPreferred: true,
					WiFiCallingConnected: true,
				},
			},
			want: `"wifiCallingConnected":true`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.resp)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			if !strings.Contains(string(got), tt.want) {
				t.Fatalf("Marshal() = %s, want field %s", got, tt.want)
			}
		})
	}
}
