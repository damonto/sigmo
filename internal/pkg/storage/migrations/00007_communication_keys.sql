-- +goose Up
ALTER TABLE conversations ADD COLUMN participant_key TEXT NOT NULL DEFAULT '';
UPDATE conversations SET participant_key = participant;
CREATE INDEX idx_conversations_participant_key ON conversations(participant_key);

-- Late imports retain their source SIM while following any completed handover.
DROP TRIGGER messages_history_defaults;
-- +goose StatementBegin
CREATE TRIGGER messages_history_defaults AFTER INSERT ON messages BEGIN
    UPDATE messages SET history_profile_id = COALESCE(NULLIF((SELECT successor_id FROM sim_profiles WHERE profile_id = NEW.profile_id), ''), NEW.profile_id),
        is_read = CASE WHEN NEW.incoming = 0 THEN 1 ELSE 0 END WHERE id = NEW.id;
END;
-- +goose StatementEnd
DROP TRIGGER calls_history_defaults;
-- +goose StatementBegin
CREATE TRIGGER calls_history_defaults AFTER INSERT ON calls BEGIN
    UPDATE calls SET history_profile_id = COALESCE(NULLIF((SELECT successor_id FROM sim_profiles WHERE profile_id = NEW.profile_id), ''), NEW.profile_id) WHERE id = NEW.id;
END;
-- +goose StatementEnd
