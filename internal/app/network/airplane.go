package network

import (
	"context"
	"errors"
	"fmt"

	wwanmodem "github.com/damonto/wwan-go/modem"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

func (n *network) AirplaneMode(ctx context.Context, modem *mmodem.Modem) (*AirplaneModeResponse, error) {
	enabled, err := modem.AirplaneMode(ctx)
	if errors.Is(err, wwanmodem.ErrNotSupported) {
		return &AirplaneModeResponse{Supported: false}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read airplane mode: %w", err)
	}
	return &AirplaneModeResponse{
		Supported: true,
		Enabled:   enabled,
	}, nil
}

func (n *network) SetAirplaneMode(ctx context.Context, modem *mmodem.Modem, req SetAirplaneModeRequest) error {
	apply := func() (bool, error) {
		if err := n.setAirplaneMode(ctx, modem, req.Enabled); err != nil {
			return false, fmt.Errorf("set airplane mode: %w", err)
		}
		n.InvalidateScan(modem)
		if err := n.preferences.SaveAirplaneMode(ctx, modem.EquipmentIdentifier, req.Enabled); err != nil {
			return true, fmt.Errorf("save airplane mode: %w", err)
		}
		return true, nil
	}
	if n.airplaneModeLifecycle != nil {
		return n.airplaneModeLifecycle.ChangeAirplaneMode(ctx, modem, req.Enabled, apply)
	}
	_, err := apply()
	return err
}
