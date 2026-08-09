package network

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	wwanmodem "github.com/damonto/wwan-go/modem"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/networkprefs"
)

type airplaneModeLifecycleProbe struct {
	calls     []string
	beforeErr error
	afterErr  error
}

func (p *airplaneModeLifecycleProbe) ChangeAirplaneMode(
	_ context.Context,
	_ *mmodem.Modem,
	enabled bool,
	change func() (bool, error),
) error {
	p.calls = append(p.calls, fmt.Sprintf("lifecycle:%t", enabled))
	if p.beforeErr != nil {
		return p.beforeErr
	}
	applied, err := change()
	p.calls = append(p.calls, fmt.Sprintf("applied:%t", applied))
	return errors.Join(err, p.afterErr)
}

func TestAirplaneModeUnsupported(t *testing.T) {
	t.Parallel()

	preferences, err := networkprefs.New(openNetworkTestStore(t))
	if err != nil {
		t.Fatalf("networkprefs.New() error = %v", err)
	}
	n, err := newNetwork(preferences, openNetworkTestStore(t), nil)
	if err != nil {
		t.Fatalf("newNetwork() error = %v", err)
	}
	modem := &mmodem.Modem{
		EquipmentIdentifier: "modem-1",
		PrimaryPort:         "ttyUSB2",
		Ports: []mmodem.ModemPort{
			{PortType: wwanmodem.PortAT, Device: "ttyUSB2"},
		},
	}

	got, err := n.AirplaneMode(context.Background(), modem)
	if err != nil {
		t.Fatalf("AirplaneMode() error = %v", err)
	}
	if got.Supported || got.Enabled {
		t.Fatalf("AirplaneMode() = %#v, want unsupported disabled response", got)
	}

	err = n.SetAirplaneMode(context.Background(), modem, SetAirplaneModeRequest{Enabled: true})
	if !errors.Is(err, wwanmodem.ErrNotSupported) {
		t.Fatalf("SetAirplaneMode() error = %v, want unsupported", err)
	}
}

func TestSetAirplaneModeCoordinatesVoLTE(t *testing.T) {
	errRadio := errors.New("set radio")
	errLifecycle := errors.New("coordinate dependent services")
	tests := []struct {
		name      string
		enabled   bool
		radioErr  error
		beforeErr error
		afterErr  error
		wantCalls []string
		wantSaved bool
		wantErr   error
	}{
		{
			name:      "coordinates disabling radio",
			enabled:   true,
			wantCalls: []string{"lifecycle:true", "radio:true", "applied:true"},
			wantSaved: true,
		},
		{
			name:      "coordinates enabling radio",
			wantCalls: []string{"lifecycle:false", "radio:false", "applied:true"},
			wantSaved: true,
		},
		{
			name:      "reports unapplied radio change",
			enabled:   true,
			radioErr:  errRadio,
			wantCalls: []string{"lifecycle:true", "radio:true", "applied:false"},
			wantErr:   errRadio,
		},
		{
			name:      "does not change radio when lifecycle setup is rejected",
			enabled:   true,
			beforeErr: errLifecycle,
			wantCalls: []string{"lifecycle:true"},
			wantErr:   errLifecycle,
		},
		{
			name:      "reports lifecycle cleanup error after radio change",
			afterErr:  errLifecycle,
			wantCalls: []string{"lifecycle:false", "radio:false", "applied:true"},
			wantSaved: true,
			wantErr:   errLifecycle,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := openNetworkTestStore(t)
			preferences, err := networkprefs.New(store)
			if err != nil {
				t.Fatalf("networkprefs.New() error = %v", err)
			}
			lifecycle := &airplaneModeLifecycleProbe{beforeErr: tt.beforeErr, afterErr: tt.afterErr}
			n, err := newNetwork(preferences, store, lifecycle)
			if err != nil {
				t.Fatalf("newNetwork() error = %v", err)
			}
			n.setAirplaneMode = func(_ context.Context, _ *mmodem.Modem, enabled bool) error {
				if enabled {
					lifecycle.calls = append(lifecycle.calls, "radio:true")
				} else {
					lifecycle.calls = append(lifecycle.calls, "radio:false")
				}
				return tt.radioErr
			}
			modem := &mmodem.Modem{EquipmentIdentifier: "modem-1"}

			err = n.SetAirplaneMode(t.Context(), modem, SetAirplaneModeRequest{Enabled: tt.enabled})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("SetAirplaneMode() error = %v, want %v", err, tt.wantErr)
			}
			if !slices.Equal(lifecycle.calls, tt.wantCalls) {
				t.Fatalf("calls = %v, want %v", lifecycle.calls, tt.wantCalls)
			}
			saved, ok, getErr := preferences.SavedAirplaneMode(t.Context(), modem.EquipmentIdentifier)
			if getErr != nil {
				t.Fatalf("SavedAirplaneMode() error = %v", getErr)
			}
			if ok != tt.wantSaved {
				t.Fatalf("SavedAirplaneMode() found = %t, want %t", ok, tt.wantSaved)
			}
			if ok && saved != tt.enabled {
				t.Fatalf("SavedAirplaneMode() enabled = %t, want %t", saved, tt.enabled)
			}
		})
	}
}
