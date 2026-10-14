# Database queries and migrations

`Store` owns validation, transactions, error context, and conversion between
database rows and application types. `Open` creates the connection using the
existing `github.com/ncruces/go-sqlite3` driver and applies pending migrations
before returning the store.

- `migrations/` contains goose SQL migrations and supplies the schema to sqlc.
- `queries/` contains SQL inputs for contacts, communication history, SIM metadata,
  conversation acknowledgments, settings, authentication, browser and native push
  subscriptions, native delivery queues, and reminders.
- `query/` contains generated Go code. Commit these files without editing them.
- `migration_legacy.go` contains the frozen upgrade path for unversioned databases.

## Changing queries

After editing `queries/*.sql`, run these commands from the repository root:

```sh
go generate ./internal/pkg/storage
go test ./...
```

`generate.go` pins the sqlc version. The first invocation needs network access to
download the tool; its compiler dependencies stay outside the application's
`go.mod`. CI regenerates the queries and rejects uncommitted output changes.

Keep generated types inside the storage layer. `Store` returns the existing
application types and converts timestamps and nullable values explicitly. Use
`query.New(tx)` for queries that must share a transaction with handwritten SQL.
Some legacy message queries and dynamic conversation searches remain handwritten.
New static application queries and queries changed for a feature belong in
`queries/`; sqlc generates their typed Go parameters and row scanning.
Historical Go migrations retain their own SQL, independent of current queries.

## Adding migrations

The initial migration sequence is:

1. Go migration 1 upgrades existing, unversioned databases while preserving
   conversation read state, SIM history, and backend identity. Empty databases
   skip this compatibility path.
2. `00002_schema.sql` creates the complete baseline. `IF NOT EXISTS` lets databases
   upgraded by migration 1 join the same migration history.
3. Go migration 3 initializes the backend identity for new databases and preserves
   existing identities.

SQL files are embedded in the binary. Each migration and its version record commit
in the same transaction. A failure rolls back that migration so a later startup
can retry it. Startup rejects databases whose version exceeds the binary's latest
migration; running an older binary does not downgrade the database.

Choose a version above the highest SQL migration and the Go migrations registered
in `migrations.go`. Use equally padded numeric filenames and begin each SQL
migration with `-- +goose Up`. Preserve published migrations and add new ones for
subsequent changes.

SIM branding belongs to `sim_profiles`: `icon` stores the last confirmed image and
`display_known` distinguishes confirmed branding (including no icon) from a cache
miss. Both fields use the existing profile identity and transaction lifecycle.

Write schema changes in SQL. Register Go migrations in `migrations.go` only when
data transformations require Go code. sqlc cannot infer schema changes made by
Go migration functions.

The legacy DDL and backfill types are historical snapshots, independent of current
application models. Keep them frozen rather than synchronizing them with new
schema changes. Tests compare columns, indexes, and foreign keys after upgrading
a historical fixture with those in a freshly created database.

Use the application's goose Provider to apply migrations. The standalone goose
CLI does not include the registered Go migrations and cannot upgrade a Sigmo
database on its own. Automatic down migrations are not provided.

After changing migrations, regenerate queries and run the public module tests.
Also run `go test -tags=esim_transfer,ims ./...` from `pro/` to verify consumers of
the public module. After changing Go dependencies, run `go mod tidy` in each
affected module and review both `go.mod` and `go.sum`.
