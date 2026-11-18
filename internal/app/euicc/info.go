package euicc

import (
	"context"
	"encoding/hex"
	"fmt"

	"github.com/damonto/sigmo/internal/pkg/lpa"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

type euicc struct {
	clients *lpa.Pool
}

func newEUICC(clients *lpa.Pool) *euicc {
	return &euicc{clients: clients}
}

func (e *euicc) Get(ctx context.Context, modem *mmodem.Modem) (*SEsResponse, error) {
	cards, err := e.clients.EUICCInfo(ctx, modem)
	if err != nil {
		return nil, fmt.Errorf("fetch eUICC info: %w", err)
	}
	response := &SEsResponse{SEs: make([]SEItemResponse, 0, len(cards))}
	for _, card := range cards {
		certificates := card.Info.Certificates
		if certificates == nil {
			certificates = []string{}
		}
		response.SEs = append(response.SEs, SEItemResponse{
			ID:        card.SE.ID,
			Label:     card.SE.Label,
			AID:       hex.EncodeToString(card.SE.AID),
			EID:       card.Info.EID,
			FreeSpace: card.Info.FreeSpace,
			SASUP: SASUPResponse{
				Name:   card.Info.SASUP.Name,
				Region: card.Info.SASUP.Region,
			},
			Certificates: certificates,
		})
	}
	return response, nil
}
