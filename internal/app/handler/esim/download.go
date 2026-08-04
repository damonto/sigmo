package esim

import (
	"context"
	"fmt"

	elpa "github.com/damonto/euicc-go/lpa"

	"github.com/damonto/sigmo/internal/pkg/lpa"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

func (p *provisioning) Download(ctx context.Context, modem *mmodem.Modem, seID string, activationCode *elpa.ActivationCode, opts *elpa.DownloadOptions) error {
	current := p.store.Snapshot()
	se, err := lpa.ResolveSE(ctx, modem, seID)
	if err != nil {
		return fmt.Errorf("resolve eUICC SE: %w", err)
	}
	client, err := lpa.NewWithAID(ctx, modem, &current, se.AID)
	if err != nil {
		return fmt.Errorf("create LPA client: %w", err)
	}
	defer func() {
		if cerr := client.Close(); cerr != nil {
			client.Logger().Warn("close LPA client", "error", cerr)
		}
	}()

	if err := client.Download(ctx, activationCode, opts); err != nil {
		return fmt.Errorf("download profile: %w", err)
	}
	return nil
}
