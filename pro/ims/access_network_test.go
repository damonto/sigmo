//go:build ims

package ims

import (
	"testing"

	"github.com/damonto/ims-go/lte"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

func TestLTEDuplexForEARFCN(t *testing.T) {
	tests := []struct {
		name    string
		earfcn  uint32
		want    string
		wantErr bool
	}{
		{name: "legacy FDD lower bound", earfcn: 0, want: lteDuplexFDD},
		{name: "legacy FDD upper bound", earfcn: 9659, want: lteDuplexFDD},
		{name: "TDD lower bound", earfcn: 36000, want: lteDuplexTDD},
		{name: "China Mobile band 41", earfcn: 40936, want: lteDuplexTDD},
		{name: "TDD upper bound", earfcn: 60254, want: lteDuplexTDD},
		{name: "extended FDD lower bound", earfcn: 65536, want: lteDuplexFDD},
		{name: "extended FDD upper bound", earfcn: 70695, want: lteDuplexFDD},
		{name: "gap after legacy FDD", earfcn: 9660, wantErr: true},
		{name: "gap after TDD", earfcn: 60255, wantErr: true},
		{name: "gap between extended FDD bands", earfcn: 70546, wantErr: true},
		{name: "above known LTE bands", earfcn: 70696, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := lteDuplexForEARFCN(tt.earfcn)
			if tt.wantErr {
				if err == nil {
					t.Fatal("lteDuplexForEARFCN() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("lteDuplexForEARFCN() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("lteDuplexForEARFCN() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLTEAccessNetworkInfo(t *testing.T) {
	tests := []struct {
		name string
		cell mmodem.LTECell
		want string
	}{
		{
			name: "TDD serving cell",
			cell: mmodem.LTECell{
				OperatorCode:     "46000",
				TrackingAreaCode: 0x8103,
				CellID:           0x3FD4DC1,
				EARFCN:           40936,
			},
			want: "3GPP-E-UTRAN-TDD;utran-cell-id-3gpp=4600081033FD4DC1",
		},
		{
			name: "FDD pads TAC and cell ID",
			cell: mmodem.LTECell{
				OperatorCode:     "310260",
				TrackingAreaCode: 0xA,
				CellID:           0x2B,
				EARFCN:           1650,
			},
			want: "3GPP-E-UTRAN-FDD;utran-cell-id-3gpp=310260000A000002B",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := lteAccessNetworkInfo(tt.cell)
			if err != nil {
				t.Fatalf("lteAccessNetworkInfo() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("lteAccessNetworkInfo() = %q, want %q", got, tt.want)
			}
			if _, err := lte.NormalizeAccessNetworkInfo(got); err != nil {
				t.Fatalf("ims-go rejected access network info %q: %v", got, err)
			}
		})
	}
}
