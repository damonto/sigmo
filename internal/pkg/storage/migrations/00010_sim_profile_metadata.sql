-- +goose Up
ALTER TABLE sim_profiles ADD COLUMN icon TEXT NOT NULL DEFAULT '';
ALTER TABLE sim_profiles ADD COLUMN display_known INTEGER NOT NULL DEFAULT 0;
