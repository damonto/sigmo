# Xcode MCP development workflow

Edit iOS source on Linux. The Mac opens one outgoing SSH connection to the development host. Both source synchronization and Streamable HTTP MCP use that connection; the development host never needs to connect to the Mac's network address. Xcode remains responsible for builds, tests, app launches, and debugger operations.

```text
Development host                          Mac
  sshd                 <-- outgoing SSH -- start-mac.sh --> ssh
  127.0.0.1:8765/mcp    === reverse -R ===> 127.0.0.1:8765 --> mcpbridge --> Xcode
  source :8766         <=== local -L ===== 127.0.0.1:8766 <-- source synchronization
```

## Requirements

- Linux: Git and Python 3.10 or later. MCP client calls and gateway tests also need FastMCP 4.1.0; `uv` can provide an isolated environment.
- An SSH endpoint on the development host reachable from the Mac, with local and remote TCP forwarding permitted. Use a stable Tailscale name/address or DNS name. No incoming Mac connection is needed.
- Mac: full Xcode with external-agent tools enabled, an iOS simulator runtime, XcodeGen 2.44.1 or later, Python 3.10 or later available as `python`, and `uv` on the terminal's PATH. Existing mise installations are supported.
- A running macOS user session. Xcode may require approval for the agent and project folder from the Xcode MCP menu-bar control.

## Start the services

In the Linux repository root:

```sh
python scripts/ios/mcp_source.py --host 10.10.10.101
```

Use the development host's LAN or Tailscale address for `--host`; keep this source service running. On the Mac, enable external-agent access under Xcode's Intelligence settings, then run:

```sh
curl -fsS http://10.10.10.101:8766/start-mac.sh | bash
```

The server fills in the SSH target using the account running the source service and the hostname from the download URL. It also includes the source service's actual bind address and port, so the SSH forward can reach it even when it only listens on the LAN address. Authentication follows the Mac's SSH configuration; SSH can ask for first-connection confirmation or credentials in the terminal. The entire launcher is parsed before startup, and its child processes receive empty standard input so they cannot consume the script from curl's pipe.

If the SSH account or hostname differs, set it once on the development host:

```sh
python scripts/ios/mcp_source.py --host 10.10.10.101 --ssh-target developer@stable-dev
```

An SSH alias supplied by `--ssh-target` must exist on the Mac. You can also override the target or port for one invocation using `bash -s --`:

```sh
curl -fsS http://10.10.10.101:8766/start-mac.sh | bash -s -- developer@stable-dev --ssh-port 2222
```

The launcher checks for occupied local ports, starts SSH, and waits up to 45 seconds for the source manifest. It then downloads the gateway, prepares its Python environment, and starts the gateway. For the address above, SSH carries both `-R 127.0.0.1:8765:127.0.0.1:8765` and `-L 127.0.0.1:8766:10.10.10.101:8766`. The gateway synchronizes source, generates the project, and opens Xcode.

