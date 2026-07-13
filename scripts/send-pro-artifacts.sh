#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUTPUT_DIR="${SIGMO_BUILD_DIR:-${ROOT_DIR}/build/pro}"
MANIFEST="${SIGMO_PRO_MANIFEST:-${OUTPUT_DIR}/artifacts.tsv}"
BOT_TOKEN="${SIGMO_PRO_TELEGRAM_BOT_TOKEN:-}"
API_BASE="${SIGMO_PRO_TELEGRAM_API:-https://api.telegram.org}"
MAX_BYTES="${SIGMO_TELEGRAM_MAX_BYTES:-52428800}"
MAX_RETRIES="${SIGMO_TELEGRAM_MAX_RETRIES:-5}"
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

send_document() {
	local chat_id="$1"
	local archive="$2"
	local text="$3"
	local attempt=0
	local http_code
	local response
	local retry_after
	local forms=(
		--form-string "chat_id=${chat_id}"
		--form "document=@${archive};filename=$(basename "${archive}")"
	)

	if [ -n "${text}" ]; then
		forms+=(--form-string "caption=${text}")
	fi

	while [ "${attempt}" -le "${MAX_RETRIES}" ]; do
		response="$(mktemp)"
		if ! http_code="$(curl --show-error --silent \
			--output "${response}" \
			--write-out '%{http_code}' \
			--request POST "${API_BASE}/bot${BOT_TOKEN}/sendDocument" \
			"${forms[@]}")"; then
			rm -f "${response}"
			echo "send Telegram document to ${chat_id}: request transport failed" >&2
			return 1
		fi

		if [[ "${http_code}" == 2* ]] && jq -e '.ok == true' "${response}" >/dev/null; then
			rm -f "${response}"
			return
		fi

		retry_after="$(jq -r '
			.parameters.retry_after //
			(.description // "" | try capture("retry after (?<seconds>[0-9]+)"; "i").seconds) //
			empty
		' "${response}")"
		if [[ "${retry_after}" =~ ^[0-9]+$ ]] && [ "${attempt}" -lt "${MAX_RETRIES}" ]; then
			rm -f "${response}"
			attempt=$((attempt + 1))
			echo "Telegram rate limited ${chat_id}; retrying in ${retry_after}s (${attempt}/${MAX_RETRIES})" >&2
			sleep "${retry_after}"
			continue
		fi

		echo "send Telegram document to ${chat_id}: HTTP ${http_code}" >&2
		jq -c . "${response}" >&2 || true
		rm -f "${response}"
		return 1
	done
}

send_documents() {
	local chat_id="$1"
	shift
	local archives=("$@")
	local archive
	local i
	local last_index
	local text

	if ! text="$(caption)"; then
		echo "build Telegram caption for ${chat_id}" >&2
		return 1
	fi

	last_index=$((${#archives[@]} - 1))
	for i in "${!archives[@]}"; do
		archive="${archives[$i]}"
		if ! check_archive "${archive}"; then
			return 1
		fi

		echo "Sending $(basename "${archive}") to ${chat_id}"
		if [ "${i}" -eq "${last_index}" ]; then
			send_document "${chat_id}" "${archive}" "${text}" || return 1
		else
			send_document "${chat_id}" "${archive}" "" || return 1
		fi
	done
}

queue_documents() {
	local chat_id="$1"
	shift
	local archives=("$@")

	if [ -z "${chat_id}" ]; then
		return
	fi

	send_attempts=$((send_attempts + 1))
	send_documents "${chat_id}" "${archives[@]}" &
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
		echo "jq is required to parse Telegram API responses" >&2
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
			queue_documents "${current_chat_id}" "${archives[@]}"
			archives=()
		fi

		current_chat_id="${chat_id}"
		archives+=("${archive}")
	done < "${MANIFEST}"

	queue_documents "${current_chat_id}" "${archives[@]}"
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
