package network

import (
	"errors"
	"log/slog"
	"slices"

	"github.com/godbus/dbus/v5"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

var (
	errUnsupportedMode  = errors.New("unsupported mode")
	errBandsRequired    = errors.New("bands are required")
	errUnsupportedBand  = errors.New("unsupported band")
	errAnyBandExclusive = errors.New("any band cannot be combined with other bands")
)

func (n *network) Modes(modem *mmodem.Modem) (*ModesResponse, error) {
	supported, err := modem.SupportedModes()
	if err != nil {
		slog.Error("read supported modes", "modem", modem.EquipmentIdentifier, "error", err)
		return nil, err
	}
	current, err := modem.CurrentModes()
	if err != nil {
		slog.Error("read current modes", "modem", modem.EquipmentIdentifier, "error", err)
		return nil, err
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

func (n *network) SetCurrentModes(modem *mmodem.Modem, req SetCurrentModesRequest) error {
	want := mmodem.ModemModePair{
		Allowed:   mmodem.ModemMode(req.Allowed),
		Preferred: mmodem.ModemMode(req.Preferred),
	}
	supported, err := modem.SupportedModes()
	if err != nil {
		slog.Error("read supported modes", "modem", modem.EquipmentIdentifier, "error", err)
		return err
	}
	if !slices.Contains(supported, want) {
		return errUnsupportedMode
	}
	if err := modem.SetCurrentModes(want); err != nil {
		slog.Error("set current modes", "modem", modem.EquipmentIdentifier, "allowed", req.Allowed, "preferred", req.Preferred, "error", err)
		return err
	}
	return nil
}

func (n *network) Bands(modem *mmodem.Modem) (*BandsResponse, error) {
	supported, err := modem.SupportedBands()
	if err != nil {
		slog.Error("read supported bands", "modem", modem.EquipmentIdentifier, "error", err)
		return nil, err
	}
	current, err := modem.CurrentBands()
	if err != nil {
		slog.Error("read current bands", "modem", modem.EquipmentIdentifier, "error", err)
		return nil, err
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

func (n *network) SetCurrentBands(modem *mmodem.Modem, req SetCurrentBandsRequest) error {
	bands := make([]mmodem.ModemBand, 0, len(req.Bands))
	for _, band := range req.Bands {
		bands = append(bands, mmodem.ModemBand(band))
	}
	if err := validateBands(modem, bands); err != nil {
		return err
	}
	if err := modem.SetCurrentBands(bands); err != nil {
		slog.Error("set current bands", "modem", modem.EquipmentIdentifier, "bands", req.Bands, "error", err)
		return err
	}
	return nil
}

func (n *network) Cells(modem *mmodem.Modem) ([]CellInfoResponse, error) {
	cells, err := modem.GetCellInfo()
	if err != nil {
		slog.Error("get cell info", "modem", modem.EquipmentIdentifier, "error", err)
		return nil, err
	}
	response := make([]CellInfoResponse, 0, len(cells))
	for _, cell := range cells {
		response = append(response, cellInfoResponse(cell))
	}
	slices.SortStableFunc(response, func(a, b CellInfoResponse) int {
		if a.Serving == b.Serving {
			return 0
		}
		if a.Serving {
			return -1
		}
		return 1
	})
	return response, nil
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

func validateBands(modem *mmodem.Modem, bands []mmodem.ModemBand) error {
	supported, err := modem.SupportedBands()
	if err != nil {
		slog.Error("read supported bands", "modem", modem.EquipmentIdentifier, "error", err)
		return err
	}
	return validateBandValues(supported, bands)
}

func validateBandValues(supported []mmodem.ModemBand, bands []mmodem.ModemBand) error {
	if len(bands) == 0 {
		return errBandsRequired
	}
	if slices.Contains(bands, mmodem.ModemBandAny) && len(bands) > 1 {
		return errAnyBandExclusive
	}
	for _, band := range bands {
		if !slices.Contains(supported, band) {
			return errUnsupportedBand
		}
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

func cellInfoResponse(raw map[string]dbus.Variant) CellInfoResponse {
	cellType := mmodem.CellType(variantUint(raw, "cell-type"))
	response := CellInfoResponse{
		Type:      cellType.String(),
		TypeValue: uint32(cellType),
		Serving:   variantBool(raw, "serving"),
	}
	response.OperatorID = variantString(raw, "operator-id")
	response.LAC = variantString(raw, "lac")
	response.TAC = variantString(raw, "tac")
	response.CellID = variantString(raw, "ci")
	response.PhysicalCellID = variantString(raw, "physical-ci")
	response.ARFCN = optionalUint(raw, "arfcn")
	response.UARFCN = optionalUint(raw, "uarfcn")
	response.EARFCN = optionalUint(raw, "earfcn")
	response.NRARFCN = optionalUint(raw, "nrarfcn")
	response.RSRP = optionalFloat(raw, "rsrp")
	response.RSRQ = optionalFloat(raw, "rsrq")
	response.SINR = optionalFloat(raw, "sinr")
	response.TimingAdvance = optionalUint(raw, "timing-advance")
	response.Bandwidth = optionalUint(raw, "bandwidth")
	response.ServingCellType = optionalUint(raw, "serving-cell-type")
	return response
}

func optionalUint(raw map[string]dbus.Variant, key string) *uint32 {
	variant, ok := raw[key]
	if !ok {
		return nil
	}
	value := uintFromVariant(variant)
	return &value
}

func optionalFloat(raw map[string]dbus.Variant, key string) *float64 {
	variant, ok := raw[key]
	if !ok {
		return nil
	}
	value := floatFromVariant(variant)
	return &value
}

func variantString(raw map[string]dbus.Variant, key string) string {
	value, ok := raw[key].Value().(string)
	if !ok {
		return ""
	}
	return value
}

func variantBool(raw map[string]dbus.Variant, key string) bool {
	value, ok := raw[key].Value().(bool)
	if !ok {
		return false
	}
	return value
}

func variantUint(raw map[string]dbus.Variant, key string) uint32 {
	return uintFromVariant(raw[key])
}

func uintFromVariant(variant dbus.Variant) uint32 {
	switch value := variant.Value().(type) {
	case uint32:
		return value
	case uint64:
		return uint32(value)
	case int:
		if value < 0 {
			return 0
		}
		return uint32(value)
	case int32:
		if value < 0 {
			return 0
		}
		return uint32(value)
	case int64:
		if value < 0 {
			return 0
		}
		return uint32(value)
	default:
		return 0
	}
}

func floatFromVariant(variant dbus.Variant) float64 {
	switch value := variant.Value().(type) {
	case float64:
		return value
	case float32:
		return float64(value)
	case int:
		return float64(value)
	case int32:
		return float64(value)
	case int64:
		return float64(value)
	case uint32:
		return float64(value)
	case uint64:
		return float64(value)
	default:
		return 0
	}
}