The launcher uses `uv run --script` with the existing `python`. The gateway declares its Python requirements and pinned dependencies in inline script metadata; uv creates and caches an isolated environment from that declaration. The Bash script does not maintain another dependency list or virtual environment. It does not install Python or uv. Set `SIGMO_MCP_DIR` before running the script to use a different Mac service/workspace directory. See [uv's script dependency guide](https://docs.astral.sh/uv/guides/scripts/).

Use `--ssh-port` for a non-default SSH port. `--source-port` and `--mcp-port` configure both forwards and the gateway together. Also set the Linux source service's `--port` and the development host's MCP client URL to match any changed ports. The downloaded script already includes the source service's port.

Alternatively, save the configured launcher before running it:

```sh
curl -fsS http://10.10.10.101:8766/start-mac.sh -o /tmp/sigmo-xcode-start.sh
bash /tmp/sigmo-xcode-start.sh
```

Keep the Linux source terminal and the single Mac terminal running. Ctrl-C in the Mac terminal stops both SSH and the gateway; the gateway explicitly closes its bridge transport, including during initialization. If SSH exits during download, dependency installation, or normal operation, the launcher stops the current work and exits. If the gateway exits, the launcher stops SSH. Cleanup targets only processes started by this invocation; it allows fifteen seconds for termination before forcing remaining process groups to exit, giving the MCP transport time to close its separately spawned bridge. Nothing is registered with LaunchAgents or another service manager.

Restart the Linux process after changing `mcp_source.py`. Rerun the curl command after changing the launcher or gateway; it downloads their current versions. Swift files and project configuration synchronize without restarting the gateway.

If the source service should only listen on loopback, omit `--host`, copy the repository's `start-mac.sh` to the Mac using SCP, and run `bash start-mac.sh user@development-host`. A raw repository copy needs an explicit SSH target; a downloaded bootstrap already has one.

When migrating from the LAN or separate-terminal workflow, stop the old source, tunnel, and gateway processes and use these commands. `--mac-host` and the separate Python tunnel launcher have been removed. The Mac gateway and the development host's MCP forward remain bound to loopback.

| Resource                                                      | Address or path                                              |
| ------------------------------------------------------------- | ------------------------------------------------------------ |
| Source manifest (development host)                            | `http://10.10.10.101:8766/manifest.json`                     |
| Source manifest (Mac, through SSH)                            | `http://127.0.0.1:8766/manifest.json`                        |
| Xcode MCP (development host's local forward)                  | `http://127.0.0.1:8765/mcp`                                  |
| Gateway status (either machine, through the forward on Linux) | `http://127.0.0.1:8765/health`                               |
| Linux snapshots                                               | `build/ios/snapshots/<revision>.tar.gz`                      |
| Mac workspace                                                 | `~/Library/Developer/SigmoMCP/workspace/`                    |
| Mac project                                                   | `~/Library/Developer/SigmoMCP/workspace/ios/Sigmo.xcodeproj` |

## Codex project configuration

The repository includes this configuration in `.codex/config.toml`:

```toml
[mcp_servers.xcode]
url = "http://127.0.0.1:8765/mcp"
startup_timeout_sec = 30
tool_timeout_sec = 1200
required = false
```

This connects to the development host's local SSH forward. The MCP client must run on that host (in the same network namespace as its SSH listener). A separate Skill is not needed to establish the connection. The server is optional so an offline Mac does not prevent other repository work.

Codex loads project MCP configuration for trusted projects. Reopen or reload the Codex project/session after adding the configuration if the `xcode` tools are not yet listed. Verify the resolved configuration from this repository with:

```sh
codex mcp get xcode --json
```

Project-scoped MCP settings and HTTP connection fields are documented in the [official Codex MCP guide](https://developers.openai.com/codex/mcp).

## Use the Xcode tools

1. Call `GatewayStatus` to inspect synchronization errors and the source revision. `SyncProject` requests an immediate synchronization.
2. Call `XcodeOpenWorkspace` with the Mac project's absolute path. Approve the agent/project in the Mac UI if Xcode requests it. The global settings switch alone does not grant project access.
3. Use the returned `workspaceIdentifier` for subsequent calls. `XcodeListWorkspaces` can also list current identifiers. Do not assume an absolute path works in tools expecting an identifier.
4. Call `XcodeListRunDestinations`, then `XcodeSwitchRunDestination` with the intended `displayTitle`. The current development simulator is `iPhone Sigmo Development`. Check the destination after regenerating or reopening the project.
5. Use `BuildProject`, `RunAllTests`, or `RunProject`. Inspect their structured results as well as MCP errors: a build or test failure may be returned in a successful tool response.
6. Use `GetBuildLog` for build diagnostics. Use `GetConsoleOutput` with the `launchSessionReference` from `RunProject` for OSLog/stdout. Newly launched apps may need a moment to produce output.

When test code changes, call `BuildProject` with `buildForTesting: true`. After synchronization or diagnostic operations, confirm the intended run destination again immediately before submitting tests; a regenerated or reopened project can restore an earlier destination. Check the new run's `fullConsoleLogsPath` for the requested cases starting and completing. If the test runner cannot launch, Xcode may return stale per-test statuses; those do not establish that the requested cases ran. The settings audits emit `SETTINGS_AUDIT_BEGIN` and `SETTINGS_AUDIT_PASS` for each language and page so their actual coverage can be verified.

If a new client connection reports that the agent is not approved, call `XcodeOpenWorkspace` again to restore the existing project association before requesting other tools. If Xcode still requests approval, use its menu-bar control.

Always edit managed source in the Linux repository. Xcode source-editing tools modify the Mac copy and will cause the next source synchronization to report a conflict. Read-only inspection and debugger operations can use Xcode directly.

## Standalone client

The Python client is useful for checking a connection independently of Codex:

```sh
uv run scripts/ios/mcp_client.py list
uv run scripts/ios/mcp_client.py call GatewayStatus
uv run scripts/ios/mcp_client.py call SyncProject
uv run scripts/ios/mcp_client.py call XcodeOpenWorkspace \
  --arguments '{"path":"/Users/damon/Library/Developer/SigmoMCP/workspace/ios/Sigmo.xcodeproj"}'

# Replace the tool and arguments with values from the live tool list.
uv run scripts/ios/mcp_client.py --timeout 1200 --output build/ios/mcp-result.json \
  call TOOL_NAME --arguments '{"PARAMETER":"VALUE"}'
```

The default endpoint is `http://127.0.0.1:8765/mcp` on the development host. Override it with `--url`. The client saves textual and structured MCP results; it does not download files referenced by Mac filesystem paths.

## Manual reconnection

- After a network change, rerun the same curl command. SSH checks the connection every 15 idle seconds and exits after approximately three unanswered checks. The launcher then stops the gateway and exits; use Ctrl-C first if it has not exited yet. There is no automatic reconnect or background service.
- Keep the Linux source service running. The Mac command recreates both the SSH tunnel and gateway, reusing the Python environment and synchronized workspace.
- Check `http://127.0.0.1:8765/health` from the development host, then run `mcp_client.py list` and `mcp_client.py call GatewayStatus`. A listening SSH port alone does not establish gateway or Xcode readiness. An MCP session may need to be reinitialized, or the Codex session reloaded, after transport loss.
- Calls are never automatically replayed by these scripts. A client that loses its response treats the outcome as unknown; inspect `GatewayStatus`, Xcode, and its logs before submitting the call again. The gateway can still record `completed` if it receives Xcode's response after the client disconnects; interruption of the gateway's own call records `unknown`. It keeps the synchronization lock until the already-submitted bridge call returns or reaches its existing timeout. The latest call state is saved separately from source snapshots, and a gateway restart changes an unfinished `running` call to `unknown`. A completed call records receipt of an MCP result, not necessarily a successful build/test; inspect the tool result too.
- A gateway restart cannot recover a running Xcode operation or its lost result. If a gateway was stopped during a call, check Xcode before restarting source synchronization or issuing another build.
- Port conflicts make SSH exit instead of leaving a partial connection running. For OpenSSH on the development host, use `AllowTcpForwarding yes` and `GatewayPorts no`. Configure `ClientAliveInterval 15` and `ClientAliveCountMax 3` there to release an old reverse listener after a broken connection; otherwise the old SSH session may keep port `8765` occupied. These scripts do not modify the host's SSH configuration. See the [OpenSSH client](https://man.openbsd.org/ssh.1), [client configuration](https://man.openbsd.org/ssh_config.5), and [server configuration](https://man.openbsd.org/sshd_config.5) references.

## Synchronization behavior

- Only saved, non-ignored regular files under `ios/` and `scripts/ios/` are exported, including uncommitted files. No commit is required.
- Snapshots are immutable archives named by their SHA-256. The manifest includes a hash for every file. Unchanged source reuses the same archive, including after a source-service restart.
- The service checks for source changes while preparing an archive and publishes only a complete snapshot. Save the files and retry if concurrent edits are detected.
- The Mac verifies the archive and file hashes before updating the workspace. Managed files changed or deleted on the Mac cause a conflict; preserve and resolve those changes before resuming synchronization. Source paths containing symlinks, including parent directories, are rejected before they can redirect writes outside the workspace.
- Synchronization and MCP tool calls share a lock. Source changes wait until the active tool call finishes. Manual builds started in Xcode do not acquire this lock; use MCP during automated verification.
- File-list or project-configuration changes regenerate the Xcode project. Ordinary Swift content edits preserve the existing project. A failed project-generation step is retried on the next synchronization.
- `GatewayStatus` records the revision associated with the latest Xcode call. `/health` shows gateway state; a successful real Xcode tool call is required to verify Xcode access.
- The Mac gateway and development host's MCP forward listen on loopback; SSH authenticates and encrypts MCP and source-sync traffic. Source requests bypass system HTTP proxies so they reach the SSH listener directly. The source service also exposes the HTTP bootstrap on its configured bind address. Use a trusted LAN or Tailscale interface for that address: HTTP authentication is not implemented. The gateway shares one persistent Apple bridge and transparently forwards Xcode tools; tool filtering and artifact download endpoints are not implemented.
- Old snapshot files are retained so in-flight downloads remain valid. Remove unused archives from `build/ios/snapshots/` while the source service is stopped if cleanup is needed.

## Validation and current limits

```sh
uv run --with fastmcp==4.1.0 \
  python -m unittest discover -s scripts/ios -p 'test_*.py' -v
```

Tests cover saved and untracked source, ignored/private files, immutable archives, concurrent edits and polls, HTTP delivery, bootstrap connection defaults and quoting, path validation, checksum failures, Mac edit conflicts, deletion synchronization, project-generation retries, and recovery after source disconnection. They also check interrupted call persistence and locking. The Bash launcher is checked with `bash -n` and subprocess fixtures that verify argument-free startup from a pipe, standard-input isolation, source readiness before gateway startup, cached dependencies, Ctrl-C cleanup, installation failure, and SSH loss during download or installation. Real FastMCP transports with a substitute Xcode process exercise shutdown during bridge initialization and an unfinished tool call.

When `sshd` and `ssh-keygen` are installed, the same suite captures the Bash launcher's SSH arguments and uses them with a temporary SSH server on loopback, disposable keys, and high ports. It checks simultaneous HTTP/stream forwarding, reconnection of the SSH transport, and remote port conflicts. No real SSH server configuration, Mac, Xcode, or modem is changed by these tests. The native workflow still requires a live check on a Mac with Xcode.

On 2026-10-07, the live MCP workflow built the app, passed one UI smoke test, launched it on the development simulator, and returned OSLog/stdout. Bundle metadata was inspected to confirm `com.damon.sigmo`, `AppIcon`, and `public.app-category.utilities`.

Physical-device installation, real APNs/PushKit delivery, and preview/screenshot artifact transfer remain separate verification tasks. `.xcresult`, full log files, and screenshots returned as Mac paths stay on the Mac. Use gateway revisions to correlate source and operations; the app's diagnostics screen shows its bundle identifier and version.

Apple's local bridge setup is described in [Giving external agents access to Xcode](https://developer.apple.com/documentation/xcode/giving-external-agents-access-to-xcode).
