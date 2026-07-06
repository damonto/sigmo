package network

import (
	"context"
	"errors"
	"testing"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	mdevice "github.com/damonto/sigmo/internal/pkg/modem/device"
)

func TestAirplaneModeUnsupported(t *testing.T) {
	t.Parallel()

	preferences, err := mmodem.NewNetworkPreferences(openNetworkTestStore(t))
	if err != nil {
		t.Fatalf("NewNetworkPreferences() error = %v", err)
	}
	n, err := newNetwork(preferences, openNetworkTestStore(t))
	if err != nil {
		t.Fatalf("newNetwork() error = %v", err)
	}
	modem := &mmodem.Modem{
		EquipmentIdentifier: "modem-1",
		PrimaryPort:         "ttyUSB2",
		Ports: []mmodem.ModemPort{
			{PortType: mmodem.ModemPortTypeAt, Device: "ttyUSB2"},
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
	if !errors.Is(err, mdevice.ErrUnsupported) {
		t.Fatalf("SetAirplaneMode() error = %v, want unsupported", err)
	}
}
