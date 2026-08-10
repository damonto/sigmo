CREATE INDEX pairings_expires_at ON pairings(expires_at);
CREATE INDEX pairings_device_status_expires ON pairings(device_id, status, expires_at);

DROP INDEX challenges_device_id;
DELETE FROM challenges
WHERE rowid NOT IN (
    SELECT MAX(rowid)
    FROM challenges
    GROUP BY device_id
);
CREATE UNIQUE INDEX challenges_device_id ON challenges(device_id);
CREATE INDEX challenges_expires_at ON challenges(expires_at);
