package esim

import (
	"context"

	elpa "github.com/damonto/euicc-go/lpa"
	sgp22 "github.com/damonto/euicc-go/v2"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

// Discover returns profiles advertised for the selected secure element.
func (s *Service) Discover(ctx context.Context, device *mmodem.Modem, seID string) ([]Discovery, error) {
	return s.provisioning.Discovery(ctx, device, seID)
}

// Download provisions a profile with caller-owned interaction callbacks.
func (s *Service) Download(ctx context.Context, device *mmodem.Modem, seID string, code *elpa.ActivationCode, opts *elpa.DownloadOptions) error {
	return s.provisioning.Download(ctx, device, seID, code, opts)
}

// UpdateNickname changes a profile's display nickname.
func (s *Service) UpdateNickname(ctx context.Context, device *mmodem.Modem, seID string, iccid sgp22.ICCID, nickname string) error {
	return s.profile.UpdateNickname(ctx, device, seID, iccid, nickname)
}
