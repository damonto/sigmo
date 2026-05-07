package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/labstack/echo/v5"

	appauth "github.com/damonto/sigmo/internal/app/auth"
	"github.com/damonto/sigmo/internal/pkg/config"
)

func TestOTPSend(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config config.Config
		want   error
	}{
		{
			name: "rejects missing auth providers",
			config: config.Config{
				App: config.App{
					OTPRequired: true,
				},
			},
			want: errAuthProviderRequired,
		},
		{
			name: "rejects disabled auth provider",
			config: config.Config{
				App: config.App{
					OTPRequired:   true,
					AuthProviders: []string{"telegram"},
				},
				Channels: map[string]config.Channel{
					"telegram": {
						Enabled:    new(false),
						BotToken:   "draft-token",
						Recipients: config.Recipients{"123456"},
					},
				},
			},
			want: errAuthProviderUnavailable,
		},
		{
			name: "rejects missing configured channel",
			config: config.Config{
				App: config.App{
					OTPRequired:   true,
					AuthProviders: []string{"telegram"},
				},
			},
			want: errAuthProviderUnavailable,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := appauth.NewStore()
			configStore := config.NewStore(&tt.config)
			otp := newOTP(configStore, store)

			err := otp.Send(context.Background())
			if !errors.Is(err, tt.want) {
				t.Fatalf("Send() error = %v, want %v", err, tt.want)
			}
			if _, _, err := store.IssueOTP(); err != nil {
				t.Fatalf("IssueOTP() after rejected Send() error = %v", err)
			}
		})
	}
}

func TestEnabledAuthProviders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config config.Config
		want   []string
	}{
		{
			name: "normalizes provider names",
			config: config.Config{
				App: config.App{
					AuthProviders: []string{" Telegram "},
				},
				Channels: map[string]config.Channel{
					"telegram": {
						Enabled: new(true),
					},
				},
			},
			want: []string{"telegram"},
		},
		{
			name: "matches channel names case-insensitively",
			config: config.Config{
				App: config.App{
					AuthProviders: []string{"telegram"},
				},
				Channels: map[string]config.Channel{
					"Telegram": {
						Enabled: new(true),
					},
				},
			},
			want: []string{"telegram"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := enabledAuthProviders(tt.config)
			if err != nil {
				t.Fatalf("enabledAuthProviders() error = %v", err)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("enabledAuthProviders() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSendOTPRejectsInvalidAuthProviderConfig(t *testing.T) {
	t.Parallel()

	h := New(config.NewStore(&config.Config{
		App: config.App{
			OTPRequired:   true,
			AuthProviders: []string{"telegram"},
		},
		Channels: map[string]config.Channel{
			"telegram": {
				Enabled:    new(false),
				BotToken:   "draft-token",
				Recipients: config.Recipients{"123456"},
			},
		},
	}), appauth.NewStore())
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/otp", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := h.SendOTP(c); err != nil {
		t.Fatalf("SendOTP() error = %v", err)
	}
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestOTPVerify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		required  bool
		issueCode bool
		code      string
		wantErr   error
		wantToken bool
	}{
		{
			name:    "disabled rejects verify",
			code:    "000000",
			wantErr: errOTPNotRequired,
		},
		{
			name:     "enabled rejects invalid code",
			required: true,
			code:     "000000",
			wantErr:  errInvalidOTP,
		},
		{
			name:      "enabled accepts issued code",
			required:  true,
			issueCode: true,
			wantToken: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := appauth.NewStore()
			code := tt.code
			if tt.issueCode {
				issued, _, err := store.IssueOTP()
				if err != nil {
					t.Fatalf("IssueOTP() error = %v", err)
				}
				code = issued
			}

			configStore := config.NewStore(&config.Config{App: config.App{OTPRequired: tt.required}})
			otp := newOTP(configStore, store)
			token, err := otp.Verify(code)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Verify() error = %v, want %v", err, tt.wantErr)
			}
			if gotToken := token != ""; gotToken != tt.wantToken {
				t.Fatalf("Verify() token present = %v, want %v", gotToken, tt.wantToken)
			}
		})
	}
}
