CREATE TABLE configuration_downloads (
    device_id TEXT PRIMARY KEY REFERENCES devices(device_id) ON DELETE CASCADE,
    minute INTEGER NOT NULL,
    count INTEGER NOT NULL CHECK (count > 0)
);
