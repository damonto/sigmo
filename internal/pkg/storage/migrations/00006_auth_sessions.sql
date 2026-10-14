-- +goose Up
-- An upgrade deliberately requires a fresh login; legacy bearer tokens have no identity.
DROP TABLE auth_tokens;

CREATE TABLE auth_sessions (
 id TEXT PRIMARY KEY,
 method TEXT NOT NULL,
 issuer TEXT NOT NULL,
 subject TEXT NOT NULL,
 policy_id TEXT NOT NULL,
 created_at INTEGER NOT NULL,
 expires_at INTEGER NOT NULL,
 revoked_at INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE auth_credentials (
 token_hash TEXT PRIMARY KEY,
 session_id TEXT NOT NULL REFERENCES auth_sessions(id) ON DELETE CASCADE,
 kind TEXT NOT NULL CHECK(kind IN ('web','access','refresh')),
 expires_at INTEGER NOT NULL,
 consumed_at INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX auth_credentials_session ON auth_credentials(session_id);
CREATE INDEX auth_sessions_expiration ON auth_sessions(expires_at);

ALTER TABLE native_push_subscriptions RENAME COLUMN auth_token_hash TO auth_session_id;
UPDATE native_push_subscriptions SET auth_session_id='';
