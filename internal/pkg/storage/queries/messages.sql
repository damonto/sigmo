-- name: InsertMessage :one
INSERT INTO messages (
    profile_id, source, external_key, sender, recipient, text,
    timestamp, status, incoming, wifi_calling, created_at, updated_at, modem_id, conversation_id
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: RecordMessageSIMProfile :exec
INSERT INTO sim_profiles (profile_id, modem_id, number) VALUES (?, ?, ?)
ON CONFLICT(profile_id) DO UPDATE SET
    modem_id = CASE WHEN excluded.modem_id <> '' THEN excluded.modem_id ELSE sim_profiles.modem_id END,
    number = CASE WHEN sim_profiles.number = '' THEN excluded.number ELSE sim_profiles.number END;
