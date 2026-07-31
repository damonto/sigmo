package modem

import (
	"context"
	"errors"
	"fmt"

	wwanmodem "github.com/damonto/wwan-go/modem"
)

const maxEUTRANCellIdentifier = 0x0FFFFFFF

var (
	errServingLTECellUnavailable = errors.New("serving LTE cell is unavailable")
	errServingLTECellEARFCN      = errors.New("serving LTE cell EARFCN is unavailable")
)

type LTECell struct {
	OperatorCode     string
	TrackingAreaCode uint16
	CellID           uint32
	EARFCN           uint32
}

func (m *Modem) ServingLTECell(ctx context.Context) (LTECell, error) {
	if m == nil || m.core == nil {
		return LTECell{}, errModemRequired
	}
	cells, err := m.core.CellInfo(ctx)
	if err != nil {
		return LTECell{}, fmt.Errorf("read modem cell info: %w", err)
	}
	for _, cell := range cells {
		if !cell.Serving || cell.Technology&wwanmodem.TechnologyLTE == 0 {
			continue
		}
		if cell.ARFCN == 0 {
			return LTECell{}, errServingLTECellEARFCN
		}
		if cell.CellID > maxEUTRANCellIdentifier {
			return LTECell{}, errors.New("serving LTE cell ID exceeds 28 bits")
		}
		return LTECell{OperatorCode: cell.OperatorID, TrackingAreaCode: uint16(cell.TrackingAreaCode), CellID: uint32(cell.CellID), EARFCN: cell.ARFCN}, nil
	}
	return LTECell{}, errServingLTECellUnavailable
}
