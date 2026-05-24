#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

source "${ROOT_DIR}/scripts/private-features.env"

PUBLIC_MODFILE="${PUBLIC_MODFILE:-go.mod}"
PUBLIC_SUMFILE="${PUBLIC_SUMFILE:-go.sum}"
PRIVATE_MODFILE="${PRIVATE_MODFILE:-${PRIVATE_GO_MODFILE}}"
PRIVATE_SUMFILE="${PRIVATE_SUMFILE:-go.private.sum}"
PRIVATE_GOPRIVATE="${PRIVATE_GOPRIVATE:-github.com/damonto/*}"

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

trim_space() {
	local value="$1"
	value="${value#"${value%%[![:space:]]*}"}"
	value="${value%"${value##*[![:space:]]}"}"
	printf '%s' "${value}"
}

read_private_modules() {
	local modules=()
	local module

	for module in ${PRIVATE_GO_MODULES:-}; do
		module="$(trim_space "${module}")"
		if [ -z "${module}" ]; then
			continue
		fi
		modules+=("${module}")
	done

	if [ "${#modules[@]}" -eq 0 ]; then
		echo "PRIVATE_GO_MODULES must contain at least one module." >&2
		return 1
	fi

	printf '%s\n' "${modules[@]}"
}

configure_private_module_auth() {
	local module="$1"

	if [ -z "${PRIVATE_MODULE_TOKEN:-}" ]; then
		return
	fi

	case "${module}" in
		github.com/*/*)
			git config --global --add \
				url."https://x-access-token:${PRIVATE_MODULE_TOKEN}@${module}".insteadOf \
				"https://${module}"
			;;
		*)
			echo "skip token rewrite for unsupported private module host: ${module}" >&2
			;;
	esac
}

main() {
	if [ ! -f "${PRIVATE_MODFILE}" ]; then
		echo "${PRIVATE_MODFILE} does not exist." >&2
		return 1
	fi

	local modules=()
	local pinned_modules=()
	local module
	local version
	local tmp_mod
	local tmp_sum
	local tmp_git_config

	mapfile -t modules < <(read_private_modules)

	export GOPRIVATE="${PRIVATE_GOPRIVATE}"

	if [ -n "${PRIVATE_MODULE_TOKEN:-}" ]; then
		if [ -z "${GIT_CONFIG_GLOBAL:-}" ]; then
			tmp_git_config="$(mktemp)"
			add_cleanup_file "${tmp_git_config}"
			export GIT_CONFIG_GLOBAL="${tmp_git_config}"
		fi
	fi

	for module in "${modules[@]}"; do
		configure_private_module_auth "${module}"

		version="$(go list -modfile="${PRIVATE_MODFILE}" -m -f '{{ .Version }}' "${module}")"
		if [ -z "${version}" ]; then
			echo "${module} is missing from ${PRIVATE_MODFILE}." >&2
			return 1
		fi

		pinned_modules+=("${module}@${version}")
	done

	tmp_mod="$(mktemp "go.private.tmp.XXXXXX.mod")"
	tmp_sum="${tmp_mod%.mod}.sum"
	add_cleanup_file "${tmp_mod}"
	add_cleanup_file "${tmp_sum}"

	cp "${PUBLIC_MODFILE}" "${tmp_mod}"
	if [ -f "${PUBLIC_SUMFILE}" ]; then
		cp "${PUBLIC_SUMFILE}" "${tmp_sum}"
	else
		: > "${tmp_sum}"
	fi

	go get -modfile="${tmp_mod}" "${pinned_modules[@]}"
	if [ -n "${PRIVATE_GO_TAGS}" ]; then
		GOFLAGS="${GOFLAGS:-} -tags=${PRIVATE_GO_TAGS}" go mod tidy -modfile="${tmp_mod}"
	else
		go mod tidy -modfile="${tmp_mod}"
	fi

	mv "${tmp_mod}" "${PRIVATE_MODFILE}"
	if [ -f "${tmp_sum}" ]; then
		mv "${tmp_sum}" "${PRIVATE_SUMFILE}"
	else
		: > "${PRIVATE_SUMFILE}"
	fi
	cleanup
	trap - EXIT
}

main "$@"
