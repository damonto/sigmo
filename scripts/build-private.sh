#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

source "${ROOT_DIR}/scripts/private-features.env"

PRIVATE_MODFILE="${PRIVATE_MODFILE:-${PRIVATE_GO_MODFILE}}"
PRIVATE_SUMFILE="${PRIVATE_SUMFILE:-${PRIVATE_MODFILE%.mod}.sum}"
OUTPUT_DIR="${SIGMO_BUILD_DIR:-${ROOT_DIR}/build}"
GOPRIVATE_PATTERN="${GOPRIVATE:-${PRIVATE_GOPRIVATE}}"
MUSL_ARM64_LIBC="libc.musl-aarch64.so.1"
MUSL_ARM64_INTERPRETER="/lib/ld-musl-aarch64.so.1"

cleanup_files=()
cleanup_dirs=()

cleanup() {
	if [ "${#cleanup_files[@]}" -gt 0 ]; then
		rm -f "${cleanup_files[@]}"
	fi
	if [ "${#cleanup_dirs[@]}" -gt 0 ]; then
		rm -rf "${cleanup_dirs[@]}"
	fi
}

add_cleanup_file() {
	cleanup_files+=("$1")
	trap cleanup EXIT
}

add_cleanup_dir() {
	cleanup_dirs+=("$1")
	trap cleanup EXIT
}

configure_private_auth() {
	local ssh_dir
	local ssh_key
	local git_config
	local ssh_cmd=()
	local git_ssh_command

	export GOPRIVATE="${GOPRIVATE_PATTERN}"
	export GONOSUMDB="${GONOSUMDB:-${GOPRIVATE_PATTERN}}"

	ssh_dir="${SIGMO_SSH_DIR:-${HOME}/.ssh}"
	ssh_key="${SIGMO_SSH_KEY:-${ssh_dir}/id_ed25519}"
	if [ ! -f "${ssh_key}" ]; then
		echo "SSH key not found: ${ssh_key}" >&2
		return 1
	fi

	git_config="$(mktemp)"
	add_cleanup_file "${git_config}"

	ssh_cmd=(ssh -i "${ssh_key}" -o IdentitiesOnly=yes)
	if [ -f "${ssh_dir}/config" ]; then
		ssh_cmd+=(-F "${ssh_dir}/config")
	fi
	if [ -f "${ssh_dir}/known_hosts" ]; then
		ssh_cmd+=(-o "UserKnownHostsFile=${ssh_dir}/known_hosts")
	fi
	printf -v git_ssh_command '%q ' "${ssh_cmd[@]}"

	export GIT_CONFIG_GLOBAL="${git_config}"
	export GIT_SSH_COMMAND="${git_ssh_command}"
	git config --global url."git@github.com:damonto/".insteadOf "https://github.com/damonto/"
}

build_frontend() {
	if [ "${SIGMO_SKIP_FRONTEND_BUILD:-0}" = "1" ]; then
		echo "Skipping frontend build because SIGMO_SKIP_FRONTEND_BUILD=1."
		return
	fi

	(
		cd "${ROOT_DIR}/web"
		bun install --frozen-lockfile
		bun run build --mode prod
	)
}

build_version() {
	if [ -n "${SIGMO_BUILD_VERSION:-}" ]; then
		printf '%s\n' "${SIGMO_BUILD_VERSION}"
		return
	fi

	git describe --always --tags --match "v*" --dirty="-dev" 2>/dev/null || printf 'dev\n'
}

copy_sumfile() {
	local from="$1"
	local to="$2"

	if [ -f "${from}" ]; then
		cp "${from}" "${to}"
		return
	fi

	: > "${to}"
}

prepare_arm64_musl_modfile() {
	local source_modfile="$1"
	local source_sumfile="$2"
	local target_modfile="$3"
	local purego_tmp
	local purego_dir

	go mod download -modfile="${source_modfile}"

	purego_tmp="$(mktemp -d)"
	add_cleanup_dir "${purego_tmp}"
	purego_dir="$(go list -modfile="${source_modfile}" -m -f '{{.Dir}}' github.com/ebitengine/purego)"
	cp -R "${purego_dir}" "${purego_tmp}/purego"

	cp "${source_modfile}" "${target_modfile}"
	copy_sumfile "${source_sumfile}" "${target_modfile%.mod}.sum"
	go mod edit -modfile="${target_modfile}" -replace=github.com/ebitengine/purego="${purego_tmp}/purego"

	TARGETARCH=arm64 PUREGO_MUSL_LIBC="${MUSL_ARM64_LIBC}" \
		"${ROOT_DIR}/scripts/patch-purego-musl.sh" "${target_modfile}"
}

build_target() {
	local name="$1"
	local goarch="$2"
	local musl="${3:-0}"
	local modfile="${PRIVATE_MODFILE}"
	local ldflags
	local output
	local go_args=()

	ldflags="-w -s -X main.BuildVersion=${BUILD_VERSION}"
	output="${OUTPUT_DIR}/sigmo-${name}"

	if [ "${musl}" = "1" ]; then
		modfile="${OUTPUT_DIR}/go.${name}.mod"
		prepare_arm64_musl_modfile "${PRIVATE_MODFILE}" "${PRIVATE_SUMFILE}" "${modfile}"
		ldflags="-I ${MUSL_ARM64_INTERPRETER} ${ldflags}"
		go_args+=(-a)
	fi

	echo "Building ${output}"
	go_args+=(
		-tags="${PRIVATE_GO_TAGS}"
		-modfile="${modfile}"
		-trimpath
		-ldflags="${ldflags}"
		-o "${output}"
		.
	)

	env GOOS=linux GOARCH="${goarch}" CGO_ENABLED=0 go build "${go_args[@]}"
}

main() {
	if [ ! -f "${ROOT_DIR}/${PRIVATE_MODFILE}" ] && [ ! -f "${PRIVATE_MODFILE}" ]; then
		echo "private modfile not found: ${PRIVATE_MODFILE}" >&2
		return 1
	fi

	cd "${ROOT_DIR}"
	mkdir -p "${OUTPUT_DIR}"
	configure_private_auth
	BUILD_VERSION="$(build_version)"
	export BUILD_VERSION

	build_frontend
	build_target "linux-amd64" "amd64"
	build_target "linux-arm64" "arm64"
	build_target "linux-arm64-musl" "arm64" "1"
}

main "$@"
