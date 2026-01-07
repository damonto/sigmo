# Sigmo (Formerly Telmo)

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

Sigmo is a self-hosted web UI and API for managing ModemManager-based cellular modems.
It focuses on eSIM profile operations, SMS, USSD, and network control, and ships as a
single Go binary with an embedded Vue 3 frontend.

## Advertisement

If you do not have an eUICC yet, you can purchase one from [eSTK.me](https://store.estk.me?code=esimcyou)
and use the coupon code `esimcyou` to get 10% off. We recommend [eSTK.me](https://store.estk.me?code=esimcyou)
if you need to perform profile downloads on iOS devices.

If you require more than 1MB of storage to install multiple eSIM profiles, we recommend
[9eSIM](https://www.9esim.com/?coupon=DAMON). Use the coupon code `DAMON` to also receive 10% off.

## Features

- eSIM profile list, download (SM-DP+), enable, rename, and delete.
- SIM slot switching and modem settings (alias, MSS, compatibility mode).
- SMS conversations (list, send, delete) and USSD sessions.
- Network scan and manual registration.
- OTP login via notification providers (Telegram or HTTP).
- Optional SMS forwarding to the same notification channels.

## Architecture

- Go backend serving `/api/v1` and static UI from embedded `web/dist`.
- Vue 3 + Vite frontend under `web/`.

## Requirements

- Linux with ModemManager running on the system D-Bus.
- Access to modem device nodes (often root or proper udev rules).
- Go 1.25+ to build the backend.
- Bun to build the web UI.

## Install (Release)

Release binaries already include the embedded web UI, so you only need a config file.

1. Download the matching binary from the GitHub Releases page:
   - https://github.com/damonto/sigmo/releases/latest
2. Binaries are named by target (examples):
   - `sigmo-linux-amd64`, `sigmo-linux-arm64`, `sigmo-linux-armhf`
   - `sigmo-linux-i386`, `sigmo-linux-ppc64le`, `sigmo-linux-s390x`, `sigmo-linux-riscv64`
3. Example install on Linux (amd64):
   ```sh
   curl -LO https://github.com/damonto/sigmo/releases/latest/download/sigmo-linux-amd64
   chmod +x sigmo-linux-amd64
   sudo install -m 0755 sigmo-linux-amd64 /usr/local/bin/sigmo
   ```
4. Create a config file (see Configuration below):
   ```sh
   sudo mkdir -p /etc/sigmo
   sudo cp configs/config.example.toml /etc/sigmo/config.toml
   ```
   If you didn't clone the repo, download the example config:
   ```sh
   curl -L https://raw.githubusercontent.com/damonto/sigmo/main/configs/config.example.toml | sudo tee /etc/sigmo/config.toml >/dev/null
   ```
5. Run:
   ```sh
   /usr/local/bin/sigmo -config /etc/sigmo/config.toml
   ```
6. Open `http://localhost:9527`.

## Build From Source

1. Copy the example config and update credentials:
   `cp configs/config.example.toml config.toml`
2. Build the web UI:
   `cd web && bun install && bun run build`
3. Build the backend:
   `go build -o sigmo ./`
4. Run:
   `./sigmo -config config.toml`
5. Open `http://localhost:9527`.

## Configuration

Sigmo reads a TOML config file (default `config.toml`, override with `-config`).
The file is also written back when you update modem settings in the UI, so it must
be writable by the Sigmo process.

### Config Files

- `configs/config.example.toml`: starting point for a fresh install.
- `config.toml`: runtime config used by `-config`/`--config`. This file is updated
  when you change modem settings in the UI.
- `init/systemd/sigmo.service`: systemd unit example.
- `init/supervisor/supervisord.conf`: supervisor example.

### Config Reference

```toml
[app]
  environment = "production"
  listen_address = "0.0.0.0:9527"
  auth_providers = ["telegram"]
  otp_required = true

[channels]
  [channels.telegram]
    bot_token = "Your Telegram Bot Token"
    recipients = [123456789]

  [channels.bark]
    endpoint = "https://api.day.app/push"
    recipients = ["your_device_key"]
    subject = "Sigmo Notification"

  [channels.gotify]
    endpoint = "https://push.example.de/message"
    recipients = ["your_app_token"]
    subject = "Sigmo Notification"
    priority = 5

  [channels.sc3]
    endpoint = "https://123.push.ft07.com/send/your_sendkey.send"
    subject = "Sigmo Notification"

  [channels.http]
    endpoint = "https://httpbin.org/post"
    [channels.http.headers]
      Authorization = "Bearer 1234567890"
      Content-Type = "application/json"

  [channels.email]
    smtp_host = "smtp.example.com"
    smtp_port = 587
    smtp_username = "user@example.com"
    smtp_password = "app_password"
    from = "sigmo@example.com"
    recipients = ["ops@example.com"]
    subject = "Sigmo Notification"
    tls_policy = "mandatory"
    ssl = false

[modems]
  [modems."YOUR_MODEM_EQUIPMENT_ID"]
    alias = "Office Modem"
    compatible = false
    mss = 240
```

Notes:

- `app.environment` is used to decide log verbosity (`production` keeps logs quieter).
- `app.listen_address` is the bind address for the HTTP server.
- `app.auth_providers` selects which channels are allowed for OTP login (`telegram`, `bark`, `gotify`, `sc3`, `http`, `email`).
- `channels.*` are also used for SMS forwarding. If no channels are configured, OTP
  login and SMS forwarding are disabled.
- `channels.bark.endpoint` defaults to `https://api.day.app/push` when empty; `channels.bark.recipients` are Bark device keys (multiple keys are sent one by one).
- `channels.bark.subject` maps to Bark `title`.
- `channels.gotify.endpoint` should point to the Gotify `/message` endpoint; `channels.gotify.recipients` are app tokens (sent one by one).
- `channels.gotify.subject` maps to Gotify `title`, and `channels.gotify.priority` maps to Gotify `priority`.
- `channels.sc3.endpoint` should include the sendkey path (for example `https://123.push.ft07.com/send/your_sendkey.send`); `channels.sc3.subject` maps to `title`.
- `channels.email.tls_policy` supports `mandatory` (default), `opportunistic`, and `none`; set `channels.email.ssl = true` for SMTPS on port 465.
- `modems` is keyed by ModemManager EquipmentIdentifier (the modem ID shown by the UI).
- `modems.*.alias` is the display name shown in the UI.
- `modems.*.compatible` enables legacy modem restarts after profile changes.
- `modems.*.mss` controls APDU payload size per transfer (64-254, default 240).
- `modems` entries are optional; they are created/updated automatically when you save
  modem settings in the UI.

## Development

- Backend: `go run ./ -config config.toml`
- Frontend dev server:
  `cd web && bun install && bun run dev`
- More frontend details in `web/README.md`.

## Service

Sample service definitions are in `init/systemd/sigmo.service` and
`init/supervisor/supervisord.conf`.

### systemd (Example)

1. Install the binary and config:
   ```sh
   sudo install -m 0755 sigmo /usr/local/bin/sigmo
   sudo mkdir -p /etc/sigmo
   sudo cp configs/config.example.toml /etc/sigmo/config.toml
   ```
2. Install the unit file:
   ```sh
   sudo install -m 0644 init/systemd/sigmo.service /etc/systemd/system/sigmo.service
   ```
3. Reload and start:
   ```sh
   sudo systemctl daemon-reload
   sudo systemctl enable --now sigmo
   ```

Note: the default unit runs as `root` and expects `/usr/local/bin/sigmo` and
`/etc/sigmo/config.toml`. Adjust `User=` and paths if you run as a dedicated user.
Make sure the config file is writable by that user.

## License

MIT. See `LICENSE`.
