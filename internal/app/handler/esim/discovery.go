package esim

import (
	"context"
	"fmt"
	"log/slog"

	sgp22 "github.com/damonto/euicc-go/v2"

	"github.com/damonto/sigmo/internal/pkg/config"
	"github.com/damonto/sigmo/internal/pkg/lpa"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

type provisioning struct {
	store *config.Store
}

func newProvisioning(store *config.Store) *provisioning {
	return &provisioning{store: store}
}

func (p *provisioning) Discover(ctx context.Context, modem *mmodem.Modem) ([]DiscoverResponse, error) {
	cfg := p.store.Snapshot()
	client, err := lpa.New(modem, &cfg)
	if err != nil {
		return nil, fmt.Errorf("create LPA client: %w", err)
	}
	defer func() {
		if cerr := client.Close(); cerr != nil {
			slog.Warn("close LPA client", "error", cerr)
		}
	}()

	imeiValue, err := modem.ThreeGPP().IMEI(ctx)
	if err != nil {
		return nil, fmt.Errorf("read modem IMEI: %w", err)
	}
	imei, err := sgp22.NewIMEI(imeiValue)
	if err != nil {
		return nil, fmt.Errorf("parse modem IMEI %s: %w", imeiValue, err)
	}

	entries, err := client.Discover(imei)
	if err != nil {
		return nil, fmt.Errorf("discover profiles: %w", err)
	}

	response := make([]DiscoverResponse, 0, len(entries))
	for _, entry := range entries {
		response = append(response, DiscoverResponse{
			EventID: entry.EventID,
			Address: entry.Address,
		})
	}
	return response, nil
}
