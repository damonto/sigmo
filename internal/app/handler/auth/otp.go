package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/damonto/sigmo/internal/app/auth"
	"github.com/damonto/sigmo/internal/pkg/notify"
	notifyevent "github.com/damonto/sigmo/internal/pkg/notify/event"
	"github.com/damonto/sigmo/internal/pkg/settings"
)

var (
	errAuthProviderRequired    = errors.New("auth provider is required")
	errAuthProviderUnavailable = errors.New("auth provider must be an enabled channel")
	errOTPNotRequired          = errors.New("otp is not required")
	errInvalidOTP              = errors.New("invalid otp")
)

type otp struct {
	settingsStore *settings.Store
	store         *auth.Store
}

func newOTP(settingsStore *settings.Store, store *auth.Store) *otp {
	return &otp{
		settingsStore: settingsStore,
		store:         store,
	}
}

func (o *otp) Required() bool {
	return o.settingsStore.OTPRequired()
}

func (o *otp) Send(ctx context.Context) error {
	current := o.settingsStore.Snapshot()
	if !current.Auth.OTPRequired {
		return nil
	}
	authProviders, err := enabledAuthProviders(current)
	if err != nil {
		return err
	}
	notifier, err := notify.New(&current)
	if err != nil {
		return fmt.Errorf("create notifier: %w", err)
	}
	code, _, err := o.store.IssueOTP()
	if err != nil {
		return fmt.Errorf("issue OTP: %w", err)
	}
	if err := notifier.Send(ctx, notifyevent.OTPEvent{Code: code}, authProviders...); err != nil {
		return fmt.Errorf("send OTP notification: %w", err)
	}
	return nil
}

func enabledAuthProviders(current settings.Settings) ([]string, error) {
	if len(current.Auth.AuthProviders) == 0 {
		return nil, errAuthProviderRequired
	}
	providers := make([]string, 0, len(current.Auth.AuthProviders))
	for _, provider := range current.Auth.AuthProviders {
		name := strings.ToLower(strings.TrimSpace(provider))
		if name == "" {
			return nil, errAuthProviderRequired
		}
		if !channelEnabled(current.Channels, name) {
			return nil, fmt.Errorf("%w: %s", errAuthProviderUnavailable, name)
		}
		providers = append(providers, name)
	}
	return providers, nil
}

func channelEnabled(channels map[string]settings.Channel, target string) bool {
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
		return "", fmt.Errorf("issue token: %w", err)
	}
	return token, nil
}
