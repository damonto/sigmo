# Official carrier configuration

Sigmo has one Go module and one binary, including IMS, eSIM transfer, call media
and carrier websheet support. Pro identifies an authorized user; there are no
product editions, Pro build tags, separate modules, or activation-only servers.
HTTP middleware, MCP authorization and capability responses
use the same live entitlement policy. Capability names are typed and unknown
names are denied. MCP permissions declare their capability explicitly; names and
module prefixes do not decide authorization. Base services remain available without a
Pro authorization. Pairing updates rights without restarting the process.

Only Wi-Fi Calling, VoLTE and eSIM Transfer require Pro authorization. Call
control and call history are base features. Calls over Wi-Fi Calling or VoLTE
still depend on those services being configured and connected.

The Alpine image includes the PC/SC client library for CCID transfer sources.
Reader access still requires the host's PC/SC service and the appropriate socket
or device mapping; building the image does not access any physical reader.

`ts43-go` and `ims-go` contain parsing, matching and protocol implementation, but
no private carrier dataset. Decode entitlement JSON into `ts43.EntitlementConfigs`
and WFC JSON into `ts43.WFCConfigs` independently with `json.Unmarshal` or
`UnmarshalBinary`. Inject transfer data through `ts43.Config.Entitlements` and
WFC data through `ims.Config.WFC` or `wfcsetup.Request.WFC`. Decode IMS JSON into
`ims.CarrierProfiles` and inject it through `ims.Config.Carriers`. All three
catalog types implement JSON and binary marshal/unmarshal interfaces; the binary
representation is the same UTF-8 JSON array. Decode before sharing with clients.

## Storage and publication

Maintain `entitlement.json`, `wfc.json` and `ims.json` outside version control.
No separate configuration repository is required. The migration preserves the
current files in the ignored local directory `build/private-carrier-config/`.
Keep an independent protected backup before removing any old repositories or
build artifacts. Files under `build/` are never a backup policy.

Create one storage key and keep it with deployment credentials, outside Git:

```sh
mkdir -p scripts/sigmo-keys
chmod 700 scripts/sigmo-keys
umask 077
openssl rand -base64 32 > scripts/sigmo-keys/config-encryption.raw.b64
```

Do not regenerate this key for each publication. Configure the same value as
the existing Worker's `SIGMO_CONFIG_ENCRYPTION_KEY` secret:

```sh
cd pro-api
bunx wrangler secret put SIGMO_CONFIG_ENCRYPTION_KEY < ../scripts/sigmo-keys/config-encryption.raw.b64
```

Prepare a validated, compressed and encrypted release locally:

```sh
scripts/publish-configs.sh /path/to/private/carrier-files
```

This writes only ciphertext and a revision pointer under `build/config-release/`.
`SIGMO_CONFIG_KEY_FILE`, `SIGMO_CONFIG_OUTPUT` and `SIGMO_CONFIG_BUCKET` override
the default key path, output directory and existing `sigmo` bucket.
`SIGMO_CONFIG_ENCRYPTION_KEY` may supply the key through the environment instead.

After reviewing the local result, publish with:

```sh
scripts/publish-configs.sh /path/to/private/carrier-files --publish
```

The script uploads `configs/releases/<sha256>.enc` before `configs/current.json`.
Each revision identifies the compressed contents of all three files. Rollback
replaces only the pointer with a previously published revision. Keep the R2
bucket private, with public domains and `r2.dev` disabled. No R2 access key or
configuration decryption key belongs in a client build.

## Delivery and cache

`GET /v1/configurations/current?recipient=<base64url-SPKI-P256-public-key>` uses
the existing device signature and Pro lease. The signature covers the complete
query string, including the recipient. The Worker checks current D1 entitlement,
device revocation and session generation on every request. It permits six
configuration requests per device per minute and records device and revision
metadata without logging the configuration itself.

The R2 object is JWE `dir`/`A256GCM`. The Worker decrypts its compressed bytes,
binds the revision to the device and session with the lease deadline, signs the
exact payload bytes using the existing license signing key and the distinct
`sigmo-carrier-config-v1\n` context, and encrypts the response with JWE
`ECDH-ES`/`A256GCM` for the recipient device. It does not expand the catalog.

