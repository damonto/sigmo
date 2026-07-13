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
PRO_TARGETS="${SIGMO_PRO_TARGETS:-linux-amd64 linux-amd64-musl linux-arm64 linux-arm64-musl linux-arm linux-arm-musl}"
TGID_DIGITS=16
TGID_PLACEHOLDER="XXXXXXXXXXXXXXXXX"

case "${OUTPUT_DIR}" in
	/*)
		OUTPUT_DIR_ABS="${OUTPUT_DIR}"
		;;
	*)
		OUTPUT_DIR_ABS="${ROOT_DIR}/${OUTPUT_DIR}"
		;;
esac
case "${MANIFEST}" in
	/*)
		MANIFEST_ABS="${MANIFEST}"
		;;
	*)
		MANIFEST_ABS="${ROOT_DIR}/${MANIFEST}"
		;;
esac

cleanup_files=()
cleanup_dirs=()
musl_modfile=""
musl_modfile_key=""
built_base_binary=""

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

musl_interpreter() {
	local goarch="$1"

	case "${goarch}" in
		amd64)
			printf '%s\n' "/lib/ld-musl-x86_64.so.1"
			;;
		arm)
			printf '%s\n' "/lib/ld-musl-armhf.so.1"
			;;
		arm64)
			printf '%s\n' "/lib/ld-musl-aarch64.so.1"
			;;
		*)
			echo "unsupported musl interpreter for GOARCH: ${goarch}" >&2
			return 1
			;;
	esac
}

target_arch() {
	local name="$1"

	if [ -n "${SIGMO_PRO_TARGET_ARCH:-}" ]; then
		printf '%s\n' "${SIGMO_PRO_TARGET_ARCH}"
		return
	fi

	case "${name}" in
		linux-amd64 | linux-amd64-musl)
			printf '%s\n' "amd64"
			;;
		linux-arm64 | linux-arm64-musl)
			printf '%s\n' "arm64"
			;;
		linux-arm | linux-arm-musl)
			printf '%s\n' "arm"
			;;
		*)
			echo "unknown Pro target: ${name}" >&2
			return 1
			;;
	esac
}

target_musl() {
	local name="$1"

	case "${SIGMO_PRO_TARGET_MUSL:-}" in
		1 | true | TRUE | yes | YES)
			printf '%s\n' "1"
			return
			;;
		0 | false | FALSE | no | NO)
			printf '%s\n' "0"
			return
			;;
		"")
			;;
		*)
			echo "invalid SIGMO_PRO_TARGET_MUSL: ${SIGMO_PRO_TARGET_MUSL}" >&2
			return 1
			;;
	esac

	case "${name}" in
		*-musl)
			printf '%s\n' "1"
			;;
		*)
			printf '%s\n' "0"
			;;
	esac
}

prepare_musl_modfile() {
	local name="$1"
	local goarch="$2"
	local key
	local source_modfile
	local source_sumfile
	local purego_tmp
	local purego_dir

	key="${name}-${goarch}"
	if [ "${musl_modfile_key}" = "${key}" ]; then
		return
	fi

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

count_pattern() {
	local file="$1"
	local pattern="$2"

	NEEDLE="${pattern}" perl -0777 -ne '
		BEGIN {
			$needle = $ENV{"NEEDLE"};
			$count = 0;
		}
		my $pos = -1;
		while (($pos = index($_, $needle, $pos + 1)) >= 0) {
			$count++;
		}
		END {
			print $count, "\n";
		}
	' "${file}"
}

tgid_watermark() {
	local chat_id="$1"
	local digits
	local padded

	digits="${chat_id}"
	if [[ "${digits}" == -* ]]; then
		echo "Telegram chat id must be non-negative to watermark: ${chat_id}" >&2
		return 1
	fi
	if [ "${#digits}" -gt "${TGID_DIGITS}" ]; then
		echo "Telegram chat id is too long to watermark: ${chat_id}" >&2
		return 1
	fi

	printf -v padded "%${TGID_DIGITS}s" "${digits}"
	padded="${padded// /0}"
	printf 'T%s\n' "${padded}"
}

patch_tgid() {
	local source="$1"
	local target="$2"
	local chat_id="$3"
	local before_count
	local placeholder_count
	local replacement
	local replacement_count

	replacement="$(tgid_watermark "${chat_id}")"
	if [ "${#replacement}" -ne "${#TGID_PLACEHOLDER}" ]; then
		echo "watermark length mismatch: ${replacement}" >&2
		return 1
	fi

	before_count="$(count_pattern "${source}" "${TGID_PLACEHOLDER}")"
	if [ "${before_count}" -ne 1 ]; then
		echo "expected one TGID placeholder in ${source}; found ${before_count}" >&2
		return 1
	fi

	cp "${source}" "${target}"
	NEEDLE="${TGID_PLACEHOLDER}" REPLACEMENT="${replacement}" perl -0777 -pi -e '
		BEGIN {
			$needle = $ENV{"NEEDLE"};
			$replacement = $ENV{"REPLACEMENT"};
			die "replacement length mismatch\n" unless length($needle) == length($replacement);
			$count = 0;
		}
		$count += s/\Q$needle\E/$replacement/g;
		END {
			die "patched $count TGID placeholders\n" unless $count == 1;
		}
	' "${target}"

	placeholder_count="$(count_pattern "${target}" "${TGID_PLACEHOLDER}")"
	if [ "${placeholder_count}" -ne 0 ]; then
		echo "TGID placeholder still present in ${target}" >&2
		return 1
	fi

	replacement_count="$(count_pattern "${target}" "${replacement}")"
	if [ "${replacement_count}" -ne 1 ]; then
		echo "expected one TGID watermark in ${target}; found ${replacement_count}" >&2
		return 1
	fi
}

recipient_dir_for_chat() {
	local chat_id="$1"
	local safe_chat_id

	safe_chat_id="${chat_id//-/_}"
	printf '%s/%s\n' "${OUTPUT_DIR}" "${safe_chat_id}"
}

build_target_base() {
	local name="$1"
	local base_version="$2"
	local goarch="$3"
	local musl="${4:-0}"
	local binary
	local build_binary
	local ldflags
	local interpreter
	local go_args=()

	binary="${OUTPUT_DIR}/targets/sigmo-pro-${name}"
	build_binary="$(root_path "${binary}")"
	mkdir -p "$(dirname "${build_binary}")"
	ldflags="-w -s -X main.BuildVersion=${base_version}-${TGID_PLACEHOLDER}"

	if [ "${musl}" = "1" ]; then
		interpreter="$(musl_interpreter "${goarch}")"
		prepare_musl_modfile "${name}" "${goarch}"
		ldflags="-I ${interpreter} ${ldflags}"
		go_args+=(-a)
		go_args+=(-modfile="${musl_modfile}")
	fi

	echo "Building base ${binary}"
	go_args+=(
		-tags="${PRO_GO_TAGS}"
		-trimpath
		-ldflags="${ldflags}"
		-o "${build_binary}"
		.
	)

	(cd "${PRO_DIR}" && env GOOS=linux GOARCH="${goarch}" CGO_ENABLED=0 go build "${go_args[@]}")
	built_base_binary="${build_binary}"
}

build_named_target_base() {
	local name="$1"
	local base_version="$2"
	local goarch
	local musl

	goarch="$(target_arch "${name}")"
	musl="$(target_musl "${name}")"
	build_target_base "${name}" "${base_version}" "${goarch}" "${musl}"
}

package_recipient_target() {
	local chat_id="$1"
	local base_binary="$2"
	local name="$3"
	local archive
	local binary
	local build_archive
	local build_binary
	local recipient_dir

	recipient_dir="$(recipient_dir_for_chat "${chat_id}")"
	binary="${recipient_dir}/sigmo-pro-${name}"
	archive="${recipient_dir}/sigmo-pro-${name}.tar.gz"
	build_binary="$(root_path "${binary}")"
	build_archive="$(root_path "${archive}")"
	mkdir -p "$(dirname "${build_binary}")"

	echo "Patching ${binary} for TGID ${chat_id}"
	patch_tgid "${base_binary}" "${build_binary}" "${chat_id}"
	package_target "${build_binary}" "${build_archive}"
}

main() {
	local recipients=()
	local archive
	local chat_id
	local i
	local package_chats=()
	local package_failed
	local package_pids=()
	local recipient_dir
	local target
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
	mkdir -p "${OUTPUT_DIR_ABS}"
	mkdir -p "$(dirname "${MANIFEST_ABS}")"
	if [ -n "${GOCACHE:-}" ]; then
		export GOCACHE="$(root_path "${GOCACHE}")"
	else
		export GOCACHE="${OUTPUT_DIR_ABS}/.go-build-cache"
	fi
	mkdir -p "${GOCACHE}"
	configure_pro_auth
	version="$(build_version)"

	build_frontend
	printf 'chat_id\ttarget\tarchive\n' > "${MANIFEST_ABS}"
	for target in ${PRO_TARGETS}; do
		build_named_target_base "${target}" "${version}"
		package_chats=()
		package_pids=()
		for chat_id in "${recipients[@]}"; do
			package_recipient_target "${chat_id}" "${built_base_binary}" "${target}" &
			package_pids+=("$!")
			package_chats+=("${chat_id}")
		done

		package_failed=0
		for i in "${!package_pids[@]}"; do
			if wait "${package_pids[$i]}"; then
				continue
			fi
			echo "package ${target} for TGID ${package_chats[$i]}" >&2
			package_failed=1
		done
		if [ "${package_failed}" -ne 0 ]; then
			return 1
		fi

	done

	for chat_id in "${recipients[@]}"; do
		for target in ${PRO_TARGETS}; do
			recipient_dir="$(recipient_dir_for_chat "${chat_id}")"
			archive="${recipient_dir}/sigmo-pro-${target}.tar.gz"
			printf '%s\t%s\t%s\n' "${chat_id}" "${target}" "${archive}" >> "${MANIFEST_ABS}"
		done
	done

	echo "Wrote artifact manifest: ${MANIFEST}"
}

main "$@"
