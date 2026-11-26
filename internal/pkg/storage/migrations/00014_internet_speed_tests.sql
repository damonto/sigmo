-- +goose Up
CREATE TABLE internet_speed_tests (
    iccid TEXT PRIMARY KEY NOT NULL,
    download_bps INTEGER NOT NULL CHECK (download_bps > 0),
    upload_bps INTEGER NOT NULL CHECK (upload_bps > 0),
    tested_at INTEGER NOT NULL
);
