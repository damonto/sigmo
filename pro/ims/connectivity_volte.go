//go:build ims

package ims

import (
	"context"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

func (c *Connectivity) VoLTEStatus(ctx context.Context, modem *mmodem.Modem) (VoLTEStatus, error) {
	if c == nil || c.volte == nil {
		return VoLTEStatus{}, ErrUnavailable
	}
	settings, err := c.volte.VoLTESettings(ctx, modem)
	if err != nil {
		return VoLTEStatus{}, err
	}
	status, err := c.volte.SessionStatus(ctx, modem, settings.Enabled)
	if err != nil {
		return VoLTEStatus{}, err
	}
	return VoLTEStatus{
		VoLTESettings:   settings,
		Connected:       status.Connected,
		State:           status.State,
		DurationSeconds: status.DurationSeconds,
	}, nil
}

func (c *Connectivity) ReplaceVoLTESettings(ctx context.Context, modem *mmodem.Modem, settings VoLTESettings) error {
	if c == nil || c.volte == nil {
		return ErrUnavailable
	}
	return c.change(modem, func() error {
		return updateVoLTESettings(ctx, modem, c.volte, settings)
	})
}
