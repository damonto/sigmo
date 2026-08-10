#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PRO_DIR="${ROOT_DIR}/pro"

source "${ROOT_DIR}/scripts/pro-features.env"

PRO_MODFILE="${PRO_MODFILE:-${PRO_GO_MODFILE}}"
PRO_SUMFILE="${PRO_SUMFILE:-${PRO_MODFILE%.mod}.sum}"
OUTPUT_DIR="${SIGMO_BUILD_DIR:-${ROOT_DIR}/build/pro}"
GOPRIVATE_PATTERN="${GOPRIVATE:-${PRO_GOPRIVATE}}"
PRO_TARGETS="${SIGMO_PRO_TARGETS:-linux-amd64 linux-amd64-musl linux-arm64 linux-arm64-musl linux-arm linux-arm-musl}"

case "${OUTPUT_DIR}" in
	/*) OUTPUT_DIR_ABS="${OUTPUT_DIR}" ;;
	*) OUTPUT_DIR_ABS="${ROOT_DIR}/${OUTPUT_DIR}" ;;
esac

cleanup_files=()
cleanup_dirs=()
musl_modfile=""
musl_modfile_key=""

cleanup() {
	if [ "${#cleanup_files[@]}" -gt 0 ]; then rm -f "${cleanup_files[@]}"; fi
	if [ "${#cleanup_dirs[@]}" -gt 0 ]; then rm -rf "${cleanup_dirs[@]}"; fi
}

add_cleanup_file() { cleanup_files+=("$1"); trap cleanup EXIT; }
add_cleanup_dir() { cleanup_dirs+=("$1"); trap cleanup EXIT; }

configure_token_auth() {
	local git_config
	git_config="$(mktemp)"
	add_cleanup_file "${git_config}"
	export GIT_CONFIG_GLOBAL="${git_config}"
	git config --global url."https://x-access-token:${SIGMO_PRO_MODULE_TOKEN}@github.com/".insteadOf "https://github.com/"
}

configure_ssh_auth() {
	local ssh_dir ssh_key git_config git_ssh_command
	local ssh_cmd=()
	ssh_dir="${SIGMO_SSH_DIR:-${HOME}/.ssh}"
	ssh_key="${SIGMO_SSH_KEY:-${ssh_dir}/id_ed25519}"
	if [ ! -f "${ssh_key}" ]; then echo "SSH key not found: ${ssh_key}" >&2; return 1; fi
	git_config="$(mktemp)"
	add_cleanup_file "${git_config}"
	ssh_cmd=(ssh -i "${ssh_key}" -o IdentitiesOnly=yes)
	if [ -f "${ssh_dir}/config" ]; then ssh_cmd+=(-F "${ssh_dir}/config"); fi
	if [ -f "${ssh_dir}/known_hosts" ]; then ssh_cmd+=(-o "UserKnownHostsFile=${ssh_dir}/known_hosts"); fi
	printf -v git_ssh_command '%q ' "${ssh_cmd[@]}"
	export GIT_CONFIG_GLOBAL="${git_config}"
	export GIT_SSH_COMMAND="${git_ssh_command}"
	git config --global url."git@github.com:damonto/".insteadOf "https://github.com/damonto/"
}

configure_pro_auth() {
	export GOPRIVATE="${GOPRIVATE_PATTERN}"
	export GONOSUMDB="${GONOSUMDB:-${GOPRIVATE_PATTERN}}"
	if [ -n "${SIGMO_PRO_MODULE_TOKEN:-}" ]; then configure_token_auth; return; fi
	if [ "${SIGMO_SKIP_PRO_AUTH:-0}" = "1" ]; then return; fi
	configure_ssh_auth
}

build_frontend() {
	if [ "${SIGMO_SKIP_FRONTEND_BUILD:-0}" = "1" ]; then return; fi
	(cd "${ROOT_DIR}/web" && bun install --frozen-lockfile && bun run build --mode prod)
}

root_path() {
	case "$1" in /*) printf '%s\n' "$1" ;; *) printf '%s/%s\n' "${ROOT_DIR}" "$1" ;; esac
}

copy_sumfile() {
	if [ -f "$1" ]; then cp "$1" "$2"; else : > "$2"; fi
}

target_arch() {
	if [ -n "${SIGMO_PRO_TARGET_ARCH:-}" ]; then printf '%s\n' "${SIGMO_PRO_TARGET_ARCH}"; return; fi
	case "$1" in
		linux-amd64|linux-amd64-musl) printf 'amd64\n' ;;
		linux-arm64|linux-arm64-musl) printf 'arm64\n' ;;
		linux-arm|linux-arm-musl) printf 'arm\n' ;;
		*) echo "unknown Pro target: $1" >&2; return 1 ;;
	esac
}

target_musl() {
	case "${SIGMO_PRO_TARGET_MUSL:-}" in
		1|true|TRUE|yes|YES) printf '1\n'; return ;;
		0|false|FALSE|no|NO) printf '0\n'; return ;;
		"") ;;
		*) echo "invalid SIGMO_PRO_TARGET_MUSL: ${SIGMO_PRO_TARGET_MUSL}" >&2; return 1 ;;
	esac
	case "$1" in *-musl) printf '1\n' ;; *) printf '0\n' ;; esac
}

musl_interpreter() {
	case "$1" in
		amd64) printf '/lib/ld-musl-x86_64.so.1\n' ;;
		arm) printf '/lib/ld-musl-armhf.so.1\n' ;;
		arm64) printf '/lib/ld-musl-aarch64.so.1\n' ;;
		*) echo "unsupported musl interpreter for GOARCH: $1" >&2; return 1 ;;
	esac
}

prepare_musl_modfile() {
	local name="$1" goarch="$2" key source_modfile source_sumfile purego_tmp purego_dir
	key="${name}-${goarch}"
	if [ "${musl_modfile_key}" = "${key}" ]; then return; fi
	source_modfile="$(root_path "${PRO_MODFILE}")"
	source_sumfile="$(root_path "${PRO_SUMFILE}")"
	musl_modfile="${OUTPUT_DIR_ABS}/go.${name}.mod"
	musl_modfile_key="${key}"
	(cd "${PRO_DIR}" && go mod download)
	purego_tmp="$(mktemp -d)"
	add_cleanup_dir "${purego_tmp}"
	purego_dir="$(cd "${PRO_DIR}" && go list -m -f '{{.Dir}}' github.com/ebitengine/purego)"
	cp -R "${purego_dir}" "${purego_tmp}/purego"
	cp "${source_modfile}" "${musl_modfile}"
	copy_sumfile "${source_sumfile}" "${musl_modfile%.mod}.sum"
	go mod edit -modfile="${musl_modfile}" -replace=github.com/ebitengine/purego="${purego_tmp}/purego"
	TARGETARCH="${goarch}" "${ROOT_DIR}/scripts/patch-purego-musl.sh" "${musl_modfile}"
}

build_target() {
	local name="$1" goarch musl binary ldflags interpreter
	local go_args=()
	goarch="$(target_arch "${name}")"
	musl="$(target_musl "${name}")"
	binary="${OUTPUT_DIR_ABS}/sigmo-pro-${name}"
	ldflags="-w -s"
	ldflags+=" -X github.com/damonto/sigmo/internal/app/buildinfo.Version=${SIGMO_BUILD_VERSION}"
	ldflags+=" -X github.com/damonto/sigmo/internal/app/buildinfo.Commit=${SIGMO_BUILD_COMMIT}"
	ldflags+=" -X github.com/damonto/sigmo/internal/app/buildinfo.Channel=${SIGMO_BUILD_CHANNEL}"
	ldflags+=" -X github.com/damonto/sigmo/internal/app/buildinfo.Edition=pro"
	ldflags+=" -X github.com/damonto/sigmo/internal/app/buildinfo.Target=${name}"
	ldflags+=" -X github.com/damonto/sigmo/internal/app/buildinfo.Distribution=standalone"
	ldflags+=" -X github.com/damonto/sigmo/internal/app/buildinfo.ReleasePublicKey=${SIGMO_RELEASE_PUBLIC_KEY}"
	ldflags+=" -X github.com/damonto/sigmo/pro/license.WorkerURL=${SIGMO_PRO_WORKER_URL}"
	ldflags+=" -X github.com/damonto/sigmo/pro/license.LicensePublicKey=${SIGMO_LICENSE_PUBLIC_KEY}"
	if [ "${musl}" = "1" ]; then
		interpreter="$(musl_interpreter "${goarch}")"
		prepare_musl_modfile "${name}" "${goarch}"
		ldflags="-I ${interpreter} ${ldflags}"
		go_args+=(-a -modfile="${musl_modfile}")
	fi
	go_args+=(-tags="${PRO_GO_TAGS}" -trimpath -ldflags="${ldflags}" -o "${binary}" .)
	echo "Building ${binary}"
	(cd "${PRO_DIR}" && env GOOS=linux GOARCH="${goarch}" CGO_ENABLED=0 go build "${go_args[@]}")
}

main() {
	if [ ! -f "$(root_path "${PRO_MODFILE}")" ]; then echo "Pro modfile not found: ${PRO_MODFILE}" >&2; return 1; fi
	: "${SIGMO_BUILD_VERSION:?SIGMO_BUILD_VERSION is required}"
	: "${SIGMO_BUILD_COMMIT:?SIGMO_BUILD_COMMIT is required}"
	: "${SIGMO_BUILD_CHANNEL:?SIGMO_BUILD_CHANNEL is required}"
	: "${SIGMO_RELEASE_PUBLIC_KEY:?SIGMO_RELEASE_PUBLIC_KEY is required}"
	: "${SIGMO_LICENSE_PUBLIC_KEY:?SIGMO_LICENSE_PUBLIC_KEY is required}"
	: "${SIGMO_PRO_WORKER_URL:?SIGMO_PRO_WORKER_URL is required}"
	mkdir -p "${OUTPUT_DIR_ABS}"
	export GOCACHE="${GOCACHE:-${OUTPUT_DIR_ABS}/.go-build-cache}"
	mkdir -p "${GOCACHE}"
	configure_pro_auth
	build_frontend
	for target in ${PRO_TARGETS}; do build_target "${target}"; done
}

main "$@"
