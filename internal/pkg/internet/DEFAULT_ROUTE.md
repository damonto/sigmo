# System default Internet route and DNS

## Previous behavior

Sigmo opens the cellular data session and configures the Linux interface through netlink. The [normal bearer](bearer_config.go), [QMAP mux](qmap.go), and [Qualcomm 410 data path](qualcomm410.go) each obtain carrier DNS addresses. Sigmo exposes those addresses through the connection API and uses them for interface-bound traffic such as its proxy and direct IMS underlay. The [bound dialer](dialer.go) configures DNS for its own sockets.

Earlier versions of **Default route** (`defaultRoute`) installed a metric-10 route in the main routing table for each available address family. The [takeover logic](default_route.go) reprioritized competing routes and recorded how to restore them. The [live preference update](preferences.go) changed routes on an already connected bearer. On a NetworkManager host, the resolver could keep sending queries to the previous connection's nameservers after the kernel switched to the cellular route.

## Supported host setups

Sigmo manages cellular sessions, interface addresses, and the routes it installs. `defaultRoute: true` requests a preferred IP route. Sigmo also tries to configure system DNS when the host supports it and the cellular link has usable carrier DNS. DNS conflicts and resolver errors leave the route in place. Secondary connections, bound proxy and IMS traffic, and temporary automation connections use their existing behavior.

| Host setup | Behavior |
| --- | --- |
| The host uses the `systemd-resolved` stub and Sigmo owns the cellular interface | Configure the cellular interface's DNS through the host system bus. NetworkManager or another manager can continue to operate the wired, Wi-Fi, and VPN links. |
| NetworkManager already manages DNS on the target cellular connection | Skip Sigmo's DNS registration and preserve the existing DNS settings. Route selection still follows the kernel routing rules. |
| The host uses NetworkManager's `dnsmasq`, a generated or static `resolv.conf`, or another unsupported resolver | Apply the route independently and let the host select its DNS servers. |

DNS integration needs access to `org.freedesktop.resolve1` and evidence that host clients use its stub. `/run/systemd/resolve/resolv.conf` lists upstream servers and allows traditional clients to bypass per-link DNS routing. Use the host resolver mode and stub listener to check the DNS path. Establish interface ownership using Sigmo's live session and the host manager's active connection state. NetworkManager's read-only `DnsManager.Mode` and `RcManager` can help diagnose how it manages DNS. [systemd-resolved client modes](https://www.freedesktop.org/software/systemd/man/latest/systemd-resolved.service.html) · [NetworkManager DNS configuration](https://networkmanager.dev/docs/api/latest/NetworkManager.conf.html) · [NetworkManager DnsManager API](https://networkmanager.dev/docs/api/latest/gdbus-org.freedesktop.NetworkManager.DnsManager.html)

## Applying a default route

Use one lifecycle operation in `internal/pkg/internet` for normal bearer, QMAP, and Qualcomm 410 connections. The hardware paths supply the live interface, route candidates, and carrier DNS. Reuse the existing route lock to serialize host-wide route and DNS writes. Acquire the per-modem lock first, as today, and leave PDN setup outside the host-wide lock. Keep D-Bus calls bounded by a timeout and use the existing detached, bounded cleanup pattern when a request is canceled.

