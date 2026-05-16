package network

import (
	"context"
	"errors"
	"fmt"
	"slices"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

var errUnsupportedMode = errors.New("unsupported mode")

func (n *network) Modes(ctx context.Context, modem *mmodem.Modem) (*ModesResponse, error) {
	supported, err := modem.SupportedModes(ctx)
	if err != nil {
		return nil, fmt.Errorf("read supported modes: %w", err)
	}
	current, err := modem.CurrentModes(ctx)
	if err != nil {
		return nil, fmt.Errorf("read current modes: %w", err)
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
	want := mmodem.ModemModePair{
		Allowed:   mmodem.ModemMode(req.Allowed),
		Preferred: mmodem.ModemMode(req.Preferred),
	}
	supported, err := modem.SupportedModes(ctx)
	if err != nil {
		return fmt.Errorf("read supported modes: %w", err)
	}
	if !slices.Contains(supported, want) {
		return errUnsupportedMode
	}
	if err := modem.SetCurrentModes(ctx, want); err != nil {
		return fmt.Errorf("set current modes: %w", err)
	}
	if n.preferences != nil {
		if err := n.preferences.SaveMode(modem.EquipmentIdentifier, want); err != nil {
			return fmt.Errorf("save current modes: %w", err)
		}
	}
	return nil
}

func modeResponse(mode mmodem.ModemModePair, current mmodem.ModemModePair) ModeResponse {
	return ModeResponse{
		Allowed:        uint32(mode.Allowed),
		Preferred:      uint32(mode.Preferred),
		AllowedLabel:   mode.Allowed.Label(),
		PreferredLabel: mode.Preferred.String(),
		Current:        mode == current,
	}
}
