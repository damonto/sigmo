#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUTPUT_DIR="${SIGMO_BUILD_DIR:-${ROOT_DIR}/build/pro}"
MANIFEST="${SIGMO_PRO_MANIFEST:-${OUTPUT_DIR}/artifacts.tsv}"
BOT_TOKEN="${SIGMO_PRO_TELEGRAM_BOT_TOKEN:-}"
API_BASE="${SIGMO_TELEGRAM_API_BASE:-https://api.telegram.org}"
MAX_BYTES="${SIGMO_TELEGRAM_MAX_BYTES:-52428800}"

file_size() {
	local path="$1"
	local size

	size="$(wc -c < "${path}")"
	size="${size//[!0-9]/}"
	printf '%s\n' "${size}"
}

commit_message() {
	if [ -n "${SIGMO_PRO_COMMIT_MESSAGE:-}" ]; then
		printf '%s\n' "${SIGMO_PRO_COMMIT_MESSAGE}"
		return
	fi

	git -C "${ROOT_DIR}" log -1 --pretty=%B 2>/dev/null || printf 'unknown'
}

caption() {
	local message

	message="$(commit_message)"
	jq -nr --arg message "${message}" '
		"Sigmo Pro\n\nCommit message:\n" + (($message | gsub("\\s+$"; ""))[0:900])
	'
}

check_archive() {
	local archive="$1"
	local size

	if [ ! -f "${archive}" ]; then
		echo "archive not found: ${archive}" >&2
		return 1
	fi

	size="$(file_size "${archive}")"
	if [ "${size}" -gt "${MAX_BYTES}" ]; then
		echo "archive exceeds Telegram document limit: ${archive} (${size} bytes > ${MAX_BYTES})" >&2
		return 1
	fi
}

append_media() {
	local media="$1"
	local attach_name="$2"
	local text="$3"

	if [ -n "${text}" ]; then
		jq -cn \
			--argjson media "${media}" \
			--arg attach "attach://${attach_name}" \
			--arg caption "${text}" \
			'$media + [{type: "document", media: $attach, caption: $caption}]'
		return
	fi

	jq -cn \
		--argjson media "${media}" \
		--arg attach "attach://${attach_name}" \
		'$media + [{type: "document", media: $attach}]'
}

send_media_group() {
	local chat_id="$1"
	shift
	local archives=("$@")
	local archive
	local attach_name
	local text
	local media="[]"
	local i
	local forms=()

	if [ "${#archives[@]}" -lt 2 ] || [ "${#archives[@]}" -gt 10 ]; then
		echo "Telegram media group requires 2-10 archives for ${chat_id}; got ${#archives[@]}" >&2
		return 1
	fi

	text="$(caption)"
	forms+=(--form "chat_id=${chat_id}")
	for i in "${!archives[@]}"; do
		archive="${archives[$i]}"
		check_archive "${archive}"

		attach_name="file${i}"
		if [ "${i}" -eq 0 ]; then
			media="$(append_media "${media}" "${attach_name}" "${text}")"
		else
			media="$(append_media "${media}" "${attach_name}" "")"
		fi
		forms+=(--form "${attach_name}=@${archive};filename=$(basename "${archive}")")
	done

	echo "Sending ${#archives[@]} Pro archives to ${chat_id} as one media group"
	curl --fail-with-body --show-error --silent \
		--request POST "${API_BASE}/bot${BOT_TOKEN}/sendMediaGroup" \
		--form "media=${media}" \
		"${forms[@]}"
	printf '\n'
}

flush_group() {
	local chat_id="$1"
	shift
	local archives=("$@")

	if [ -z "${chat_id}" ]; then
		return
	fi
	send_media_group "${chat_id}" "${archives[@]}"
}

main() {
	local chat_id
	local target
	local archive
	local current_chat_id=""
	local archives=()

	if [ -z "${BOT_TOKEN}" ]; then
		echo "SIGMO_PRO_TELEGRAM_BOT_TOKEN is required" >&2
		return 1
	fi
	if [ ! -f "${MANIFEST}" ]; then
		echo "artifact manifest not found: ${MANIFEST}" >&2
		return 1
	fi
	if ! command -v jq >/dev/null 2>&1; then
		echo "jq is required to build Telegram media group JSON" >&2
		return 1
	fi

	while IFS=$'\t' read -r chat_id target archive; do
		if [ "${chat_id}" = "chat_id" ]; then
			continue
		fi
		if [ -z "${chat_id}${target}${archive}" ]; then
			continue
		fi

		if [ -n "${current_chat_id}" ] && [ "${chat_id}" != "${current_chat_id}" ]; then
			flush_group "${current_chat_id}" "${archives[@]}"
			archives=()
		fi

		current_chat_id="${chat_id}"
		archives+=("${archive}")
	done < "${MANIFEST}"

	flush_group "${current_chat_id}" "${archives[@]}"
}

main "$@"
