# Sigmo repository instructions

Maintain the existing Sigmo architecture with small, reviewable changes. Prefer clear, idiomatic code that a developer new to Go can follow. These instructions describe repository conventions; dependency manifests, source code, and tool configuration define the current implementation.

## Scope and workflow

- Follow explicit user instructions first, then applicable repository instructions. More specific instructions in a descendant directory take precedence within that directory. Read [apps/web/AGENTS.md](apps/web/AGENTS.md) before changing Web code.
- Inspect the working tree, nearby code, and relevant tests before editing. Preserve unrelated and concurrent changes; do not reset, revert, or reformat them.
- Keep changes within the requested scope. Reuse existing packages and extension points; do not migrate frameworks, reorganize directories, or introduce abstractions as incidental cleanup.
- Verify dependency versions and APIs against the affected module's manifests and existing usage. When more information is needed, use Context7 for official documentation if available, then official docs, `go doc`, or dependency source. Do not invent APIs or treat documentation examples as verified execution.
- Write documentation and code comments in English. Communicate with the user in their language, explaining non-obvious tradeoffs without prescribed praise, tutorials, or slogans.

## Repository map and boundaries

| Location | Responsibility |
| --- | --- |
| Repository root | Public Go module, `github.com/damonto/sigmo`; use the Go version declared in `go.mod`. |
| `internal/app/ims`, `internal/app/esimtransfer`, `internal/pkg/license` | Unified IMS, transfer and official configuration integration. |
| `internal/app/router/` | Echo v5 routes; response and error conventions live in `internal/app/httpapi/`. |
| `internal/pkg/storage/` | Go SQLite persistence, sqlc queries, and goose migrations. |
| `apps/web/` | Vue, TypeScript, Vite, and Bun client; production `dist` assets are embedded in the Go binary. |
| `apps/ios/` | SwiftUI client; `project.yml` is the source for the generated Xcode project. |
| `pro-api/` | Separate TypeScript/Hono Cloudflare Worker with its own persistence and tooling. |

- IMS, transfer and licensing share the root Go module. Keep `server.Runtime` extension points and register feature routes through `Runtime.AddRoute`. No private carrier JSON may be embedded in code or checked into Git.
- Keep clients under `apps/`; use `apps/android/` if Android support is added.
- A local `go.work` may reference sibling library checkouts; do not make those local paths a requirement for other developers or CI.
- Preserve existing API contracts. Use resource-oriented routes and appropriate HTTP methods/status codes for new endpoints, following Echo's `:param` syntax and existing response helpers. Do not rename published routes solely to enforce a naming preference.
- For API changes, check affected Go request/response types, Web API clients and types, iOS consumers, English/Chinese localization, and contract fixtures. Update the affected producers and consumers together.

## Go implementation conventions

