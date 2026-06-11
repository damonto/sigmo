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
