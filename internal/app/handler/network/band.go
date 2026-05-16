package network

import (
	"context"
	"errors"
	"fmt"
	"slices"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

var (
	errBandsRequired    = errors.New("bands are required")
	errUnsupportedBand  = errors.New("unsupported band")
	errDuplicateBand    = errors.New("duplicate band")
	errAnyBandExclusive = errors.New("any band cannot be combined with other bands")
)

func (n *network) Bands(ctx context.Context, modem *mmodem.Modem) (*BandsResponse, error) {
	supported, err := modem.SupportedBands(ctx)
	if err != nil {
		return nil, fmt.Errorf("read supported bands: %w", err)
	}
	current, err := modem.CurrentBands(ctx)
	if err != nil {
		return nil, fmt.Errorf("read current bands: %w", err)
	}

	currentValues := bandValues(current)
	response := &BandsResponse{
		Supported: make([]BandResponse, 0, len(supported)),
		Current:   currentValues,
	}
	for _, band := range supported {
		response.Supported = append(response.Supported, BandResponse{
			Value:   uint32(band),
			Label:   band.String(),
			Current: slices.Contains(current, band),
		})
	}
	return response, nil
}

func (n *network) SetCurrentBands(ctx context.Context, modem *mmodem.Modem, req SetCurrentBandsRequest) error {
	bands := make([]mmodem.ModemBand, 0, len(req.Bands))
	for _, band := range req.Bands {
		bands = append(bands, mmodem.ModemBand(band))
	}
	if err := n.validateBands(ctx, modem, bands); err != nil {
		return err
	}
	if err := modem.SetCurrentBands(ctx, bands); err != nil {
		return fmt.Errorf("set current bands: %w", err)
	}
	if n.preferences != nil {
		if err := n.preferences.SaveBands(modem.EquipmentIdentifier, bands); err != nil {
			return fmt.Errorf("save current bands: %w", err)
		}
	}
	return nil
}

func (n *network) validateBands(ctx context.Context, modem *mmodem.Modem, bands []mmodem.ModemBand) error {
	supported, err := modem.SupportedBands(ctx)
	if err != nil {
		return fmt.Errorf("read supported bands: %w", err)
	}
	return validateBandValues(supported, bands)
}

func validateBandValues(supported []mmodem.ModemBand, bands []mmodem.ModemBand) error {
	if len(bands) == 0 {
		return errBandsRequired
	}
	seen := make(map[mmodem.ModemBand]struct{}, len(bands))
	for _, band := range bands {
		if _, ok := seen[band]; ok {
			return errDuplicateBand
		}
		seen[band] = struct{}{}
		if !slices.Contains(supported, band) {
			return errUnsupportedBand
		}
	}
	if slices.Contains(bands, mmodem.ModemBandAny) && len(bands) > 1 {
		return errAnyBandExclusive
	}
	return nil
}

func bandValues(bands []mmodem.ModemBand) []uint32 {
	values := make([]uint32, 0, len(bands))
	for _, band := range bands {
		values = append(values, uint32(band))
	}
	return values
}
