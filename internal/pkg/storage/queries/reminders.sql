-- name: UpsertReminder :exec
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
    revision = reminders.revision + 1;

-- name: Reminder :one
SELECT * FROM reminders WHERE profile_type = ? AND profile_id = ?;

-- name: DueReminders :many
SELECT * FROM reminders WHERE next_at <= ? ORDER BY next_at ASC;

-- name: NextReminderAt :one
SELECT next_at FROM reminders ORDER BY next_at ASC LIMIT 1;

-- name: DeleteReminder :exec
DELETE FROM reminders WHERE profile_type = ? AND profile_id = ?;

-- name: ClaimReminder :execrows
UPDATE reminders SET revision = revision + 1, updated_at = ?
WHERE profile_type = ? AND profile_id = ? AND revision = ?;

-- name: DeleteClaimedReminder :execrows
DELETE FROM reminders WHERE profile_type = ? AND profile_id = ? AND revision = ?;

-- name: AdvanceClaimedReminder :execrows
UPDATE reminders SET next_at = ?, updated_at = ?, revision = revision + 1
WHERE profile_type = ? AND profile_id = ? AND revision = ?;
