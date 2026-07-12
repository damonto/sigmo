package network

import (
	"context"
	"errors"
	"fmt"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	wwan "github.com/damonto/sigmo/internal/pkg/modem/wwan"
)

func (n *network) AirplaneMode(ctx context.Context, modem *mmodem.Modem) (*AirplaneModeResponse, error) {
	device, err := mmodem.OpenDevice(modem)
	if errors.Is(err, wwan.ErrUnsupported) {
		return &AirplaneModeResponse{Supported: false}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open device: %w", err)
	}
	enabled, err := device.AirplaneMode(ctx)
	if err != nil {
		return nil, fmt.Errorf("read airplane mode: %w", err)
	}
	return &AirplaneModeResponse{
		Supported: true,
		Enabled:   enabled,
	}, nil
}

func (n *network) SetAirplaneMode(ctx context.Context, modem *mmodem.Modem, req SetAirplaneModeRequest) error {
	device, err := mmodem.OpenDevice(modem)
	if errors.Is(err, wwan.ErrUnsupported) {
		return wwan.ErrUnsupported
	}
	if err != nil {
		return fmt.Errorf("open device: %w", err)
	}
	if err := device.SetAirplaneMode(ctx, req.Enabled); err != nil {
		return fmt.Errorf("set airplane mode: %w", err)
	}
	if err := n.preferences.SaveAirplaneMode(ctx, modem.EquipmentIdentifier, req.Enabled); err != nil {
		return fmt.Errorf("save airplane mode: %w", err)
	}
	return nil
}
