-- name: SaveAppState :exec
INSERT INTO app_state(scope, key, value_json, updated_at) VALUES (?, ?, ?, ?)
ON CONFLICT(scope, key) DO UPDATE SET
    value_json = excluded.value_json,
    updated_at = excluded.updated_at;

-- name: AppStateValue :one
SELECT value_json FROM app_state WHERE scope = ? AND key = ?;

-- name: DeleteAppStateScope :exec
DELETE FROM app_state WHERE scope = ?;

-- name: DeleteAppStateValue :exec
DELETE FROM app_state WHERE scope = ? AND key = ?;

-- name: ListAppStateValues :many
SELECT scope, value_json FROM app_state WHERE scope LIKE ? AND key = ?;
