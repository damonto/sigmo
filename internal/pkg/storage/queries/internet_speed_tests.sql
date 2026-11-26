-- name: LatestInternetSpeedTest :one
SELECT download_bps, upload_bps, tested_at FROM internet_speed_tests WHERE iccid = ?;

-- name: SaveInternetSpeedTest :exec
INSERT INTO internet_speed_tests (iccid, download_bps, upload_bps, tested_at)
VALUES (?, ?, ?, ?)
ON CONFLICT (iccid) DO UPDATE SET
    download_bps = excluded.download_bps,
    upload_bps = excluded.upload_bps,
    tested_at = excluded.tested_at;
