package storage

import (
	"context"
	"database/sql"
	"fmt"
)

func normalizeConversationKeys(ctx context.Context, tx *sql.Tx, profileID string) error {
	rows, err := tx.QueryContext(ctx, `SELECT c.id, c.participant, COALESCE(p.region, '') FROM conversations c LEFT JOIN sim_profiles p ON p.profile_id = c.profile_id WHERE (? = '' OR c.profile_id = ?)`, profileID, profileID)
	if err != nil {
		return fmt.Errorf("read historical participants: %w", err)
	}
	type participant struct{ id, number, region string }
	var participants []participant
	for rows.Next() {
		var value participant
		if err := rows.Scan(&value.id, &value.number, &value.region); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan historical participant: %w", err)
		}
		participants = append(participants, value)
	}
	err = rows.Err()
	_ = rows.Close() // Release the cursor before updating its rows.
	if err != nil {
		return fmt.Errorf("read historical participants: %w", err)
	}
	for _, value := range participants {
		if _, err := tx.ExecContext(ctx, `UPDATE conversations SET participant_key = ? WHERE id = ?`, ParticipantKey(value.number, value.region), value.id); err != nil {
			return fmt.Errorf("normalize historical participant: %w", err)
		}
	}
	return nil
}

func migrateCommunicationKeys(ctx context.Context, tx *sql.Tx) error {
	if err := normalizeConversationKeys(ctx, tx, ""); err != nil {
		return err
	}
	// Only an answered legacy call proves successful outgoing communication.
	// New calls record acceptance explicitly; opening a selector never seeds a preference.
	_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO communication_preferences(participant, profile_id, updated_at)
        SELECT participant, history_profile_id, stamp FROM (
            SELECT participant, history_profile_id, stamp, ROW_NUMBER() OVER (PARTITION BY participant ORDER BY julianday(stamp) DESC) AS rank FROM (
                SELECT c.participant_key AS participant, m.history_profile_id, m.timestamp AS stamp FROM messages m JOIN conversations c ON c.id = m.conversation_id WHERE m.incoming = 0 AND m.status IN ('sent', 'delivered')
                UNION ALL SELECT number, history_profile_id, started_at FROM calls WHERE direction = 'outgoing' AND answered_at <> '0001-01-01T00:00:00Z'
            )
        ) WHERE rank = 1`)
	if err != nil {
		return fmt.Errorf("seed communication preferences: %w", err)
	}
	return nil
}
