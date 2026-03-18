package auth

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/damonto/sigmo/internal/app/auth"
	"github.com/damonto/sigmo/internal/pkg/config"
	"github.com/damonto/sigmo/internal/pkg/notify"
)

var (
	errAuthProviderRequired = errors.New("auth provider is required")
	errInvalidOTP           = errors.New("invalid otp")
)

type Service struct {
	cfg   *config.Config
	store *auth.Store
}

func NewService(cfg *config.Config, store *auth.Store) *Service {
	return &Service{
		cfg:   cfg,
		store: store,
	}
}

func (s *Service) OTPRequired() bool {
	return s.cfg.App.OTPRequired
}

func (s *Service) SendOTP() error {
	if !s.OTPRequired() {
		return nil
	}
	if len(s.cfg.App.AuthProviders) == 0 {
		return errAuthProviderRequired
	}
	code, _, err := s.store.IssueOTP()
	if err != nil {
		slog.Error("failed to issue OTP", "error", err)
		return err
	}
	notifier, err := notify.New(s.cfg)
	if err != nil {
		slog.Error("failed to create notifier", "error", err)
		return err
	}
	if err := notifier.Send(notify.TextMessage{Text: fmt.Sprintf("Your verification code is %s", code)}, s.cfg.App.AuthProviders...); err != nil {
		slog.Error("failed to send OTP notification", "error", err)
		return err
	}
	return nil
}

func (s *Service) VerifyOTP(code string) (string, error) {
	if s.OTPRequired() && !s.store.VerifyOTP(code) {
		return "", errInvalidOTP
	}
	token, _, err := s.store.IssueToken()
	if err != nil {
		slog.Error("failed to issue token", "error", err)
		return "", err
	}
	return token, nil
}
