# Sigmo Pro

Sigmo Pro is released under the [MIT License](../LICENSE).

Sigmo Pro is the nested Go module for features that should not affect the public
`go.mod`: eSIM Quick Transfer, IMS access, carrier Websheets, WebRTC call media,
and voice codec support.

The Pro module imports the public Sigmo module through:

```go
replace github.com/damonto/sigmo => ..
```

## Features

- **eSIM Quick Transfer** (`esim_transfer`): Transfers supported physical SIM or eSIM lines from another modem or CCID reader to the target eUICC through TS.43 carrier flows.
- **IMS access** (`ims`): Adds Sigmo-managed VoWiFi and VoLTE SMS, USSD, call control, WebRTC media, and carrier websheet flows.
- **Websheets** (`esim_transfer` or `ims`): Pro-owned carrier websheet proxy routes.
- **Voice media** (`ims`): Bridges browser WebRTC audio to the codecs provided by `ims-go`.

## Structure

Pro mirrors the public module layout:

```text
pro/
  internal/app/handler/...
  internal/pkg/...
```

HTTP routes are registered from the Pro entrypoint through `server.Runtime`
extensions. The public router stays unaware of Pro handlers and Pro packages.

## Local Development

Use normal Go module auth for private Pro dependencies:

```bash
export GOPRIVATE=github.com/damonto/*
git config --global url."git@github.com:damonto/".insteadOf "https://github.com/damonto/"
```

Run Pro with both features:

```bash
cd pro
go run -tags=esim_transfer,ims . --db-path=../sigmo.db --debug
```

Or build first and run the binary with the permissions needed to access
ModemManager:

```bash
cd pro
go build -tags=esim_transfer,ims -o ../sigmo-pro .
sudo ../sigmo-pro --db-path=/var/lib/sigmo/sigmo.db
```

Prefer building as your normal user and running the binary with `sudo`. Running
`sudo go run` makes Go and Git use root's module cache and Git/SSH configuration.

The repository helper uses `scripts/pro-features.env`, builds with the normal
user's Go cache, and starts the temporary `go run` binary with `sudo`:

```bash
./scripts/dev.sh
```

## Dependency Updates

Update public dependencies and Pro dependencies together:

```bash
./scripts/update-pro-deps.sh
```

By default, Pro-only modules are pinned to the remote `HEAD` pseudo-version.
Use tagged release versions instead with:

```bash
./scripts/update-pro-deps.sh --module-version=tags
```

`--pseudo` and `--tags` are short aliases for the two modes.

## CI

GitHub Actions Pro builds pass `PRO_GO_TAGS` and `PRO_GO_MODFILE=pro/go.mod`.
Private module access uses the repository secret `SIGMO_PRO_MODULE_TOKEN`.
Pull request builds keep using the public module manifest so Pro module
credentials are not exposed.

## License

Sigmo Pro is released under the [MIT License](../LICENSE).
