package storage

import (
	"context"
	"database/sql"
	"fmt"
)

func migrateNativeCalls(ctx context.Context, tx *sql.Tx) error {
	for _, column := range []struct{ table, name, definition string }{
		{"calls", "owner_client_id", "TEXT NOT NULL DEFAULT ''"},
		{"native_push_subscriptions", "calls", "INTEGER NOT NULL DEFAULT 0"},
	} {
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pragma_table_info(?) WHERE name = ?)`, column.table, column.name).Scan(&exists); err != nil {
			return fmt.Errorf("inspect call schema: %w", err)
		}
		if !exists {
			// All identifiers and definitions are fixed above, never request data.
			if _, err := tx.ExecContext(ctx, "ALTER TABLE "+column.table+" ADD COLUMN "+column.name+" "+column.definition); err != nil {
				return fmt.Errorf("extend call schema: %w", err)
			}
		}
	}
	_, err := tx.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS native_call_outbox (
			subscription_id TEXT NOT NULL REFERENCES native_push_subscriptions(id) ON DELETE CASCADE,
			call_id TEXT NOT NULL REFERENCES calls(id) ON DELETE CASCADE,
			delivered INTEGER NOT NULL DEFAULT 0,
			next_at INTEGER NOT NULL,
			expires_at INTEGER NOT NULL,
			PRIMARY KEY(subscription_id, call_id)
		)
	`)
	if err != nil {
		return fmt.Errorf("create call outbox: %w", err)
	}
	return nil
}
