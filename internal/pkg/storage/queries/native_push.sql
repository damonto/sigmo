-- name: AdvanceMessageState :exec
UPDATE message_state SET revision = revision + 1 WHERE id = 1;

-- name: EnqueueNativePush :exec
INSERT INTO native_push_outbox (subscription_id, event_id, revision, content_json, next_at, expires_at)
SELECT id, CAST(sqlc.arg(event_id) AS TEXT), (SELECT revision FROM message_state WHERE id = 1),
    CAST(sqlc.arg(content_json) AS TEXT), CAST(sqlc.arg(next_at) AS INTEGER), CAST(sqlc.arg(expires_at) AS INTEGER)
FROM native_push_subscriptions
WHERE CAST(sqlc.arg(kind) AS TEXT) = 'badge'
    OR (enabled = 1 AND ((sqlc.arg(kind) = 'sms' AND sms = 1) OR (sqlc.arg(kind) = 'reminder' AND reminders = 1)))
ON CONFLICT(subscription_id, event_id) DO UPDATE SET
    revision = excluded.revision,
    content_json = excluded.content_json,
    next_at = MAX(native_push_outbox.next_at, excluded.next_at),
    expires_at = excluded.expires_at,
    attempts = 0
WHERE excluded.event_id = 'badge';

-- name: DeleteReplacedNativeSubscription :exec
DELETE FROM native_push_subscriptions WHERE installation_id = ? AND id <> ?;

-- name: UpsertNativeSubscription :execrows
INSERT INTO native_push_subscriptions (
    id, installation_id, label, backend_name, send_token, encryption_key,
    auth_session_id, sms, reminders, enabled, calls
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    installation_id = excluded.installation_id,
    label = excluded.label,
    backend_name = excluded.backend_name,
    send_token = excluded.send_token,
    encryption_key = excluded.encryption_key,
    auth_session_id = excluded.auth_session_id,
    sms = excluded.sms,
    reminders = excluded.reminders,
    enabled = excluded.enabled,
    calls = excluded.calls
WHERE installation_id <> excluded.installation_id OR label <> excluded.label
    OR backend_name <> excluded.backend_name OR send_token <> excluded.send_token
    OR encryption_key <> excluded.encryption_key OR auth_session_id <> excluded.auth_session_id
    OR sms <> excluded.sms OR reminders <> excluded.reminders
    OR enabled <> excluded.enabled OR calls <> excluded.calls;

-- name: NativeSubscription :one
SELECT * FROM native_push_subscriptions WHERE id = ?;

-- name: UpdateNativeSubscription :exec
UPDATE native_push_subscriptions
SET sms = ?, reminders = ?, calls = ?, enabled = ?, label = ?, auth_session_id = ?
WHERE id = ?;

-- name: NativeSubscriptions :many
SELECT * FROM native_push_subscriptions ORDER BY label, id;

-- name: DeleteNativeSubscription :exec
DELETE FROM native_push_subscriptions WHERE id = ?;

-- name: DeleteNativeSubscriptionIfCurrent :execrows
DELETE FROM native_push_subscriptions
WHERE id = ? AND installation_id = ? AND send_token = ? AND auth_session_id = ?;

-- name: DeleteExpiredNativeDeliveries :exec
DELETE FROM native_push_outbox WHERE expires_at <= ?;

-- name: NativeDeliveries :many
SELECT sqlc.embed(s), o.event_id, o.revision, o.content_json, o.attempts, o.expires_at
FROM native_push_outbox o
JOIN native_push_subscriptions s ON s.id = o.subscription_id
WHERE o.next_at <= ?
ORDER BY o.next_at, o.event_id LIMIT 20;

-- name: FinishNativeDelivery :exec
DELETE FROM native_push_outbox WHERE subscription_id = ? AND event_id = ? AND revision = ?;

-- name: DeferNativeDelivery :exec
UPDATE native_push_outbox SET next_at = MAX(next_at, CAST(sqlc.arg(next_at) AS INTEGER))
WHERE subscription_id = sqlc.arg(subscription_id) AND event_id = sqlc.arg(event_id);

-- name: RetryNativeDelivery :exec
UPDATE native_push_outbox SET attempts = attempts + 1, next_at = MAX(next_at, CAST(sqlc.arg(next_at) AS INTEGER))
WHERE subscription_id = sqlc.arg(subscription_id) AND event_id = sqlc.arg(event_id) AND revision = sqlc.arg(revision);
