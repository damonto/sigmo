-- name: EndInterruptedCalls :exec
UPDATE calls SET state = 'ended', reason = 'server_restarted', ended_at = sqlc.arg(now), updated_at = sqlc.arg(now)
WHERE state NOT IN ('ended', 'failed');

-- name: CancelInterruptedCallInvitations :exec
DELETE FROM native_call_outbox WHERE delivered = 0;

-- name: SaveCall :execrows
INSERT INTO calls (
    id, profile_id, modem_id, route, direction, number, state, hold_state, reason,
    started_at, answered_at, ended_at, updated_at, owner_client_id
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    profile_id = excluded.profile_id,
    modem_id = excluded.modem_id,
    route = excluded.route,
    direction = excluded.direction,
    number = excluded.number,
    state = excluded.state,
    hold_state = excluded.hold_state,
    reason = excluded.reason,
    started_at = excluded.started_at,
    answered_at = CASE WHEN excluded.answered_at = '0001-01-01T00:00:00Z' THEN calls.answered_at ELSE excluded.answered_at END,
    ended_at = excluded.ended_at,
    updated_at = excluded.updated_at,
    owner_client_id = CASE WHEN excluded.owner_client_id <> '' THEN excluded.owner_client_id ELSE calls.owner_client_id END;

-- name: SaveCallPreservingTerminal :execrows
INSERT INTO calls (
    id, profile_id, modem_id, route, direction, number, state, hold_state, reason,
    started_at, answered_at, ended_at, updated_at, owner_client_id
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    profile_id = excluded.profile_id,
    modem_id = excluded.modem_id,
    route = excluded.route,
    direction = excluded.direction,
    number = excluded.number,
    state = excluded.state,
    hold_state = excluded.hold_state,
    reason = excluded.reason,
    started_at = excluded.started_at,
    answered_at = CASE WHEN excluded.answered_at = '0001-01-01T00:00:00Z' THEN calls.answered_at ELSE excluded.answered_at END,
    ended_at = excluded.ended_at,
    updated_at = excluded.updated_at,
    owner_client_id = CASE WHEN excluded.owner_client_id <> '' THEN excluded.owner_client_id ELSE calls.owner_client_id END
WHERE NOT (calls.state IN ('ended', 'failed') AND excluded.state NOT IN ('ended', 'failed'));

-- name: ReadCall :one
SELECT c.*, CAST(COALESCE((SELECT p.label FROM sim_profiles p WHERE p.profile_id = c.profile_id), '') AS TEXT) AS line_label, CAST((SELECT backend_id FROM message_state WHERE id = 1) AS TEXT) AS backend_id
FROM calls c WHERE c.id = ?;

-- name: DeleteCall :execrows
DELETE FROM calls WHERE id = ? AND profile_id = ? AND modem_id = ?;

-- name: ListCalls :many
SELECT c.*, CAST(COALESCE((SELECT p.label FROM sim_profiles p WHERE p.profile_id = c.profile_id), '') AS TEXT) AS line_label, CAST((SELECT backend_id FROM message_state WHERE id = 1) AS TEXT) AS backend_id
FROM calls c WHERE c.profile_id = ? AND c.modem_id = ?
    AND (json_array_length(CAST(sqlc.arg(search_patterns) AS TEXT)) = 0
        OR EXISTS (SELECT 1 FROM json_each(CAST(sqlc.arg(search_patterns) AS TEXT))
            WHERE c.number LIKE value ESCAPE '\'))
ORDER BY updated_at DESC LIMIT sqlc.arg(page_limit);

-- name: ClaimCall :execrows
UPDATE calls SET owner_client_id = ?
WHERE id = ? AND owner_client_id = '' AND state NOT IN ('ended', 'failed');

-- name: ReleaseCallClaim :exec
UPDATE calls SET owner_client_id = '' WHERE id = ? AND owner_client_id = ? AND state IN ('ringing', 'answering');
