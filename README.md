# Sigmo (Formerly Telmo)

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/damonto/sigmo)](https://goreportcard.com/report/github.com/damonto/sigmo)
[![Release](https://img.shields.io/github/v/release/damonto/sigmo.svg)](https://github.com/damonto/sigmo/releases/latest)

**Sigmo** is a modern, self-hosted web UI and API for managing ModemManager-based cellular modems. It ships as a single binary with an embedded Vue 3 frontend, designed to be lightweight and easy to deploy.

Sigmo focuses on advanced eSIM operations, SMS management, and network control. Pro-only features are documented in [pro/README.md](pro/README.md).

## ✨ Features

- **📱 eSIM Management**: List, download (SM-DP+), enable, rename, and delete eSIM profiles.
- **📩 SMS Center**: Full conversational view for SMS, send/delete capability, and USSD session support.
- **⚙️ Modem Control**: SIM slot switching, network scanning, manual registration, and preference configuration (Alias, MSS).
- **🔒 Secure Access**: OTP-based login system via Telegram, HTTP, Email, and more.
- **🔔 Notifications**: Forward incoming SMS and login tokens to Telegram, Bark, Gotify, Email, etc.
- **🚀 Portable**: Single Go binary with no external runtime dependencies (except ModemManager).

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
- **Service**: `ModemManager` running on the system D-Bus when using the binary directly. The Docker image includes `ModemManager` and starts it inside the container.
- **Permissions**: Root access or proper `udev` rules to access modem device nodes.
---

## 📥 Installation

Sigmo is distributed as a static binary. You do not need to install Node.js or Go to run it.

### 1. Download Binary

Grab the latest release for your architecture from the [GitHub Releases](https://github.com/damonto/sigmo/releases/latest).

```bash
# Example for Linux AMD64
curl -LO https://github.com/damonto/sigmo/releases/latest/download/sigmo-linux-amd64
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

The Docker image includes the embedded Vue frontend and installs `dbus`, `ModemManager`, `qmi-utils` for QMI proxy support, and `libmbim-tools` in the runtime image.

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

The compose setup uses `network_mode: host` because Sigmo's internet connection feature configures the modem network interface and host routes. Docker port publishing is disabled in this mode; use `--listen-address` to choose the listening address and port.

The container runs with `privileged: true` so Sigmo and ModemManager can access modem devices. `/run` is mounted as tmpfs so stale D-Bus sockets cannot survive container restarts. On hosts with strict Docker or udev policies, keep `/dev`, `/run/udev`, and `/sys` mounted as shown in `compose.yaml`.

Sigmo stores Internet Always On settings, modem network Mode/Bands preferences,
and manual network registration preferences in SQLite so they can be restored
after modem reloads, program restarts, and system reboots.

## ⚙️ Configuration Reference

Startup configuration is provided through flags:

| Flag                 | Default                          | Description                                      |
| :------------------- | :------------------------------- | :----------------------------------------------- |
| `--listen-address`   | `0.0.0.0:9527`                   | HTTP bind address.                               |
| `--db-path`          | `$XDG_DATA_HOME/sigmo/sigmo.db`  | SQLite database path.                            |
| `--debug`            | `false`                          | Enable debug logging and internal API errors.    |
| `--version`          | `false`                          | Print the build version and exit.                |

Runtime settings are managed in the Web UI and stored in SQLite:

- Login OTP policy and auth providers.
- Notification channels: Telegram, Bark, Gotify, ServerChan, HTTP webhook, and Email.
- Internet proxy listener and password.
- Modem alias, compatibility mode, and APDU MSS.
- Internet APN preferences and Always On state.
- Network mode, bands, and manual registration preferences.

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

> **Note**: The default service runs as `root` to ensure access to ModemManager. If running as a non-root user, verify `udev` rules for the modem and write permissions for the SQLite database path.

---

## 🏗️ Development

If you wish to contribute or modify the source:

1.  **Prerequisites**: Go 1.25+, Bun (for Vue).
2.  **Build Frontend**:
    ```bash
    cd web && bun install && bun run build
    ```
3.  **Run Backend**:

    ```bash
    go run ./ --listen-address=0.0.0.0:9527 --db-path=./sigmo.db --debug
    ```

    _Or for frontend hot-reload:_ `cd web && bun run dev`

4.  **Build Docker Image**:
    ```bash
    docker build -t sigmo:local .
    ```

---

## 📄 License

This repository, including the `pro/` module, is released under the
[MIT License](LICENSE).
