# Sigmo Pro API

This Worker owns the Telegram device pairing flow, signed Pro leases, private
release metadata, and short-lived R2 download tickets. The R2 bucket must stay
private: do not enable an `r2.dev` hostname, public bucket access, or a custom
domain for it. Binaries are read only through the `sigmo_pro_updates` Worker
binding.

## Resources

The checked-in Wrangler configuration expects:

- D1 database: `sigmo-licenses`
- private R2 bucket: `sigmo-pro-updates`
- Telegram bot username: `SigmoProBot`

Create new resources only when they do not already exist:

```bash
bunx wrangler d1 create sigmo-licenses
bunx wrangler r2 bucket create sigmo-pro-updates
```

If D1 is recreated, copy the returned database ID into `wrangler.jsonc`.
Apply the schema before deploying the Worker:

```bash
bunx wrangler d1 migrations apply sigmo-licenses --remote
```

## Database access and migrations

Runtime CRUD uses the native `D1Database` binding through the concrete
`src/database.ts` data-access type. Checked-in SQL files under `migrations/`
are the only database schema source and are applied with Wrangler.

Create a migration after changing the data model, then review the generated SQL
before applying it:

```bash
bunx wrangler d1 migrations create sigmo-licenses describe_schema_change
bunx wrangler d1 migrations apply sigmo-licenses --local
```

Replace `describe_schema_change` with a concise migration name.

