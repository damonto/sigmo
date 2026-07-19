# Permission and tool map

`list_authorized_modems` is available to every valid API key and returns only currently available modems inside its grant. All other tools require the listed permission and an authorized `modemId`.

| Permission | Tools |
| --- | --- |
| `modem.read` | `get_modem_status` |
| `sim.read` | `list_sim_cards`, `list_secure_elements` |
| `sim.switch` | `switch_sim_slot` |
| `sim.update` | `update_msisdn` |
| `esim.read` | `list_esim_profiles`, `discover_esim_profiles` |
| `esim.download` | `download_esim_profile` |
| `esim.manage` | `enable_esim_profile`, `rename_esim_profile` |
| `esim.delete` | `delete_esim_profile` |
| `sms.read` | `list_sms_conversations`, `list_sms_messages` |
| `sms.send` | `send_sms` |
| `sms.delete` | `delete_sms_conversation` |
| `ussd.execute` | `execute_ussd` |
| `network.read` | `list_networks`, `get_network_modes`, `get_network_bands`, `get_airplane_mode` |
| `network.register` | `register_network` |
| `network.power` | `set_airplane_mode` |
| `internet.read` | `get_internet_connection`, `get_public_ip` |
| `internet.connect` | `connect_internet`, `disconnect_internet` |
| `internet.configure` | `set_internet_preferences` |
| `wifi_calling.read` | `get_wifi_calling` |
| `wifi_calling.write` | `set_wifi_calling`, `reconnect_wifi_calling`, `disconnect_wifi_calling` |
| `volte.read` | `get_volte` |
| `volte.write` | `set_volte` |
| `calls.read` | `list_calls` |
| `calls.delete` | `delete_call_record` |

The running Sigmo build may omit Pro permissions and tools. Wi-Fi Calling, VoLTE, and call-record permissions require the IMS build feature. An all-modems grant covers future modems dynamically; a fixed grant never expands.

Tools that send data, interrupt service, change routing, enable profiles, or delete records require a server-side form confirmation. If the MCP client cannot provide elicitation, perform the operation in the Sigmo Web UI.

## Structured outputs

Use `structuredContent` for every successful tool call. The server returns a fixed object shape and advertises the complete field descriptions in each tool's `outputSchema`; do not parse the short text summary.

| Tool | Stable top-level output | Identifier and value notes |
| --- | --- | --- |
| `list_authorized_modems` | `{ "modems": [] }` | `modems[].id` is the modem IMEI and the exact `modemId` for later tools; `name` is its configured alias or model. |
| `get_modem_status` | `{ "modem": { ... } }` | Returns device identity, revision, lifecycle state, and lock status. SIM, network, and IMS state use their dedicated tools. |
| `list_sim_cards` | `{ "sims": [] }` | `sims[].identifier` is the SIM ICCID; `active` marks the selected slot. |
| `list_secure_elements` | `{ "ses": [] }` | `ses[].id` is the exact `seId`; `freeSpace` is measured in bytes. |
| `list_esim_profiles` | `{ "ses": [] }` | Use a profile's exact `seId` and `iccid` for management tools. |
| `discover_esim_profiles` | `{ "profiles": [] }` | Each item contains the discovery `eventId` and SM-DP+ `address`. |
| `list_sms_conversations`, `list_sms_messages` | `{ "messages": [] }` | `timestamp` is UTC; `incoming` indicates received messages and `routed` indicates Sigmo route processing. |
| `list_networks` | `{ "networks": [] }` | Use the exact `operatorCode` with `register_network`. |
| `get_network_modes` | `{ "supported": [], "current": { ... } }` | Numeric `allowed` and `preferred` values are configuration values; labels explain them. |
| `get_network_bands` | `{ "supported": [], "current": [] }` | Use numeric `value` entries when configuring bands outside this Skill. |
| `get_internet_connection`, `connect_internet`, `set_internet_preferences` | connection object | Durations are seconds and traffic counters are bytes. MCP redacts APN and proxy passwords in responses. |
| `get_public_ip` | `{ "ip": "", "country": "", "organization": "" }` | Empty strings mean enrichment data is unavailable. |
| `list_calls` | `{ "calls": [] }` | `callID` is the exact record identifier; timestamps are RFC 3339 strings and unanswered/unended times are empty strings. |
| write tools | `{ "success": true }` or operation-specific object | `send_sms` returns `{ "to": "..." }`; `execute_ussd` returns `{ "reply": "..." }`. |

Fields not summarized here remain part of the fixed output and are defined in the live `outputSchema`. Prefer the schema over examples when the running Sigmo version adds fields.
