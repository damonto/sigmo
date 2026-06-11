#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PRO_DIR="${ROOT_DIR}/pro"

source "${ROOT_DIR}/scripts/pro-features.env"

PRO_MODFILE="${PRO_MODFILE:-${PRO_GO_MODFILE}}"
PRO_SUMFILE="${PRO_SUMFILE:-${PRO_MODFILE%.mod}.sum}"
OUTPUT_DIR="${SIGMO_BUILD_DIR:-${ROOT_DIR}/build/pro}"
MANIFEST="${SIGMO_PRO_MANIFEST:-${OUTPUT_DIR}/artifacts.tsv}"
GOPRIVATE_PATTERN="${GOPRIVATE:-${PRO_GOPRIVATE}}"
MUSL_ARM64_LIBC="libc.musl-aarch64.so.1"
MUSL_ARM64_INTERPRETER="/lib/ld-musl-aarch64.so.1"

cleanup_files=()
cleanup_dirs=()
musl_modfile=""

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

trim_chat_id() {
	local chat_id="$1"

	chat_id="${chat_id//$'\r'/}"
	chat_id="${chat_id//$'\t'/}"
	chat_id="${chat_id// /}"
	printf '%s\n' "${chat_id}"
}

load_recipients() {
	local chat_id
	local raw
	local recipients=()

	if [ "$#" -gt 0 ]; then
		recipients=("$@")
	else
		raw="${SIGMO_PRO_TELEGRAM_CHAT_IDS:-}"
		if [ -z "${raw}" ] && [ -n "${SIGMO_PRO_TELEGRAM_CHAT_ID:-}" ]; then
			raw="${SIGMO_PRO_TELEGRAM_CHAT_ID}"
		fi
		if [ -z "${raw}" ]; then
			echo "SIGMO_PRO_TELEGRAM_CHAT_IDS is required" >&2
			return 1
		fi
		mapfile -t recipients < <(printf '%s\n' "${raw}" | tr ',' '\n')
	fi

	for chat_id in "${recipients[@]}"; do
		chat_id="$(trim_chat_id "${chat_id}")"
		case "${chat_id}" in
			"" | \#*)
				continue
				;;
		esac
		if [[ ! "${chat_id}" =~ ^-?[0-9]+$ ]]; then
			echo "invalid Telegram chat id: ${chat_id}" >&2
			return 1
		fi
		printf '%s\n' "${chat_id}"
	done
}

configure_token_auth() {
	local git_config

	git_config="$(mktemp)"
	add_cleanup_file "${git_config}"

	export GIT_CONFIG_GLOBAL="${git_config}"
	git config --global url."https://x-access-token:${SIGMO_PRO_MODULE_TOKEN}@github.com/".insteadOf "https://github.com/"
}

