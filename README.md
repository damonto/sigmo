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

Sigmo can start without a config file. If `--config` is not provided, it creates
`$HOME/.config/sigmo/config.toml` with safe defaults. You can also pass an
explicit config path if you prefer managing the file yourself.

### 3. Run

Start the service.

```bash
/usr/local/bin/sigmo
```

Visit `http://localhost:9527` to access the UI.

### Docker Compose

The Docker image includes the embedded Vue frontend and installs `dbus`, `ModemManager`, `qmi-utils`, and `libmbim-tools` in the runtime image.

1.  **Config**:

    The compose setup mounts `./config` to the container config directory.
    Sigmo creates `./config/config.toml` on first start if it does not exist.

2.  **Start**:

    ```bash
    docker compose pull
    docker compose up -d
    ```

3.  **Open UI**:
    Visit `http://localhost:9527`, or the port configured by `[app].listen_address`.

The compose setup uses `network_mode: host` because Sigmo's internet connection feature configures the modem network interface and host routes. Docker port publishing is disabled in this mode; use `[app].listen_address` in `config.toml` to choose the listening address and port.

The container runs with `privileged: true` so Sigmo and ModemManager can access modem devices. `/run` is mounted as tmpfs so stale D-Bus sockets cannot survive container restarts. On hosts with strict Docker or udev policies, keep `/dev`, `/run/udev`, and `/sys` mounted as shown in `compose.yaml`.

If you enable **Always On** for an internet connection, Sigmo stores the last connection settings in the XDG state directory (`$XDG_STATE_HOME/sigmo/internet-always-on.json`, or `$HOME/.local/state/sigmo/internet-always-on.json`). Sigmo also stores modem network Mode/Bands preferences in the same state directory (`network-preferences.json`) so they can be restored after modem reloads, program restarts, and system reboots. In the Docker image, the default state directory is `/root/.local/state/sigmo`; mount that directory as a volume if you want Docker container recreation to preserve boot-time internet auto-connect settings and network preferences.

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
They are not written to the `[modems]` section of `config.toml`.

---

## ⚙️ Configuration Reference

Sigmo runs using a TOML configuration file. When `--config` is omitted, the default is `$HOME/.config/sigmo/config.toml`.

> **⚠️ Important**: This file is **Read-Write**.
> When you update modem aliases or settings via the Web UI, Sigmo **writes the changes back** to this file. Ensure the Sigmo process has **write permissions** to the config file.

Internet **Always On** state is not stored in this config file. Sigmo keeps the last Always On connection settings in the XDG state directory so it can reconnect after an unexpected disconnect or system restart.

### 1. `[app]` General Settings

Controls the core application behavior, network binding, and security policies.

```toml
[app]
  environment = "production"
  listen_address = "0.0.0.0:9527"
  auth_providers = ["telegram", "email"]
  otp_required = true
```

| Parameter            | Type    | Description                                                                                                                                                      |
| :------------------- | :------ | :--------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **`environment`**    | String  | The running environment. Set to `"production"` to minimize logs (recommended). Set to `"development"` to enable verbose debug logging.                           |
| **`listen_address`** | String  | The IP and Port to bind the HTTP server. <br>`0.0.0.0:9527` listens on all interfaces.<br>`127.0.0.1:9527` restricts access to localhost.                        |
| **`auth_providers`** | Array   | **Allowed login channels**. The values listed here must match the configuration block names in `[channels]` (e.g., `telegram`, `bark`, `email`).                 |
| **`otp_required`**   | Boolean | Enforce OTP (One-Time Password) for login. <br>`true`: Secure mode (Recommended).<br>`false`: No login required (Insecure, for isolated internal networks only). |

### 2. `[channels]` Notification & Auth

Configures channels used for receiving **Login OTPs** and **Forwarded SMS**.

> **Note**: Each channel supports `enabled`. Existing configs without this option are treated as enabled. Set `enabled = false` to keep channel settings while disabling OTP and SMS delivery for that channel.

#### Telegram

```toml
[channels.telegram]
  enabled = true
  bot_token = "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11"
  recipients = ["123456789", "987654321"]
```

- `enabled`: Enable this channel. Set to `false` to keep the settings but stop using it.
- `bot_token`: The token received from @BotFather.
- `recipients`: Array of Strings Chat IDs authorized to receive messages.

#### Bark (iOS Push)

```toml
[channels.bark]
  endpoint = "https://api.day.app"
  recipients = ["device_key_1", "device_key_2"]
```

- `endpoint`: Bark server URL. Leave empty to use the official `https://api.day.app`. Sigmo automatically appends `/push`.
- `recipients`: List of Device Keys from the Bark App.

