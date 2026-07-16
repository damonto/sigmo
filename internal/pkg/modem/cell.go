package modem

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/godbus/dbus/v5"
)

const (
	modemCellTypeLTE        = 5
	servingCellTypeUnknown  = 0
	servingCellTypePrimary  = 1
	maxEUTRANCellIdentifier = 0x0FFFFFFF
)

var (
	errServingLTECellUnavailable = errors.New("serving LTE cell is unavailable")
	errServingLTECellEARFCN      = errors.New("serving LTE cell EARFCN is unavailable")
)

// LTECell contains the identity of the primary LTE serving cell.
type LTECell struct {
	OperatorCode     string
	TrackingAreaCode uint16
	CellID           uint32
	EARFCN           uint32
}

// ServingLTECell returns the primary serving cell, falling back to the sole
// serving cell reported by ModemManager versions without carrier aggregation metadata.
func (m *Modem) ServingLTECell(ctx context.Context) (LTECell, error) {
	if m == nil || m.dbusObject == nil {
		return LTECell{}, errors.New("modem is required")
	}

	var cells []map[string]dbus.Variant
	if err := m.dbusObject.CallWithContext(ctx, ModemInterface+".GetCellInfo", 0).Store(&cells); err != nil {
		return LTECell{}, fmt.Errorf("read modem cell info: %w", err)
	}
	var fallback map[string]dbus.Variant
	var multipleFallbacks bool
	for _, raw := range cells {
		if variantUint[uint32](raw, "cell-type") != modemCellTypeLTE || !boolFromVariant(raw["serving"]) {
			continue
		}
		servingCellType, ok := raw["serving-cell-type"]
		if !ok {
			if fallback == nil {
				fallback = raw
			} else {
				multipleFallbacks = true
			}
			continue
		}
		servingCellTypeValue, ok := servingCellType.Value().(uint32)
		if !ok {
			return LTECell{}, fmt.Errorf("read serving LTE cell type: unexpected D-Bus type %T", servingCellType.Value())
		}
		if servingCellTypeValue == servingCellTypeUnknown {
			if fallback == nil {
				fallback = raw
			} else {
				multipleFallbacks = true
			}
			continue
		}
		if servingCellTypeValue == servingCellTypePrimary {
			return lteCellFromDBus(raw)
		}
	}
	if fallback != nil && !multipleFallbacks {
		return lteCellFromDBus(fallback)
	}
	return LTECell{}, errServingLTECellUnavailable
}

func lteCellFromDBus(raw map[string]dbus.Variant) (LTECell, error) {
	operatorCode := strings.TrimSpace(variantString(raw, "operator-id"))
	if len(operatorCode) != 5 && len(operatorCode) != 6 {
		return LTECell{}, errors.New("serving LTE cell operator code must contain 5 or 6 digits")
	}
	for _, digit := range operatorCode {
		if digit < '0' || digit > '9' {
			return LTECell{}, errors.New("serving LTE cell operator code must contain only digits")
		}
	}

	tac, err := strconv.ParseUint(strings.TrimSpace(variantString(raw, "tac")), 16, 16)
	if err != nil {
		return LTECell{}, fmt.Errorf("parse serving LTE cell tracking area code: %w", err)
	}
	cellID, err := strconv.ParseUint(strings.TrimSpace(variantString(raw, "ci")), 16, 32)
	if err != nil {
		return LTECell{}, fmt.Errorf("parse serving LTE cell ID: %w", err)
	}
	if cellID > maxEUTRANCellIdentifier {
		return LTECell{}, errors.New("serving LTE cell ID exceeds 28 bits")
	}

	earfcnVariant, ok := raw["earfcn"]
	if !ok {
		return LTECell{}, errServingLTECellEARFCN
	}
	earfcn, ok := earfcnVariant.Value().(uint32)
	if !ok {
		return LTECell{}, fmt.Errorf("read serving LTE cell EARFCN: unexpected D-Bus type %T", earfcnVariant.Value())
	}
	return LTECell{
		OperatorCode:     operatorCode,
		TrackingAreaCode: uint16(tac),
		CellID:           uint32(cellID),
		EARFCN:           earfcn,
	}, nil
}
