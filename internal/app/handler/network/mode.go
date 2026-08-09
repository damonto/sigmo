package network

import (
	"context"
	"errors"
	"fmt"
	"slices"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	wwanmodem "github.com/damonto/wwan-go/modem"
)

var (
	errUnsupportedMode        = errors.New("unsupported mode")
	errCurrentModeUnavailable = errors.New("current mode is not in supported modes")
)

func (n *network) Modes(ctx context.Context, modem *mmodem.Modem) (*ModesResponse, error) {
	supported, current, err := modem.Modes(ctx)
	if err != nil {
		return nil, fmt.Errorf("read modes: %w", err)
	}
	if !slices.Contains(supported, current) {
		return nil, errCurrentModeUnavailable
	}

	response := &ModesResponse{
		Supported: make([]ModeResponse, 0, len(supported)),
		Current:   modeResponse(current, current),
	}
	for _, mode := range supported {
		response.Supported = append(response.Supported, modeResponse(mode, current))
	}
	return response, nil
}

func (n *network) SetCurrentModes(ctx context.Context, modem *mmodem.Modem, req SetCurrentModesRequest) error {
	want := wwanmodem.Mode{
		Allowed:   wwanmodem.Technology(req.Allowed),
		Preferred: wwanmodem.Technology(req.Preferred),
	}
	supported, current, err := modem.Modes(ctx)
	if err != nil {
		return fmt.Errorf("read modes: %w", err)
	}
	if !slices.Contains(supported, want) {
		return errUnsupportedMode
	}
	if current != want {
		if err := modem.SetCurrentModes(ctx, want); err != nil {
			return fmt.Errorf("set current modes: %w", err)
		}
		n.InvalidateScan(modem)
	}
	if err := n.preferences.SaveMode(ctx, modem.EquipmentIdentifier, want); err != nil {
		return fmt.Errorf("save current modes: %w", err)
	}
	return nil
}

func modeResponse(mode wwanmodem.Mode, current wwanmodem.Mode) ModeResponse {
	return ModeResponse{
		Allowed:        uint64(mode.Allowed),
		Preferred:      uint64(mode.Preferred),
		AllowedLabel:   technologyLabel(mode.Allowed),
		PreferredLabel: technologyLabel(mode.Preferred),
		Current:        mode == current,
	}
}
