-- name: ListSIMLines :many
SELECT profile_id, modem_id, label, number, region, kind, se_id, deletion_state, successor_id,
    (SELECT COUNT(*) FROM messages WHERE history_profile_id = sim_profiles.profile_id) AS message_count,
    (SELECT COUNT(*) FROM calls WHERE history_profile_id = sim_profiles.profile_id) AS call_count
FROM sim_profiles ORDER BY label, number, profile_id;

-- name: MarkProfileDeletion :exec
UPDATE sim_profiles SET deletion_state = sqlc.arg(state),
    successor_id = CASE WHEN sqlc.arg(state) = '' THEN '' ELSE successor_id END
WHERE profile_id = sqlc.arg(profile_id);

-- name: ReconcileProfileDeletion :execrows
UPDATE sim_profiles SET deletion_state = sqlc.arg(new_state),
    successor_id = CASE WHEN sqlc.arg(new_state) = '' THEN '' ELSE successor_id END
WHERE profile_id = sqlc.arg(profile_id) AND deletion_state = sqlc.arg(expected_state)
    AND successor_id = sqlc.arg(expected_successor);

-- name: RememberCommunication :exec
INSERT INTO communication_preferences(participant, profile_id, updated_at)
VALUES (sqlc.arg(participant), COALESCE(NULLIF((SELECT p.successor_id FROM sim_profiles p WHERE p.profile_id = sqlc.arg(profile_id)), ''), sqlc.arg(profile_id)), sqlc.arg(updated_at))
ON CONFLICT(participant) DO UPDATE SET profile_id = excluded.profile_id, updated_at = excluded.updated_at
WHERE julianday(excluded.updated_at) >= julianday(communication_preferences.updated_at);

-- name: PreferredCommunication :one
SELECT profile_id, updated_at FROM communication_preferences WHERE participant = ?;

-- name: ReadAssignmentSource :one
SELECT deletion_state, successor_id FROM sim_profiles WHERE profile_id = ?;

-- name: ValidAssignmentTarget :one
SELECT EXISTS(SELECT 1 FROM sim_profiles WHERE profile_id = ? AND deletion_state = '');

-- name: AssignMessageHistory :exec
UPDATE messages SET history_profile_id = sqlc.arg(target) WHERE history_profile_id = sqlc.arg(source);

-- name: AssignCallHistory :exec
UPDATE calls SET history_profile_id = sqlc.arg(target) WHERE history_profile_id = sqlc.arg(source);

-- name: AssignCommunicationPreferences :exec
UPDATE communication_preferences SET profile_id = sqlc.arg(target) WHERE profile_id = sqlc.arg(source);

-- name: AssignProfileSuccessors :exec
UPDATE sim_profiles SET successor_id = sqlc.arg(target) WHERE successor_id = sqlc.arg(source);

-- name: CompleteHistoryAssignment :exec
UPDATE sim_profiles SET deletion_state = 'assigned', successor_id = sqlc.arg(target) WHERE profile_id = sqlc.arg(source);

-- name: ReadCallSnapshot :one
SELECT CAST(COALESCE(MAX(rowid), 0) AS INTEGER) FROM calls;

-- name: ListCallHistory :many
SELECT c.*, CAST(COALESCE((SELECT p.label FROM sim_profiles p WHERE p.profile_id = c.profile_id), '') AS TEXT) AS line_label, CAST((SELECT backend_id FROM message_state WHERE id = 1) AS TEXT) AS backend_id
FROM calls c WHERE
    (CAST(sqlc.arg(profile_id) AS TEXT) = '' OR c.history_profile_id = sqlc.arg(profile_id))
    AND (CAST(sqlc.arg(modem_id) AS TEXT) = '' OR c.history_profile_id IN (SELECT p.profile_id FROM sim_profiles p WHERE p.modem_id = sqlc.arg(modem_id)))
    AND (CAST(sqlc.arg(search) AS TEXT) = '' OR like(sqlc.arg(search_pattern), c.number, '\')
        OR c.number IN (SELECT n.number_key FROM contact_numbers n JOIN contacts contact ON contact.id = n.contact_id WHERE like(sqlc.arg(search_pattern), contact.name, '\')))
    AND (CAST(sqlc.arg(missed) AS INTEGER) = 0 OR (c.direction = 'incoming' AND c.state IN ('ended', 'failed') AND c.answered_at = '0001-01-01T00:00:00Z'))
    AND rowid <= CAST(sqlc.arg(snapshot) AS INTEGER)
    AND (CAST(sqlc.arg(cursor_id) AS TEXT) = '' OR (julianday(c.started_at), c.id) < (julianday(CAST(sqlc.arg(cursor_timestamp) AS TEXT)), sqlc.arg(cursor_id)))
ORDER BY julianday(c.started_at) DESC, c.id DESC LIMIT sqlc.arg(page_limit);
