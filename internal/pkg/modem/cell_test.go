package modem

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestServingLTECell(t *testing.T) {
	errCellInfo := errors.New("cell info unavailable")
	tests := []struct {
		name            string
		cells           []map[string]dbus.Variant
		callErr         error
		want            LTECell
		wantErr         error
		wantErrContains string
	}{
		{
			name: "uses legacy serving cell without carrier aggregation metadata",
			cells: []map[string]dbus.Variant{
				lteCellVariants(false, servingCellTypeUnknown, "46001", "1", "1", 1650),
				{
					"cell-type": dbus.MakeVariant(uint32(6)),
					"serving":   dbus.MakeVariant(true),
				},
				lteCellVariants(true, servingCellTypeUnknown, " 46000 ", " 8103 ", " 3FD4DC1 ", 40936),
			},
			want: LTECell{
				OperatorCode:     "46000",
				TrackingAreaCode: 0x8103,
				CellID:           0x3FD4DC1,
				EARFCN:           40936,
			},
		},
		{
			name: "prefers primary cell over secondary and legacy cells",
			cells: []map[string]dbus.Variant{
				lteCellVariants(true, 2, "46001", "1111", "1111111", 1650),
				lteCellVariants(true, servingCellTypeUnknown, "46002", "2222", "2222222", 1650),
				lteCellVariants(true, servingCellTypePrimary, "46000", "8103", "3FD4DC1", 40936),
			},
			want: LTECell{
				OperatorCode:     "46000",
				TrackingAreaCode: 0x8103,
				CellID:           0x3FD4DC1,
				EARFCN:           40936,
			},
		},
		{
			name:    "rejects secondary cell without primary cell",
			cells:   []map[string]dbus.Variant{lteCellVariants(true, 2, "46000", "8103", "3FD4DC1", 40936)},
			wantErr: errServingLTECellUnavailable,
		},
		{
			name: "rejects ambiguous legacy serving cells",
			cells: []map[string]dbus.Variant{
				lteCellVariants(true, servingCellTypeUnknown, "46000", "8103", "3FD4DC1", 40936),
				lteCellVariants(true, servingCellTypeUnknown, "46001", "8104", "3FD4DC2", 40937),
			},
			wantErr: errServingLTECellUnavailable,
		},
		{
			name:            "rejects invalid operator code",
			cells:           []map[string]dbus.Variant{lteCellVariants(true, servingCellTypePrimary, "46A00", "8103", "3FD4DC1", 40936)},
			wantErrContains: "operator code must contain only digits",
		},
		{
			name:            "rejects oversized tracking area code",
			cells:           []map[string]dbus.Variant{lteCellVariants(true, servingCellTypePrimary, "46000", "10000", "3FD4DC1", 40936)},
			wantErrContains: "tracking area code: strconv.ParseUint",
		},
		{
			name: "rejects missing EARFCN",
			cells: []map[string]dbus.Variant{
				{
					"cell-type":         dbus.MakeVariant(uint32(modemCellTypeLTE)),
					"serving":           dbus.MakeVariant(true),
					"serving-cell-type": dbus.MakeVariant(uint32(servingCellTypePrimary)),
					"operator-id":       dbus.MakeVariant("46000"),
					"tac":               dbus.MakeVariant("8103"),
					"ci":                dbus.MakeVariant("3FD4DC1"),
				},
			},
			wantErr: errServingLTECellEARFCN,
		},
		{
			name:    "reports missing serving LTE cell",
			cells:   []map[string]dbus.Variant{},
			wantErr: errServingLTECellUnavailable,
		},
		{
			name:    "wraps modem error",
			callErr: errCellInfo,
			wantErr: errCellInfo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			method := ModemInterface + ".GetCellInfo"
			object := &fakeBusObject{
				errors:  map[string][]error{method: {tt.callErr}},
				outputs: map[string][]any{method: {tt.cells}},
			}
			got, err := (&Modem{dbusObject: object}).ServingLTECell(context.Background())
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("ServingLTECell() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if tt.wantErrContains != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErrContains) {
					t.Fatalf("ServingLTECell() error = %v, want containing %q", err, tt.wantErrContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("ServingLTECell() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("ServingLTECell() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func lteCellVariants(serving bool, servingCellType uint32, operatorCode, tac, cellID string, earfcn uint32) map[string]dbus.Variant {
	cell := map[string]dbus.Variant{
		"cell-type":   dbus.MakeVariant(uint32(modemCellTypeLTE)),
		"serving":     dbus.MakeVariant(serving),
		"operator-id": dbus.MakeVariant(operatorCode),
		"tac":         dbus.MakeVariant(tac),
		"ci":          dbus.MakeVariant(cellID),
		"earfcn":      dbus.MakeVariant(earfcn),
	}
	if servingCellType != servingCellTypeUnknown {
		cell["serving-cell-type"] = dbus.MakeVariant(servingCellType)
	}
	return cell
}
