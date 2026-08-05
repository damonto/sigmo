package esim

import (
	"context"
	"fmt"

	sgp22 "github.com/damonto/euicc-go/v2"

	"github.com/damonto/sigmo/internal/pkg/lpa"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

type provisioning struct {
	clients *lpa.Pool
}

func newProvisioning(clients *lpa.Pool) *provisioning {
	return &provisioning{clients: clients}
}

func (p *provisioning) Discovery(ctx context.Context, modem *mmodem.Modem, seID string) ([]DiscoverResponse, error) {
	client, err := p.clients.Acquire(ctx, modem, seID)
	if err != nil {
		return nil, fmt.Errorf("create LPA client: %w", err)
	}
	defer func() {
		if cerr := client.Close(); cerr != nil {
			client.Logger().Warn("close LPA client", "error", cerr)
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

	entries, err := client.Discovery(imei)
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
