#!/usr/bin/env sh
set -eu

modfile="${1:-}"
modfile_arg=""
if [ -n "${modfile}" ]; then
	modfile_arg="-modfile=${modfile}"
fi

musl_libc="${PUREGO_MUSL_LIBC:-}"
if [ -z "${musl_libc}" ]; then
	goarch="${TARGETARCH:-$(go env GOARCH)}"
	case "${goarch}" in
		386)
			musl_libc="libc.musl-x86.so.1"
			;;
		amd64)
			musl_libc="libc.musl-x86_64.so.1"
			;;
		arm)
			case "${TARGETVARIANT:-}" in
				v6)
					musl_libc="libc.musl-armhf.so.1"
					;;
				"" | v7)
					musl_libc="libc.musl-armv7.so.1"
					;;
				*)
					echo "unsupported musl ARM variant for purego patch: ${TARGETVARIANT}" >&2
					exit 1
					;;
			esac
			;;
		arm64)
			musl_libc="libc.musl-aarch64.so.1"
			;;
		loong64)
			musl_libc="libc.musl-loongarch64.so.1"
			;;
		ppc64le | riscv64 | s390x)
			musl_libc="libc.musl-${goarch}.so.1"
			;;
		*)
			echo "unsupported musl GOARCH for purego patch: ${goarch}; set PUREGO_MUSL_LIBC explicitly" >&2
			exit 1
			;;
	esac
fi

purego_dir="$(go list -m ${modfile_arg} -f '{{.Dir}}' github.com/ebitengine/purego)"

chmod -R u+w "${purego_dir}"

patch_file() {
	file="${purego_dir}/$1"
	from="$2"
	to="$3"

	if grep -q "\"${from}\"" "${file}"; then
		sed -i "s/\"${from}\"/\"${to}\"/g" "${file}"
		return
	fi
	if grep -Fq "\"${to}\"" "${file}"; then
		return
	else
		echo "purego musl patch target not found in ${file}: ${from}" >&2
		exit 1
	fi
}

patch_file "dlfcn_nocgo_linux.go" "libdl\\.so\\.2" "${musl_libc}"
patch_file "internal/fakecgo/zsymbols_linux.go" "libc\\.so\\.6" "${musl_libc}"
patch_file "internal/fakecgo/zsymbols_linux.go" "libpthread\\.so\\.0" "${musl_libc}"
