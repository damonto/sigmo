#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
if [ "$#" -lt 1 ] || [ "$#" -gt 2 ]; then
	echo "Usage: scripts/publish-configs.sh INPUT_DIRECTORY [--publish]" >&2
	exit 1
fi
if [ "$#" = 2 ] && [ "$2" != "--publish" ]; then
	echo "The only optional argument is --publish" >&2
	exit 1
fi

INPUT_DIR="$(cd "$1" && pwd)"
OUTPUT_DIR="${SIGMO_CONFIG_OUTPUT:-${ROOT_DIR}/build/config-release}"
KEY_FILE="${SIGMO_CONFIG_KEY_FILE:-${ROOT_DIR}/scripts/sigmo-keys/config-encryption.raw.b64}"
case "${OUTPUT_DIR}" in /*) ;; *) OUTPUT_DIR="${ROOT_DIR}/${OUTPUT_DIR}" ;; esac
case "${KEY_FILE}" in /*) ;; *) KEY_FILE="$(pwd)/${KEY_FILE}" ;; esac
args=(-input "${INPUT_DIR}" -output "${OUTPUT_DIR}")
if [ -z "${SIGMO_CONFIG_ENCRYPTION_KEY:-}" ]; then args+=(-key-file "${KEY_FILE}"); fi
revision="$(cd "${ROOT_DIR}" && go run ./cmd/carrierconfig "${args[@]}")"
echo "Prepared encrypted configuration ${revision} in ${OUTPUT_DIR}"

if [ "${2:-}" = "--publish" ]; then
	cd "${ROOT_DIR}/pro-api"
	bucket="${SIGMO_CONFIG_BUCKET:-sigmo}"
	# Publish the complete immutable snapshot before replacing the small pointer.
	bunx wrangler r2 object put "${bucket}/configs/releases/${revision}.enc" \
		--remote --file "${OUTPUT_DIR}/releases/${revision}.enc" --content-type application/jose
	bunx wrangler r2 object put "${bucket}/configs/current.json" \
		--remote --file "${OUTPUT_DIR}/current.json" --content-type application/json
fi
