#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WEB_DIR="${SIGMO_WEB_DIR:-${ROOT_DIR}/web}"
UI_DIR="${SIGMO_SHADCN_UI_DIR:-${WEB_DIR}/src/components/ui}"

if ! command -v bun >/dev/null 2>&1; then
	echo "bun is required to update shadcn-vue components." >&2
	exit 1
fi

if [ ! -f "${WEB_DIR}/components.json" ]; then
	echo "components.json not found in ${WEB_DIR}." >&2
	exit 1
fi

export BUN_TMPDIR="${BUN_TMPDIR:-/tmp}"
export BUN_INSTALL="${BUN_INSTALL:-/tmp/bun-install}"

components=("$@")
if [ "${#components[@]}" -eq 0 ]; then
	if [ ! -d "${UI_DIR}" ]; then
		echo "UI component directory not found: ${UI_DIR}" >&2
		exit 1
	fi

	mapfile -t components < <(find "${UI_DIR}" -mindepth 1 -maxdepth 1 -type d -printf '%f\n' | sort)
fi

if [ "${#components[@]}" -eq 0 ]; then
	echo "No shadcn-vue components to update." >&2
	exit 1
fi

printf 'Updating shadcn-vue components in %s:\n' "${WEB_DIR}"
printf '  %s\n' "${components[@]}"

exec bunx --bun shadcn-vue@latest add --overwrite --yes --cwd "${WEB_DIR}" "${components[@]}"
