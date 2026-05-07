package esim

import (
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

func (p *provisioning) Discover(modem *mmodem.Modem) ([]DiscoverResponse, error) {
	cfg := p.store.Snapshot()
	client, err := lpa.New(modem, &cfg)
	if err != nil {
		slog.Error("failed to create LPA client", "modem", modem.EquipmentIdentifier, "error", err)
		return nil, err
	}
	defer func() {
		if cerr := client.Close(); cerr != nil {
			slog.Warn("failed to close LPA client", "error", cerr)
		}
	}()

	imeiValue, err := modem.ThreeGPP().IMEI()
	if err != nil {
		slog.Error("failed to read modem IMEI", "modem", modem.EquipmentIdentifier, "error", err)
		return nil, err
	}
	imei, err := sgp22.NewIMEI(imeiValue)
	if err != nil {
		slog.Error("invalid IMEI", "modem", modem.EquipmentIdentifier, "imei", imeiValue, "error", err)
		return nil, err
	}

	entries, err := client.Discover(imei)
	if err != nil {
		slog.Error("failed to discover profiles", "modem", modem.EquipmentIdentifier, "error", err)
		return nil, err
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
