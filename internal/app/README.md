# Application boundaries

`server` builds and shares services that own state or lifecycle, and owns
background tasks and shutdown. `router/router.go` defines all
HTTP paths, methods, middleware, and handler bindings. Feature registration
continues through `Runtime.AddRoute`.

Application services own operations that coordinate devices, persistence, or
multiple resources. HTTP handlers decode requests, call those services, and
map their results to responses. MCP calls application services directly.

| Package | Responsibility |
| --- | --- |
| `esim` | Profile inventory, provisioning, activation, durable deletion intent, and reminder cleanup. HTTP, MCP, and transfer use the same service instance. |
| `esimtransfer` | Transfer workflows, peer approvals, active connections, and their shutdown lifecycle. |
| `modem` | Modem observations and SIM operations shared by HTTP and MCP. Inventory, SIM control, preferences, and reminders have separate HTTP handlers. |
| `network` | Network selection, radio preferences, and shared scan workers. The service owns scan shutdown. |
| `euicc` | Secure-element inventory through the shared LPA pool. |
| `simprofile` | Live SIM inventory reconciliation and validation of history assignments. |
| `call` | Live calls, archived history, and WebRTC media, with separate HTTP handlers for each resource. |
| `handler/communication` | Independent contact, message-thread, and SIM-profile HTTP adapters. |
| `handler/settings` | An aggregate settings reader and separate authentication, proxy, and notification adapters. |

Keep transport-specific concerns such as WebSocket messages, cookies, response
status codes, and HTTP error mapping in handlers. Keep business side effects
in the shared application operation so HTTP, MCP, and background workflows
cannot omit them by constructing different handlers.
