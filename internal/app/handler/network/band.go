package network

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	wwanmodem "github.com/damonto/wwan-go/modem"
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

	return bandsResponse(supported, current), nil
}

func bandsResponse(supported []wwanmodem.Band, current []wwanmodem.Band) *BandsResponse {
	automatic := len(current) == 0 || sameBandSet(current, supported)
	currentValues := bandValues(current)
	if automatic {
		currentValues = []BandValue{{}}
	}
	response := &BandsResponse{
		Supported: []BandResponse{{Value: BandValue{}, Label: "Any", Current: automatic}},
		Current:   currentValues,
	}
	for _, band := range supported {
		response.Supported = append(response.Supported, BandResponse{
			Value:   bandValue(band),
			Label:   bandLabel(band),
			Current: !automatic && slices.Contains(current, band),
		})
	}
	return response
}

func sameBandSet(left []wwanmodem.Band, right []wwanmodem.Band) bool {
	leftSet := make(map[wwanmodem.Band]struct{}, len(left))
	for _, band := range left {
		leftSet[band] = struct{}{}
	}
	rightSet := make(map[wwanmodem.Band]struct{}, len(right))
	for _, band := range right {
		rightSet[band] = struct{}{}
	}
	return maps.Equal(leftSet, rightSet)
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
	if slices.Contains(values, BandValue{}) {
		if len(values) > 1 {
			return nil, errAnyBandExclusive
		}
		return nil, nil
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
	seen := make(map[wwanmodem.Band]struct{}, len(bands))
	for _, band := range bands {
		if _, ok := seen[band]; ok {
			return errDuplicateBand
		}
		seen[band] = struct{}{}
		if !slices.Contains(supported, band) {
			return errUnsupportedBand
		}
	}
	return nil
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
