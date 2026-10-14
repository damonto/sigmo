package storage

import (
	"encoding/json"
	"testing"
)

func TestAuthenticationMigration(t *testing.T) {
	tests := []struct {
		name, auth, channels, mode string
		wantError                  bool
	}{
		{"independent OTP", `{"otpRequired":true,"authProviders":["EMAIL","email"],"tokenValidityDays":45}`, `{"email":{"enabled":false,"host":"mail.example"},"telegram":{"token":"saved"}}`, "otp", false},
		{"anonymous", `{"otpRequired":false}`, `{}`, "none", false},
		{"missing channel", `{"otpRequired":true,"authProviders":["email"]}`, `{}`, "", true},
		{"invalid lifetime", `{"tokenValidityDays":181}`, `{}`, "", true},
		{"invalid channel flag", `{}`, `{"email":{"enabled":"yes"}}`, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := testStore(t)
			ctx := t.Context()
			for key, value := range map[string]string{"auth.settings": tt.auth, "notification.channels": tt.channels} {
				if _, err := s.db.ExecContext(ctx, `INSERT OR REPLACE INTO app_state(scope,key,value_json,updated_at) VALUES('global',?,?, '')`, key, value); err != nil {
					t.Fatal(err)
				}
			}
			tx, err := s.db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			err = migrateAuthentication(ctx, tx)
			if tt.wantError {
				if err == nil {
					t.Error("invalid legacy settings were accepted")
				}
				if err := tx.Rollback(); err != nil {
					t.Fatal(err)
				}
				var raw string
				if err := s.db.QueryRowContext(ctx, `SELECT value_json FROM app_state WHERE scope='global' AND key='auth.settings'`).Scan(&raw); err != nil {
					t.Fatal(err)
				}
				if raw != tt.auth {
					t.Fatal("rollback changed legacy authentication")
				}
				return
			}
			if err != nil {
				_ = tx.Rollback()
				t.Fatal(err)
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			read := func(key string, dst any) {
				t.Helper()
				var raw string
				if err := s.db.QueryRowContext(ctx, `SELECT value_json FROM app_state WHERE scope='global' AND key=?`, key).Scan(&raw); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal([]byte(raw), dst); err != nil {
					t.Fatal(err)
				}
			}
			var auth struct {
				Mode           string
				OTP            struct{ Channels []string }
				SessionMaxDays int
			}
			read("auth.settings", &auth)
			if auth.Mode != tt.mode {
				t.Fatalf("mode = %q", auth.Mode)
			}
			var count int
			if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM app_state WHERE key='notification.channels'`).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				t.Fatal("legacy configuration was retained")
			}
			if tt.mode != "otp" {
				return
			}
			var notifications struct{ Channels []string }
			read("notification.settings", &notifications)
			if len(notifications.Channels) != 1 || notifications.Channels[0] != "telegram" || len(auth.OTP.Channels) != 1 || auth.OTP.Channels[0] != "email" || auth.SessionMaxDays != 45 {
				t.Fatalf("migration mixed delivery selection: auth=%+v notifications=%+v", auth, notifications)
			}
			var channels map[string]map[string]json.RawMessage
			read("delivery.channels", &channels)
			if _, exists := channels["email"]["enabled"]; exists {
				t.Fatal("legacy enabled flag survived")
			}
			if string(channels["email"]["host"]) != `"mail.example"` {
				t.Fatal("delivery configuration changed")
			}
		})
	}
}
