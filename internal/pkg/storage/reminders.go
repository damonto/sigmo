package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/damonto/sigmo/internal/pkg/storage/query"
)

// Reminder keeps the SIM identity and revision used to claim scheduled notifications.
type Reminder struct {
	ProfileType string
	ProfileID   string
	ModemID     string
	SEID        string
	ProfileName string
	NextAt      time.Time
	RepeatDays  *int
	Content     string
	Revision    int64
}

const maxReminderRepeatDays = 3650

// UpsertReminder replaces the schedule and increments its revision.
func (s *Store) UpsertReminder(ctx context.Context, reminder Reminder) error {
	if err := validateReminderRecord(reminder); err != nil {
		return err
	}
	now := nowText()
	var repeatDays sql.NullInt64
	if reminder.RepeatDays != nil {
		repeatDays = sql.NullInt64{Int64: int64(*reminder.RepeatDays), Valid: true}
	}
	err := query.New(s.db).UpsertReminder(ctx, query.UpsertReminderParams{
		ProfileType: reminder.ProfileType,
		ProfileID:   reminder.ProfileID,
		ModemID:     reminder.ModemID,
		SEID:        reminder.SEID,
		ProfileName: reminder.ProfileName,
		NextAt:      reminderTimeText(reminder.NextAt),
		RepeatDays:  repeatDays,
		Content:     reminder.Content,
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		return fmt.Errorf("save reminder: %w", err)
	}
	return nil
}

// Reminder reports whether the requested SIM has a reminder.
func (s *Store) Reminder(ctx context.Context, profileType, profileID string) (Reminder, bool, error) {
	profileType = strings.TrimSpace(profileType)
	profileID = strings.TrimSpace(profileID)
	if profileType == "" || profileID == "" {
		return Reminder{}, false, errors.New("reminder profile identity is required")
	}

	row, err := query.New(s.db).Reminder(ctx, query.ReminderParams{
		ProfileType: profileType,
		ProfileID:   profileID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return Reminder{}, false, nil
	}
	if err != nil {
		return Reminder{}, false, fmt.Errorf("read reminder: %w", err)
	}
	reminder, err := reminderFromRow(row)
	if err != nil {
		return Reminder{}, false, err
	}
	return reminder, true, nil
}

// DueReminders returns reminders in scheduled order, preserving subsecond precision.
func (s *Store) DueReminders(ctx context.Context, now time.Time) ([]Reminder, error) {
	rows, err := query.New(s.db).DueReminders(ctx, reminderTimeText(now))
	if err != nil {
		return nil, fmt.Errorf("list due reminders: %w", err)
	}
	reminders := make([]Reminder, 0, len(rows))
	for _, row := range rows {
		reminder, err := reminderFromRow(row)
		if err != nil {
			return nil, err
		}
		reminders = append(reminders, reminder)
	}
	return reminders, nil
}

// NextReminderAt reports the earliest scheduled notification time.
func (s *Store) NextReminderAt(ctx context.Context) (time.Time, bool, error) {
	raw, err := query.New(s.db).NextReminderAt(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("read next reminder: %w", err)
	}
	nextAt, err := parseReminderTime(raw)
	if err != nil {
		return time.Time{}, false, err
	}
	return nextAt, true, nil
}

// DeleteReminder removes the reminder if it exists.
func (s *Store) DeleteReminder(ctx context.Context, profileType, profileID string) error {
	profileType = strings.TrimSpace(profileType)
	profileID = strings.TrimSpace(profileID)
	if profileType == "" || profileID == "" {
		return errors.New("reminder profile identity is required")
	}
	if err := query.New(s.db).DeleteReminder(ctx, query.DeleteReminderParams{
		ProfileType: profileType,
		ProfileID:   profileID,
	}); err != nil {
		return fmt.Errorf("delete reminder: %w", err)
	}
	return nil
}

// ClaimReminder advances the matching revision and queues its native notification atomically.
func (s *Store) ClaimReminder(ctx context.Context, reminder Reminder) (Reminder, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Reminder{}, false, fmt.Errorf("begin reminder claim: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // A committed claim has nothing to roll back.
	changed, err := query.New(tx).ClaimReminder(ctx, query.ClaimReminderParams{
		UpdatedAt:   nowText(),
		ProfileType: reminder.ProfileType,
		ProfileID:   reminder.ProfileID,
		Revision:    reminder.Revision,
	})
	if err != nil {
		return Reminder{}, false, fmt.Errorf("claim reminder: %w", err)
	}
	if changed == 0 {
		return Reminder{}, false, nil
	}
	if err := enqueueReminderPush(ctx, tx, reminder); err != nil {
		return Reminder{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return Reminder{}, false, fmt.Errorf("commit reminder claim: %w", err)
	}
	reminder.Revision++
	return reminder, true, nil
}

// DeleteClaimedReminder deletes only the claimed revision, preserving concurrent edits.
func (s *Store) DeleteClaimedReminder(ctx context.Context, reminder Reminder) (bool, error) {
	changed, err := query.New(s.db).DeleteClaimedReminder(ctx, query.DeleteClaimedReminderParams{
		ProfileType: reminder.ProfileType,
		ProfileID:   reminder.ProfileID,
		Revision:    reminder.Revision,
	})
	if err != nil {
		return false, fmt.Errorf("delete claimed reminder: %w", err)
	}
	return changed > 0, nil
}

// AdvanceClaimedReminder reschedules only the claimed revision, preserving concurrent edits.
func (s *Store) AdvanceClaimedReminder(ctx context.Context, reminder Reminder, nextAt time.Time) (bool, error) {
	changed, err := query.New(s.db).AdvanceClaimedReminder(ctx, query.AdvanceClaimedReminderParams{
		NextAt:      reminderTimeText(nextAt),
		UpdatedAt:   nowText(),
		ProfileType: reminder.ProfileType,
		ProfileID:   reminder.ProfileID,
		Revision:    reminder.Revision,
	})
	if err != nil {
		return false, fmt.Errorf("advance claimed reminder: %w", err)
	}
	return changed > 0, nil
}

func validateReminderRecord(reminder Reminder) error {
	if strings.TrimSpace(reminder.ProfileType) == "" {
		return errors.New("reminder profile type is required")
	}
	if strings.TrimSpace(reminder.ProfileID) == "" {
		return errors.New("reminder profile id is required")
	}
	if reminder.NextAt.IsZero() {
		return errors.New("reminder time is required")
	}
	if reminder.RepeatDays != nil && (*reminder.RepeatDays <= 0 || *reminder.RepeatDays > maxReminderRepeatDays) {
		return errors.New("reminder repeat days must be between 1 and 3650")
	}
	if strings.TrimSpace(reminder.Content) == "" {
		return errors.New("reminder content is required")
	}
	return nil
}

func parseReminderTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse reminder time: %w", err)
	}
	return parsed.UTC(), nil
}

func reminderTimeText(value time.Time) string {
	return value.UTC().Format("2006-01-02T15:04:05.000000000Z")
}

func reminderFromRow(row query.Reminder) (Reminder, error) {
	nextAt, err := parseReminderTime(row.NextAt)
	if err != nil {
		return Reminder{}, err
	}
	reminder := Reminder{
		ProfileType: row.ProfileType,
		ProfileID:   row.ProfileID,
		ModemID:     row.ModemID,
		SEID:        row.SEID,
		ProfileName: row.ProfileName,
		NextAt:      nextAt,
		Content:     row.Content,
		Revision:    row.Revision,
	}
	if row.RepeatDays.Valid {
		days := int(row.RepeatDays.Int64)
		reminder.RepeatDays = &days
	}
	return reminder, nil
}