1. Check the host routes per IP family. If a better or equal route owned by another manager prevents Sigmo's route from winning, report the route conflict. Reserve route reprioritization for known Sigmo routes and restoration of recorded legacy changes. A route failure restores the previous state. An online update keeps the previous bearer, and a failed new connection follows bearer cleanup. [NetworkManager route and DNS settings](https://networkmanager.dev/docs/api/latest/settings-ipv4.html)
2. Install the routes and finish the route and connection update. Then check the resolver and target link, and select usable carrier DNS addresses for the available IP families. Leave an active external `~.` scope, including one set by a full-tunnel VPN, in place. A more specific routing domain, such as `~corp.example`, still handles names under that domain. [DNS routing rules](https://www.freedesktop.org/software/systemd/man/latest/systemd-resolved.service.html)
3. Where supported, replace the previous Sigmo `~.` registration using `SetLinkDNS` and `SetLinkDomains` on `org.freedesktop.resolve1.Manager`. Use the live interface index and routing-only domain `.` (`~.` in `resolvectl`). A failure in DNS setup or its record leaves the installed routes in place. Roll back partial DNS writes and keep incomplete cleanup for retry. [resolved D-Bus API](https://www.freedesktop.org/software/systemd/man/latest/org.freedesktop.resolve1.html)

Use one Sigmo-owned `~.` scope at a time. The most recently promoted eligible connection supplies it. When two modems carry different IP families, both can retain their eligible routes while the last successful promotion supplies the DNS scope. When that modem is demoted or drops, restore the previous eligible Sigmo scope if its link and route are still present. Otherwise, removing Sigmo's scope lets the host's existing DNS policy take effect again.

## Cleanup and restart

The [SQLite key-value store](connection_state.go) keeps three records. `internet.route_promotion` records an in-progress route change and its previous routes. `internet.route` records competing routes that Sigmo reprioritized. `internet.system_dns` records Sigmo's DNS scopes and any incomplete DNS write. The DNS record is opened after route promotion completes.

- **Disable, disconnect, or invalidate.** Release the DNS registration during live demotion, bearer cleanup, QMAP mux removal, and modem generation or SIM changes. Compare the current DNS servers and domains against the values Sigmo wrote before removing them. A DNS server with a custom port or TLS server name belongs to the host's configuration. Revalidate the boot and interface identity before applying stored cleanup so a reused interface name or index does not receive old settings. Retain the cleanup record on a D-Bus error and retry it. Once routes have been restored, DNS retries leave them in place. A verified link removal releases its per-link resolver state. [resolved D-Bus API](https://www.freedesktop.org/software/systemd/man/latest/org.freedesktop.resolve1.html)
- **Recover after startup or resolver restart.** Reuse [startup recovery](../../app/server/server.go) and run recurring reconciliation separately from Always On reconnects. Include already-connected QMAP sessions and default routes that are not Always On. Reapply missing DNS while the owner remains preferred for at least one IP family. If another connection claims `~.`, release Sigmo's DNS registration and keep the cellular routes. If all preferred routes are lost, demote the tracked default connection. The reconciliation also runs when no modem is present and stops with the server.
- **Upgrade an existing default route.** Adopt it for DNS only when the live bearer and route can be attributed to Sigmo. A host with unsupported DNS can keep, demote, or reconnect its route-only default.

## API and deployment

Keep the `defaultRoute` field and the [existing HTTP routes](../../app/router/router.go). `defaultRoute` reports the route, and `dns` contains the carrier's DNS addresses. [Problem Details handling](../../app/handler/internet/handler.go) reports route failures. Sigmo logs DNS failures and retries incomplete cleanup. Internet reachability remains a separate check.

The [native Linux service](../../../init/systemd/sigmo.service) can configure DNS when the service account can reach the host system bus and write the cellular link's DNS settings. The default [Docker Compose](../../../compose.yaml) configuration uses host networking for routes. DNS integration additionally needs host bus access under a suitable policy and a resolver path shared by Sigmo and host applications. Use `resolvectl` for diagnostics.

## Implementation order and acceptance

1. Verify that default routing succeeds with unsupported DNS, missing carrier DNS, D-Bus write failure, or an external `~.` scope. Verify that a genuine route conflict still prevents promotion.
2. Exercise connect, live preferences, and cleanup for all three cellular paths. Verify promotion, demotion, switching between modems, and restoring the prior owner. Inject route and DNS failures separately, then verify that DNS cleanup never undoes a successful route change. Exercise proxy and automation connections with `defaultRoute: false`.
3. Verify recovery from existing route records, incomplete writes, a missing modem or mux, reused interface indices, and a resolver restart. Confirm that a VPN's more specific DNS domains still resolve and that an external `~.` changes DNS ownership only.
4. Validate a NetworkManager and resolved host with controlled fixtures before shipping. Capture the following from the host before promotion, after promotion, and after demotion or disconnect.

   ```sh
   readlink -f /etc/resolv.conf
   ip -4 route get 192.0.2.1
   ip -6 route get 2001:db8::1
   resolvectl status
   resolvectl query example.com
   getent ahosts example.com
   ```

   Use a controlled, uncached public hostname for the DNS assertion and a fixture's internal hostname for split DNS. Confirm the selected link and upstream in the resolver or DNS fixture logs. A successful answer alone cannot identify the upstream. The `ip route get` examples use documentation addresses to inspect the kernel's route selection without sending packets. After demotion, confirm that the original default and DNS policy are again in effect.

Implementation checks include `go test ./...` and `go vet ./...`. Run the shared contract check and affected client checks if error codes or translations change. Run real modem and host-route integration scenarios only in an authorized test environment.
