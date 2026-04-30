package auth

import (
	"errors"
	"testing"

	appauth "github.com/damonto/sigmo/internal/app/auth"
	"github.com/damonto/sigmo/internal/pkg/config"
)

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

			otp := newOTP(&config.Config{App: config.App{OTPRequired: tt.required}}, store)
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
