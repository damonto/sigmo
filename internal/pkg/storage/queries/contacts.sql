-- name: CreateContact :exec
INSERT INTO contacts(id, name, notes, updated_at) VALUES (?, ?, ?, ?);

-- name: UpdateContact :execrows
UPDATE contacts SET name = ?, notes = ?, updated_at = ? WHERE id = ?;

-- name: ClearContactNumbers :exec
DELETE FROM contact_numbers WHERE contact_id = ?;

-- name: AddContactNumber :exec
INSERT INTO contact_numbers(contact_id, position, label, number, region, number_key)
VALUES (?, ?, ?, ?, ?, ?);

-- name: ListContacts :many
SELECT id, name, notes, CAST(avatar IS NOT NULL AS INTEGER) AS has_avatar, avatar_color, updated_at
FROM contacts c
WHERE CAST(sqlc.arg(search) AS TEXT) = '' OR like(sqlc.arg(search_pattern), c.name, '\')
    OR EXISTS (SELECT 1 FROM contact_numbers n WHERE n.contact_id = c.id
        AND (like(sqlc.arg(search_pattern), n.number, '\') OR like(sqlc.arg(search_pattern), n.number_key, '\')))
ORDER BY name COLLATE NOCASE, id;

-- name: ReadContact :one
SELECT id, name, notes, CAST(avatar IS NOT NULL AS INTEGER) AS has_avatar, avatar_color, updated_at
FROM contacts WHERE id = ?;

-- name: ListContactNumbers :many
SELECT label, number, region, number_key FROM contact_numbers WHERE contact_id = ? ORDER BY position;

-- name: DeleteContact :exec
DELETE FROM contacts WHERE id = ?;

-- name: ReadContactAvatar :one
SELECT avatar FROM contacts WHERE id = ?;

-- name: SetContactAvatar :execrows
UPDATE contacts SET avatar = ?, avatar_color = ?, updated_at = ? WHERE id = ?;
