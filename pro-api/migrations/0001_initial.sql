CREATE TABLE entitlements (
    telegram_id INTEGER PRIMARY KEY,
    status TEXT NOT NULL CHECK (status IN ('active', 'revoked')),
    expires_at TEXT,
    max_devices INTEGER NOT NULL DEFAULT 3 CHECK (max_devices > 0),
    display_name TEXT NOT NULL DEFAULT '',
    username TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE devices (
    device_id TEXT PRIMARY KEY,
    telegram_id INTEGER NOT NULL REFERENCES entitlements(telegram_id),
    public_key TEXT NOT NULL,
    created_at TEXT NOT NULL,
    last_seen_at TEXT NOT NULL,
    revoked_at TEXT
);
CREATE INDEX devices_telegram_id ON devices(telegram_id);

CREATE TABLE pairings (
    pairing_id TEXT PRIMARY KEY,
    poll_token_hash TEXT NOT NULL,
    device_id TEXT NOT NULL,
    public_key TEXT NOT NULL,
    telegram_id INTEGER,
    status TEXT NOT NULL CHECK (status IN ('pending', 'active', 'expired')),
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL
);

CREATE TABLE challenges (
    challenge_hash TEXT PRIMARY KEY,
    device_id TEXT NOT NULL REFERENCES devices(device_id),
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL
);
CREATE INDEX challenges_device_id ON challenges(device_id);
