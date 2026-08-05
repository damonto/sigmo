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
	ses, err := e.clients.SecureElements(ctx, modem)
	if err != nil {
		return nil, fmt.Errorf("discover eUICC SEs: %w", err)
	}
	response := &SEsResponse{SEs: make([]SEItemResponse, 0, len(ses))}
	for _, se := range ses {
		item := SEItemResponse{
			ID:    se.ID,
			Label: se.Label,
			AID:   hex.EncodeToString(se.AID),
		}
		client, err := e.clients.Acquire(ctx, modem, se.ID)
		if err != nil {
			modem.Logger().Warn("create LPA client for eUICC info", "seId", se.ID, "error", err)
			return nil, fmt.Errorf("create LPA client for %s: %w", se.ID, err)
		}
		info, err := client.Info()
		if err != nil {
			if cerr := client.Close(); cerr != nil {
				client.Logger().Warn("failed to close LPA client", "error", cerr)
			}
			err = fmt.Errorf("fetch eUICC info for %s: %w", se.ID, err)
			modem.Logger().Warn("fetch eUICC info", "seId", se.ID, "error", err)
			return nil, err
		}
		item.EID = info.EID
		item.FreeSpace = info.FreeSpace
		item.SASUP = SASUPResponse{
			Name:   info.SASUP.Name,
			Region: info.SASUP.Region,
		}
		item.Certificates = append([]string{}, info.Certificates...)
		if cerr := client.Close(); cerr != nil {
			client.Logger().Warn("failed to close LPA client", "error", cerr)
		}
		response.SEs = append(response.SEs, item)
	}
	return response, nil
}
