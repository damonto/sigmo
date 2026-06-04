# Sigmo (Formerly Telmo)

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Report Card](https://goreportcard.com/badge/github.com/damonto/sigmo)](https://goreportcard.com/report/github.com/damonto/sigmo)
[![Release](https://img.shields.io/github/v/release/damonto/sigmo.svg)](https://github.com/damonto/sigmo/releases/latest)

**Sigmo** is a modern, self-hosted web UI and API for managing ModemManager-based cellular modems. It ships as a single binary with an embedded Vue 3 frontend, designed to be lightweight and easy to deploy.

Sigmo focuses on advanced eSIM operations, eSIM Quick Transfer, SMS management, Wi-Fi Calling (VoWiFi), and network control.

## ✨ Features

- **📱 eSIM Management**: List, download (SM-DP+), enable, rename, and delete eSIM profiles.
- **🔁 eSIM Quick Transfer**: Transfer supported physical SIM or eSIM lines from another modem or CCID reader to the target eUICC through TS.43 carrier flows.
- **📩 SMS Center**: Full conversational view for SMS, send/delete capability, and USSD session support.
- **📶 Wi-Fi Calling (VoWiFi)**: Connect supported profiles to carrier IMS over Wi-Fi, complete carrier websheets, and route SMS, USSD, and calls through Wi-Fi Calling when available.
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
- **eSIM Quick Transfer**: Requires a build with the `esim_transfer` feature, a target modem with eUICC support, a separate source modem or CCID reader, and carrier TS.43 transfer support for the source line.
- **Wi-Fi Calling**: Requires a build with the `wifi_calling` feature, an active SIM/eSIM profile with carrier VoWiFi support, a reachable carrier ePDG/IMS service, and modem SIM access through QMI, MBIM, or AT ports.

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

---

## 🔁 eSIM Quick Transfer

eSIM Quick Transfer is exposed only when the running build reports the
`esimTransfer` capability from `/api/v1/capabilities`. Private builds enable it
with the `esim_transfer` Go build tag.

What it adds:

- A transfer entry in the eSIM install dialog.
- Source discovery from other connected modems and CCID readers.
- Transferable line discovery for supported physical SIM and eSIM sources.
- Carrier TS.43 transfer progress, including user prompts, carrier Websheets, SM-DS discovery, profile download, profile enablement, and completion.
- Source profile deletion confirmation when the carrier requires it.

Usage flow:

1. Open the target modem's eSIM page and choose **Transfer from another device**.
2. Select a source modem or CCID reader. CCID sources require the original device IMEI.
3. Load transferable lines and select the line to transfer.
4. Confirm the transfer warning. The original SIM or eSIM may become invalid after the carrier accepts the transfer.
5. Keep both source and target devices connected until Sigmo finishes downloading and enabling the transferred profile.

The source and target must be different devices. If the carrier does not expose
a TS.43 transfer entitlement for that line, Sigmo marks it as unsupported before
the transfer starts.

---

## 📶 Wi-Fi Calling (VoWiFi)

Wi-Fi Calling is exposed only when the running build reports the `wifiCalling`
capability from `/api/v1/capabilities`. Private builds enable it with the
`wifi_calling` Go build tag.

What it adds:

- Per-profile Wi-Fi Calling settings in the modem settings page.
- Carrier setup through an embedded Websheet when the carrier requires entitlement confirmation.
- Wi-Fi Calling status on modem cards and SIM/profile selectors.
- SMS, USSD, and voice calls over Wi-Fi Calling when the profile is connected.
- A preference toggle to use Wi-Fi Calling for messages, USSD, and calls when it is available.

Usage flow:

1. Open the modem's settings page.
2. Enable **Wi-Fi Calling** for the active SIM/eSIM profile.
3. If Sigmo shows that carrier setup is required, open the carrier Websheet and complete the flow.
4. Wait for the status to become connected.
5. Send SMS, run USSD, or place calls as usual. When the preference toggle is enabled and Wi-Fi Calling is connected, Sigmo routes those actions through Wi-Fi Calling.

Wi-Fi Calling settings are stored per profile in Sigmo's application database.

---

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

4.  **Private feature build**:

    The public module does not build eSIM Transfer or Wi-Fi Calling by default.
    Private builds use `go.private.mod` and download the private TS.43 and VoWiFi
    modules through normal Go module auth:

    ```bash
    export GOPRIVATE=github.com/damonto/*
    source scripts/private-features.env
    go run -tags="${PRIVATE_GO_TAGS}" -modfile="${PRIVATE_GO_MODFILE}" . --db-path=./sigmo.db --debug
    ```

    To use SSH for private modules locally:

    ```bash
    export GOPRIVATE=github.com/damonto/*
    git config --global url."git@github.com:damonto/".insteadOf "https://github.com/damonto/"
    source scripts/private-features.env
    go build -tags="${PRIVATE_GO_TAGS}" -modfile="${PRIVATE_GO_MODFILE}" -o sigmo .
    sudo ./sigmo --db-path=/var/lib/sigmo/sigmo.db
    ```

    Prefer building as your normal user and running the binary with `sudo`.
    Running `sudo go run` makes Go and Git use root's module cache and Git/SSH
    configuration, which is why it may prompt for a GitHub username.

    This repository also includes a local helper that uses
    `/home/user/.ssh/id_ed25519` over SSH, builds with your normal user's Go
    cache, and starts the temporary `go run` binary with `sudo`:

    ```bash
    ./scripts/dev.sh
    ```

    Use the private tags and modfile from `scripts/private-features.env` for private
    builds and tests. It currently enables `esim_transfer,wifi_calling`. Future
    private features should use their own build tag and register a capability so
    the frontend can discover them from `/api/v1/capabilities`.

    GitHub Actions enables private features only when the repository variable
    `SIGMO_PRIVATE_FEATURES` is set to `true`. Private Go module access uses the
    repository secret `SIGMO_PRIVATE_MODULE_TOKEN`.

    Pull requests from branches in this repository that update `go.mod` or
    `go.sum` automatically sync `go.private.mod` and `go.private.sum` when
    private features are enabled. Fork pull requests are skipped so private
    module credentials are not exposed.

    To sync the private manifest locally after changing public dependencies:

    ```bash
    ./scripts/sync-private-go-mod.sh
    ```

6.  **Build Docker Image**:
    ```bash
    docker build -t sigmo:local .
    ```

---

## 📄 License

Released under the [MIT License](LICENSE).
