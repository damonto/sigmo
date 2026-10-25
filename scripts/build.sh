#!/usr/bin/env bash
set -euo pipefail
export GOWORK=off

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUTPUT_DIR="${SIGMO_BUILD_DIR:-${ROOT_DIR}/build/releases}"
TARGETS="${SIGMO_TARGETS:-linux-amd64 linux-amd64-musl linux-arm64 linux-arm64-musl linux-arm linux-arm-musl linux-386 linux-ppc64le linux-riscv64 linux-s390x}"

case "${OUTPUT_DIR}" in
	/*) OUTPUT_DIR_ABS="${OUTPUT_DIR}" ;;
	*) OUTPUT_DIR_ABS="${ROOT_DIR}/${OUTPUT_DIR}" ;;
esac

cleanup_dirs=()
musl_modfile=""

cleanup() {
	if [ "${#cleanup_dirs[@]}" -gt 0 ]; then rm -rf "${cleanup_dirs[@]}"; fi
}
trap cleanup EXIT

build_frontend() {
	if [ "${SIGMO_SKIP_FRONTEND_BUILD:-0}" = "1" ]; then return; fi
	(cd "${ROOT_DIR}/apps/web" && bun install --frozen-lockfile && bun run build --mode prod)
}

target_arch() {
	case "$1" in
		linux-386) printf '386\n' ;;
		linux-ppc64le) printf 'ppc64le\n' ;;
		linux-riscv64) printf 'riscv64\n' ;;
		linux-s390x) printf 's390x\n' ;;
		linux-amd64|linux-amd64-musl) printf 'amd64\n' ;;
		linux-arm64|linux-arm64-musl) printf 'arm64\n' ;;
		linux-arm|linux-arm-musl) printf 'arm\n' ;;
		*) echo "unknown target: $1" >&2; return 1 ;;
	esac
}

musl_interpreter() {
	case "$1" in
		amd64) printf '/lib/ld-musl-x86_64.so.1\n' ;;
		arm) printf '/lib/ld-musl-armhf.so.1\n' ;;
		arm64) printf '/lib/ld-musl-aarch64.so.1\n' ;;
		*) echo "unsupported musl interpreter for GOARCH: $1" >&2; return 1 ;;
	esac
}

prepare_musl_modfile() {
	local goarch="$1" temp_dir purego_dir
	temp_dir="$(mktemp -d)"
	cleanup_dirs+=("${temp_dir}")
	musl_modfile="${temp_dir}/go.mod"
	purego_dir="$(cd "${ROOT_DIR}" && go list -m -f '{{.Dir}}' github.com/ebitengine/purego)"
	cp -R "${purego_dir}" "${temp_dir}/purego"
	cp "${ROOT_DIR}/go.mod" "${musl_modfile}"
	cp "${ROOT_DIR}/go.sum" "${temp_dir}/go.sum"
	go mod edit -modfile="${musl_modfile}" -replace=github.com/ebitengine/purego="${temp_dir}/purego"
	TARGETARCH="${goarch}" "${ROOT_DIR}/scripts/patch-purego-musl.sh" "${musl_modfile}"
}

build_target() {
	local name="$1" goarch binary ldflags interpreter
	local go_args=()
	goarch="$(target_arch "${name}")"
	binary="${OUTPUT_DIR_ABS}/sigmo-${name}"
	ldflags="-w -s"
	ldflags+=" -X github.com/damonto/sigmo/internal/app/buildinfo.Version=${SIGMO_BUILD_VERSION}"
	ldflags+=" -X github.com/damonto/sigmo/internal/app/buildinfo.Commit=${SIGMO_BUILD_COMMIT}"
	ldflags+=" -X github.com/damonto/sigmo/internal/app/buildinfo.Channel=${SIGMO_BUILD_CHANNEL}"
	ldflags+=" -X github.com/damonto/sigmo/internal/app/buildinfo.Target=${name}"
	ldflags+=" -X github.com/damonto/sigmo/internal/app/buildinfo.Distribution=${SIGMO_DISTRIBUTION:-developer}"
	ldflags+=" -X github.com/damonto/sigmo/internal/app/buildinfo.ReleasePublicKey=${SIGMO_RELEASE_PUBLIC_KEY:-}"
	ldflags+=" -X github.com/damonto/sigmo/internal/pkg/license.WorkerURL=${SIGMO_WORKER_URL:-https://sigmo-pro.952007.xyz}"
	ldflags+=" -X github.com/damonto/sigmo/internal/pkg/nativepush.GatewayURL=${SIGMO_PUSH_GATEWAY_URL:-https://sigmo-pro.952007.xyz}"
	if [ -n "${SIGMO_LICENSE_PUBLIC_KEY:-}" ]; then
		ldflags+=" -X github.com/damonto/sigmo/internal/pkg/license.LicensePublicKey=${SIGMO_LICENSE_PUBLIC_KEY}"
	fi
	if [[ "${name}" == *-musl ]]; then
		interpreter="$(musl_interpreter "${goarch}")"
		prepare_musl_modfile "${goarch}"
		ldflags="-I ${interpreter} ${ldflags}"
		go_args+=(-modfile="${musl_modfile}")
	fi
	go_args+=(-trimpath -ldflags="${ldflags}" -o "${binary}" .)
	echo "Building ${binary}"
	(cd "${ROOT_DIR}" && env GOOS=linux GOARCH="${goarch}" GOARM=7 CGO_ENABLED=0 go build "${go_args[@]}")
}

main() {
	: "${SIGMO_BUILD_VERSION:?SIGMO_BUILD_VERSION is required}"
	: "${SIGMO_BUILD_COMMIT:?SIGMO_BUILD_COMMIT is required}"
	: "${SIGMO_BUILD_CHANNEL:?SIGMO_BUILD_CHANNEL is required}"
	if [ "${SIGMO_DISTRIBUTION:-developer}" = "standalone" ]; then
		: "${SIGMO_RELEASE_PUBLIC_KEY:?SIGMO_RELEASE_PUBLIC_KEY is required for release builds}"
	fi
	mkdir -p "${OUTPUT_DIR_ABS}"
	export GOCACHE="${GOCACHE:-${OUTPUT_DIR_ABS}/.go-build-cache}"
	mkdir -p "${GOCACHE}"
	build_frontend
	(cd "${ROOT_DIR}" && go mod download)
	for target in ${TARGETS}; do build_target "${target}"; done
}

main "$@"
