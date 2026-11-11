# Client CCID transfer over Remote APDU

An iPhone or iPad can supply the source SIM/eUICC through a locally connected CCID reader. The target remains a modem managed by Sigmo. iOS discovers readers and forwards raw APDUs with CryptoTokenKit; the Sigmo backend continues to run the existing USIM, LPA and TS.43 code, including profile activation and source-deletion confirmation.

This uses the existing `esimTransfer` feature. There is no separate feature flag. Server-attached readers and modem sources keep their existing behavior. Client readers are discovered by iOS and are not returned by the backend's HTTP source-list endpoint.

## Connection and browsing

Use the existing authenticated WebSocket endpoint:

```text
GET /api/v1/modems/:id/esim-transfers/sessions
```

iOS requires an HTTPS backend, yielding a WSS connection. Selecting a local reader binds that connection to its exact slot name. The version handshake is performed before any card session opens:

```json
{"type":"source","version":1,"sourceType":"client_ccid","sourceId":"USB reader"}
{"type":"source_ready","version":1}
```

The client requests profiles on the same socket:

```json
{"type":"profiles","sourceType":"client_ccid","sourceId":"USB reader","sourceImei":"123456789012345"}
```

The backend issues card requests as needed and returns `{"type":"profiles","profiles":[...]}` using the existing transfer-profile representation. The source IMEI initially uses the target modem's IMEI in iOS and remains editable. After profile selection, the client sends the existing `start` message with `seId`, `profileId`, `sourceImei`, `sourceType: "client_ccid"` and the bound `sourceId`. Browsing and transfer share one socket; no reader grant is persisted or transferable to another connection.

## Card operations

Each operation has a decimal-string `id`, increasing from `"1"` across the entire socket. `open` creates a new `connectionId` equal to that request's ID. Only one card session and one card operation may be active at a time.

```json
{"type":"card_request","card":{"id":"1","connectionId":"1","operation":"open"}}
{"type":"card_response","card":{"id":"1","connectionId":"1"}}
{"type":"card_request","card":{"id":"2","connectionId":"1","operation":"transmit","command":"AKQAAA=="}}
{"type":"card_response","card":{"id":"2","connectionId":"1","data":"kAA="}}
{"type":"card_request","card":{"id":"3","connectionId":"1","operation":"close"}}
{"type":"card_response","card":{"id":"3","connectionId":"1"}}
```

`command` and `data` are base64-encoded raw APDU bytes. Responses include SW1/SW2 unchanged; the backend interprets them. Commands must contain 4–65,544 bytes and responses 2–65,538 bytes. The native reader's smaller input limit also applies. Successful `open` and `close` replies have no data. A reader error can reply with `error: "reader_interrupted"`; it ends the grant.

Closing a native card session allows the backend to reopen it during the same flow, using a new `connectionId`. It does not permit substituting a different reader or SIM. Malformed replies, duplicate/stale IDs, unsupported operations, timeouts and transport failures terminate the connection. APDUs and peer-provided error details are not included in bridge error logs.

## Hot plugging and interruption

iOS observes both reader-list changes and individual slot states. A connected reader without a ready SIM remains visible but cannot be selected. Inserting a SIM enables selection; unplugging the reader removes it from the list.

Binding a reader pins one `TKSmartCard` object across native sessions and observes its validity. Removing either the reader or SIM invalidates the grant, closes the socket, releases pending native operations and clears the selectable profiles. A quick reinsert does not revive that grant. Returning to the source list, selecting a source and reading its profiles again starts a new session. Leaving the flow also releases the card and observers. Native session cleanup balances successful `beginSession` calls, including callbacks arriving after cancellation.

The native operation deadline is 25 seconds; the server's card-operation deadline is 30 seconds. Browsing waits up to 45 seconds per server message, including the initial protocol handshake. Interactive transfer prompts retain the existing unbounded user wait. iOS background execution is finite; expiration cancels the operation.

There is no automatic reconnect or APDU replay. After a disconnect, an APDU may already have executed even if its response was lost. Check the source and target state before manually retrying.

## Verification

