#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

source "${ROOT_DIR}/scripts/private-features.env"

PRIVATE_MODFILE="${PRIVATE_MODFILE:-${PRIVATE_GO_MODFILE}}"
PRIVATE_GOPRIVATE="${PRIVATE_GOPRIVATE:-github.com/damonto/*}"
SSH_DIR="${SIGMO_SSH_DIR:-/home/user/.ssh}"
SSH_KEY="${SIGMO_SSH_KEY:-${SSH_DIR}/id_ed25519}"

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
			printf 'git@github.com:%s/%s.git' "${owner}" "${repo}"
			;;
		*)
			echo "unsupported private module host: ${module}" >&2
			return 1
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
	local repo_url
	local commit
	local version
	local git_config
	local goflags
	local ssh_cmd=()
	local git_ssh_command

	mapfile -t modules < <(read_private_modules)

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
	export GOPRIVATE="${PRIVATE_GOPRIVATE}"
	export GONOSUMDB="${GONOSUMDB:-${PRIVATE_GOPRIVATE}}"

	git config --global url."git@github.com:damonto/".insteadOf "https://github.com/damonto/"

	cd "${ROOT_DIR}"

	for module in "${modules[@]}"; do
		repo_url="$(github_repo_url "${module}")"
		commit="$(git ls-remote "${repo_url}" HEAD | awk '{print $1}')"
		if [ -z "${commit}" ]; then
			echo "could not resolve HEAD for ${module}" >&2
			return 1
		fi

		version="$(go list -modfile="${PRIVATE_MODFILE}" -m -f '{{ .Version }}' "${module}@${commit}")"
		if [ -z "${version}" ]; then
			echo "could not resolve Go version for ${module}@${commit}" >&2
			return 1
		fi

		printf '%s %s\n' "${module}" "${version}"
		pinned_modules+=("${module}@${version}")
	done

	go get -modfile="${PRIVATE_MODFILE}" "${pinned_modules[@]}"

	goflags="${GOFLAGS:-}"
	if [ -n "${PRIVATE_GO_TAGS}" ]; then
		goflags="${goflags} -tags=${PRIVATE_GO_TAGS}"
	fi

	if [ -n "${goflags//[[:space:]]/}" ]; then
		GOFLAGS="${goflags}" go mod tidy -modfile="${PRIVATE_MODFILE}"
	else
		go mod tidy -modfile="${PRIVATE_MODFILE}"
	fi

	cleanup
	trap - EXIT
}

main "$@"
