#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

source "${ROOT_DIR}/scripts/private-features.env"

PUBLIC_MODFILE="${PUBLIC_MODFILE:-go.mod}"
PUBLIC_SUMFILE="${PUBLIC_SUMFILE:-go.sum}"
PRIVATE_MODFILE="${PRIVATE_MODFILE:-${PRIVATE_GO_MODFILE}}"
PRIVATE_SUMFILE="${PRIVATE_SUMFILE:-go.private.sum}"
PRIVATE_GOPRIVATE="${PRIVATE_GOPRIVATE:-github.com/damonto/*}"
SSH_DIR="${SIGMO_SSH_DIR:-${HOME}/.ssh}"
SSH_KEY="${SIGMO_SSH_KEY:-${SSH_DIR}/id_ed25519}"

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
	local path
	local owner

	case "${module}" in
		github.com/*/*)
			path="${module#github.com/}"
			owner="${path%%/*}"
			if [ -n "${PRIVATE_MODULE_TOKEN:-}" ]; then
				git config --global \
					url."https://x-access-token:${PRIVATE_MODULE_TOKEN}@github.com/${owner}/".insteadOf \
					"https://github.com/${owner}/"
				return
			fi

			git config --global \
				url."git@github.com:${owner}/".insteadOf \
				"https://github.com/${owner}/"
			;;
		*)
			echo "skip Git auth rewrite for unsupported private module host: ${module}" >&2
			;;
	esac
}

private_module_version() {
	local module="$1"

	awk -v module="${module}" '$1 == module { print $2; exit }' "${PRIVATE_MODFILE}"
}

github_repo_url() {
	local module="$1"
	local path
	local owner
	local rest
	local repo

	case "${module}" in
		github.com/*/*)
			path="${module#github.com/}"
			owner="${path%%/*}"
			rest="${path#*/}"
			repo="${rest%%/*}"
			printf 'https://github.com/%s/%s.git' "${owner}" "${repo}"
			;;
		*)
			echo "unsupported private module host: ${module}" >&2
			return 1
			;;
	esac
}

resolve_head_version() {
	local module="$1"
	local repo_url
	local commit

	repo_url="$(github_repo_url "${module}")"
	commit="$(git ls-remote "${repo_url}" HEAD | awk '{print $1}')"
	if [ -z "${commit}" ]; then
		echo "could not resolve HEAD for ${module}" >&2
		return 1
	fi

	go list -modfile="${PRIVATE_MODFILE}" -m -f '{{ .Version }}' "${module}@${commit}"
}

resolve_private_module_version() {
	local module="$1"
	local version

	version="$(private_module_version "${module}")"
	if [ -z "${version}" ]; then
		echo "${module} is missing from ${PRIVATE_MODFILE}." >&2
		return 1
	fi
	if [ "${version}" = "v0.0.0" ]; then
		resolve_head_version "${module}"
		return
	fi

	printf '%s\n' "${version}"
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
	local ssh_cmd=()
	local git_ssh_command

	mapfile -t modules < <(read_private_modules)

	if [ -z "${PRIVATE_MODULE_TOKEN:-}" ]; then
		ssh_cmd=(ssh)
		if [ -f "${SSH_KEY}" ]; then
			ssh_cmd+=(-i "${SSH_KEY}" -o IdentitiesOnly=yes)
		fi
		if [ -f "${SSH_DIR}/config" ]; then
			ssh_cmd+=(-F "${SSH_DIR}/config")
		fi
		if [ -f "${SSH_DIR}/known_hosts" ]; then
			ssh_cmd+=(-o "UserKnownHostsFile=${SSH_DIR}/known_hosts")
		fi
		printf -v git_ssh_command '%q ' "${ssh_cmd[@]}"
		export GIT_SSH_COMMAND="${git_ssh_command}"
	else
		unset GIT_SSH_COMMAND
	fi

	tmp_git_config="$(mktemp)"
	add_cleanup_file "${tmp_git_config}"
	export GIT_CONFIG_GLOBAL="${tmp_git_config}"
	export GOPRIVATE="${PRIVATE_GOPRIVATE}"
	export GONOSUMDB="${GONOSUMDB:-${PRIVATE_GOPRIVATE}}"

	for module in "${modules[@]}"; do
		configure_private_module_auth "${module}"

		version="$(resolve_private_module_version "${module}")"
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