Go tests use a local WebSocket server and a fake card to exercise the ISO 7816 adapter, session binding, cancellation, stale responses and failures. Session cancellation also closes the socket to interrupt LPA calls that use a background context. iOS tests inject reader discovery and card operations to cover browsing-to-transfer socket reuse, hot plugging, removal during an APDU, cleanup, malformed messages, concurrent receivers and response loss. These tests do not access real readers or modems. Physical-device signing, reader compatibility and a real carrier transfer still require hardware validation.

## Transfer from another Sigmo

The download/add-eSIM screen offers **Transfer from another Sigmo**. Enter the
source service's base URL, including any reverse-proxy prefix. The target backend
connects directly to the source; neither client receives the other service's
login credentials. HTTPS uses normal certificate verification. Plain HTTP is
restricted to loopback, private network and CGNAT/VPN addresses, checked at dial
time. Existing entry access controls still apply: a proxy that requires its own
credentials must allow this direct connection through an operator-configured
route. A remote transfer does not bypass that boundary.

The requester supplies only the target device and source service address. The
source owner receives the request on any authenticated Web/iOS page, selects a
local modem or server-attached CCID reader and a transferable eSIM, and approves
that exact profile moving to the target IMEI/EID. Approval includes deleting the
source profile when required by the carrier. The target immediately begins the
existing carrier and installation flow; it never chooses a source or profile.

Available profiles are disclosed only through the source service's authenticated
request subscription. The direct service connection receives no catalog: it
learns only the approved profile after the source owner's decision. Physical SIMs
and another iOS client's reader are excluded. Source activation, identity reads,
AKA and deletion run on the source backend. Carrier interaction and installation
run on the target backend. Source EID/ICCID and target IMEI/EID are rechecked
before transfer. The source owner supplies the source IMEI for a CCID reader.
No arbitrary APDU endpoint is exposed between services.

The target client uses its existing authenticated transfer socket, beginning
with `{"type":"source","version":1,"sourceType":"sigmo","sourceUrl":"https://source.example/prefix","seId":"target-se"}`.
It waits at `waiting_authorization` until the source owner chooses and approves.
A version-1 `source_selected` message contains only `sourceProfile` and
`sourceName` for that choice; ordinary transfer progress/events follow without a
second client command. Requester-supplied source IDs, profile IDs and source
IMEIs are rejected.

The direct service socket is `GET /api/v1/esim-transfers/remote-sessions`.
Version 1 begins with an `authorize` request and a `pending` acknowledgement,
then an `approved` response containing only the source owner's choice.
Authenticated clients subscribe to `GET /api/v1/esim-transfer-requests/events`
and receive complete `requests` snapshots, including local `sources` options
while approval is pending. A decision is
`POST /api/v1/esim-transfer-requests/:id/decisions` with
`{phase: "authorization", revision, accept, selection: {sourceType, sourceId, profileId, sourceImei?}}`.
Rejection omits `selection`. An accepted choice is resolved against the server's
read-only catalog snapshot, never against client-supplied profile metadata.
Stale, repeated and disconnected-session decisions return HTTP 409 with
`transfer_request_expired`; invalid selections return HTTP 400.

The request expires after five minutes, including source enumeration and
selection. One connection grants one transfer, with no persistent pairing or
reusable token. Success, refusal, expiry, cancellation, failure, shutdown or
disconnect ends the grant. Passive approval subscriptions can reconnect to
obtain pending requests; transfer/card operations never reconnect or replay.
An interrupted operation may already have changed card state: both clients show
an uncertain result and require the user to inspect source and target before
starting another request.

Local fake-peer tests cover source-owned selection, absence of requester catalog
access, exact-profile deletion, revocation, concurrent decisions, device changes,
unsupported protocol rejection and transport cleanup. Hardware and real carrier
transfers remain separate validation.

### Remote protocol lifecycle

Once the source replies `ready`, each target-to-source operation carries an
increasing numeric `id`, starting at 1. A `reply` echoes that exact `id`. A stale,
duplicate or unmatched ID terminates the session; an uncertain operation is
never replayed. Error frames use a stable `code` (`declined`, `expired`, `unsupported`, or
`ended`); peer-provided diagnostic text is not used for error classification.
Device, SIM identity and AKA payloads use Sigmo-owned JSON types rather than
serializing a dependency's structs directly.

Progress is coalesced separately from card operations so UI callbacks do not
wait for remote acknowledgements. Feature shutdown cancels and joins the
hijacked request scopes, including device cleanup, before closing shared pools.
