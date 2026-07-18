package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"testing"

	"github.com/labstack/echo/v5"

	appauth "github.com/damonto/sigmo/internal/app/auth"
	"github.com/damonto/sigmo/internal/pkg/settings"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

func TestOTPSend(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		settings settings.Settings
		want     error
	}{
		{
			name: "rejects missing auth providers",
			settings: settings.Settings{
				Auth: settings.Auth{
					OTPRequired: true,
				},
			},
			want: errAuthProviderRequired,
		},
		{
			name: "rejects disabled auth provider",
			settings: settings.Settings{
				Auth: settings.Auth{
					OTPRequired:   true,
					AuthProviders: []string{"telegram"},
				},
				Channels: map[string]settings.Channel{
					"telegram": {
						Enabled:    new(false),
						BotToken:   "draft-token",
						Recipients: settings.Recipients{"123456"},
					},
				},
			},
			want: errAuthProviderUnavailable,
		},
		{
			name: "rejects missing configured channel",
			settings: settings.Settings{
				Auth: settings.Auth{
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

			store := newAuthTestStore(t)
			settingsStore := settings.NewMemoryStore(&tt.settings)
			otp := newOTP(settingsStore, store)

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
		name     string
		settings settings.Settings
		want     []string
	}{
		{
			name: "normalizes provider names",
			settings: settings.Settings{
				Auth: settings.Auth{
					AuthProviders: []string{" Telegram "},
				},
				Channels: map[string]settings.Channel{
					"telegram": {
						Enabled: new(true),
					},
				},
			},
			want: []string{"telegram"},
		},
		{
			name: "matches channel names case-insensitively",
			settings: settings.Settings{
				Auth: settings.Auth{
					AuthProviders: []string{"telegram"},
				},
				Channels: map[string]settings.Channel{
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

			got, err := enabledAuthProviders(tt.settings)
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

	h := New(settings.NewMemoryStore(&settings.Settings{
		Auth: settings.Auth{
			OTPRequired:   true,
			AuthProviders: []string{"telegram"},
		},
		Channels: map[string]settings.Channel{
			"telegram": {
				Enabled:    new(false),
				BotToken:   "draft-token",
				Recipients: settings.Recipients{"123456"},
			},
		},
	}), newAuthTestStore(t))
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

			store := newAuthTestStore(t)
			code := tt.code
			if tt.issueCode {
				issued, _, err := store.IssueOTP()
				if err != nil {
					t.Fatalf("IssueOTP() error = %v", err)
				}
				code = issued
			}

			settingsStore := settings.NewMemoryStore(&settings.Settings{Auth: settings.Auth{OTPRequired: tt.required}})
			otp := newOTP(settingsStore, store)
			token, err := otp.Verify(t.Context(), code)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Verify() error = %v, want %v", err, tt.wantErr)
			}
			if gotToken := token != ""; gotToken != tt.wantToken {
				t.Fatalf("Verify() token present = %v, want %v", gotToken, tt.wantToken)
			}
			if tt.wantToken {
				if _, err := otp.Verify(t.Context(), code); !errors.Is(err, errInvalidOTP) {
					t.Fatalf("Verify() reused OTP error = %v, want %v", err, errInvalidOTP)
				}
			}
		})
	}
}

func newAuthTestStore(t *testing.T) *appauth.Store {
	t.Helper()
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "sigmo.db"))
	if err != nil {
		t.Fatalf("storage.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})
	store, err := appauth.NewStore(db)
	if err != nil {
		t.Fatalf("appauth.NewStore() error = %v", err)
	}
	return store
}
