# Contacts and communication history

The public backend owns contacts, SMS threads, SIM inventory, line preferences and
history assignments. Pro (`ims`) additionally exposes backend-wide call history.
Clients discover `contacts`, `messageThreads`, `historyAssignments` and `callHistory`
through the existing capabilities endpoint. Deploy the backend first. Clients keep
the device-specific routes available when these capabilities are absent.

The iOS root has Devices, Messages, Calls and Contacts. The last selected backend
is shared across the three communication tabs. The Web client uses its current
backend. Contact IDs and SIM IDs are meaningful only inside that backend.

## Data and migrations

Append-only migrations add contact metadata and numbers, bounded avatar blobs,
message-level read state, history ownership, line preferences and participant
lookup keys. Existing conversation IDs, message fingerprints, call UUIDs and
recording associations remain unchanged. Existing conversation watermarks backfill
message reads. A later region-aware key backfill groups known phone numbers while
retaining the original addresses; unknown national numbers, short codes and
alphanumeric senders are never matched by suffix.

Both original `profileId` and current `historyProfileId` are returned for messages
and calls. Line filters apply to current ownership; displayed source labels refer
to the original line. Source conversations continue resolving old notifications.

## API

All routes use the existing authenticated `/api/v1` group.

| Method | Path | Behavior |
| --- | --- | --- |
| GET, POST | `/contacts` | Search (`q`) or create a contact |
| GET, PUT, DELETE | `/contacts/:id` | Read, replace metadata or delete |
| GET, PUT, DELETE | `/contacts/:id/avatar` | Authenticated thumbnail; upload or remove |
| GET | `/message-threads` | List threads; `conversationId` resolves an old conversation to `{id}` |
| GET | `/message-threads/:id` | Thread summary |
| GET | `/message-threads/:id/messages` | Paged messages with original source identities |
| GET | `/conversations/:conversationId/messages` | Original SIM conversation pages and read state |
| PATCH | `/message-threads/:id` | Acknowledge displayed `messageIds`, or set `markedUnread` |
| DELETE | `/message-threads/:id` | Delete source conversations and preserve deduplication tombstones |
| GET | `/calls` | Pro call archive; `missed=true` filters before pagination |
| GET, DELETE | `/calls/:id` | Pro record detail or deletion (terminal calls only) |
| GET | `/sim-profiles` | Cached identities with freshly verified availability and history counts |
| GET | `/communication-preferences?participant=...` | Last confirmed outgoing line |
| PUT | `/sim-profiles/:id/history-assignment` | Assign deleted history using `{targetProfileId}` |

History lists support `q`, `profileId`, `modemId`, `limit` (1–100) and opaque
`cursor`. A cursor is tied to its filters; start over after changing a filter.
Insertion snapshots and timestamp/ID cursors prevent concurrent arrivals from
shifting subsequent pages. Thread message pages are returned oldest first within
each page; prepend older pages. Reading a page does not mark it read: acknowledge
only displayed message IDs. Incoming messages arriving later stay unread.

Conversation message requests always return a page, defaulting to 50 messages;
`limit` accepts 1–100 and `cursor` loads the next older page. The response contains
`items`, `nextCursor`, `conversation` metadata and the backend-wide `backendId`,
`revision`, and `unreadCount`. This path filters by the immutable conversation ID,
so assigning history to another SIM does not mix source conversations. When a
refreshed head has a different cursor, clients adopt the new snapshot so that
backdated imports remain reachable. They catch up new pages until reaching retained
history and skip already displayed pages when loading older messages. An unchanged
head cursor preserves the reader's existing pagination progress.

Contacts accept `name`, `notes` and `numbers` (`label`, `number`, optional two-letter
`region`). Name-only entries are valid. The server computes number `key` values;
duplicate matches deliberately display the phone number. Deleting a contact does
not remove history. Avatar uploads accept JPEG/PNG up to 5 MiB and 4096 pixels per
side; the server center-crops to at most 512 square pixels, stores PNG and returns
`hasAvatar`, `avatarColor` and `updatedAt`. Clients orient and crop photos first.

## Outgoing communication

Use the returned preference only if the line is currently available. A remembered
unavailable line requires explicit reselection. Without a preference, select a
single available line or ask when several exist. Composers always display the
number and modem. Submit the chosen `expectedProfileId` through existing call/SMS
APIs. SIM changes between composing and submitting must fail without sending.

Only confirmed SMS sends and accepted outgoing dials update the backend
preference. Opening selectors, incoming traffic and failed/uncertain submissions
do not. Web event monitoring is separate from its one pinned audio session.
iOS calls retain their original backend and line when root tabs change.

## Deletion and history assignment

Deletion intent is persisted before hardware mutation. Confirmed deletion leaves
a durable `deleted` line; clients offer assignment immediately and in history
lists until handled. Unknown results remain `pending` until a successful inventory
can establish presence or absence. A locked/offline inventory never proves a SIM
has disappeared. Active profiles retain the existing deletion prohibition.

An assignment validates the target against fresh inventory, then atomically moves
history ownership and preferences. It does not enable the target, rewrite source
identities, resend messages or repeat hardware deletion. Inactive eSIMs and
physical SIMs on other modems can receive history. Repeated submissions are
idempotent, and subsequent assignments carry inherited records forward. Late
imports follow the completed successor while keeping their original source SIM.

## Local validation

Use fakes, temporary databases and fixtures, without real modem/eSIM mutations.

```sh
go generate ./internal/pkg/storage
go test ./...
(cd pro && go test -tags=esim_transfer,ims ./...)
(cd apps/web && bun run test:unit --run && bun run type-check && bun run build)
```

Run iOS builds and tests through `scripts/ios/MCP.md` on a simulator. New fixtures
and tests are `CommunicationFixture`, `CommunicationTests` and
`CommunicationFlowTests`; the `communication` Debug scenario uses only local data.
