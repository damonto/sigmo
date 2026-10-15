# Active SIM PIN management

Sigmo manages PIN1 on the currently active physical SIM or enabled eSIM using QMI or MBIM. Inactive profiles must be selected separately. PIN2 and carrier personalization locks are outside this API.

`GET /api/v1/modems/:id/sim-pin` returns `supported`, `enabled`, `lock`, `pinRetries`, `pukRetries`, and `simToken`. Protection and verification are independent. Nullable protection and retry fields mean unknown. Lock values are `none`, `pin`, `puk`, `permanently_blocked`, `other`, or `unknown`.

| Method and modem-relative path | JSON body | Result |
| --- | --- | --- |
| `PUT /sim-pin/protection` | `enabled`, `pin`, `simToken` | 204 |
| `POST /sim-pin-changes` | `oldPin`, `newPin`, `simToken` | 204 |
| `POST /sim-unlocks` | `pin`, `simToken` | 204 |
| `POST /sim-unlocks` | `puk`, `newPin`, `simToken` | 204 |

All credential values are strings. PIN is 4–8 ASCII digits; PUK is exactly eight. Leading zeros are significant. Legacy PIN-only unlock requests may omit `simToken`; every new client should supply it. New management and PUK requests require the token.

The token binds the modem instance and SIM activation. A slot or eSIM activation invalidates it before sending the switching command, including when the switch times out. Status and credential commands share the SIM reservation with profile and slot operations. Writes re-read status under that reservation and reject stale targets. An already satisfied protection setting succeeds without verifying the supplied PIN again.

Each request sends at most one credential command. A successful response remains successful if a later status read fails. After any response, read status again. Transport failure cannot establish whether the device accepted the command: `sim_pin_result_unknown` requires status inspection, never automatic resubmission. Unlock enables the modem afterward; `enable_modem_after_unlock_failed` means the SIM is already unlocked and the radio needs a separate operation.

Problems use the shared Problem Details contract. Codes distinguish invalid input (`sim_pin_invalid_request`), wrong PIN/PUK (`sim_pin_incorrect`, `sim_puk_incorrect`), PUK required (`sim_puk_required`), permanent blocking (`sim_permanently_blocked`), stale target (`sim_target_changed`), conflicting state (`sim_pin_state_conflict`), unsupported (`sim_pin_unsupported`), unavailable status (`sim_pin_unavailable`), and unknown result. Retry counts are refreshed through GET. The legacy `unlockSupported` field remains true only for a known PIN1 lock.

Web uses one dialog from the active profile's menu or the physical SIM menu. iOS uses a SIM PIN destination in SIM details and a separate unlock sheet, which closes as soon as unlocking is acknowledged. Its parent refreshes the modem independently. iOS failures use native alerts. Locked cards also have an entry without an ICCID or profile list. Old servers hide management and retain PIN-only unlocking. Clients keep credentials only in the active form, clear them on submission/exit/target change, disable duplicate writes, and ignore obsolete responses. Status refreshes automatically after operations; there is no manual refresh control.

MCP uses `get_sim_pin_status` under `sim.read`; its four credential tools require a separate `sim.pin` grant. See [the tool map](../../../skills/sigmo-control/references/tools.md) and [workflow](../../../skills/sigmo-control/references/workflows.md). Credentials are tool arguments, never elicitation fields. Confirmation describes only the operation and target. The existing idempotency cache covers identical submissions for 30 minutes; restart clears it. Audits and responses contain no credentials.

Automated tests use protocol doubles and local client fixtures. The offline iOS demo uses PIN `1234` and PUK `12345678`; these are fictional local values. No test consumes a real SIM attempt.
