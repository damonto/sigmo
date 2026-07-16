//go:build ims

package ims

import (
	"fmt"

	"github.com/damonto/ims-go/lte"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

const (
	lteDuplexFDD = "FDD"
	lteDuplexTDD = "TDD"
)

func lteAccessNetworkInfo(cell mmodem.LTECell) (string, error) {
	duplex, err := lteDuplexForEARFCN(cell.EARFCN)
	if err != nil {
		return "", err
	}
	value := fmt.Sprintf(
		"3GPP-E-UTRAN-%s;utran-cell-id-3gpp=%s%04X%07X",
		duplex,
		cell.OperatorCode,
		cell.TrackingAreaCode,
		cell.CellID,
	)
	return lte.NormalizeAccessNetworkInfo(value)
}

func lteDuplexForEARFCN(earfcn uint32) (string, error) {
	// 3GPP TS 36.101 assigns separate EARFCN ranges to paired FDD and
	// unpaired TDD bands. Reject gaps so unknown future values cannot be
	// silently advertised as the wrong radio access type.
	switch {
	case earfcn <= 9659:
		return lteDuplexFDD, nil
	case earfcn >= 36000 && earfcn <= 60254:
		return lteDuplexTDD, nil
	case earfcn >= 65536 && earfcn <= 70545:
		return lteDuplexFDD, nil
	case earfcn >= 70596 && earfcn <= 70695:
		return lteDuplexFDD, nil
	default:
		return "", fmt.Errorf("unsupported LTE EARFCN %d", earfcn)
	}
}
