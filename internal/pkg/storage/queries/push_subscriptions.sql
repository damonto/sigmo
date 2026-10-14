-- name: UpsertPushSubscription :one
INSERT INTO push_subscriptions (
    id, endpoint, p256dh, auth, label, user_agent, platform, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(endpoint) DO UPDATE SET
    p256dh = excluded.p256dh,
    auth = excluded.auth,
    label = excluded.label,
    user_agent = excluded.user_agent,
    platform = excluded.platform,
    updated_at = excluded.updated_at
RETURNING *;

-- name: ListPushSubscriptions :many
SELECT * FROM push_subscriptions
ORDER BY label COLLATE NOCASE, created_at;

-- name: RenamePushSubscription :one
UPDATE push_subscriptions
SET label = ?, updated_at = ?
WHERE id = ?
RETURNING *;

-- name: DeletePushSubscription :execrows
DELETE FROM push_subscriptions WHERE id = ?;