#### Gotify (Self-Hosted)

```toml
[channels.gotify]
  endpoint = "https://push.example.com"
  recipients = ["AsDh82..."]
  priority = 5
```

- `endpoint`: Base URL of your Gotify server (do not add `/message`; it is appended automatically).
- `recipients`: List of Gotify **Application Tokens**.
- `priority`: Message priority (Integer), default is 5.

#### ServerChan (SendKey)

```toml
[channels.sc3]
  endpoint = "https://<uid>.push.ft07.com/send/<sendkey>.send"
```

- `endpoint`: The full URL including the SendKey.

#### HTTP (Webhook)

```toml
[channels.http]
  endpoint = "https://httpbin.org/post"
  [channels.http.headers]
    Authorization = "Bearer secret_token"
    Content-Type = "application/json"
```

- `endpoint`: The target Webhook URL.
- `headers`: Key-Value pairs for custom HTTP headers. Sigmo sends a JSON envelope like `{"kind":"otp","payload":{...}}` or `{"kind":"sms","payload":{...}}`.

#### Email (SMTP)

```toml
[channels.email]
  smtp_host = "smtp.gmail.com"
  smtp_port = 587
  smtp_username = "yourname@gmail.com"
  smtp_password = "app_password"
  from = "Sigmo <yourname@gmail.com>"
  recipients = ["admin@example.com"]
  tls_policy = "mandatory"
  ssl = false
```

- `smtp_host` / `smtp_port`: Server address and port.
- `smtp_username` / `smtp_password`: Credentials (App Passwords are recommended).
- `recipients`: List of email addresses to receive notifications.
- `tls_policy`: STARTTLS enforcement.
  - `mandatory`: Enforce TLS (Default, Recommended).
  - `opportunistic`: Try TLS, fall back to plain text if unavailable.
  - `none`: No TLS.
- `ssl`: Use implicit SSL (usually for port 465). Set `true` for port 465. Set `false` for port 587 (when using `tls_policy`).

### 3. `[modems]` Hardware Settings

This section is **auto-generated** by Sigmo when you save settings in the Web UI. You generally do not need to write this manually.

Entries are keyed by the ModemManager **Equipment Identifier**.

```toml
[modems]
  [modems."123456789012345"]
    alias = "Office 5G Stick"
    compatible = false
    mss = 240
```

| Parameter        | Type    | Default | Description                                                                                                                                                                                                                                          |
| :--------------- | :------ | :------ | :--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **`alias`**      | String  | (None)  | **Custom Name**. Displayed in the Web UI to help identify specific modems/SIMs.                                                                                                                                                                      |
| **`compatible`** | Boolean | `false` | **Compatibility Mode**. Some older modems lose network connectivity after switching eSIM profiles unless fully rebooted. If enabled, Sigmo will try to restart the modem device after profile operations.                                            |
| **`mss`**        | Int     | `240`   | **Max Segment Size**. Controls the APDU payload size (range 64-254) for SIM communication.<br>• If you experience errors during profile download, try lowering this value (e.g., 128 or 64).<br>• Most modern modems work fine with the default 240. |

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

> **Note**: The default service runs as `root` to ensure access to ModemManager. If running as a non-root user, verify `udev` rules for the modem and file permissions for `/etc/sigmo/config.toml`.

---

## 🏗️ Development

If you wish to contribute or modify the source:

1.  **Prerequisites**: Go 1.25+, Bun (for Vue).
2.  **Setup Config**: `cp configs/config.example.toml config.toml`
3.  **Build Frontend**:
    ```bash
    cd web && bun install && bun run build
    ```
4.  **Run Backend**:

    ```bash
    go run ./ -config config.toml
    ```

    _Or for frontend hot-reload:_ `cd web && bun run dev`

5.  **Private feature build**:

    The public module does not build eSIM Transfer or Wi-Fi Calling by default.
    Private builds use `go.private.mod` and download the private TS.43 and VoWiFi
    modules through normal Go module auth:

    ```bash
    export GOPRIVATE=github.com/damonto/*
    source scripts/private-features.env
    go run -tags="${PRIVATE_GO_TAGS}" -modfile="${PRIVATE_GO_MODFILE}" . -config config.toml
    ```

    To use SSH for private modules locally:

    ```bash
    export GOPRIVATE=github.com/damonto/*
    git config --global url."git@github.com:damonto/".insteadOf "https://github.com/damonto/"
    source scripts/private-features.env
    go build -tags="${PRIVATE_GO_TAGS}" -modfile="${PRIVATE_GO_MODFILE}" -o sigmo .
    sudo ./sigmo -config configs/config.toml
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
