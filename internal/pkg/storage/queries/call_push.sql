-- name: CancelNativeCall :exec
DELETE FROM native_call_outbox WHERE call_id = ? AND delivered = 0;

-- name: EnqueueNativeCall :exec
INSERT OR IGNORE INTO native_call_outbox (subscription_id, call_id, next_at, expires_at)
SELECT id, CAST(sqlc.arg(call_id) AS TEXT), CAST(sqlc.arg(next_at) AS INTEGER), CAST(sqlc.arg(expires_at) AS INTEGER)
FROM native_push_subscriptions WHERE enabled = 1 AND calls = 1;

-- name: DeleteExpiredNativeCalls :exec
DELETE FROM native_call_outbox WHERE expires_at <= ?;

-- name: NativeCallDeliveries :many
-- Read each invitation and its grant from the same snapshot to exclude deleted calls or subscriptions.
SELECT sqlc.embed(s), c.id AS call_id, c.modem_id, c.profile_id, c.number, o.expires_at,
    CAST((SELECT backend_id FROM message_state WHERE id = 1) AS TEXT) AS backend_id
FROM native_call_outbox o
JOIN native_push_subscriptions s ON s.id = o.subscription_id
JOIN calls c ON c.id = o.call_id
WHERE o.delivered = 0 AND o.next_at <= ? AND s.enabled = 1 AND s.calls = 1
    AND c.state = 'ringing' AND c.direction = 'incoming' AND c.owner_client_id = ''
ORDER BY o.next_at LIMIT 20;

-- name: FinishNativeCall :exec
UPDATE native_call_outbox SET delivered = ?, next_at = ? WHERE subscription_id = ? AND call_id = ?;
