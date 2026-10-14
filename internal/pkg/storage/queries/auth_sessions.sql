-- name: DeleteExpiredAuthSessions :exec
DELETE FROM auth_sessions WHERE expires_at <= ?;

-- name: CreateAuthSession :exec
INSERT INTO auth_sessions(id, method, issuer, subject, policy_id, created_at, expires_at)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: CreateAuthCredential :exec
INSERT INTO auth_credentials(token_hash, session_id, kind, expires_at) VALUES (?, ?, ?, ?);

-- name: AuthSessionByCredential :one
SELECT s.* FROM auth_sessions s JOIN auth_credentials c ON c.session_id = s.id
WHERE c.token_hash = sqlc.arg(token_hash) AND c.kind = sqlc.arg(kind)
AND c.consumed_at = 0 AND c.expires_at > sqlc.arg(now)
AND s.expires_at > sqlc.arg(now) AND s.revoked_at = 0;

-- name: AuthRefreshCredential :one
SELECT s.*, c.consumed_at, c.expires_at AS credential_expires_at
FROM auth_sessions s JOIN auth_credentials c ON c.session_id = s.id
WHERE c.token_hash = ? AND c.kind = 'refresh';

-- name: ConsumeAuthCredential :exec
UPDATE auth_credentials SET consumed_at = ? WHERE token_hash = ?;

-- name: RevokeAuthSession :exec
UPDATE auth_sessions SET revoked_at = ? WHERE id = ?;

-- name: AuthSessionActive :one
SELECT EXISTS(SELECT 1 FROM auth_sessions WHERE id = ? AND policy_id = ? AND revoked_at = 0 AND expires_at > ?);

