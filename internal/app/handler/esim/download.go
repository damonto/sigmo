package esim

import (
	"context"
	"log/slog"

	elpa "github.com/damonto/euicc-go/lpa"

	"github.com/damonto/sigmo/internal/pkg/lpa"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

func (s *Service) Download(ctx context.Context, modem *mmodem.Modem, activationCode *elpa.ActivationCode, opts *elpa.DownloadOptions) error {
	client, err := lpa.New(modem, s.cfg)
	if err != nil {
		slog.Error("failed to create LPA client", "modem", modem.EquipmentIdentifier, "error", err)
		return err
	}
	defer func() {
		if cerr := client.Close(); cerr != nil {
			slog.Warn("failed to close LPA client", "error", cerr)
		}
	}()

	if err := client.Download(ctx, activationCode, opts); err != nil {
		slog.Error("failed to download profile", "modem", modem.EquipmentIdentifier, "error", err)
		return err
	}
	return nil
}
