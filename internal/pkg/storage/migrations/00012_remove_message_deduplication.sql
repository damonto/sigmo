-- +goose Up
-- Rebuild messages to remove both content and external-key uniqueness.
-- Save child rows before dropping the parent while foreign keys stay enabled.
CREATE TEMP TABLE saved_modem_message_refs AS
SELECT message_id, modem_id, generation, storage, ref_id FROM modem_message_refs;
CREATE TEMP TABLE saved_message_sequence AS SELECT seq FROM sqlite_sequence WHERE name = 'messages';
DROP TABLE modem_message_refs;

CREATE TABLE messages_without_deduplication (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    profile_id TEXT NOT NULL,
    source TEXT NOT NULL,
    external_key TEXT NOT NULL,
    sender TEXT NOT NULL,
    recipient TEXT NOT NULL,
    text TEXT NOT NULL,
    timestamp TEXT NOT NULL,
    status TEXT NOT NULL,
    incoming INTEGER NOT NULL,
    wifi_calling INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    conversation_id TEXT NOT NULL DEFAULT '',
    modem_id TEXT NOT NULL DEFAULT '',
    history_profile_id TEXT NOT NULL DEFAULT '',
    is_read INTEGER NOT NULL DEFAULT 0
);
INSERT INTO messages_without_deduplication (
    id, profile_id, source, external_key, sender, recipient, text, timestamp,
    status, incoming, wifi_calling, created_at, updated_at, conversation_id,
    modem_id, history_profile_id, is_read
)
SELECT id, profile_id, source, external_key, sender, recipient, text, timestamp,
    status, incoming, wifi_calling, created_at, updated_at, conversation_id,
    modem_id, history_profile_id, is_read FROM messages;
DROP TABLE messages;
ALTER TABLE messages_without_deduplication RENAME TO messages;
-- Keep IDs above deleted rows referenced by client snapshots and read watermarks.
UPDATE sqlite_sequence SET seq = MAX(seq, COALESCE((SELECT seq FROM saved_message_sequence), 0)) WHERE name = 'messages';
DROP TABLE saved_message_sequence;

CREATE INDEX idx_messages_profile_timestamp ON messages(profile_id, timestamp);
CREATE INDEX idx_messages_profile_participants ON messages(profile_id, sender, recipient);
CREATE INDEX idx_messages_conversation ON messages(conversation_id, id);
CREATE INDEX idx_messages_history ON messages(history_profile_id, id);
CREATE INDEX idx_messages_conversation_time ON messages(conversation_id, julianday(timestamp), id);
CREATE INDEX idx_messages_external_key ON messages(profile_id, source, external_key);

-- +goose StatementBegin
CREATE TRIGGER messages_history_defaults AFTER INSERT ON messages BEGIN
    UPDATE messages SET history_profile_id = COALESCE(NULLIF((SELECT successor_id FROM sim_profiles WHERE profile_id = NEW.profile_id), ''), NEW.profile_id),
        is_read = CASE WHEN NEW.incoming = 0 THEN 1 ELSE 0 END WHERE id = NEW.id;
END;
-- +goose StatementEnd

CREATE TABLE modem_message_refs (
    message_id INTEGER NOT NULL,
    modem_id TEXT NOT NULL,
    generation INTEGER NOT NULL DEFAULT 0,
    storage INTEGER NOT NULL,
    ref_id INTEGER NOT NULL,
    PRIMARY KEY (message_id, modem_id, generation, storage, ref_id),
    FOREIGN KEY (message_id) REFERENCES messages(id) ON DELETE CASCADE
);
INSERT INTO modem_message_refs (message_id, modem_id, generation, storage, ref_id)
SELECT message_id, modem_id, generation, storage, ref_id FROM saved_modem_message_refs;
DROP TABLE saved_modem_message_refs;
CREATE INDEX idx_modem_message_ref ON modem_message_refs(modem_id, generation, storage, ref_id);

DROP TABLE IF EXISTS deleted_messages;
