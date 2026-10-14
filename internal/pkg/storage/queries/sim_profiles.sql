-- name: SIMProfileNumbers :many
SELECT profile_id, number FROM sim_profiles
WHERE profile_id IN (SELECT value FROM json_each(CAST(sqlc.arg(profile_ids) AS TEXT)));

-- name: SIMProfileRegion :one
SELECT CAST(COALESCE((SELECT region FROM sim_profiles WHERE profile_id = ?), '') AS TEXT) AS region;

-- name: RecordSIMProfile :exec
INSERT INTO sim_profiles (profile_id, modem_id, label, number, region, kind, se_id)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(profile_id) DO UPDATE SET
    modem_id = excluded.modem_id,
    label = CASE WHEN excluded.label <> '' THEN excluded.label ELSE sim_profiles.label END,
    number = CASE WHEN excluded.number <> '' THEN excluded.number ELSE sim_profiles.number END,
    region = CASE WHEN excluded.region <> '' THEN excluded.region ELSE sim_profiles.region END,
    kind = CASE WHEN excluded.kind <> '' THEN excluded.kind ELSE sim_profiles.kind END,
    se_id = CASE WHEN excluded.se_id <> '' THEN excluded.se_id ELSE sim_profiles.se_id END;

-- name: ReadSIMDisplay :one
SELECT label, icon FROM sim_profiles WHERE profile_id = ? AND display_known = 1;

-- name: RecordSIMDisplay :exec
UPDATE sim_profiles SET icon = ?, display_known = 1 WHERE profile_id = ?;

-- name: InvalidateSIMDisplay :exec
UPDATE sim_profiles SET display_known = 0 WHERE profile_id = ?;

-- name: BackfillMessageModem :exec
UPDATE messages SET modem_id = ? WHERE profile_id = ? AND modem_id = '';

-- name: ProfileConversationParticipants :many
SELECT c.id, c.participant, CAST(COALESCE(p.region, '') AS TEXT) AS region
FROM conversations c LEFT JOIN sim_profiles p ON p.profile_id = c.profile_id
WHERE c.profile_id = ?;

-- name: UpdateConversationParticipantKey :exec
UPDATE conversations SET participant_key = ? WHERE id = ?;
