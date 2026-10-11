CREATE TABLE push_installations (
    id TEXT PRIMARY KEY,
    management_hash TEXT NOT NULL,
    device_token TEXT NOT NULL,
    environment TEXT NOT NULL CHECK(environment IN ('development', 'production')),
    label TEXT NOT NULL,
    token_revision INTEGER NOT NULL DEFAULT 1,
    badge_revision INTEGER NOT NULL DEFAULT 0,
    updated_at INTEGER NOT NULL
);
CREATE UNIQUE INDEX push_installation_token ON push_installations(environment, device_token) WHERE device_token <> '';

CREATE TABLE push_subscriptions (
    id TEXT PRIMARY KEY,
    installation_id TEXT NOT NULL REFERENCES push_installations(id) ON DELETE CASCADE,
    backend_id TEXT NOT NULL,
    send_hash TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    sms INTEGER NOT NULL DEFAULT 1,
    reminders INTEGER NOT NULL DEFAULT 1,
    revision INTEGER NOT NULL DEFAULT -1,
    unread_count INTEGER NOT NULL DEFAULT 0,
    UNIQUE(installation_id, backend_id)
);

CREATE TABLE push_deliveries (
    subscription_id TEXT NOT NULL REFERENCES push_subscriptions(id) ON DELETE CASCADE,
    event_id TEXT NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('pending', 'sent')),
    lease_token TEXT NOT NULL,
    lease_until INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    PRIMARY KEY(subscription_id, event_id)
);
CREATE INDEX push_deliveries_expiry ON push_deliveries(expires_at);

CREATE TABLE push_rate_limits (
    id TEXT PRIMARY KEY,
    window INTEGER NOT NULL,
    count INTEGER NOT NULL,
    expires_at INTEGER NOT NULL
);
