-- +goose Up
CREATE TABLE sim_automations (
    id TEXT PRIMARY KEY NOT NULL,
    profile_id TEXT NOT NULL REFERENCES sim_profiles(profile_id),
    modem_id TEXT NOT NULL,
    se_id TEXT NOT NULL,
    eid TEXT NOT NULL,
    scheduled_at INTEGER NOT NULL,
    repeat_days INTEGER NOT NULL CHECK (repeat_days BETWEEN 0 AND 3650),
    action TEXT NOT NULL,
    next_at INTEGER NOT NULL,
    running_at INTEGER NOT NULL DEFAULT 0,
    started_at INTEGER NOT NULL DEFAULT 0,
    last_run TEXT NOT NULL DEFAULT '',
    restore_state TEXT NOT NULL DEFAULT '',
    revision INTEGER NOT NULL DEFAULT 1,
    UNIQUE (modem_id, profile_id, se_id)
);
CREATE INDEX sim_automations_by_profile ON sim_automations(profile_id);
CREATE INDEX sim_automations_by_due ON sim_automations(next_at) WHERE next_at > 0;
CREATE UNIQUE INDEX sim_automations_running_by_modem ON sim_automations(modem_id) WHERE running_at > 0;
