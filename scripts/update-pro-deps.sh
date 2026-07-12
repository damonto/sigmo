#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

source "${ROOT_DIR}/scripts/pro-features.env"

PRO_MODFILE="${PRO_MODFILE:-${PRO_GO_MODFILE}}"
PRO_GOPRIVATE="${PRO_GOPRIVATE:-github.com/damonto/*}"
SSH_DIR="${SIGMO_SSH_DIR:-${HOME}/.ssh}"
SSH_KEY="${SIGMO_SSH_KEY:-${SSH_DIR}/id_ed25519}"
MODULE_TOKEN="${PRO_MODULE_TOKEN:-}"
MODULE_VERSION_MODE="${PRO_MODULE_VERSION_MODE:-pseudo}"

case "${PRO_MODFILE}" in
	/*)
		PRO_MODFILE_PATH="${PRO_MODFILE}"
		;;
	*)
		PRO_MODFILE_PATH="${ROOT_DIR}/${PRO_MODFILE}"
		;;
esac
PRO_DIR="${PRO_DIR:-$(dirname "${PRO_MODFILE_PATH}")}"

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

read_pro_modules() {
	local module

	for module in ${PRO_GO_MODULES:-}; do
		module="$(trim_space "${module}")"
		if [ -n "${module}" ]; then
			printf '%s\n' "${module}"
		fi
	done
}

usage() {
	cat <<EOF
Usage: scripts/update-pro-deps.sh [--module-version=pseudo|tags]

Options:
  --module-version=pseudo  Pin Pro modules to the remote HEAD pseudo-version. This is the default.
  --module-version=tags    Pin Pro modules to the latest tagged release version.
  --pseudo                 Alias for --module-version=pseudo.
  --tags                   Alias for --module-version=tags.
EOF
}

parse_args() {
	local arg

	while [ "$#" -gt 0 ]; do
		arg="$1"
		case "${arg}" in
			--module-version=pseudo | --module-version=head | --pseudo | --head)
				MODULE_VERSION_MODE="pseudo"
				;;
			--module-version=tags | --module-version=tag | --module-version=release | --tags | --tagged | --release)
				MODULE_VERSION_MODE="tags"
				;;
			-h | --help)
				usage
				exit 0
				;;
			*)
				echo "unknown argument: ${arg}" >&2
				usage >&2
				return 1
				;;
		esac
		shift
	done
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
			echo "unsupported Pro module host: ${module}" >&2
			return 1
			;;
	esac
}

configure_go_auth() {
	local git_config
	local ssh_cmd=()
	local git_ssh_command

	git_config="$(mktemp)"
	add_cleanup_file "${git_config}"

	export GIT_CONFIG_GLOBAL="${git_config}"
	export GOPRIVATE="${GOPRIVATE:-${PRO_GOPRIVATE}}"
	export GONOSUMDB="${GONOSUMDB:-${GOPRIVATE}}"

	if [ -n "${MODULE_TOKEN}" ]; then
		unset GIT_SSH_COMMAND
		git config --global \
			url."https://x-access-token:${MODULE_TOKEN}@github.com/damonto/".insteadOf \
			"https://github.com/damonto/"
		return
	fi

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
	git config --global url."git@github.com:damonto/".insteadOf "https://github.com/damonto/"
}

is_pseudo_version() {
	local version="$1"

	[[ "${version}" =~ [-.]([0-9]{14})-[0-9a-f]{12}$ ]]
}

resolve_pseudo_version() {
	local module="$1"
	local repo_url
	local commit

	repo_url="$(github_repo_url "${module}")"
	commit="$(git ls-remote "${repo_url}" HEAD | awk '{print $1}')"
	if [ -z "${commit}" ]; then
		echo "could not resolve HEAD for ${module}" >&2
		return 1
	fi

	# The public dependency update may change the module graph used by the Pro
	# module through its local sigmo replace. Allow this lookup to record any
	# newly required checksums before the pinned go get runs.
	(cd "${PRO_DIR}" && go list -mod=mod -m -f '{{ .Version }}' "${module}@${commit}")
}

resolve_tag_version() {
	local module="$1"
	local version

	version="$(cd "${PRO_DIR}" && go list -m -f '{{ .Version }}' "${module}@latest")"
	if is_pseudo_version "${version}"; then
		echo "${module}@latest resolved to ${version}; no tagged release version is available." >&2
		return 1
	fi

	printf '%s\n' "${version}"
}

resolve_pro_module_version() {
	local module="$1"

	case "${MODULE_VERSION_MODE}" in
		pseudo)
			resolve_pseudo_version "${module}"
			;;
		tags)
			resolve_tag_version "${module}"
			;;
		*)
			echo "unsupported module version mode: ${MODULE_VERSION_MODE}" >&2
			return 1
			;;
	esac
}

go_with_pro_tags() {
	local goflags="${GOFLAGS:-}"

	if [ -n "${PRO_GO_TAGS:-}" ]; then
		goflags="${goflags} -tags=${PRO_GO_TAGS}"
	fi

	if [ -n "${goflags//[[:space:]]/}" ]; then
		GOFLAGS="${goflags}" go "$@"
		return
	fi

	go "$@"
}

update_public_deps() {
	echo "Updating public module dependencies"
	(cd "${ROOT_DIR}" && go get -u ./... && go mod tidy)
}

update_pro_deps() {
	local modules=()
	local pinned_modules=()
	local module
	local module_version
	local version

	mapfile -t modules < <(read_pro_modules)

	echo "Updating Pro module dependencies with ${MODULE_VERSION_MODE} module versions"

	for module in "${modules[@]}"; do
		version="$(resolve_pro_module_version "${module}")"
		echo "${module} ${version}"
		pinned_modules+=("${module}@${version}")
	done

	if [ "${#pinned_modules[@]}" -gt 0 ]; then
		# go get loads the current module graph before applying requested upgrades.
		# Rewrite stale requirements first so pre-rename versions cannot block it.
		for module_version in "${pinned_modules[@]}"; do
			(cd "${PRO_DIR}" && go mod edit -require="${module_version}")
		done
	fi

	(cd "${PRO_DIR}" && go_with_pro_tags get -u ./...)

	if [ "${#pinned_modules[@]}" -gt 0 ]; then
		(cd "${PRO_DIR}" && go get "${pinned_modules[@]}")
	fi

	(cd "${PRO_DIR}" && go_with_pro_tags mod tidy)
}

main() {
	parse_args "$@"

	if [ ! -f "${PRO_DIR}/go.mod" ]; then
		echo "Pro go.mod not found: ${PRO_DIR}/go.mod" >&2
		return 1
	fi

	configure_go_auth
	update_public_deps
	update_pro_deps

	cleanup
	trap - EXIT
}

main "$@"
