//go:build !wifi_calling

package wificalling

import (
	"context"

	"github.com/damonto/sigmo/internal/pkg/storage"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

type Config struct {
	Store      *storage.Store
	OnIncoming IncomingSMSFunc
}

type coordinator struct {
	settings *SettingsStore
}

func New(cfg Config) Coordinator {
	return &coordinator{settings: NewSettingsStore(cfg.Store)}
}

func (c *coordinator) Run(ctx context.Context, registry *mmodem.Registry) error {
	<-ctx.Done()
	return nil
}

func (c *coordinator) Settings(ctx context.Context, modem *mmodem.Modem) (Settings, error) {
	profileID, err := modem.ProfileID(ctx)
	if err != nil {
		return Settings{}, err
	}
	return c.settings.Get(ctx, profileID)
}

func (c *coordinator) UpdateSettings(ctx context.Context, modem *mmodem.Modem, settings Settings) error {
	profileID, err := modem.ProfileID(ctx)
	if err != nil {
		return err
	}
	return c.settings.Put(ctx, profileID, settings)
}

func (c *coordinator) Status(ctx context.Context, modem *mmodem.Modem) (Status, error) {
	settings, err := c.Settings(ctx, modem)
	if err != nil {
		return Status{}, err
	}
	return Status{Settings: settings}, nil
}

func (c *coordinator) SendSMS(ctx context.Context, modem *mmodem.Modem, to string, text string) (storage.Message, error) {
	return storage.Message{}, ErrUnavailable
}

func (c *coordinator) ExecuteUSSD(ctx context.Context, modem *mmodem.Modem, action string, code string) (string, error) {
	return "", ErrUnavailable
}
