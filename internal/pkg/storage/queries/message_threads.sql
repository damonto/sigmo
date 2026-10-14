-- name: ReadThreadLatest :one
SELECT m.id, m.conversation_id, m.profile_id, m.history_profile_id, m.modem_id,
    m.sender, m.recipient, m.text, m.timestamp, m.incoming, m.is_read, m.status, CAST(COALESCE(origin.label, '') AS TEXT) AS line_label, CAST(COALESCE(origin.number, '') AS TEXT) AS line_number
    FROM messages m LEFT JOIN sim_profiles origin ON origin.profile_id = m.profile_id
    LEFT JOIN sim_profiles l ON l.profile_id = m.history_profile_id JOIN conversations c ON c.id = m.conversation_id WHERE c.participant_key = ? ORDER BY julianday(m.timestamp) DESC, m.id DESC LIMIT 1;

-- name: ReadThreadMessage :one
SELECT m.id, m.conversation_id, m.profile_id, m.history_profile_id, m.modem_id,
    m.sender, m.recipient, m.text, m.timestamp, m.incoming, m.is_read, m.status, CAST(COALESCE(origin.label, '') AS TEXT) AS line_label, CAST(COALESCE(origin.number, '') AS TEXT) AS line_number
    FROM messages m LEFT JOIN sim_profiles origin ON origin.profile_id = m.profile_id
    LEFT JOIN sim_profiles l ON l.profile_id = m.history_profile_id JOIN conversations c ON c.id = m.conversation_id WHERE m.id = ?;

-- name: ReadThreadCounts :one
SELECT CAST(COALESCE(SUM(CASE WHEN m.incoming = 1 AND m.is_read = 0 THEN 1 ELSE 0 END), 0) AS INTEGER) AS unread_count, CAST(COALESCE(MAX(c.marked_unread), 0) AS INTEGER) AS marked_unread FROM messages m JOIN conversations c ON c.id = m.conversation_id WHERE c.participant_key = ?;

-- name: ReadMessageSnapshot :one
SELECT CAST(COALESCE(MAX(id), 0) AS INTEGER) FROM messages;

-- name: ListMessageThreads :many
WITH ranked AS (
        SELECT c.participant_key, m.id, m.timestamp,
            SUM(CASE WHEN m.incoming = 1 AND m.is_read = 0 THEN 1 ELSE 0 END) OVER (PARTITION BY c.participant_key) AS unread,
            MAX(c.marked_unread) OVER (PARTITION BY c.participant_key) AS marked,
            MAX(CASE WHEN like(sqlc.arg(search_pattern), m.text, '\') THEN 1 ELSE 0 END) OVER (PARTITION BY c.participant_key) AS text_match,
            ROW_NUMBER() OVER (PARTITION BY c.participant_key ORDER BY julianday(m.timestamp) DESC, m.id DESC) AS row_rank
        FROM messages m JOIN conversations c ON c.id = m.conversation_id LEFT JOIN sim_profiles l ON l.profile_id = m.history_profile_id
        WHERE  (CAST(sqlc.arg(profile_id) AS TEXT) = '' OR m.history_profile_id = sqlc.arg(profile_id)) AND (CAST(sqlc.arg(modem_id) AS TEXT) = '' OR l.modem_id = sqlc.arg(modem_id)) AND m.id <= sqlc.arg(snapshot)
    ) SELECT participant_key, id, CAST(unread AS INTEGER) AS unread_count, CAST(marked AS INTEGER) AS marked_unread FROM ranked
      WHERE row_rank = 1 AND (CAST(sqlc.arg(cursor_id) AS TEXT) = '' OR (julianday(timestamp), id) < (julianday(CAST(sqlc.arg(cursor_timestamp) AS TEXT)), CAST(CAST(sqlc.arg(cursor_id) AS TEXT) AS INTEGER)))
        AND (CAST(sqlc.arg(search) AS TEXT) = '' OR like(sqlc.arg(search_pattern), participant_key, '\')
          OR EXISTS (SELECT 1 FROM contact_numbers n JOIN contacts contact ON contact.id = n.contact_id WHERE n.number_key = participant_key AND like(sqlc.arg(search_pattern), contact.name, '\'))
          OR text_match = 1)
      ORDER BY julianday(timestamp) DESC, id DESC LIMIT sqlc.arg(page_limit);

-- name: ListThreadMessages :many
SELECT m.id, m.conversation_id, m.profile_id, m.history_profile_id, m.modem_id,
    m.sender, m.recipient, m.text, m.timestamp, m.incoming, m.is_read, m.status, CAST(COALESCE(origin.label, '') AS TEXT) AS line_label, CAST(COALESCE(origin.number, '') AS TEXT) AS line_number
    FROM messages m LEFT JOIN sim_profiles origin ON origin.profile_id = m.profile_id
    LEFT JOIN sim_profiles l ON l.profile_id = m.history_profile_id JOIN conversations c ON c.id = m.conversation_id WHERE c.participant_key = sqlc.arg(participant) AND  (CAST(sqlc.arg(profile_id) AS TEXT) = '' OR m.history_profile_id = sqlc.arg(profile_id)) AND (CAST(sqlc.arg(modem_id) AS TEXT) = '' OR l.modem_id = sqlc.arg(modem_id)) AND m.id <= sqlc.arg(snapshot) AND (CAST(sqlc.arg(cursor_id) AS TEXT) = '' OR (julianday(m.timestamp), m.id) < (julianday(CAST(sqlc.arg(cursor_timestamp) AS TEXT)), CAST(sqlc.arg(cursor_id) AS INTEGER))) ORDER BY julianday(m.timestamp) DESC, m.id DESC LIMIT sqlc.arg(page_limit);

-- name: CountInvalidThreadReads :one
SELECT COUNT(*) FROM json_each(CAST(sqlc.arg(message_json) AS TEXT)) j WHERE NOT EXISTS (SELECT 1 FROM messages m JOIN conversations c ON c.id = m.conversation_id WHERE m.id = j.value AND c.participant_key = sqlc.arg(participant));

-- name: ReadDisplayedMessages :exec
UPDATE messages SET is_read = 1 WHERE id IN (SELECT value FROM json_each(CAST(sqlc.arg(message_json) AS TEXT)));

-- name: MarkThreadUnread :exec
UPDATE conversations SET marked_unread = ? WHERE participant_key = ?;

-- name: ClearDisplayedThreadMarks :exec
UPDATE conversations SET marked_unread = 0 WHERE id IN (SELECT conversation_id FROM messages WHERE id IN (SELECT value FROM json_each(CAST(sqlc.arg(message_json) AS TEXT))));

-- name: TombstoneThreadMessages :exec
INSERT OR IGNORE INTO deleted_messages(fingerprint) SELECT fingerprint FROM messages WHERE conversation_id IN (SELECT id FROM conversations WHERE participant_key = ?) AND fingerprint <> '';

-- name: ClearThreadNotifications :exec
DELETE FROM native_push_outbox WHERE json_extract(content_json, '$.conversationId') IN (SELECT id FROM conversations WHERE participant_key = ?);

-- name: DeleteThreadMessages :exec
DELETE FROM messages WHERE conversation_id IN (SELECT id FROM conversations WHERE participant_key = ?);

-- name: DeleteThreadConversations :exec
DELETE FROM conversations WHERE participant_key = ?;

-- name: ThreadParticipant :one
SELECT participant_key FROM conversations WHERE id = ?;
