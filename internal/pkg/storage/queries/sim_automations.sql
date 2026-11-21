-- name: CreateSIMAutomation :one
INSERT INTO sim_automations (id, profile_id, modem_id, se_id, eid, scheduled_at, repeat_days, action, next_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: ReadSIMAutomation :one
SELECT * FROM sim_automations WHERE profile_id = ? AND id = ?;

-- name: ListSIMAutomations :many
SELECT * FROM sim_automations WHERE profile_id = ? ORDER BY (next_at = 0), next_at, id;

-- name: UpdateSIMAutomation :one
UPDATE sim_automations SET scheduled_at = ?, repeat_days = ?, action = ?, next_at = ?, revision = revision + 1
WHERE profile_id = ? AND id = ? AND revision = ? AND running_at = 0 AND restore_state = ''
RETURNING *;

-- name: DeleteSIMAutomation :execrows
DELETE FROM sim_automations WHERE profile_id = ? AND id = ? AND running_at = 0 AND restore_state = '';

-- name: PendingSIMAutomationRecovery :many
SELECT * FROM sim_automations WHERE running_at > 0 OR restore_state <> '' ORDER BY started_at, id;

-- name: NextSIMAutomationAfter :one
SELECT CAST(COALESCE(MIN(next_at), 0) AS INTEGER)
FROM sim_automations
WHERE next_at > 0 AND next_at > sqlc.arg(after_at) AND running_at = 0 AND restore_state = '';

-- name: DueSIMAutomations :many
SELECT * FROM sim_automations
WHERE next_at > 0 AND next_at <= ? AND running_at = 0 AND restore_state = ''
ORDER BY next_at, id;

-- name: SIMAutomationModemBusy :one
SELECT CAST(EXISTS(SELECT 1 FROM sim_automations WHERE modem_id = ? AND (running_at > 0 OR restore_state <> '')) AS INTEGER);

-- name: ClaimSIMAutomation :one
UPDATE sim_automations SET running_at = ?, started_at = ?, next_at = ?, revision = revision + 1
WHERE id = ? AND revision = ? AND running_at = 0 AND restore_state = ''
RETURNING *;

-- name: SetSIMAutomationRestore :execrows
UPDATE sim_automations SET restore_state = ?
WHERE id = ? AND revision = ? AND running_at > 0;

-- name: FinishSIMAutomation :execrows
UPDATE sim_automations SET last_run = ?, restore_state = ?, running_at = 0, started_at = 0
WHERE id = ? AND revision = ? AND running_at > 0;

-- name: UpdateSIMAutomationRecovery :execrows
UPDATE sim_automations SET last_run = ?, restore_state = ?
WHERE id = ? AND revision = ? AND running_at = 0 AND restore_state <> '';

-- name: StopSIMAutomationsForDeletedProfile :exec
UPDATE sim_automations SET next_at = 0, revision = revision + (running_at = 0 AND restore_state = '')
WHERE profile_id = ? AND modem_id = ? AND se_id = ?;

-- name: SIMAutomationLine :one
SELECT profile_id, modem_id, kind, se_id, deletion_state FROM sim_profiles WHERE profile_id = ?;

-- name: ListSIMAutomationSummaries :many
SELECT id, profile_id, se_id, next_at,
       CAST(COALESCE(CASE WHEN json_valid(action) THEN json_extract(action, '$.type') END, '') AS TEXT) AS action_type
FROM sim_automations
WHERE modem_id = ?
ORDER BY profile_id, se_id;
