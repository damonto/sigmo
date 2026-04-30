package auth

import (
	"context"
	"errors"
	"log/slog"

	"github.com/damonto/sigmo/internal/app/auth"
	"github.com/damonto/sigmo/internal/pkg/config"
	"github.com/damonto/sigmo/internal/pkg/notify"
	notifyevent "github.com/damonto/sigmo/internal/pkg/notify/event"
)

var (
	errAuthProviderRequired = errors.New("auth provider is required")
	errOTPNotRequired       = errors.New("otp is not required")
	errInvalidOTP           = errors.New("invalid otp")
)

type otp struct {
	cfg   *config.Config
	store *auth.Store
}

func newOTP(cfg *config.Config, store *auth.Store) *otp {
	return &otp{
		cfg:   cfg,
		store: store,
	}
}

func (o *otp) Required() bool {
	return o.cfg.App.OTPRequired
}

func (o *otp) Send(ctx context.Context) error {
	if !o.Required() {
		return nil
	}
	if len(o.cfg.App.AuthProviders) == 0 {
		return errAuthProviderRequired
	}
	code, _, err := o.store.IssueOTP()
	if err != nil {
		slog.Error("failed to issue OTP", "error", err)
		return err
	}
	notifier, err := notify.New(o.cfg)
	if err != nil {
		slog.Error("failed to create notifier", "error", err)
		return err
	}
	if err := notifier.Send(ctx, notifyevent.OTPEvent{Code: code}, o.cfg.App.AuthProviders...); err != nil {
		slog.Error("failed to send OTP notification", "error", err)
		return err
	}
	return nil
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
		slog.Error("failed to issue token", "error", err)
		return "", err
	}
	return token, nil
}