configure_ssh_auth() {
	local ssh_dir
	local ssh_key
	local git_config
	local ssh_cmd=()
	local git_ssh_command

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

configure_pro_auth() {
	export GOPRIVATE="${GOPRIVATE_PATTERN}"
	export GONOSUMDB="${GONOSUMDB:-${GOPRIVATE_PATTERN}}"

	if [ -n "${SIGMO_PRO_MODULE_TOKEN:-}" ]; then
		configure_token_auth
		return
	fi
	if [ "${SIGMO_SKIP_PRO_AUTH:-0}" = "1" ]; then
		return
	fi

	configure_ssh_auth
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

root_path() {
	local path="$1"

	case "${path}" in
		/*)
			printf '%s\n' "${path}"
			;;
		*)
			printf '%s\n' "${ROOT_DIR}/${path}"
			;;
	esac
}

prepare_arm64_musl_modfile() {
	local source_modfile
	local source_sumfile
	local purego_tmp
	local purego_dir

	if [ -n "${musl_modfile}" ]; then
		return
	fi

	source_modfile="$(root_path "${PRO_MODFILE}")"
	source_sumfile="$(root_path "${PRO_SUMFILE}")"
	musl_modfile="${OUTPUT_DIR}/go.linux-arm64-musl.mod"

	(cd "${PRO_DIR}" && go mod download)

	purego_tmp="$(mktemp -d)"
	add_cleanup_dir "${purego_tmp}"
	purego_dir="$(cd "${PRO_DIR}" && go list -m -f '{{.Dir}}' github.com/ebitengine/purego)"
	cp -R "${purego_dir}" "${purego_tmp}/purego"

	cp "${source_modfile}" "${musl_modfile}"
	copy_sumfile "${source_sumfile}" "${musl_modfile%.mod}.sum"
	go mod edit -modfile="${musl_modfile}" -replace=github.com/ebitengine/purego="${purego_tmp}/purego"

	TARGETARCH=arm64 PUREGO_MUSL_LIBC="${MUSL_ARM64_LIBC}" \
		"${ROOT_DIR}/scripts/patch-purego-musl.sh" "${musl_modfile}"
}

package_target() {
	local binary="$1"
	local archive="$2"
	local filename

	filename="$(basename "${binary}")"
	(
		cd "$(dirname "${binary}")"
		tar -czf "${archive}" "${filename}"
	)
}

build_target() {
	local chat_id="$1"
	local base_version="$2"
	local recipient_dir="$3"
	local name="$4"
	local goarch="$5"
	local musl="${6:-0}"
	local binary
	local archive
	local ldflags
	local go_args=()

	binary="${recipient_dir}/sigmo-pro-${name}"
	archive="${recipient_dir}/sigmo-pro-${name}.tar.gz"
	ldflags="-w -s -X main.BuildVersion=${base_version}-TGID-${chat_id}"

	if [ "${musl}" = "1" ]; then
		prepare_arm64_musl_modfile
		ldflags="-I ${MUSL_ARM64_INTERPRETER} ${ldflags}"
		go_args+=(-a)
		go_args+=(-modfile="${musl_modfile}")
	fi

	echo "Building ${binary}"
	go_args+=(
		-tags="${PRO_GO_TAGS}"
		-trimpath
		-ldflags="${ldflags}"
		-o "${binary}"
		.
	)

	(cd "${PRO_DIR}" && env GOOS=linux GOARCH="${goarch}" CGO_ENABLED=0 go build "${go_args[@]}")
	package_target "${binary}" "${archive}"
	printf '%s\t%s\t%s\n' "${chat_id}" "${name}" "${archive}" >> "${MANIFEST}"
}

build_recipient() {
	local chat_id="$1"
	local base_version="$2"
	local safe_chat_id
	local recipient_dir

	safe_chat_id="${chat_id//-/_}"
	recipient_dir="${OUTPUT_DIR}/TGID-${safe_chat_id}"
	mkdir -p "${recipient_dir}"

	build_target "${chat_id}" "${base_version}" "${recipient_dir}" "linux-amd64" "amd64"
	build_target "${chat_id}" "${base_version}" "${recipient_dir}" "linux-arm64" "arm64"
	build_target "${chat_id}" "${base_version}" "${recipient_dir}" "linux-arm64-musl" "arm64" "1"
}

main() {
	local recipients=()
	local chat_id
	local version

	if [ ! -f "$(root_path "${PRO_MODFILE}")" ]; then
		echo "Pro modfile not found: ${PRO_MODFILE}" >&2
		return 1
	fi

	mapfile -t recipients < <(load_recipients "$@")
	if [ "${#recipients[@]}" -eq 0 ]; then
		echo "no Telegram chat ids provided" >&2
		return 1
	fi

	cd "${ROOT_DIR}"
	mkdir -p "${OUTPUT_DIR}"
	export GOCACHE="${GOCACHE:-${OUTPUT_DIR}/.go-build-cache}"
	mkdir -p "${GOCACHE}"
	configure_pro_auth
	version="$(build_version)"

	build_frontend
	printf 'chat_id\ttarget\tarchive\n' > "${MANIFEST}"
	for chat_id in "${recipients[@]}"; do
		build_recipient "${chat_id}" "${version}"
	done

	echo "Wrote artifact manifest: ${MANIFEST}"
}

main "$@"
