#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUTPUT_DIR="${SIGMO_BUILD_DIR:-${ROOT_DIR}/build/pro}"
MANIFEST="${SIGMO_PRO_MANIFEST:-${OUTPUT_DIR}/artifacts.tsv}"
BOT_TOKEN="${SIGMO_PRO_TELEGRAM_BOT_TOKEN:-}"
API_BASE="${SIGMO_TELEGRAM_API_BASE:-https://api.telegram.org}"
MAX_BYTES="${SIGMO_TELEGRAM_MAX_BYTES:-52428800}"
send_attempts=0
send_failures=0
send_chats=()
send_pids=()

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
	local media="[]"
	local i
	local last_index
	local text
	local forms=()

	if [ "${#archives[@]}" -lt 2 ] || [ "${#archives[@]}" -gt 10 ]; then
		echo "Telegram media group requires 2-10 archives for ${chat_id}; got ${#archives[@]}" >&2
		return 1
	fi
	if ! text="$(caption)"; then
		echo "build Telegram caption for ${chat_id}" >&2
		return 1
	fi

	last_index=$((${#archives[@]} - 1))
	forms+=(--form "chat_id=${chat_id}")
	for i in "${!archives[@]}"; do
		archive="${archives[$i]}"
		if ! check_archive "${archive}"; then
			return 1
		fi

		attach_name="file${i}"
		if [ "${i}" -eq "${last_index}" ]; then
			if ! media="$(append_media "${media}" "${attach_name}" "${text}")"; then
				echo "build Telegram media JSON for ${chat_id}" >&2
				return 1
			fi
		elif ! media="$(append_media "${media}" "${attach_name}" "")"; then
			echo "build Telegram media JSON for ${chat_id}" >&2
			return 1
		fi
		forms+=(--form "${attach_name}=@${archive};filename=$(basename "${archive}")")
	done

	echo "Sending ${#archives[@]} Pro archives to ${chat_id} as one media group"
	if ! curl --fail-with-body --show-error --silent \
		--request POST "${API_BASE}/bot${BOT_TOKEN}/sendMediaGroup" \
		--form "media=${media}" \
		"${forms[@]}"; then
		echo "send Telegram media group to ${chat_id}" >&2
		return 1
	fi
	printf '\n'
}

queue_group() {
	local chat_id="$1"
	shift
	local archives=("$@")

	if [ -z "${chat_id}" ]; then
		return
	fi

	send_attempts=$((send_attempts + 1))
	send_media_group "${chat_id}" "${archives[@]}" &
	send_pids+=("$!")
	send_chats+=("${chat_id}")
}

main() {
	local chat_id
	local target
	local archive
	local current_chat_id=""
	local i
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
			queue_group "${current_chat_id}" "${archives[@]}"
			archives=()
		fi

		current_chat_id="${chat_id}"
		archives+=("${archive}")
	done < "${MANIFEST}"

	queue_group "${current_chat_id}" "${archives[@]}"
	for i in "${!send_pids[@]}"; do
		if wait "${send_pids[$i]}"; then
			continue
		fi
		send_failures=$((send_failures + 1))
		echo "Continuing after Telegram send failure for ${send_chats[$i]}." >&2
	done
	if [ "${send_attempts}" -gt 0 ] && [ "${send_failures}" -eq "${send_attempts}" ]; then
		echo "sending Pro archives failed for all ${send_attempts} Telegram chat ids" >&2
		return 1
	fi
	if [ "${send_failures}" -gt 0 ]; then
		echo "sending Pro archives failed for ${send_failures}/${send_attempts} Telegram chat ids" >&2
	fi
}

main "$@"
