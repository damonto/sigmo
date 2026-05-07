package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/damonto/sigmo/internal/app/auth"
	"github.com/damonto/sigmo/internal/pkg/config"
	"github.com/damonto/sigmo/internal/pkg/notify"
	notifyevent "github.com/damonto/sigmo/internal/pkg/notify/event"
)

var (
	errAuthProviderRequired    = errors.New("auth provider is required")
	errAuthProviderUnavailable = errors.New("auth provider must be an enabled channel")
	errOTPNotRequired          = errors.New("otp is not required")
	errInvalidOTP              = errors.New("invalid otp")
)

type otp struct {
	configStore *config.Store
	store       *auth.Store
}

func newOTP(configStore *config.Store, store *auth.Store) *otp {
	return &otp{
		configStore: configStore,
		store:       store,
	}
}

func (o *otp) Required() bool {
	return o.configStore.OTPRequired()
}

func (o *otp) Send(ctx context.Context) error {
	cfg := o.configStore.Snapshot()
	if !cfg.App.OTPRequired {
		return nil
	}
	authProviders, err := enabledAuthProviders(cfg)
	if err != nil {
		return err
	}
	notifier, err := notify.New(&cfg)
	if err != nil {
		slog.Error("create notifier", "error", err)
		return err
	}
	code, _, err := o.store.IssueOTP()
	if err != nil {
		slog.Error("issue OTP", "error", err)
		return err
	}
	if err := notifier.Send(ctx, notifyevent.OTPEvent{Code: code}, authProviders...); err != nil {
		slog.Error("send OTP notification", "error", err)
		return err
	}
	return nil
}

func enabledAuthProviders(cfg config.Config) ([]string, error) {
	if len(cfg.App.AuthProviders) == 0 {
		return nil, errAuthProviderRequired
	}
	providers := make([]string, 0, len(cfg.App.AuthProviders))
	for _, provider := range cfg.App.AuthProviders {
		name := strings.ToLower(strings.TrimSpace(provider))
		if name == "" {
			return nil, errAuthProviderRequired
		}
		if !channelEnabled(cfg.Channels, name) {
			return nil, fmt.Errorf("%w: %s", errAuthProviderUnavailable, name)
		}
		providers = append(providers, name)
	}
	return providers, nil
}

func channelEnabled(channels map[string]config.Channel, target string) bool {
	for name, channel := range channels {
		if strings.EqualFold(strings.TrimSpace(name), target) {
			return channel.IsEnabled()
		}
	}
	return false
}

func (o *otp) Verify(code string) (string, error) {
	if !o.Required() {
		return "", errOTPNotRequired
	}
	if !o.store.VerifyOTP(code) {
		return "", errInvalidOTP
	}
	token, _, err := o.store.IssueToken()
	if err != nil {
		slog.Error("issue token", "error", err)
		return "", err
	}
	return token, nil
}
