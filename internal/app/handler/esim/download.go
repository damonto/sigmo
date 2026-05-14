package esim

import (
	"context"
	"fmt"
	"log/slog"

	elpa "github.com/damonto/euicc-go/lpa"

	"github.com/damonto/sigmo/internal/pkg/lpa"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

func (p *provisioning) Download(ctx context.Context, modem *mmodem.Modem, activationCode *elpa.ActivationCode, opts *elpa.DownloadOptions) error {
	cfg := p.store.Snapshot()
	client, err := lpa.New(modem, &cfg)
	if err != nil {
		return fmt.Errorf("create LPA client: %w", err)
	}
	defer func() {
		if cerr := client.Close(); cerr != nil {
			slog.Warn("close LPA client", "error", cerr)
		}
	}()

	if err := client.Download(ctx, activationCode, opts); err != nil {
		return fmt.Errorf("download profile: %w", err)
	}
	return nil
}
