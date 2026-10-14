package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Frozen JSON snapshots intentionally do not depend on the current settings package.
func migrateAuthentication(ctx context.Context, tx *sql.Tx) error {
	var auth struct {
		OTPRequired       bool     `json:"otpRequired"`
		AuthProviders     []string `json:"authProviders"`
		TokenValidityDays int      `json:"tokenValidityDays"`
	}
	read := func(key string, dst any) error {
		var raw string
		err := tx.QueryRowContext(ctx, `SELECT value_json FROM app_state WHERE scope='global' AND key=?`, key).Scan(&raw)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		return json.Unmarshal([]byte(raw), dst)
	}
	if err := read("auth.settings", &auth); err != nil {
		return fmt.Errorf("read legacy authentication: %w", err)
	}
	channels := map[string]map[string]json.RawMessage{}
	if err := read("notification.channels", &channels); err != nil {
		return fmt.Errorf("read legacy channels: %w", err)
	}
	notifications := []string{}
	for name, channel := range channels {
		enabled := true
		if raw, ok := channel["enabled"]; ok && string(raw) != "null" {
			if err := json.Unmarshal(raw, &enabled); err != nil {
				return fmt.Errorf("read channel %s enabled flag: %w", name, err)
			}
		}
		if enabled {
			notifications = append(notifications, name)
		}
		delete(channel, "enabled")
	}
	slices.Sort(notifications)
	providers := []string{}
	for _, name := range auth.AuthProviders {
		name = strings.ToLower(strings.TrimSpace(name))
		if _, ok := channels[name]; !ok {
			return fmt.Errorf("OTP references missing channel %q", name)
		}
		if !slices.Contains(providers, name) {
			providers = append(providers, name)
		}
	}
	slices.Sort(providers)
	mode := "none"
	if auth.OTPRequired {
		if len(providers) == 0 {
			return errors.New("legacy OTP has no delivery channels")
		}
		mode = "otp"
	}
	if auth.TokenValidityDays == 0 {
		auth.TokenValidityDays = 30
	}
	if auth.TokenValidityDays < 1 || auth.TokenValidityDays > 180 {
		return errors.New("invalid legacy token validity")
	}
	newAuth := struct {
		Mode           string `json:"mode"`
		SessionMaxDays int    `json:"sessionMaxDays"`
		OTP            struct {
			Channels []string `json:"channels"`
		} `json:"otp"`
	}{Mode: mode, SessionMaxDays: auth.TokenValidityDays}
	newAuth.OTP.Channels = providers
	entries := []struct {
		key   string
		value any
	}{
		{"auth.settings", newAuth}, {"delivery.channels", channels},
		{"notification.settings", struct {
			Channels []string `json:"channels"`
		}{notifications}},
	}
	for _, entry := range entries {
		data, err := json.Marshal(entry.value)
		if err != nil {
			return fmt.Errorf("encode migrated %s: %w", entry.key, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO app_state(scope,key,value_json,updated_at) VALUES('global',?,?,?)
		ON CONFLICT(scope,key) DO UPDATE SET value_json=excluded.value_json,updated_at=excluded.updated_at`, entry.key, string(data), nowText()); err != nil {
			return fmt.Errorf("save migrated %s: %w", entry.key, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM app_state WHERE scope='global' AND key='notification.channels'`); err != nil {
		return fmt.Errorf("remove legacy channels: %w", err)
	}
	return nil
}
