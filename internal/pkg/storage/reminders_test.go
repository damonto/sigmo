package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestReminderStorage(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	base := time.Date(2026, 7, 18, 10, 30, 0, 0, time.UTC)
	repeat := 7

	first := Reminder{
		ProfileType: "esim",
		ProfileID:   "8985200012345678901",
		ModemID:     "modem-1",
		SEID:        "se0",
		ProfileName: "Travel",
		NextAt:      base,
		RepeatDays:  &repeat,
		Content:     "Renew the plan",
	}
	if err := store.UpsertReminder(ctx, first); err != nil {
		t.Fatalf("UpsertReminder() error = %v", err)
	}

	t.Run("get", func(t *testing.T) {
		got, ok, err := store.GetReminder(ctx, first.ProfileType, first.ProfileID)
		if err != nil {
			t.Fatalf("GetReminder() error = %v", err)
		}
		if !ok {
			t.Fatal("GetReminder() found = false, want true")
		}
		if !got.NextAt.Equal(base) || got.RepeatDays == nil || *got.RepeatDays != repeat || got.Content != first.Content {
			t.Fatalf("GetReminder() = %+v, want %+v", got, first)
		}
	})

	t.Run("next and due", func(t *testing.T) {
		later := first
		later.ProfileID = "8985200012345678902"
		later.NextAt = base.Add(500 * time.Millisecond)
		if err := store.UpsertReminder(ctx, later); err != nil {
			t.Fatalf("UpsertReminder(later) error = %v", err)
		}
		t.Cleanup(func() {
			if err := store.DeleteReminder(ctx, later.ProfileType, later.ProfileID); err != nil {
				t.Errorf("DeleteReminder(later) error = %v", err)
			}
		})

		nextAt, ok, err := store.NextReminderAt(ctx)
		if err != nil {
			t.Fatalf("NextReminderAt() error = %v", err)
		}
		if !ok || !nextAt.Equal(base) {
			t.Fatalf("NextReminderAt() = (%v, %v), want (%v, true)", nextAt, ok, base)
		}
		due, err := store.DueReminders(ctx, base.Add(-time.Minute))
		if err != nil {
			t.Fatalf("DueReminders(before) error = %v", err)
		}
		if len(due) != 0 {
			t.Fatalf("DueReminders(before) length = %d, want 0", len(due))
		}
		due, err = store.DueReminders(ctx, base.Add(250*time.Millisecond))
		if err != nil {
			t.Fatalf("DueReminders(at) error = %v", err)
		}
		if len(due) != 1 || due[0].ProfileID != first.ProfileID {
			t.Fatalf("DueReminders(at) = %+v, want one reminder", due)
		}
	})

	t.Run("upsert replaces profile reminder", func(t *testing.T) {
		next := first
		next.NextAt = base.Add(24 * time.Hour)
		next.RepeatDays = nil
		next.Content = "Updated"
		if err := store.UpsertReminder(ctx, next); err != nil {
			t.Fatalf("UpsertReminder(update) error = %v", err)
		}
		got, ok, err := store.GetReminder(ctx, next.ProfileType, next.ProfileID)
		if err != nil || !ok {
			t.Fatalf("GetReminder(update) = (%+v, %v, %v)", got, ok, err)
		}
		if got.RepeatDays != nil || got.Content != next.Content || !got.NextAt.Equal(next.NextAt) {
			t.Fatalf("GetReminder(update) = %+v, want %+v", got, next)
		}
	})

	t.Run("delete", func(t *testing.T) {
		if err := store.DeleteReminder(ctx, first.ProfileType, first.ProfileID); err != nil {
			t.Fatalf("DeleteReminder() error = %v", err)
		}
		_, ok, err := store.GetReminder(ctx, first.ProfileType, first.ProfileID)
		if err != nil {
			t.Fatalf("GetReminder(after delete) error = %v", err)
		}
		if ok {
			t.Fatal("GetReminder(after delete) found = true, want false")
		}
	})
}

func TestReminderRevisionMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite3", "file:"+path)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	_, err = db.Exec(`
		CREATE TABLE reminders (
			profile_type TEXT NOT NULL,
			profile_id TEXT NOT NULL,
			modem_id TEXT NOT NULL,
			se_id TEXT NOT NULL,
			profile_name TEXT NOT NULL,
			next_at TEXT NOT NULL,
			repeat_days INTEGER,
			content TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (profile_type, profile_id)
		);
		INSERT INTO reminders (
			profile_type, profile_id, modem_id, se_id, profile_name,
			next_at, repeat_days, content, created_at, updated_at
		) VALUES (
			'esim', '8985200012345678901', 'modem-1', 'se0', 'Travel',
			'2026-07-18T10:30:00.000000000Z', NULL, 'Renew',
			'2026-07-18T09:00:00Z', '2026-07-18T09:00:00Z'
		)
	`)
	if err != nil {
		_ = db.Close()
		t.Fatalf("create legacy reminders table: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("legacy db Close() error = %v", err)
	}

	store, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("Store.Close() error = %v", err)
		}
	})
	got, ok, err := store.GetReminder(context.Background(), "esim", "8985200012345678901")
	if err != nil || !ok {
		t.Fatalf("GetReminder() = (%+v, %v, %v), want migrated reminder", got, ok, err)
	}
	if got.Revision != 1 {
		t.Fatalf("Revision = %d, want 1", got.Revision)
	}
}

func TestReminderStorageValidation(t *testing.T) {
	base := Reminder{
		ProfileType: "psim",
		ProfileID:   "8985200012345678901",
		NextAt:      time.Date(2026, 7, 18, 10, 30, 0, 0, time.UTC),
		Content:     "Top up",
	}
	zero := 0
	negative := -1
	tooLarge := maxReminderRepeatDays + 1
	tests := []struct {
		name   string
		mutate func(*Reminder)
	}{
		{name: "missing profile type", mutate: func(value *Reminder) { value.ProfileType = "" }},
		{name: "missing profile id", mutate: func(value *Reminder) { value.ProfileID = "" }},
		{name: "missing time", mutate: func(value *Reminder) { value.NextAt = time.Time{} }},
		{name: "zero repeat", mutate: func(value *Reminder) { value.RepeatDays = &zero }},
		{name: "negative repeat", mutate: func(value *Reminder) { value.RepeatDays = &negative }},
		{name: "repeat overflow", mutate: func(value *Reminder) { value.RepeatDays = &tooLarge }},
		{name: "empty content", mutate: func(value *Reminder) { value.Content = "  " }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value := base
			tt.mutate(&value)
			if err := validateReminderRecord(value); err == nil {
				t.Fatal("validateReminderRecord() error = nil, want error")
			}
		})
	}
}

func TestReminderClaimProtectsNewerRevision(t *testing.T) {
	tests := []struct {
		name         string
		updateBefore bool
		finish       string
		wantClaim    bool
	}{
		{name: "edit before claim skips stale delivery", updateBefore: true},
		{name: "edit after claim blocks stale delete", finish: "delete", wantClaim: true},
		{name: "edit after claim blocks stale advance", finish: "advance", wantClaim: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			store := testStore(t)
			original := Reminder{
				ProfileType: "esim",
				ProfileID:   "8985200012345678901",
				ModemID:     "modem-1",
				SEID:        "se0",
				ProfileName: "Travel",
				NextAt:      time.Now().Add(-time.Minute),
				Content:     "Original",
			}
			if err := store.UpsertReminder(ctx, original); err != nil {
				t.Fatalf("UpsertReminder(original) error = %v", err)
			}
			due, err := store.DueReminders(ctx, time.Now())
			if err != nil || len(due) != 1 {
				t.Fatalf("DueReminders() = (%+v, %v), want one reminder", due, err)
			}

			replacement := original
			replacement.NextAt = time.Now().Add(time.Hour)
			replacement.Content = "Replacement"
			if tt.updateBefore {
				if err := store.UpsertReminder(ctx, replacement); err != nil {
					t.Fatalf("UpsertReminder(replacement) error = %v", err)
				}
			}
			claimed, ok, err := store.ClaimReminder(ctx, due[0])
			if err != nil {
				t.Fatalf("ClaimReminder() error = %v", err)
			}
			if ok != tt.wantClaim {
				t.Fatalf("ClaimReminder() claimed = %v, want %v", ok, tt.wantClaim)
			}
			if ok {
				if err := store.UpsertReminder(ctx, replacement); err != nil {
					t.Fatalf("UpsertReminder(replacement) error = %v", err)
				}
				var applied bool
				switch tt.finish {
				case "delete":
					applied, err = store.DeleteClaimedReminder(ctx, claimed)
				case "advance":
					applied, err = store.AdvanceClaimedReminder(ctx, claimed, time.Now().Add(24*time.Hour))
				}
				if err != nil {
					t.Fatalf("finish claimed reminder error = %v", err)
				}
				if applied {
					t.Fatal("finish claimed reminder applied = true, want false")
				}
			}

			got, exists, err := store.GetReminder(ctx, replacement.ProfileType, replacement.ProfileID)
			if err != nil || !exists {
				t.Fatalf("GetReminder() = (%+v, %v, %v), want replacement", got, exists, err)
			}
			if got.Content != replacement.Content || !got.NextAt.Equal(replacement.NextAt) {
				t.Fatalf("GetReminder() = %+v, want replacement %+v", got, replacement)
			}
		})
	}
}
