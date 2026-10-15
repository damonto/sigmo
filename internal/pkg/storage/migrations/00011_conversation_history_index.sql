-- +goose Up
CREATE INDEX idx_messages_conversation_time
ON messages(conversation_id, julianday(timestamp), id);
