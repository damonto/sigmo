package storage

import (
	"context"
	"crypto/rand"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Migrate applies pending migrations before application queries use the database.
func (s *Store) Migrate(ctx context.Context) error {
	if s == nil || s.db == nil {
		return errors.New("storage is nil")
	}
	files, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return fmt.Errorf("open database migrations: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectSQLite3, s.db, files,
		goose.WithDisableGlobalRegistry(true),
		goose.WithGoMigrations(
			goose.NewGoMigration(1, &goose.GoFunc{RunTx: migrateLegacy}, nil),
			goose.NewGoMigration(3, &goose.GoFunc{RunTx: initializeMessageState}, nil),
			goose.NewGoMigration(5, &goose.GoFunc{RunTx: migrateAuthentication}, nil),
			goose.NewGoMigration(8, &goose.GoFunc{RunTx: migrateCommunicationKeys}, nil),
		),
	)
	if err != nil {
		return fmt.Errorf("configure database migrations: %w", err)
	}
	current, target, err := provider.GetVersions(ctx)
	if err != nil {
		return fmt.Errorf("read database migration versions: %w", err)
	}
	// Goose Up only checks for pending migrations; it also accepts newer schemas.
	if current > target {
		return fmt.Errorf("database schema version %d is newer than supported version %d", current, target)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("migrate database: %w", err)
	}
	return nil
}

func initializeMessageState(ctx context.Context, tx *sql.Tx) error {
	// Existing installations retain the identity used by clients and push subscriptions.
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO message_state (id, backend_id) VALUES (1, ?)`, rand.Text()); err != nil {
		return fmt.Errorf("initialize backend identity: %w", err)
	}
	return nil
}
