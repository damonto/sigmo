# HTTP resource conventions

All paths below are relative to `/api/v1`. `router.go` is the source of truth
for methods, middleware, and handler bindings.

## Resource boundaries

| Resource | Read | Write |
| --- | --- | --- |
| Available network operators | `GET /modems/:id/networks` | Network scans are created separately under `/modems/:id/network-scans`. |
| Network configuration | `GET /modems/:id/network-settings/{registration,airplane-mode,modes,bands}` | `PUT` on the same URI replaces the writable configuration. |
| Physical SIM selection | Included in the modem representation | `PUT /modems/:id/sim-slot-selection` with `{"slot":2}`. Repeating the selected slot succeeds without switching again. |
| Secure elements | `GET /modems/:id/secure-elements` | Installed profiles live under `/modems/:id/secure-elements/:seId/esim-profiles/:iccid`. |
| eSIM inventory | `GET /modems/:id/esim-profiles` | Activation is a selection: `PUT /modems/:id/secure-elements/:seId/active-esim-profile` with `{"iccid":"..."}`. |
| Modem message conversations | `GET /modems/:id/message-conversations` and `GET /modems/:id/message-conversations/:participant/messages` | Delete a modem conversation at `/modems/:id/message-conversations/:participant`. Send SMS with `POST /modems/:id/messages`. |

Mode and band configuration responses retain `supported` metadata and the
`current` setting. Writes replace `current`: `{"current":{"allowed":4,"preferred":0}}`
for modes and `{"current":[{"technology":4,"number":1}]}` for bands.
Supported values and display labels are server-owned metadata.

## Methods and operation results

- Use `GET` to read resources, `PUT` to replace a desired setting, `PATCH` for
  supported partial updates, and `DELETE` to remove resources.
- `POST` can create a resource or process an operation. A synchronous query or
  confirmed submission can return `200`; it need not pretend to create a job.
- For created resources with an addressable member URI, return `201` and a
  `Location`. Preserve encoded parent identifiers when constructing that URI.
- Asynchronous update installation and Wi-Fi Calling reconnection return `202`
  with a `Location` clients can read for status. A `202` is acceptance, not
  confirmation that the operation completed.
- SMS submission does not return a newly allocated message URI; its `200`
  response remains a submission receipt.
- Notification-channel authentication tests complete synchronously with `204`.
  OIDC tests create a transaction with `201` and a pollable `Location`.

Nested deletion URLs retain their modem and secure-element scope. An absent
reminder or already-deleted profile can return `204` on retry. A stale URL must
not delete a reminder that moved elsewhere or change another location's profile
history. Deleting an active profile returns `409` because its current state
prevents deletion. Completed history assignments survive deletion retries.

## Protocol endpoints

Interactive download, transfer, SIM Toolkit, call-event, and WebRTC signaling
endpoints use WebSocket. Their HTTP `GET` performs the opening handshake;
subsequent messages belong to that protocol rather than REST. A plain HTTP
request must not begin a modem mutation before the upgrade succeeds.
OIDC redirects, carrier callbacks, MCP, and the capability-scoped carrier proxy
also retain their protocol-specific contracts.

## Breaking migration

The Web and iOS clients migrate together with the backend. Old SIM slot,
`/ses`, `/esims`, `/networks/*` configuration, and modem message-conversation
paths have no compatibility aliases. The `ses` JSON response property is a
separate wire-model name and remains unchanged.

These conventions apply HTTP method semantics from
[RFC 9110](https://www.rfc-editor.org/rfc/rfc9110.html#section-9) and WebSocket
handshake semantics from
[RFC 6455](https://www.rfc-editor.org/rfc/rfc6455.html#section-1.3).
