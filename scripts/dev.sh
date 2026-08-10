#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PRO_DIR="${ROOT_DIR}/pro"

source "${ROOT_DIR}/scripts/pro-features.env"

SSH_DIR="${SIGMO_SSH_DIR:-/home/user/.ssh}"
SSH_KEY="${SIGMO_SSH_KEY:-${SSH_DIR}/id_ed25519}"
DB_PATH="${SIGMO_DB_PATH:-${ROOT_DIR}/build/sigmo-dev.db}"
OUTPUT="${SIGMO_DEV_BIN:-${ROOT_DIR}/build/sigmo-dev}"
GOPRIVATE_PATTERN="${GOPRIVATE:-${PRO_GOPRIVATE}}"
KEYS_DIR="${SIGMO_KEYS_DIR:-${ROOT_DIR}/scripts/sigmo-keys}"
PRO_WORKER_URL="${SIGMO_PRO_WORKER_URL:-https://sigmo-pro-api.id.workers.dev}"

LICENSE_PUBLIC_KEY="${SIGMO_LICENSE_PUBLIC_KEY:-}"
if [ -z "${LICENSE_PUBLIC_KEY}" ]; then
	license_public_key_file="${KEYS_DIR}/license-public.raw.b64"
	if [ ! -s "${license_public_key_file}" ]; then
		echo "License public key not found: ${license_public_key_file}" >&2
		exit 1
	fi
	LICENSE_PUBLIC_KEY="$(<"${license_public_key_file}")"
fi

RELEASE_PUBLIC_KEY="${SIGMO_RELEASE_PUBLIC_KEY:-}"
if [ -z "${RELEASE_PUBLIC_KEY}" ]; then
	release_public_key_file="${KEYS_DIR}/release-public.raw.b64"
	if [ ! -s "${release_public_key_file}" ]; then
		echo "Release public key not found: ${release_public_key_file}" >&2
		exit 1
	fi
	RELEASE_PUBLIC_KEY="$(<"${release_public_key_file}")"
fi

if [ ! -f "${SSH_KEY}" ]; then
	echo "SSH key not found: ${SSH_KEY}" >&2
	exit 1
fi

cleanup_files=()

cleanup() {
	if [ "${#cleanup_files[@]}" -gt 0 ]; then
		rm -f "${cleanup_files[@]}"
	fi
}

add_cleanup_file() {
	cleanup_files+=("$1")
	trap cleanup EXIT
}

ssh_cmd=(ssh -i "${SSH_KEY}" -o IdentitiesOnly=yes)
if [ -f "${SSH_DIR}/config" ]; then
	ssh_cmd+=(-F "${SSH_DIR}/config")
fi
if [ -f "${SSH_DIR}/known_hosts" ]; then
	ssh_cmd+=(-o "UserKnownHostsFile=${SSH_DIR}/known_hosts")
fi
printf -v git_ssh_command '%q ' "${ssh_cmd[@]}"

git_config="$(mktemp)"
add_cleanup_file "${git_config}"

export GIT_CONFIG_GLOBAL="${git_config}"
export GIT_SSH_COMMAND="${git_ssh_command}"
export GOPRIVATE="${GOPRIVATE_PATTERN}"

git config --global url."git@github.com:damonto/".insteadOf "https://github.com/damonto/"

cd "${PRO_DIR}"
go_args=()
if [ -n "${PRO_GO_TAGS}" ]; then
	go_args+=(-tags="${PRO_GO_TAGS}")
fi
ldflags="-X github.com/damonto/sigmo/internal/app/buildinfo.ReleasePublicKey=${RELEASE_PUBLIC_KEY}"
ldflags+=" -X github.com/damonto/sigmo/pro/license.WorkerURL=${PRO_WORKER_URL}"
ldflags+=" -X github.com/damonto/sigmo/pro/license.LicensePublicKey=${LICENSE_PUBLIC_KEY}"
go_args+=(-ldflags="${ldflags}")

args=("$@")
if [ "${#args[@]}" -eq 0 ]; then
	args=(--db-path "${DB_PATH}" --debug)
fi

if [ "${SIGMO_BUILD_ONLY:-}" = "1" ]; then
	mkdir -p "$(dirname "${OUTPUT}")"
	go build "${go_args[@]}" -o "${OUTPUT}" .
	exit 0
fi

if [ "${SIGMO_NO_SUDO:-}" = "1" ]; then
	exec go run "${go_args[@]}" . "${args[@]}"
fi

exec go run -exec sudo "${go_args[@]}" . "${args[@]}"
