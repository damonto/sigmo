package euicc

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/damonto/sigmo/internal/pkg/config"
	"github.com/damonto/sigmo/internal/pkg/lpa"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

type euicc struct {
	store *config.Store
}

func newEUICC(store *config.Store) *euicc {
	return &euicc{
		store: store,
	}
}

func (e *euicc) Get(modem *mmodem.Modem) (*EuiccResponse, error) {
	cfg := e.store.Snapshot()
	client, err := lpa.New(modem, &cfg)
	if err != nil {
		if errors.Is(err, lpa.ErrNoSupportedAID) {
			return nil, err
		}
		return nil, fmt.Errorf("create LPA client: %w", err)
	}
	defer func() {
		if cerr := client.Close(); cerr != nil {
			slog.Warn("failed to close LPA client", "error", cerr)
		}
	}()

	info, err := client.Info()
	if err != nil {
		return nil, fmt.Errorf("fetch eUICC info: %w", err)
	}
	return &EuiccResponse{
		EID:       info.EID,
		FreeSpace: info.FreeSpace,
		SASUP: SASUPResponse{
			Name:   info.SASUP.Name,
			Region: info.SASUP.Region,
		},
		Certificates: info.Certificates,
	}, nil
}