Queries use prepared statements and explicit result types. Device-limit
activation uses `D1Database.batch()` so its
[multi-statement operation remains atomic](https://developers.cloudflare.com/d1/worker-api/d1-database/#batch).
Session rotation uses a generation and refresh-token compare-and-swap in the
same batch that consumes its challenge.

The authorization schema is pre-release and intentionally has no upgrade path.
Recreate the D1 database before deployment if an older copy of these migrations
has already been applied.

## Secrets and signing keys

Configure all required Worker secrets interactively so their values do not
appear in shell history:

```bash
bunx wrangler secret put SIGMO_TELEGRAM_BOT_TOKEN
bunx wrangler secret put SIGMO_TELEGRAM_WEBHOOK_SECRET
bunx wrangler secret put SIGMO_LICENSE_PRIVATE_KEY
bunx wrangler secret put SIGMO_LICENSE_PUBLIC_KEY
bunx wrangler secret put SIGMO_RELEASE_PUBLIC_KEY
bunx wrangler secret put SIGMO_DOWNLOAD_TICKET_SECRET
bunx wrangler secret put SIGMO_ADMIN_TELEGRAM_IDS
```

Formats:

- `SIGMO_LICENSE_PRIVATE_KEY`: Base64-encoded Ed25519 PKCS#8 private key.
- `SIGMO_LICENSE_PUBLIC_KEY`: Base64-encoded 32-byte raw Ed25519 public key.
- `SIGMO_RELEASE_PUBLIC_KEY`: Base64-encoded 32-byte raw Ed25519 public key
  used to verify release manifests before issuing downloads.
- `SIGMO_DOWNLOAD_TICKET_SECRET`: independently generated high-entropy HMAC
  secret of at least 32 characters.
- `SIGMO_ADMIN_TELEGRAM_IDS`: comma-separated positive Telegram numeric user
  IDs.

The license key pair signs device leases. It must be different from the release
key pair used by CI to sign update manifests. CI accepts a Base64 raw 64-byte
Ed25519 private key or Base64 PKCS#8 key as `SIGMO_RELEASE_PRIVATE_KEY`; release
binaries embed the corresponding Base64 raw 32-byte public key through
`SIGMO_RELEASE_PUBLIC_KEY`.

Also configure these repository values for Pro publishing:

- Secrets: `SIGMO_RELEASE_PRIVATE_KEY`, `SIGMO_RELEASE_PUBLIC_KEY`,
  `SIGMO_LICENSE_PUBLIC_KEY`, `SIGMO_R2_ACCESS_KEY_ID`,
  `SIGMO_R2_SECRET_ACCESS_KEY`, and `SIGMO_R2_ACCOUNT_ID`.
- Variables: `SIGMO_PRO_WORKER_URL` and `SIGMO_R2_BUCKET`.

The R2 access key should be scoped only to the Pro update bucket.

## Telegram webhook

Deploy first, then configure the bot webhook to:

```text
https://<worker-host>/v1/telegram-updates
```

Set Telegram's `secret_token` to the exact value stored in
`SIGMO_TELEGRAM_WEBHOOK_SECRET`. Keep both the bot token and webhook secret out
of command history and deployment logs.

Administrators can manage entitlements with:

```text
/grant <telegram_id> [max_devices] [expires_at]
/revoke <telegram_id>
/status <telegram_id>
/entitlements
/devices [telegram_id]
/revoke_device <device_id>
```

The optional `expires_at` argument uses `YYYY-MM-DD`. The Worker stores it as
the end of that UTC day (`23:59:59.999Z`). Omit it for a permanent entitlement.
`/entitlements` lists every currently active, unexpired Telegram entitlement.

Users can interact with the Bot through:

```text
/start [pairing_id]
/download [stable|dev]
/download <stable|dev> <target>
/devices
/revoke_device <device_id>
```

Running `/start` without a pairing ID shows the user's numeric Telegram ID,
current entitlement status, and the Stable and Dev download commands. An active
entitlement can download either channel. `/download` defaults to Stable and
returns six Linux target buttons when the release is complete; the explicit
form returns one target button. Bootstrap links expire after 5 minutes and can
be consumed only once.

The Bot mints Bootstrap tickets only while handling a command received through
the verified Telegram webhook. There is no public ticket-minting HTTP API.

The Worker reconciles Telegram's command menu every hour through a Cron Trigger.
Regular private chats receive the user commands, while each configured
administrator chat receives the full command set. When an administrator is
removed from `SIGMO_ADMIN_TELEGRAM_IDS`, the next reconciliation deletes that
chat-specific menu so Telegram falls back to the regular menu.

Cloudflare Workers do not provide a process startup hook, so the Cron Trigger is
the automatic reconciliation point after a deployment. Command names,
descriptions, and help usage come from one catalog; BotFather does not need a
separate command update.

## HTTP API

The Worker exposes resource-oriented endpoints under `/v1`:

```text
POST /v1/telegram-updates
POST /v1/license-pairings
GET  /v1/license-pairings/:id
POST /v1/license-challenges
POST /v1/license-leases
GET  /v1/release-channels/:channel/releases/latest?target=<target>
GET  /v1/downloads/:ticket
```

Every JSON error uses a stable machine-readable code and an English message:

```json
{
  "error_code": "license_pairing_not_found",
  "message": "pairing not found"
}
```

Clients must localize errors from `error_code`; `message` is the English
fallback and is not a stable programmatic value.

## Abuse protection

The Worker limits each device to three unexpired pairing resources and one
outstanding startup challenge. Production deployment must also apply Cloudflare
Rate Limiting rules to the public creation endpoints:

- `POST /v1/license-pairings`: limit by source IP.
- `POST /v1/license-challenges`: limit by source IP and, when available, the
  `deviceId` request field.
- `POST /v1/license-leases`: limit repeated invalid attempts by source IP.

Keep the Telegram webhook protected by its secret header; do not expose a WAF
exception that bypasses the Worker secret check.

## Import existing Pro users

Create a local SQL file that is not committed, with one row per Telegram numeric
user ID. Use an ISO 8601 UTC timestamp for `created_at` and `updated_at`:

```sql
INSERT INTO entitlements
  (telegram_id, status, expires_at, max_devices, created_at, updated_at)
VALUES
  (10001, 'active', NULL, 3, '2026-08-09T12:00:00Z', '2026-08-09T12:00:00Z');
```

Import it with:

```bash
bunx wrangler d1 execute sigmo-licenses --remote --file ./entitlements.sql
```

Delete the local import file after verifying `/status <telegram_id>`.

## Verify and deploy

```bash
bun install --frozen-lockfile
bun run type-check
bun run test
bun run dry-run
bun run deploy
```

The generated `worker-configuration.d.ts` is committed; run `bun run types`
after changing bindings, variables, or required secrets.

## R2 release layout and retention

CI uploads all artifacts to a versioned prefix, uploads the signed manifest,
then switches the channel's fixed `latest` manifest:

```text
stable/latest/manifest.json
stable/versions/v1.2.3/manifest.json
stable/versions/v1.2.3/manifest.json.sig
stable/versions/v1.2.3/sigmo-pro-<target>.gz

dev/latest/manifest.json
dev/versions/dev-01234567/manifest.json
dev/versions/dev-01234567/manifest.json.sig
dev/versions/dev-01234567/sigmo-pro-<target>.gz
```

After the switch succeeds, CI removes every other version prefix in that
channel. Stable and Dev therefore retain one complete release each. Automatic
updates use five-minute tickets bound to the device, channel, version, target,
and R2 object path. First installation uses five-minute, single-use Bootstrap
tickets bound to the Telegram user, channel, version, target, and object path.
Every Bootstrap download rechecks the entitlement, so revocation or expiry
takes effect immediately. Bootstrap downloads require a full GET; device
updates continue to support Range responses. The R2 bucket remains private and
is never exposed through a presigned direct URL. Every artifact is a
gzip-compressed executable; the Worker returns
the compressed bytes with `Content-Type: application/gzip` and never sets
`Content-Encoding`.

## Bootstrap order

1. Deploy D1, private R2, Worker secrets, Worker, and Telegram webhook.
2. Publish complete Pro Stable and Dev releases, including each channel's
   `latest/manifest.json` and six Linux artifacts.
3. Import existing Telegram users as active entitlements; grant new users from
   the administrator Bot commands.
4. Ask the user to open the Bot and run `/start` to verify their Telegram ID and
   entitlement status.
5. The user runs `/download stable` (or `/download dev`) and downloads the
   matching `.gz` artifact from the 15-minute Worker link. After downloading,
   run `gzip -d sigmo-pro-<target>.gz` and `chmod +x sigmo-pro-<target>`.
6. On first startup, the Pro client opens `/start <pairing_id>` and completes
   device authorization.
7. The authorized device continues through the existing lease-based automatic
   update flow.
8. Remove legacy Telegram file-distribution secrets and scripts after the Bot
   download flow is verified in production.
