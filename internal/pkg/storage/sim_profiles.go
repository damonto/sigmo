package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// SIMProfile stores the last known identity and display metadata of a physical SIM or eSIM.
type SIMProfile struct {
	ProfileID string
	ModemID   string
	Label     string
	Number    string
}

func migrateSIMProfiles(ctx context.Context, tx *sql.Tx) error {
	var legacy bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE type = 'table' AND name = 'message_lines')`).Scan(&legacy); err != nil {
		return fmt.Errorf("inspect SIM profile schema: %w", err)
	}
	if legacy {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE message_lines RENAME TO sim_profiles`); err != nil {
			return fmt.Errorf("rename SIM profile table: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS sim_profiles (
		profile_id TEXT PRIMARY KEY,
		modem_id TEXT NOT NULL DEFAULT '',
		label TEXT NOT NULL DEFAULT '',
		number TEXT NOT NULL DEFAULT ''
	)`); err != nil {
		return fmt.Errorf("create SIM profile schema: %w", err)
	}
	if legacy {
		return backfillSIMProfileNumbers(ctx, tx)
	}
	return nil
}

// backfillSIMProfileNumbers runs only when upgrading a legacy schema. Existing
// observations take precedence over SMS, which may predate a number change.
func backfillSIMProfileNumbers(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `UPDATE sim_profiles SET number = COALESCE((
		SELECT TRIM(CASE WHEN incoming = 1 THEN recipient ELSE sender END)
		FROM messages WHERE profile_id = sim_profiles.profile_id
			AND TRIM(CASE WHEN incoming = 1 THEN recipient ELSE sender END) <> ''
		ORDER BY timestamp DESC, id DESC LIMIT 1
	), '') WHERE number = ''`)
	if err != nil {
		return fmt.Errorf("backfill SIM profile numbers: %w", err)
	}
	return nil
}

// SIMProfileNumbers returns known numbers for the requested ICCIDs, including inactive SIMs.
func (s *Store) SIMProfileNumbers(ctx context.Context, profileIDs []string) (map[string]string, error) {
	numbers := make(map[string]string)
	if len(profileIDs) == 0 {
		return numbers, nil
	}
	// A JSON array keeps the query below SQLite's parameter limit for large eUICCs.
	ids, err := json.Marshal(profileIDs)
	if err != nil {
		return nil, fmt.Errorf("encode SIM profile IDs: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT profile_id, number FROM sim_profiles
		WHERE profile_id IN (SELECT value FROM json_each(?))`, string(ids))
	if err != nil {
		return nil, fmt.Errorf("read SIM profile numbers: %w", err)
	}
	defer func() { _ = rows.Close() }() // Rows.Err preserves iteration failures.
	for rows.Next() {
		var id, number string
		if err := rows.Scan(&id, &number); err != nil {
			return nil, fmt.Errorf("scan SIM profile number: %w", err)
		}
		numbers[id] = number
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read SIM profile numbers: %w", err)
	}
	return numbers, nil
}

// RecordSIMProfile saves confirmed SIM metadata and associates unassigned history.
// Empty metadata preserves previously known values, including numbers of inactive SIMs.
func (s *Store) RecordSIMProfile(ctx context.Context, profile SIMProfile) error {
	profile.ProfileID = strings.TrimSpace(profile.ProfileID)
	profile.ModemID = strings.TrimSpace(profile.ModemID)
	profile.Label = strings.TrimSpace(profile.Label)
	profile.Number = strings.TrimSpace(profile.Number)
	if profile.ProfileID == "" || profile.ModemID == "" {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin SIM profile update: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // Keep SIM metadata and historical attribution together.
	_, err = tx.ExecContext(ctx, `
		INSERT INTO sim_profiles (profile_id, modem_id, label, number) VALUES (?, ?, ?, ?)
		ON CONFLICT(profile_id) DO UPDATE SET
			modem_id = excluded.modem_id,
			label = CASE WHEN excluded.label <> '' THEN excluded.label ELSE sim_profiles.label END,
			number = CASE WHEN excluded.number <> '' THEN excluded.number ELSE sim_profiles.number END
	`, profile.ProfileID, profile.ModemID, profile.Label, profile.Number)
	if err != nil {
		return fmt.Errorf("record SIM profile: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE messages SET modem_id = ? WHERE profile_id = ? AND modem_id = ''`, profile.ModemID, profile.ProfileID); err != nil {
		return fmt.Errorf("backfill message device: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit SIM profile update: %w", err)
	}
	return nil
}
