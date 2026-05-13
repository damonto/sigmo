package network

import (
	"errors"
	"testing"

	"github.com/godbus/dbus/v5"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

func TestModeResponse(t *testing.T) {
	tests := []struct {
		name    string
		mode    mmodem.ModemModePair
		current mmodem.ModemModePair
		want    ModeResponse
	}{
		{
			name: "current",
			mode: mmodem.ModemModePair{
				Allowed:   mmodem.ModemMode4G,
				Preferred: mmodem.ModemModeNone,
			},
			current: mmodem.ModemModePair{
				Allowed:   mmodem.ModemMode4G,
				Preferred: mmodem.ModemModeNone,
			},
			want: ModeResponse{
				Allowed:        uint32(mmodem.ModemMode4G),
				Preferred:      uint32(mmodem.ModemModeNone),
				AllowedLabel:   "4G",
				PreferredLabel: "None",
				Current:        true,
			},
		},
		{
			name: "supported",
			mode: mmodem.ModemModePair{
				Allowed:   mmodem.ModemMode4G | mmodem.ModemMode5G,
				Preferred: mmodem.ModemMode5G,
			},
			current: mmodem.ModemModePair{
				Allowed:   mmodem.ModemMode4G,
				Preferred: mmodem.ModemModeNone,
			},
			want: ModeResponse{
				Allowed:        uint32(mmodem.ModemMode4G | mmodem.ModemMode5G),
				Preferred:      uint32(mmodem.ModemMode5G),
				AllowedLabel:   "4G + 5G",
				PreferredLabel: "5G",
				Current:        false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := modeResponse(tt.mode, tt.current); got != tt.want {
				t.Fatalf("modeResponse() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestValidateBandValues(t *testing.T) {
	tests := []struct {
		name      string
		supported []mmodem.ModemBand
		bands     []mmodem.ModemBand
		wantErr   error
	}{
		{
			name:      "accept supported bands",
			supported: []mmodem.ModemBand{mmodem.ModemBandAny, 71, 378},
			bands:     []mmodem.ModemBand{71, 378},
		},
		{
			name:      "reject empty",
			supported: []mmodem.ModemBand{mmodem.ModemBandAny, 71},
			wantErr:   errBandsRequired,
		},
		{
			name:      "reject unsupported",
			supported: []mmodem.ModemBand{mmodem.ModemBandAny, 71},
			bands:     []mmodem.ModemBand{72},
			wantErr:   errUnsupportedBand,
		},
		{
			name:      "reject any with other bands",
			supported: []mmodem.ModemBand{mmodem.ModemBandAny, 71},
			bands:     []mmodem.ModemBand{mmodem.ModemBandAny, 71},
			wantErr:   errAnyBandExclusive,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateBandValues(tt.supported, tt.bands)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("validateBandValues() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestCellInfoResponse(t *testing.T) {
	rsrp := -88.5
	tests := []struct {
		name string
		raw  map[string]dbus.Variant
		want CellInfoResponse
	}{
		{
			name: "lte serving cell",
			raw: map[string]dbus.Variant{
				"cell-type":   dbus.MakeVariant(uint32(mmodem.CellTypeLTE)),
				"serving":     dbus.MakeVariant(true),
				"operator-id": dbus.MakeVariant("310260"),
				"tac":         dbus.MakeVariant("100"),
				"ci":          dbus.MakeVariant("abcdef"),
				"earfcn":      dbus.MakeVariant(uint32(39150)),
				"rsrp":        dbus.MakeVariant(rsrp),
			},
			want: CellInfoResponse{
				Type:       "LTE",
				TypeValue:  uint32(mmodem.CellTypeLTE),
				Serving:    true,
				OperatorID: "310260",
				TAC:        "100",
				CellID:     "abcdef",
				EARFCN:     uint32Ptr(39150),
				RSRP:       &rsrp,
			},
		},
		{
			name: "5g missing optional fields",
			raw: map[string]dbus.Variant{
				"cell-type": dbus.MakeVariant(uint32(mmodem.CellType5GNR)),
			},
			want: CellInfoResponse{
				Type:      "5GNR",
				TypeValue: uint32(mmodem.CellType5GNR),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cellInfoResponse(tt.raw)
			if !equalCellInfo(got, tt.want) {
				t.Fatalf("cellInfoResponse() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func equalCellInfo(a, b CellInfoResponse) bool {
	return a.Type == b.Type &&
		a.TypeValue == b.TypeValue &&
		a.Serving == b.Serving &&
		a.OperatorID == b.OperatorID &&
		a.TAC == b.TAC &&
		a.CellID == b.CellID &&
		equalUintPtr(a.EARFCN, b.EARFCN) &&
		equalFloatPtr(a.RSRP, b.RSRP)
}

func equalUintPtr(a, b *uint32) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func equalFloatPtr(a, b *float64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func uint32Ptr(value uint32) *uint32 {
	return &value
}
