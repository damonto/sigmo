package storage

import (
	"encoding/json"
	"errors"
	"maps"
	"testing"
)

func TestAppStateBatchAtomicity(t *testing.T) {
	for _, tt := range []struct {
		name         string
		invalidJSON  bool
		rejectSecond bool
	}{
		{name: "commit all settings"},
		{name: "reject invalid JSON", invalidJSON: true},
		{name: "roll back first write when second fails", rejectSecond: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := testStore(t)
			ctx := t.Context()
			for _, key := range []string{"auth", "channels"} {
				if err := s.Put(ctx, "settings", key, "old"); err != nil {
					t.Fatal(err)
				}
			}
			if tt.rejectSecond {
				// Fail after one write regardless of the map's iteration order.
				if _, err := s.db.ExecContext(ctx, `
					CREATE TRIGGER reject_second_setting BEFORE UPDATE ON app_state
					WHEN (SELECT COUNT(*) FROM app_state WHERE scope = 'settings' AND value_json = '"new"') = 1
					BEGIN SELECT RAISE(ABORT, 'reject second setting'); END
				`); err != nil {
					t.Fatal(err)
				}
			}
			values := map[string]json.RawMessage{"auth": json.RawMessage(`"new"`), "channels": json.RawMessage(`"new"`)}
			if tt.invalidJSON {
				values["channels"] = json.RawMessage(`{`)
			}
			err := s.PutBatch(ctx, "settings", values)
			wantError := tt.invalidJSON || tt.rejectSecond
			if (err != nil) != wantError {
				t.Fatalf("PutBatch() error = %v, want error %v", err, wantError)
			}
			want := "new"
			if wantError {
				want = "old"
			}
			for _, key := range []string{"auth", "channels"} {
				var got string
				if err := s.Get(ctx, "settings", key, &got); err != nil {
					t.Fatal(err)
				}
				if got != want {
					t.Errorf("Get(%q) = %q, want %q", key, got, want)
				}
			}
		})
	}
}

func TestAppStateDeletionScope(t *testing.T) {
	for _, tt := range []struct {
		name  string
		scope string
		key   string
	}{
		{name: "single key", scope: " profile:a ", key: " enabled "},
		{name: "whole scope", scope: " profile:a "},
		{name: "empty scope is a no-op"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := testStore(t)
			ctx := t.Context()
			for _, scope := range []string{"profile:a", "profile:b", "modem:a"} {
				for _, key := range []string{"enabled", "other"} {
					if err := s.Put(ctx, scope, key, true); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := s.Delete(ctx, tt.scope, tt.key); err != nil {
				t.Fatal(err)
			}
			got, err := s.ListRaw(ctx, " profile: ", " enabled ")
			if err != nil {
				t.Fatal(err)
			}
			want := map[string]string{"profile:b": "true"}
			if tt.scope == "" {
				want["profile:a"] = "true"
			}
			if !maps.Equal(got, want) {
				t.Errorf("ListRaw() = %v, want %v", got, want)
			}
			for _, scope := range []string{"profile:a", "profile:b", "modem:a"} {
				var value bool
				err := s.Get(ctx, scope, "other", &value)
				if scope == "profile:a" && tt.scope != "" && tt.key == "" {
					if !errors.Is(err, ErrNotFound) {
						t.Errorf("Get(%q, other) error = %v, want ErrNotFound", scope, err)
					}
					continue
				}
				if err != nil || !value {
					t.Errorf("Get(%q, other) = %v, %v, want true, nil", scope, value, err)
				}
			}
		})
	}
}
