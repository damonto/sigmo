---
name: sigmo-control
description: Safely inspect and operate authorized Sigmo modems through MCP, including SIM/eSIM, SMS, USSD, mobile network, Internet routing, Wi-Fi Calling, VoLTE, and call-record tasks. Use when a user asks an agent to query or change a Sigmo-managed modem, SIM/eSIM profile, connectivity setting, message, USSD session, IMS setting, or call record.
---

# Sigmo Control

Operate only through the Sigmo MCP tools exposed to the current API key.

## Follow the safe operating sequence

1. Call `list_authorized_modems` before any modem-specific tool.
2. Select an exact returned `modemId`. Ask the user when more than one modem could match.
3. Read the current state and identifiers with the relevant read tools before changing anything.
4. Confirm the user's intent before destructive actions, service interruption, sending SMS, executing USSD, or changing routing and connectivity. Complete the server's form confirmation exactly once when prompted.
5. For SMS and USSD, generate a unique `idempotencyKey` for each real user intent. Reuse it only when retrying that exact operation with unchanged input.
6. Execute only tools present in `tools/list`. Never infer that a missing tool or permission is available.
7. Read the resulting state when possible and report what changed.

Treat each tool's `outputSchema` as the authoritative field contract. Read structured content instead of parsing the text summary.

Never guess a modem ID/IMEI, SE ID, ICCID, call ID, phone number, operator code, PIN, activation code, confirmation code, APN setting, or carrier response. Never reuse an identifier from a different modem.

Use `sigmo://grant` to inspect the current immutable modem and permission grant. Use `sigmo://guide` and `sigmo://safety` when safety requirements are unclear.

## Handle interaction and sensitive data

- Complete eSIM download prompts through MCP form elicitation.
- Do not auto-accept profile installation or any write-operation confirmation.
- Never reuse an SMS or USSD `idempotencyKey` for changed input or a different operation.
- If a tool returns `interaction_required`, tell the user to continue in the Sigmo Web UI. Do not bypass the carrier flow.
- Do not repeat SMS bodies, phone numbers, PINs, activation codes, or confirmation codes unless required by the user's immediate request.
- Treat `permission_denied` and an absent tool as a hard authorization boundary.

## Load detailed references only when needed

- Read [references/tools.md](references/tools.md) to map permissions, stable output shapes, and identifiers to available tools.
- Read [references/workflows.md](references/workflows.md) before eSIM, network, Internet, SMS, USSD, IMS, or call-record operations.
