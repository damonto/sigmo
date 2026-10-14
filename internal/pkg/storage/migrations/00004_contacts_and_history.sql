-- +goose Up
CREATE TABLE contacts (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    notes TEXT NOT NULL DEFAULT '',
    avatar BLOB,
    avatar_color TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL
);
CREATE TABLE contact_numbers (
    contact_id TEXT NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
    position INTEGER NOT NULL,
    label TEXT NOT NULL,
    number TEXT NOT NULL,
    region TEXT NOT NULL DEFAULT '',
    number_key TEXT NOT NULL,
    PRIMARY KEY (contact_id, position)
);
CREATE INDEX idx_contact_number_key ON contact_numbers(number_key);
CREATE TABLE communication_preferences (
    participant TEXT PRIMARY KEY,
    profile_id TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
ALTER TABLE messages ADD COLUMN history_profile_id TEXT NOT NULL DEFAULT '';
ALTER TABLE messages ADD COLUMN is_read INTEGER NOT NULL DEFAULT 0;
UPDATE messages SET history_profile_id = profile_id,
    is_read = CASE WHEN incoming = 0 OR id <= COALESCE((SELECT read_through_id FROM conversations WHERE id = messages.conversation_id), 0) THEN 1 ELSE 0 END;
ALTER TABLE calls ADD COLUMN history_profile_id TEXT NOT NULL DEFAULT '';
UPDATE calls SET history_profile_id = profile_id;
ALTER TABLE sim_profiles ADD COLUMN region TEXT NOT NULL DEFAULT '';
ALTER TABLE sim_profiles ADD COLUMN kind TEXT NOT NULL DEFAULT '';
ALTER TABLE sim_profiles ADD COLUMN se_id TEXT NOT NULL DEFAULT '';
ALTER TABLE sim_profiles ADD COLUMN deletion_state TEXT NOT NULL DEFAULT '';
ALTER TABLE sim_profiles ADD COLUMN successor_id TEXT NOT NULL DEFAULT '';
CREATE INDEX idx_messages_history ON messages(history_profile_id, id);
CREATE INDEX idx_calls_history ON calls(history_profile_id, started_at, id);
-- +goose StatementBegin
CREATE TRIGGER messages_history_defaults AFTER INSERT ON messages BEGIN
    UPDATE messages SET history_profile_id = NEW.profile_id, is_read = CASE WHEN NEW.incoming = 0 THEN 1 ELSE 0 END WHERE id = NEW.id;
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER calls_history_defaults AFTER INSERT ON calls BEGIN
    UPDATE calls SET history_profile_id = NEW.profile_id WHERE id = NEW.id;
END;
-- +goose StatementEnd
