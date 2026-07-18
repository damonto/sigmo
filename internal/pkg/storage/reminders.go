package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

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

func (s *Store) UpsertReminder(ctx context.Context, reminder Reminder) error {
	if err := validateReminderRecord(reminder); err != nil {
		return err
	}
	now := nowText()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO reminders (
			profile_type, profile_id, modem_id, se_id, profile_name,
			next_at, repeat_days, content, created_at, updated_at, revision
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1)
		ON CONFLICT(profile_type, profile_id) DO UPDATE SET
			modem_id = excluded.modem_id,
			se_id = excluded.se_id,
			profile_name = excluded.profile_name,
			next_at = excluded.next_at,
			repeat_days = excluded.repeat_days,
			content = excluded.content,
			updated_at = excluded.updated_at,
			revision = reminders.revision + 1
	`, reminder.ProfileType, reminder.ProfileID, reminder.ModemID, reminder.SEID,
		reminder.ProfileName, reminderTimeText(reminder.NextAt), reminder.RepeatDays,
		reminder.Content, now, now)
	if err != nil {
		return fmt.Errorf("save reminder: %w", err)
	}
	return nil
}

func (s *Store) GetReminder(ctx context.Context, profileType, profileID string) (Reminder, bool, error) {
	profileType = strings.TrimSpace(profileType)
	profileID = strings.TrimSpace(profileID)
	if profileType == "" || profileID == "" {
		return Reminder{}, false, errors.New("reminder profile identity is required")
	}

	var reminder Reminder
	var nextAt string
	var repeatDays sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		SELECT profile_type, profile_id, modem_id, se_id, profile_name,
			next_at, repeat_days, content, revision
		FROM reminders
		WHERE profile_type = ? AND profile_id = ?
	`, profileType, profileID).Scan(
		&reminder.ProfileType, &reminder.ProfileID, &reminder.ModemID, &reminder.SEID,
		&reminder.ProfileName, &nextAt, &repeatDays, &reminder.Content, &reminder.Revision,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Reminder{}, false, nil
	}
	if err != nil {
		return Reminder{}, false, fmt.Errorf("read reminder: %w", err)
	}
	if reminder.NextAt, err = parseReminderTime(nextAt); err != nil {
		return Reminder{}, false, err
	}
	if repeatDays.Valid {
		days := int(repeatDays.Int64)
		reminder.RepeatDays = &days
	}
	return reminder, true, nil
}

func (s *Store) DueReminders(ctx context.Context, now time.Time) ([]Reminder, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT profile_type, profile_id, modem_id, se_id, profile_name,
			next_at, repeat_days, content, revision
		FROM reminders
		WHERE next_at <= ?
		ORDER BY next_at ASC
	`, reminderTimeText(now))
	if err != nil {
		return nil, fmt.Errorf("list due reminders: %w", err)
	}
	defer rows.Close()

	reminders := make([]Reminder, 0)
	for rows.Next() {
		reminder, err := scanReminder(rows)
		if err != nil {
			return nil, err
		}
		reminders = append(reminders, reminder)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list due reminders: %w", err)
	}
	return reminders, nil
}

func (s *Store) NextReminderAt(ctx context.Context) (time.Time, bool, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT next_at FROM reminders ORDER BY next_at ASC LIMIT 1`).Scan(&raw)
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

func (s *Store) DeleteReminder(ctx context.Context, profileType, profileID string) error {
	profileType = strings.TrimSpace(profileType)
	profileID = strings.TrimSpace(profileID)
	if profileType == "" || profileID == "" {
		return errors.New("reminder profile identity is required")
	}
	if _, err := s.db.ExecContext(ctx, `
		DELETE FROM reminders WHERE profile_type = ? AND profile_id = ?
	`, profileType, profileID); err != nil {
		return fmt.Errorf("delete reminder: %w", err)
	}
	return nil
}

func (s *Store) ClaimReminder(ctx context.Context, reminder Reminder) (Reminder, bool, error) {
	result, err := s.db.ExecContext(ctx, `
		UPDATE reminders
		SET revision = revision + 1, updated_at = ?
		WHERE profile_type = ? AND profile_id = ? AND revision = ?
	`, nowText(), reminder.ProfileType, reminder.ProfileID, reminder.Revision)
	if err != nil {
		return Reminder{}, false, fmt.Errorf("claim reminder: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return Reminder{}, false, fmt.Errorf("read claimed reminder count: %w", err)
	}
	if changed == 0 {
		return Reminder{}, false, nil
	}
	reminder.Revision++
	return reminder, true, nil
}

func (s *Store) DeleteClaimedReminder(ctx context.Context, reminder Reminder) (bool, error) {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM reminders
		WHERE profile_type = ? AND profile_id = ? AND revision = ?
	`, reminder.ProfileType, reminder.ProfileID, reminder.Revision)
	if err != nil {
		return false, fmt.Errorf("delete claimed reminder: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read deleted reminder count: %w", err)
	}
	return changed > 0, nil
}

func (s *Store) AdvanceClaimedReminder(ctx context.Context, reminder Reminder, nextAt time.Time) (bool, error) {
	result, err := s.db.ExecContext(ctx, `
		UPDATE reminders
		SET next_at = ?, updated_at = ?, revision = revision + 1
		WHERE profile_type = ? AND profile_id = ? AND revision = ?
	`, reminderTimeText(nextAt), nowText(), reminder.ProfileType, reminder.ProfileID, reminder.Revision)
	if err != nil {
		return false, fmt.Errorf("advance claimed reminder: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read advanced reminder count: %w", err)
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

func scanReminder(rows *sql.Rows) (Reminder, error) {
	var reminder Reminder
	var nextAt string
	var repeatDays sql.NullInt64
	if err := rows.Scan(
		&reminder.ProfileType, &reminder.ProfileID, &reminder.ModemID, &reminder.SEID,
		&reminder.ProfileName, &nextAt, &repeatDays, &reminder.Content, &reminder.Revision,
	); err != nil {
		return Reminder{}, fmt.Errorf("scan reminder: %w", err)
	}
	var err error
	if reminder.NextAt, err = parseReminderTime(nextAt); err != nil {
		return Reminder{}, err
	}
	if repeatDays.Valid {
		days := int(repeatDays.Int64)
		reminder.RepeatDays = &days
	}
	return reminder, nil
}