The backend keeps an Ed25519 installation identity for signed requests and a
separate P-256 key at `<device.identity>.configs.key` for configuration decryption. It stores only
the encrypted response at `<device.identity>.configs`, with file mode `0600`.
Decryption, signature verification, revision checks and bounded decompression
happen in the Go process; browsers and iOS clients never receive the catalog.

An unexpired signed cache can be used during a network outage. Its deadline is
never extended locally. Explicit service revocation removes the cache. Base
application services continue without a Pro lease. A verified, unexpired cache
also restores the feature authorization after a restart without requiring a
network connection. A newer online lease takes precedence over an older cached
grant. Configuration HTTP requests run outside the state lock, so revocation and
pairing do not wait for a download; a revoked in-flight response cannot restore
rights or recreate its disk cache. The license status response contains `authorized` and
`deviceId`; there is no compatibility fallback for older servers.

This prevents casual copying of encrypted files between installations. A user
who controls an authorized installation can still extract plaintext or copy its
private keys. There is no remote erasure of data already extracted.

## Code licenses

Both SDKs use GPLv3 for their original code. Sigmo's original code keeps its MIT
license. A combined binary incorporating GPL libraries has GPL distribution
requirements; see [source and distribution licenses](THIRD_PARTY_NOTICES.md).
Preserve the SDKs' existing third-party notices, including the netstack and EVS
reference implementation terms. Repository visibility does not replace a license.

## Rollout and verification

1. Release `ts43-go` before `ims-go`, then update Sigmo's module versions and
   checksums. Always use a new module version when release contents change.
2. Configure the Worker storage secret, prepare and upload a configuration
   revision, and apply `0006_configuration_downloads.sql` to the existing D1
   database. These are deployment operations, not test commands.
3. Deploy the Worker before the unified Sigmo binary and updated clients.
4. Publish the unified binary and updated clients as a new version. There is no
   old-client compatibility path. The official license public key and Worker URL
   ship as public defaults and may be overridden by release linker flags.

The single `build.yml` workflow runs the frontend build once and builds every
Linux target with `scripts/build.sh`. Target names determine the architecture and
musl variant, so artifact names cannot disagree with separate architecture flags. Stable tags and the moving `dev` prerelease
publish the same `sigmo-linux-*.gz` artifacts and signed `sigmo-update.json` to
GitHub. R2 receives a copy for the existing bot download service. Updates use the
public GitHub source for every user; a Pro authorization does not select builds.
Publishing is serialized per channel and retains immutable R2 versions for
rollback and existing download tickets. GitHub expires temporary CI artifacts
according to their configured retention period.
For a local build:

```sh
SIGMO_BUILD_VERSION=dev-01234567 SIGMO_BUILD_COMMIT=0123456789abcdef0123456789abcdef01234567 \
SIGMO_BUILD_CHANNEL=dev SIGMO_TARGETS=linux-amd64 scripts/build.sh
```

Standalone release builds additionally set `SIGMO_DISTRIBUTION=standalone` and
`SIGMO_RELEASE_PUBLIC_KEY`. Use `SIGMO_WORKER_URL` to override the official API
address at build time. No private carrier files are required for a build.

For configuration rollout, reuse the existing storage encryption key. Keep an
independent protected backup of the carrier files and key. From the repository
root, run:

```sh
cd pro-api
bun install --frozen-lockfile
bunx wrangler secret put SIGMO_CONFIG_ENCRYPTION_KEY < ../scripts/sigmo-keys/config-encryption.raw.b64
cd ..
scripts/publish-configs.sh /path/to/private/carrier-files --publish
cd pro-api
bun run deploy:production
```

This reuses the existing bucket, database and authorization keys. Confirm that
the R2 bucket remains private. Deploy the Worker before pushing the Sigmo release
that activates the new configuration flow. Check the CI signing/R2 credentials
and the `SIGMO_WORKER_URL` repository variable before pushing `main` or a release
tag. Later configuration updates only need the publishing script; clients fetch
the updated revision on their next configuration load or refresh.

Run public Go checks at the repository root. There is no separate Pro module.
For optional private catalog regression tests in the sibling libraries, set
`TS43_CARRIER_CONFIG_DIR` for `ts43-go` (containing `entitlement.json` and
`wfc.json`) or `IMS_CARRIER_CONFIG_DIR` for `ims-go` (containing `ims.json`).
Ordinary public parser and protocol tests require no private files.

