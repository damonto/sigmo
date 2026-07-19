package euicc

import (
	"encoding/hex"
	"fmt"

	"github.com/damonto/sigmo/internal/pkg/lpa"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/settings"
)

type euicc struct {
	store *settings.Store
}

func newEUICC(store *settings.Store) *euicc {
	return &euicc{
		store: store,
	}
}

func (e *euicc) Get(modem *mmodem.Modem) (*SEsResponse, error) {
	current := e.store.Snapshot()
	ses, err := lpa.DiscoverSEs(modem)
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
		client, err := lpa.NewWithAID(modem, &current, se.AID)
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
