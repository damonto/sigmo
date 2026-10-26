# Sigmo (Formerly Telmo)

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Release](https://img.shields.io/github/v/release/damonto/sigmo.svg)](https://github.com/damonto/sigmo/releases/latest)

**Sigmo** is a modern, self-hosted web UI and API for managing cellular modems through QMI and MBIM. It ships as a single binary with an embedded Vue 3 frontend, designed to be lightweight and easy to deploy.

Sigmo focuses on advanced eSIM operations, SMS management, and network control.

## ✨ Features

- **📱 eSIM Management**: List, download (SM-DP+), enable, rename, and delete eSIM profiles.
- **📩 SMS Center**: Full conversational view for SMS, send/delete capability, and USSD session support.
- **⚙️ Modem Control**: SIM slot switching, network scanning, manual registration, and preference configuration (Alias, MSS).
- **🔒 Secure Access**: Independent OTP delivery, standard OIDC, and Cloudflare Access for Web and iOS. See [OIDC](#oidc) and [Cloudflare Access](#cloudflare-access).
- **🔔 Notifications**: Forward incoming SMS and login tokens to Telegram, Bark, Gotify, Email, etc.
- **🚀 Portable**: Single Go binary with no mandatory modem-control daemon.

---

## 🛍️ Recommended Hardware & Offers

> Support the project and get reliable hardware for your setup.

- **Need an eUICC?**
  We recommend **[eSTK.me](https://store.estk.me?code=esimcyou)**. It is highly reliable for iOS profile downloads.

  > 🎁 Use code `esimcyou` for **10% off**.

- **Need more storage?**
  If you require >1MB storage to install multiple eSIM profiles, we recommend **[9eSIM](https://www.9esim.com/?coupon=DAMON)**.
  > 🎁 Use code `DAMON` for **10% off**.

---

## 🛠 Architecture & Requirements

**Architecture:**

- **Backend**: Go serving `/api/v1` and static assets.
- **Frontend**: Vue 3 + Vite (Embedded in the binary).

**System Requirements:**

- **OS**: Linux.
- **Device access**: Access to the modem QMI, MBIM, AT, and network device nodes. Direct control requires read and write permission on the QMI or MBIM node.
- **Permissions**: Root access or proper `udev` rules to access modem device nodes.

Sigmo automatically tries `qmi-proxy` or `mbim-proxy` before opening a control node directly. This allows Sigmo to coexist with `ModemManager` when the matching proxy socket is available. If no proxy is reachable, Sigmo falls back to direct access; do not let Sigmo and another process open the same QMI or MBIM control node directly.

---

## 📥 Installation

Sigmo is distributed as a static binary. You do not need to install Node.js or Go to run it.

### 1. Download Binary

Grab the latest release for your architecture from the [GitHub Releases](https://github.com/damonto/sigmo/releases/latest).

```bash
# Example for Linux AMD64
curl -LO https://github.com/damonto/sigmo/releases/latest/download/sigmo-linux-amd64.gz
gzip -d sigmo-linux-amd64.gz
chmod +x sigmo-linux-amd64
sudo install -m 0755 sigmo-linux-amd64 /usr/local/bin/sigmo
```

### 2. Configure

Sigmo uses command-line flags for startup settings and stores runtime settings
in SQLite. On first start, login OTP is disabled so you can open the Web UI and
configure notification channels and authentication.

### 3. Run

Start the service.

```bash
/usr/local/bin/sigmo --listen-address=0.0.0.0:9527 --db-path=/var/lib/sigmo/sigmo.db
```

Visit `http://localhost:9527` to access the UI.

### Docker Compose

The Docker image includes the embedded Vue frontend and can reuse `qmi-proxy` or `mbim-proxy` running on the host. The proxy utilities do not need to be installed in the container.

1.  **Data**:

    The compose setup mounts `./data` to the container data directory.
    Sigmo stores application settings, messages, calls, internet preferences,
    and network preferences in SQLite.

2.  **Start**:

    ```bash
    docker compose pull
    docker compose up -d
    ```

3.  **Open UI**:
    Visit `http://localhost:9527`, or pass `--listen-address` to choose another address.

The compose setup uses `network_mode: host` because Sigmo's internet connection feature configures the modem network interface and host routes. Host networking also makes the host's abstract `qmi-proxy` and `mbim-proxy` Unix sockets visible to the container. Docker port publishing is disabled in this mode; use `--listen-address` to choose the listening address and port.

The container runs with `privileged: true` so Sigmo can access modem devices and configure network interfaces. On hosts with strict Docker or udev policies, keep `/dev`, `/run/udev`, and `/sys` mounted as shown in `compose.yaml`.

The container shares the host's modem device nodes. A host `ModemManager` can coexist with Sigmo when it uses the matching proxy; if the proxy socket is unavailable, Sigmo's direct fallback can conflict with it.

Sigmo stores Internet Always On settings, modem network Mode/Bands preferences,
and manual network registration preferences in SQLite so they can be restored
after modem reloads, program restarts, and system reboots.

## ⚙️ Configuration Reference

Startup configuration is provided through flags:

| Flag               | Default                         | Description                                   |
| :----------------- | :------------------------------ | :-------------------------------------------- |
| `--listen-address` | `0.0.0.0:9527`                  | HTTP bind address.                            |
| `--db-path`        | `$XDG_DATA_HOME/sigmo/sigmo.db` | SQLite database path.                         |
| `--debug`          | `false`                         | Enable debug logging and internal API errors. |
| `--version`        | `false`                         | Print the build version and exit.             |

Runtime settings are managed in the Web UI and stored in SQLite:

- Authentication mode, OTP delivery channels, OIDC, and Cloudflare Access.
- Notification channels: Telegram, Bark, Gotify, ServerChan, HTTP webhook, and Email.
- Internet proxy listener and password.
- Modem alias, compatibility mode, and APDU MSS.
- Internet APN preferences and Always On state.
- Network mode, bands, and manual registration preferences.

### OIDC

1. Create a confidential OIDC client with the exact callback
   `https://<Sigmo host>/api/v1/auth/oidc/callback`.
2. In **Settings > Authentication**, select `OIDC` and enter the HTTPS issuer,
   client ID, client secret, and allowed `sub` values.
3. Use **Test**, then save.

### Cloudflare Access

1. Put Sigmo behind a Cloudflare Access Self-hosted application and block
   direct access to the origin. Enable Managed OAuth and Dynamic Client
   Registration with `https://sigmo-pro.952007.xyz/auth/callback` as the
   callback.
2. In **Settings > Authentication**, select `Cloudflare Access` and enter the
   team URL, application AUD, and allowed `sub` values.
3. Use **Test**, then save.

Use HTTPS and keep the proxy's external `Host` and `X-Forwarded-Proto`.

---

## 💻 Service Deployment

To run Sigmo as a background service, use Systemd.

### Systemd Example

1.  **Install Unit File**:
    ```bash
    sudo install -m 0644 init/systemd/sigmo.service /etc/systemd/system/sigmo.service
    ```
2.  **Enable & Start**:
    ```bash
    sudo systemctl daemon-reload
    sudo systemctl enable --now sigmo
    ```

> **Note**: The default service runs as `root` to access modem device nodes and configure network interfaces. If running as a non-root user, verify `udev` rules, network capabilities, and write permissions for the SQLite database path.

---

## 🏗️ Development

Client applications live in `apps/`: [Web](apps/web/README.md) and
[iOS](apps/ios/README.md).
The Go backend, including IMS and eSIM transfer, uses the root Go module. Official carrier configuration is delivered through the existing Pro API.

If you wish to contribute or modify the source:

1.  **Prerequisites**: Go 1.27.1, Bun (for Vue).
2.  **Build Frontend**:
    ```bash
    (cd apps/web && bun install && bun run build)
    ```
3.  **Run Backend**:

    ```bash
    go run ./ --listen-address=0.0.0.0:9527 --db-path=./sigmo.db --debug
    ```

    _Or for frontend hot-reload:_ `cd apps/web && bun run dev`. Vite proxies `/api` to `http://127.0.0.1:9527`, keeping browser requests and authentication cookies on the frontend origin. For a different backend, set `SIGMO_DEV_BACKEND=http://<host>:<port>` in `apps/web/.env.local` or the shell. Keep `VITE_API_BASE_URL=/api/v1`; an absolute backend URL bypasses the proxy.

4.  **Build Docker Image**:
    ```bash
    docker build -t sigmo:local .
    ```

### Carrier configuration publishing

From the repository root, use a private directory containing `entitlement.json`,
`wfc.json`, and `ims.json`, and reuse the existing encryption key at
`scripts/sigmo-keys/config-encryption.raw.b64`. Keep the carrier files and key
outside version control.

```sh
cd pro-api
bun install --frozen-lockfile
bunx wrangler secret put SIGMO_CONFIG_ENCRYPTION_KEY < ../scripts/sigmo-keys/config-encryption.raw.b64
cd ..
scripts/publish-configs.sh /path/to/private/carrier-files --publish
cd pro-api
bun run deploy:production
```

---

## 📄 License

Sigmo's original source code is released under the [MIT License](LICENSE).
The unified binary links `ts43-go` and `ims-go`, which are licensed under GPLv3.
See [source and distribution licenses](THIRD_PARTY_NOTICES.md) for the combined
binary's distribution requirements and third-party notices.
