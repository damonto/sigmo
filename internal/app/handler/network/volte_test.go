package network

import (
	"context"
	"errors"
	"testing"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	mdevice "github.com/damonto/sigmo/internal/pkg/modem/device"
)

type fakeVoLTEDevice struct {
	status mdevice.VoLTEStatus
	err    error
}

func (d fakeVoLTEDevice) VoLTEStatus(context.Context) (mdevice.VoLTEStatus, error) {
	return d.status, d.err
}

func TestSetVoLTE(t *testing.T) {
	tests := []struct {
		name           string
		req            SetVoLTERequest
		supported      bool
		occupied       bool
		saved          bool
		wantPreference bool
		wantErr        error
		wantManaged    bool
	}{
		{
			name:           "start managing",
			req:            SetVoLTERequest{Managed: true},
			supported:      true,
			wantPreference: true,
			wantManaged:    true,
		},
		{
			name:           "start managing occupied IMS",
			req:            SetVoLTERequest{Managed: true},
			supported:      true,
			occupied:       true,
			wantPreference: true,
			wantManaged:    true,
		},
		{
			name:    "device unavailable",
			req:     SetVoLTERequest{Managed: true},
			wantErr: errVoLTEUnavailable,
		},
		{
			name:  "stop managing clears preference",
			req:   SetVoLTERequest{Managed: false},
			saved: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			previous := openVoLTEDevice
			openVoLTEDevice = func(*mmodem.Modem) (volteDevice, error) {
				return fakeVoLTEDevice{status: mdevice.VoLTEStatus{Supported: tt.supported, Occupied: tt.occupied}}, nil
			}
			t.Cleanup(func() {
				openVoLTEDevice = previous
			})

			preferences, err := mmodem.NewNetworkPreferences(openNetworkTestStore(t))
			if err != nil {
				t.Fatalf("NewNetworkPreferences() error = %v", err)
			}
			n, err := newNetwork(preferences, openNetworkTestStore(t))
			if err != nil {
				t.Fatalf("newNetwork() error = %v", err)
			}
			modem := &mmodem.Modem{EquipmentIdentifier: "modem-1"}
			if tt.saved {
				if err := preferences.SaveVoLTE(context.Background(), modem.EquipmentIdentifier, true); err != nil {
					t.Fatalf("SaveVoLTE() error = %v", err)
				}
			}

			err = n.SetVoLTE(context.Background(), modem, tt.req)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("SetVoLTE() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("SetVoLTE() error = %v", err)
			}
			got, ok, err := preferences.SavedVoLTE(context.Background(), modem.EquipmentIdentifier)
			if err != nil {
				t.Fatalf("SavedVoLTE() error = %v", err)
			}
			if ok != tt.wantPreference || got != tt.wantManaged {
				t.Fatalf("SavedVoLTE() = %v, %v; want %v, %v", got, ok, tt.wantManaged, tt.wantPreference)
			}
		})
	}
}

func TestVoLTE(t *testing.T) {
	tests := []struct {
		name      string
		managed   bool
		supported bool
		occupied  bool
		want      VoLTEResponse
	}{
		{
			name:      "available",
			supported: true,
			want:      VoLTEResponse{CanEnable: true},
		},
		{
			name:      "managed",
			managed:   true,
			supported: true,
			want:      VoLTEResponse{Managed: true, CanEnable: true},
		},
		{
			name:      "registered modem IMS remains available",
			supported: true,
			occupied:  true,
			want:      VoLTEResponse{CanEnable: true, ModemRegistered: true},
		},
		{
			name: "unavailable",
			want: VoLTEResponse{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			previous := openVoLTEDevice
			openVoLTEDevice = func(*mmodem.Modem) (volteDevice, error) {
				return fakeVoLTEDevice{status: mdevice.VoLTEStatus{Supported: tt.supported, Occupied: tt.occupied}}, nil
			}
			t.Cleanup(func() {
				openVoLTEDevice = previous
			})

			preferences, err := mmodem.NewNetworkPreferences(openNetworkTestStore(t))
			if err != nil {
				t.Fatalf("NewNetworkPreferences() error = %v", err)
			}
			n, err := newNetwork(preferences, openNetworkTestStore(t))
			if err != nil {
				t.Fatalf("newNetwork() error = %v", err)
			}
			modem := &mmodem.Modem{EquipmentIdentifier: "modem-1"}
			if tt.managed {
				if err := preferences.SaveVoLTE(context.Background(), modem.EquipmentIdentifier, true); err != nil {
					t.Fatalf("SaveVoLTE() error = %v", err)
				}
			}

			got, err := n.VoLTE(context.Background(), modem)
			if err != nil {
				t.Fatalf("VoLTE() error = %v", err)
			}
			if *got != tt.want {
				t.Fatalf("VoLTE() = %+v, want %+v", *got, tt.want)
			}
		})
	}
}
