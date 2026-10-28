#!/usr/bin/env bash
set -euo pipefail
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
KEYS_DIR="${SIGMO_KEYS_DIR:-${ROOT_DIR}/scripts/sigmo-keys}"
LICENSE_PUBLIC_KEY="${SIGMO_LICENSE_PUBLIC_KEY:-}"
if [ -z "${LICENSE_PUBLIC_KEY}" ] && [ -s "${KEYS_DIR}/license-public.raw.b64" ]; then
    LICENSE_PUBLIC_KEY="$(<"${KEYS_DIR}/license-public.raw.b64")"
fi
RELEASE_PUBLIC_KEY="${SIGMO_RELEASE_PUBLIC_KEY:-}"
if [ -z "${RELEASE_PUBLIC_KEY}" ] && [ -s "${KEYS_DIR}/release-public.raw.b64" ]; then
    RELEASE_PUBLIC_KEY="$(<"${KEYS_DIR}/release-public.raw.b64")"
fi
ldflags="-X github.com/damonto/sigmo/internal/app/buildinfo.ReleasePublicKey=${RELEASE_PUBLIC_KEY}"
ldflags+=" -X github.com/damonto/sigmo/internal/pkg/license.WorkerURL=${SIGMO_WORKER_URL:-https://sigmo.952007.xyz}"
if [ -n "${LICENSE_PUBLIC_KEY}" ]; then
    ldflags+=" -X github.com/damonto/sigmo/internal/pkg/license.LicensePublicKey=${LICENSE_PUBLIC_KEY}"
fi
ldflags+=" -X github.com/damonto/sigmo/internal/pkg/nativepush.GatewayURL=${SIGMO_PUSH_GATEWAY_URL:-https://sigmo.952007.xyz}"
cd "${ROOT_DIR}"
output="${SIGMO_DEV_BIN:-${ROOT_DIR}/build/sigmo-dev}"
mkdir -p "$(dirname "${output}")"
go build -ldflags="${ldflags}" -o "${output}" .
if [ "${SIGMO_BUILD_ONLY:-0}" = "1" ]; then exit 0; fi
args=("$@")
if [ "$#" = "0" ]; then args=(--db-path "${SIGMO_DB_PATH:-${ROOT_DIR}/build/sigmo-dev.db}" --debug); fi
if [ "${SIGMO_NO_SUDO:-0}" = "1" ]; then exec "${output}" "${args[@]}"; fi
exec sudo "${output}" "${args[@]}"
