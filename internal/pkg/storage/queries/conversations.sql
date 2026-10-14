-- name: RecordMessageConversation :exec
INSERT INTO conversations (id, profile_id, participant, participant_key) VALUES (?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET participant_key = excluded.participant_key;

-- name: ReadMessageState :one
SELECT backend_id, revision,
    CAST(COALESCE((SELECT SUM(MAX(c.marked_unread, (
        SELECT COUNT(*) FROM messages unread
        WHERE unread.conversation_id = c.id AND unread.incoming = 1 AND unread.is_read = 0
    ))) FROM conversations c), 0) AS INTEGER) AS unread_count
FROM message_state WHERE id = 1;

-- name: ReadConversation :one
SELECT c.id, c.profile_id, c.participant,
    CAST(COALESCE(l.modem_id, '') AS TEXT) AS modem_id,
    CAST(COALESCE(l.label, '') AS TEXT) AS line_label,
    CAST(COALESCE(l.number, '') AS TEXT) AS line_number,
    c.read_through_id, c.marked_unread,
    CAST(MAX(c.marked_unread, (SELECT COUNT(*) FROM messages unread
        WHERE unread.conversation_id = c.id AND unread.incoming = 1 AND unread.is_read = 0
    )) AS INTEGER) AS unread_count
FROM conversations c LEFT JOIN sim_profiles l ON l.profile_id = c.profile_id
WHERE c.id = ?;

-- name: ReadConversationAcknowledgment :one
SELECT read_through_id, marked_unread FROM conversations WHERE id = ?;

-- name: ValidConversationWatermark :one
SELECT EXISTS(SELECT 1 FROM messages WHERE conversation_id = ? AND id = ?);

-- name: AcknowledgeConversationMessages :exec
UPDATE messages SET is_read = 1 WHERE conversation_id = ? AND id <= sqlc.arg(read_through_id);

-- name: AcknowledgeConversation :exec
UPDATE conversations SET read_through_id = ?, marked_unread = ? WHERE id = ?;
