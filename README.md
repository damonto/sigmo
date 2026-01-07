# Sigmo (Formerly Telmo)

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Report Card](https://goreportcard.com/badge/github.com/damonto/sigmo)](https://goreportcard.com/report/github.com/damonto/sigmo)
[![Release](https://img.shields.io/github/v/release/damonto/sigmo.svg)](https://github.com/damonto/sigmo/releases/latest)

**Sigmo** is a modern, self-hosted web UI and API for managing ModemManager-based cellular modems. It ships as a single binary with an embedded Vue 3 frontend, designed to be lightweight and easy to deploy.

Sigmo focuses on advanced eSIM operations, SMS management, and network control.

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
- **Service**: `ModemManager` running on the system D-Bus.
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

Create the configuration directory and file.

```bash
sudo mkdir -p /etc/sigmo
# Download example config
curl -L https://raw.githubusercontent.com/damonto/sigmo/main/configs/config.example.toml | sudo tee /etc/sigmo/config.toml >/dev/null
```

### 3. Run

Start the service.

```bash
/usr/local/bin/sigmo -config /etc/sigmo/config.toml
```

Visit `http://localhost:9527` to access the UI.

---

## ⚙️ Configuration Reference

Sigmo runs using a TOML configuration file (default: `/etc/sigmo/config.toml`).

> **⚠️ Important**: This file is **Read-Write**.
> When you update modem aliases or settings via the Web UI, Sigmo **writes the changes back** to this file. Ensure the Sigmo process has **write permissions** to the config file.

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

> **Note**: If no channels are configured, OTP login and SMS forwarding features will be automatically disabled.

#### Telegram

```toml
[channels.telegram]
  bot_token = "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11"
  recipients = [123456789, 987654321]
```

- `bot_token`: The token received from @BotFather.
- `recipients`: Array of Integer Chat IDs authorized to receive messages.

#### Bark (iOS Push)

```toml
[channels.bark]
  endpoint = "https://api.day.app"
  recipients = ["device_key_1", "device_key_2"]
  subject = "Sigmo Alert"
```

- `endpoint`: Bark server URL. Leave empty to use the official `https://api.day.app`. Sigmo automatically appends `/push`.
- `recipients`: List of Device Keys from the Bark App.
- `subject`: The title of the push notification.

#### Gotify (Self-Hosted)

```toml
[channels.gotify]
  endpoint = "https://push.example.com"
  recipients = ["AsDh82..."]
  subject = "Sigmo Notification"
  priority = 5
```

- `endpoint`: Base URL of your Gotify server (do not add `/message`; it is appended automatically).
- `recipients`: List of Gotify **Application Tokens**.
- `priority`: Message priority (Integer), default is 5.

#### ServerChan (SendKey)

```toml
[channels.sc3]
  endpoint = "https://<uid>.push.ft07.com/send/<sendkey>.send"
  subject = "Sigmo Login"
```

- `endpoint`: The full URL including the SendKey.
- `subject`: Message title.

#### HTTP (Webhook)

```toml
[channels.http]
  endpoint = "https://httpbin.org/post"
  [channels.http.headers]
    Authorization = "Bearer secret_token"
    Content-Type = "application/json"
```

- `endpoint`: The target Webhook URL.
- `headers`: Key-Value pairs for custom HTTP headers. Sigmo sends a JSON payload containing the message body.

#### Email (SMTP)

```toml
[channels.email]
  smtp_host = "smtp.gmail.com"
  smtp_port = 587
  smtp_username = "yourname@gmail.com"
  smtp_password = "app_password"
  from = "Sigmo <yourname@gmail.com>"
  recipients = ["admin@example.com"]
  subject = "Sigmo Alert"
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

| Parameter        | Type    | Default | Description                                                                                                                                                                                                                                                 |
| :--------------- | :------ | :------ | :---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **`alias`**      | String  | (None)  | **Custom Name**. Displayed in the Web UI to help identify specific modems/SIMs.                                                                                                                                                                             |
| **`compatible`** | Boolean | `false` | **Compatibility Mode**. Some older modems lose network connectivity after switching eSIM profiles unless fully rebooted. If enabled, Sigmo will try to restart the modem device after profile operations.                                                   |
| **`mss`**        | Int     | `240`   | **Max Segment Size**. Controls the APDU payload size (range 64-254) for SIM communication.<br>• If you experience errors during profile download/enable, try lowering this value (e.g., 128 or 64).<br>• Most modern modems work fine with the default 240. |

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

---

## 📄 License

Released under the [MIT License](LICENSE).
