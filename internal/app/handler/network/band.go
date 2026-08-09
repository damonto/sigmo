package network

import (
	"context"
	"errors"
	"fmt"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	wwanmodem "github.com/damonto/wwan-go/modem"
)

var (
	errBandsRequired           = errors.New("bands are required")
	errUnsupportedBand         = errors.New("unsupported band")
	errDuplicateBand           = errors.New("duplicate band")
	errCurrentBandsUnavailable = errors.New("current bands are unavailable")
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
	current = filterCurrentBands(supported, current)
	if len(current) == 0 {
		return nil, errCurrentBandsUnavailable
	}

	return bandsResponse(supported, current), nil
}

func bandsResponse(supported []wwanmodem.Band, current []wwanmodem.Band) *BandsResponse {
	currentSet := bandSet(current)
	response := &BandsResponse{
		Supported: make([]BandResponse, 0, len(supported)),
		Current:   bandValues(current),
	}
	for _, band := range supported {
		_, currentBand := currentSet[band]
		response.Supported = append(response.Supported, BandResponse{
			Value:   bandValue(band),
			Label:   bandLabel(band),
			Current: currentBand,
		})
	}
	return response
}

func filterCurrentBands(supported []wwanmodem.Band, current []wwanmodem.Band) []wwanmodem.Band {
	supportedSet := bandSet(supported)
	seen := make(map[wwanmodem.Band]struct{}, len(current))
	filtered := make([]wwanmodem.Band, 0, len(current))
	for _, band := range current {
		if _, ok := supportedSet[band]; !ok {
			continue
		}
		if _, ok := seen[band]; ok {
			continue
		}
		seen[band] = struct{}{}
		filtered = append(filtered, band)
	}
	return filtered
}

func (n *network) SetCurrentBands(ctx context.Context, modem *mmodem.Modem, req SetCurrentBandsRequest) error {
	bands, err := n.validateBands(ctx, modem, req.Bands)
	if err != nil {
		return err
	}
	if err := modem.SetCurrentBands(ctx, bands); err != nil {
		return fmt.Errorf("set current bands: %w", err)
	}
	n.InvalidateScan(modem)
	if err := n.preferences.SaveBands(ctx, modem.EquipmentIdentifier, bands); err != nil {
		return fmt.Errorf("save current bands: %w", err)
	}
	return nil
}

func (n *network) validateBands(ctx context.Context, modem *mmodem.Modem, values []BandValue) ([]wwanmodem.Band, error) {
	if len(values) == 0 {
		return nil, errBandsRequired
	}

	bands := make([]wwanmodem.Band, 0, len(values))
	for _, value := range values {
		bands = append(bands, wwanmodem.Band{Technology: wwanmodem.Technology(value.Technology), Number: value.Number})
	}
	supported, err := modem.SupportedBands(ctx)
	if err != nil {
		return nil, fmt.Errorf("read supported bands: %w", err)
	}
	if err := validateBandValues(supported, bands); err != nil {
		return nil, err
	}
	return bands, nil
}

func validateBandValues(supported []wwanmodem.Band, bands []wwanmodem.Band) error {
	if len(bands) == 0 {
		return errBandsRequired
	}
	supportedSet := bandSet(supported)
	seen := make(map[wwanmodem.Band]struct{}, len(bands))
	for _, band := range bands {
		if _, ok := seen[band]; ok {
			return errDuplicateBand
		}
		seen[band] = struct{}{}
		if _, ok := supportedSet[band]; !ok {
			return errUnsupportedBand
		}
	}
	return nil
}

func bandSet(bands []wwanmodem.Band) map[wwanmodem.Band]struct{} {
	set := make(map[wwanmodem.Band]struct{}, len(bands))
	for _, band := range bands {
		set[band] = struct{}{}
	}
	return set
}

func bandValues(bands []wwanmodem.Band) []BandValue {
	values := make([]BandValue, 0, len(bands))
	for _, band := range bands {
		values = append(values, bandValue(band))
	}
	return values
}

func bandValue(band wwanmodem.Band) BandValue {
	return BandValue{Technology: uint64(band.Technology), Number: band.Number}
}
