# Missing phone numbers

`mdn` implements the independent phone-number prefetch entitlement used by
compatible carriers. It sends batched `getAuthentication`/`getPhoneNumber`
requests over HTTPS, processes EAP-AKA challenges with the current USIM and
continues with `postChallenge`. The prefetch nonce uses signing version 2.
Number signatures are not used for this lookup; no tokens or signatures are
retained. This is not an iMessage registration implementation.

`internal/app/mdn` owns the background lifecycle. It first restores an ICCID's
known number from SIM profile storage, including after Pro expires. Only Pro
can start new entitlement requests. A lookup is tied to a modem generation and
SIM activation. Authorization and identity are rechecked before HTTPS requests,
SIM authentication and result publication. Cancellation and joining prevent
retired work from updating a replacement SIM or a renewed authorization. Each
lookup owns its cancellation and completion signal; retired results cannot
update numbers or retry state. SIM slot reservations cover card reads and AKA,
never network waits. The modem's shared QMI/MBIM session remains
owned by the modem; the lookup closes its independent SIM reader.

The effective number priority is modem/SIM or user-written number, IMS number,
then MDN/history. An MDN result fills an empty number atomically. The existing
number-change event persists it in SIM profile storage. Clearing the IMS observation does not clear
MDN/history. No number is written to the SIM.

Temporary network, HTTP 429/5xx and carrier 6005/6050 failures back off through
30 seconds, 1 minute, 5 minutes, 15 minutes and 1 hour. A larger bounded
`Retry-After` wins. Permanent failures wait for another SIM activation,
authorization change or changed catalog contents. Reloading identical profiles
preserves both temporary backoff and permanent failure state. Polling every
30 seconds covers license changes and missed registry events; operation guards
apply immediately.

Status 6020 means `STATUS_PNR_NOT_AVAILABLE`, not an authentication failure.
It stops this lookup rather than retrying a service that did not provide a
number. Error diagnostics include the selected carrier and PLMN, and whether
an unsuccessful response contained a number field, without logging its value.

Carrier profiles arrive through the existing signed, encrypted Pro catalog.
The optional `mdn` field preserves schema version 1 compatibility. Private
`mdn.json` and its offline generator live in the separate ignored repository
`scripts/carrier-config`, alongside IMS configuration. No carrier endpoints are
embedded in production code. Protocol 1, newer signing formats and other
unimplemented authentication requirements remain excluded. Advertisement of a
service does not guarantee every subscriber can use it.

Tests use synthetic SIM authentication, scripted HTTP transports, temporary
storage and fake time; they do not operate modems or contact carriers.