Use the [Google Go Style Guide](https://google.github.io/styleguide/go/guide) as the baseline for handwritten Go code. Repository-specific rules and local consistency take precedence; apply style improvements to code in scope.

- Prefer the standard library and existing dependencies. Use `slices`, `maps`, and modern Go features when they improve clarity; ordinary loops remain appropriate for side effects and error handling.
- Name packages and types for their responsibility. Avoid catch-all utility packages and vague names; do not ban useful domain names by suffix. Follow Go initialisms (`HTTP`, `URL`, `ID`) and conventional short names such as `ctx`, `err`, and `mu`.
- Prefer concrete types by default. Define small interfaces at the consumer when substitution, testing, or an external boundary requires them. A single implementation alone is not a reason to introduce or delete an interface.
- Prefer composition with explicit fields; embed only when promoting the embedded API is intentional. Keep transport, storage, and domain types separate when their responsibilities differ, even if their fields currently match.
- Keep functions focused and control flow flat. Group related configuration when it makes call sites clearer; use functional options only when their flexibility is needed, not because an arbitrary parameter count was exceeded.
- Prefer concrete types or appropriate type parameters over `any` in business logic. Use `any`, reflection, or `unsafe` only for a clear requirement that simpler typed code cannot reasonably meet. Preserve required external and generated signatures.
- Handle errors explicitly. Add meaningful context with `fmt.Errorf("action: %w", err)` at boundaries without repeating existing context; use `errors.New` for fixed errors and `errors.Is`/`errors.As` for classification. Keep messages lowercase, without trailing punctuation or redundant "failed to" prefixes. Make intentional error suppression clear, explaining non-obvious cases.
- Pass `ctx context.Context` as the first parameter when needed, and propagate cancellation. Normally pass contexts to methods rather than storing them. Every goroutine needs an owner and a defined exit path; use cancellation and joining where its caller owns completion. Choose mutexes or channels for the actual coordination need.
- Make resource ownership and cleanup explicit. Use `defer` when cleanup belongs at function exit; use a smaller function or explicit cleanup when resources must be released on each loop iteration.
- Run `gofmt` on changed handwritten Go files. Give exported APIs Go doc comments beginning with their names; explain contracts, constraints, and non-obvious decisions instead of narrating syntax.

## Go storage: sqlc and goose

These rules apply to `internal/pkg/storage/`, not the separate `pro-api/` persistence implementation. Use the existing `github.com/ncruces/go-sqlite3` driver with `database/sql`, sqlc for query generation, and goose for migrations. sqlc is a SQL code generator, not an ORM. See the [storage guide](internal/pkg/storage/README.md) for implementation details.

### Queries and generated code

- Put new static application queries in `internal/pkg/storage/queries/*.sql`. Migrate existing handwritten queries when they are part of the task. Dynamic SQL may remain handwritten; bind values and allowlist any dynamic identifiers instead of interpolating request data.
- The root `sqlc.yaml` reads schema from `internal/pkg/storage/migrations/`. Do not maintain a second current-schema definition; historical fixtures and frozen legacy DDL are intentional snapshots.
- Generate into `internal/pkg/storage/query/`. Do not add another `internal/` layer, edit generated Go by hand, or post-process it for style. Change SQL or generator configuration, regenerate, and include generated output with the source changes.
- Keep the generator version pinned in `internal/pkg/storage/generate.go`; do not use `@latest` or add sqlc compiler dependencies to the application's module.
- Keep generated rows and parameter types inside the storage layer. `Store` owns validation, error context, conversions, and transaction orchestration; callers use application types. Do not add a generic repository or an interface solely to wrap generated methods.
- Use `query.New(tx)` within an existing `*sql.Tx`. All operations in the transaction, including handwritten SQL, must use that transaction rather than the store's database connection.

### Migrations and existing data

- Add SQL migrations with zero-padded numeric filenames and `-- +goose Up`. Choose each new migration version above the highest version in both SQL files and Go migrations registered in `migrations.go`; SQL filenames do not show every used version.
- Preserve published migration history. Keep `migration_legacy.go`, historical models, and backfill behavior independent of evolving application types. Add new migrations instead of rewriting old upgrade paths.
- Put schema changes in SQL so sqlc can see them. Register Go migrations only for data transformations that require Go.
- Retain embedded migrations and automatic migration in `Open` before application queries run. Preserve rejection of databases newer than the binary and atomic recording of each migration with its version.
- Apply migrations through the application's goose Provider. The standalone goose CLI lacks the registered Go migrations. Do not add automatic down migrations or mark unapplied legacy upgrades as completed.
- Verify fresh database creation, affected legacy upgrades and data preservation, repeated startup, rollback/retry, and newer-schema rejection. Verify transaction atomicity using temporary databases.

## Tests and validation

Test observable behavior and regressions. Prefer Go's `testing` package and table-driven subtests for multiple cases; keep linear lifecycle or integration tests linear. Test doubles must preserve the relevant contract, including errors and cancellation, rather than reproduce an entire implementation.

Use fakes, temporary databases, and local test servers by default. Tests must not operate real modems, eSIM profiles, network routes, or external notification services without explicit authorization. Run focused checks while iterating, then the applicable checks below before finishing.

| Change scope | Working directory | Required checks |
| --- | --- | --- |
| Public Go code | Repository root | `go test ./...` and `go vet ./...` |
| Query SQL, schema migrations, sqlc config or generator | Repository root | `go generate ./internal/pkg/storage`; review output and confirm a second run produces identical files. Run public checks for storage implementation or schema changes. |
| Web code | `apps/web/` | `bun run test:unit --run` and `bun run type-check` |
| Web build or embedded assets | `apps/web/` | Also run `bun run build --mode prod` and the affected Go build. |
| Pro API code | `pro-api/` | `bun run lint`, `bun run format:check`, `bun run type-check`, and `bun run test`; add `bun run dry-run` for build/config changes. |
| Shared HTTP error contract | Repository root | `python3 scripts/sync-http-contract.py --check` and affected consumer tests. |
| iOS code | macOS/Xcode | Follow [apps/ios/README.md](apps/ios/README.md) and [scripts/ios/MCP.md](scripts/ios/MCP.md); build/test the `Sigmo` scheme with local fixtures. |
| iOS settings or localization | Repository root | Also run `python3 apps/ios/scripts/check_settings.py`. |
| Documentation only | Affected paths | Check accuracy, referenced paths/commands, and formatting; software tests are unnecessary unless executable behavior changes. |

- For cross-layer features, run checks for each affected module/client. For release or build changes, validate the corresponding build commands without deploying as a test.
- After Go dependency changes, run `go mod tidy` in each affected module and review `go.mod`/`go.sum`. Use the declared package manager and preserve relevant lockfiles for JavaScript dependency changes.
- Scope generation to the affected package. Do not use `go generate ./...` for routine SQL changes; other generators may fetch remote data. Review generated additions and deletions, not only tracked-file diffs.
- `apps/web` lint and format scripts modify files. Run them only when prepared to review their edits, and avoid unrelated formatting changes. Deployment and remote migration commands are not validation steps.
- If a required tool, platform, or dependency is unavailable, report the exact limitation and which checks remain unrun. Do not claim success based on source inspection or substitute checks alone.

## Review and handoff

Review the final diff for correctness, scope, API compatibility, data preservation, and generated-file consistency. Summarize the outcome, relevant decisions, checks actually run, and any remaining limitation. Explain a non-obvious Go choice briefly when it helps the reader; do not turn routine changes into a tutorial.
