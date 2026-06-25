#!/usr/bin/env bash
set -euo pipefail

OPENCORE_AMR_VERSION="${OPENCORE_AMR_VERSION:-0.1.6}"
VO_AMRWBENC_VERSION="${VO_AMRWBENC_VERSION:-0.1.3}"

OPENCORE_AMR_URL="${OPENCORE_AMR_URL:-https://sourceforge.net/projects/opencore-amr/files/opencore-amr/opencore-amr-${OPENCORE_AMR_VERSION}.tar.gz/download}"
VO_AMRWBENC_URL="${VO_AMRWBENC_URL:-https://sourceforge.net/projects/opencore-amr/files/vo-amrwbenc/vo-amrwbenc-${VO_AMRWBENC_VERSION}.tar.gz/download}"

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
build_dir="${repo_root}/build"
out_dir="${repo_root}/pro/voicecodec/assets"
out_wasm="${out_dir}/opencore-amr.wasm"

opencore_archive="${build_dir}/opencore-amr-${OPENCORE_AMR_VERSION}.tar.gz"
vo_archive="${build_dir}/vo-amrwbenc-${VO_AMRWBENC_VERSION}.tar.gz"

require_tool() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required tool: $1" >&2
    exit 1
  fi
}

fetch_archive() {
  local url="$1"
  local archive="$2"
  if [[ -f "${archive}" ]]; then
    return
  fi
  mkdir -p "$(dirname "${archive}")"
  echo "download ${url}"
  curl -L --fail --retry 3 --output "${archive}" "${url}"
}

append_find() {
  local -n out="$1"
  shift
  local file
  while IFS= read -r -d '' file; do
    out+=("${file}")
  done < <(find "$@" -print0 | sort -z)
}

require_tool curl
require_tool tar

CC="${CC:-clang}"
CXX="${CXX:-clang++}"
require_tool "${CC}"
require_tool "${CXX}"

WASI_TARGET="${WASI_TARGET:-wasm32-wasip1}"
WASI_SYSROOT="${WASI_SYSROOT:-}"
wasi_sysroot_has_headers() {
  local sysroot="$1"
  [[ -f "${sysroot}/include/stdlib.h" ]] ||
    [[ -f "${sysroot}/include/${WASI_TARGET}/stdlib.h" ]] ||
    [[ -f "${sysroot}/include/wasm32-wasi/stdlib.h" ]]
}

if [[ -z "${WASI_SYSROOT}" ]]; then
  for candidate in \
    /opt/wasi-sdk/share/wasi-sysroot \
    /usr/share/wasi-sysroot \
    /usr/local/share/wasi-sysroot; do
    if wasi_sysroot_has_headers "${candidate}"; then
      WASI_SYSROOT="${candidate}"
      break
    fi
  done
fi
if [[ -z "${WASI_SYSROOT}" ]] || ! wasi_sysroot_has_headers "${WASI_SYSROOT}"; then
  echo "missing WASI sysroot; install wasi-sdk or set WASI_SYSROOT" >&2
  exit 1
fi

fetch_archive "${OPENCORE_AMR_URL}" "${opencore_archive}"
fetch_archive "${VO_AMRWBENC_URL}" "${vo_archive}"

work_dir="$(mktemp -d)"
trap 'rm -rf "${work_dir}"' EXIT

tar -xzf "${opencore_archive}" -C "${work_dir}"
tar -xzf "${vo_archive}" -C "${work_dir}"

opencore_dir="${work_dir}/opencore-amr-${OPENCORE_AMR_VERSION}"
vo_dir="${work_dir}/vo-amrwbenc-${VO_AMRWBENC_VERSION}"

bridge="${work_dir}/sigmo_amr_wasi_bridge.cpp"
cat >"${bridge}" <<'CPP'
#include "interf_dec.h"
#include "interf_enc.h"
#include "dec_if.h"
#include "enc_if.h"

extern "C" {

void* sigmo_amrnb_decoder_create() { return Decoder_Interface_init(); }
void sigmo_amrnb_decoder_destroy(void* state) { Decoder_Interface_exit(state); }
void sigmo_amrnb_decode(void* state, const unsigned char* frame, short* pcm, int bfi) {
    Decoder_Interface_Decode(state, frame, pcm, bfi);
}
void* sigmo_amrnb_encoder_create(int dtx) { return Encoder_Interface_init(dtx); }
void sigmo_amrnb_encoder_destroy(void* state) { Encoder_Interface_exit(state); }
int sigmo_amrnb_encode(void* state, int mode, const short* pcm, unsigned char* out) {
    return Encoder_Interface_Encode(state, static_cast<Mode>(mode), pcm, out, 0);
}

void* sigmo_amrwb_decoder_create() { return D_IF_init(); }
void sigmo_amrwb_decoder_destroy(void* state) { D_IF_exit(state); }
void sigmo_amrwb_decode(void* state, const unsigned char* frame, short* pcm, int bfi) {
    D_IF_decode(state, frame, pcm, bfi);
}
void* sigmo_amrwb_encoder_create() { return E_IF_init(); }
void sigmo_amrwb_encoder_destroy(void* state) { E_IF_exit(state); }
int sigmo_amrwb_encode(void* state, int mode, const short* pcm, unsigned char* out, int dtx) {
    return E_IF_encode(state, mode, pcm, out, dtx);
}

}
CPP

bridge_sources=("${bridge}")
opencore_sources=(
  "${opencore_dir}/amrnb/wrapper.cpp"
  "${opencore_dir}/amrwb/wrapper.cpp"
)
vo_sources=(
  "${vo_dir}/wrapper.c"
  "${vo_dir}/common/cmnMemory.c"
)

amrnb_base="${opencore_dir}/opencore/codecs_v2/audio/gsm_amr/amr_nb"
append_find opencore_sources "${amrnb_base}/dec/src" -maxdepth 1 -type f -name '*.cpp' \
  ! -name 'decoder_gsm_amr.cpp' \
  ! -name 'pvgsmamrdecoder.cpp'
append_find opencore_sources "${amrnb_base}/enc/src" -maxdepth 1 -type f -name '*.cpp' \
  ! -name 'gsmamr_encoder_wrapper.cpp'
append_find opencore_sources "${amrnb_base}/common/src" -maxdepth 1 -type f -name '*.cpp' \
  ! -name 'bits2prm.cpp' \
  ! -name 'copy.cpp' \
  ! -name 'div_32.cpp' \
  ! -name 'l_abs.cpp' \
  ! -name 'r_fft.cpp' \
  ! -name 'vad1.cpp' \
  ! -name 'vad2.cpp'

amrwb_dec_src="${opencore_dir}/opencore/codecs_v2/audio/gsm_amr/amr_wb/dec/src"
append_find opencore_sources "${amrwb_dec_src}" -maxdepth 1 -type f -name '*.cpp' \
  ! -name 'decoder_amr_wb.cpp'
append_find vo_sources "${vo_dir}/amrwbenc/src" -maxdepth 1 -type f -name '*.c'

opencore_include_flags=(
  "-I${opencore_dir}/oscl"
  "-I${opencore_dir}/amrnb"
  "-I${opencore_dir}/amrwb"
  "-I${amrnb_base}/dec/src"
  "-I${amrnb_base}/dec/include"
  "-I${amrnb_base}/enc/src"
  "-I${amrnb_base}/enc/include"
  "-I${amrnb_base}/common/include"
  "-I${opencore_dir}/opencore/codecs_v2/audio/gsm_amr/common/dec/include"
  "-I${opencore_dir}/opencore/codecs_v2/audio/gsm_amr/amr_wb/dec/src"
  "-I${opencore_dir}/opencore/codecs_v2/audio/gsm_amr/amr_wb/dec/include"
)
vo_include_flags=(
  "-I${vo_dir}"
  "-I${vo_dir}/amrwbenc/inc"
  "-I${vo_dir}/common/include"
)
bridge_include_flags=("${opencore_include_flags[@]}" "${vo_include_flags[@]}")

object_dir="${work_dir}/objects"
mkdir -p "${object_dir}" "${out_dir}"
objects=()

compile_group() {
  local compiler="$1"
  local label="$2"
  local standard="$3"
  local -n source_group="$4"
  local -n flag_group="$5"
  local index=0
  local source
  for source in "${source_group[@]}"; do
    local object="${object_dir}/${label}_${index}.o"
    "${compiler}" --target="${WASI_TARGET}" --sysroot="${WASI_SYSROOT}" "${source}" \
      "${flag_group[@]}" \
      "${standard}" \
      -O3 \
      -flto \
      -fno-exceptions \
      -c \
      -o "${object}"
    objects+=("${object}")
    index=$((index + 1))
  done
}

compile_group "${CXX}" bridge "-std=gnu++14" bridge_sources bridge_include_flags
compile_group "${CXX}" opencore "-std=gnu++14" opencore_sources opencore_include_flags
compile_group "${CC}" vo "-std=c99" vo_sources vo_include_flags

"${CC}" --target="${WASI_TARGET}" --sysroot="${WASI_SYSROOT}" "${objects[@]}" \
  -O3 \
  -flto \
  -nostartfiles \
  -Wl,--no-entry \
  -Wl,--export=malloc \
  -Wl,--export=free \
  -Wl,--export=sigmo_amrnb_decoder_create \
  -Wl,--export=sigmo_amrnb_decoder_destroy \
  -Wl,--export=sigmo_amrnb_decode \
  -Wl,--export=sigmo_amrnb_encoder_create \
  -Wl,--export=sigmo_amrnb_encoder_destroy \
  -Wl,--export=sigmo_amrnb_encode \
  -Wl,--export=sigmo_amrwb_decoder_create \
  -Wl,--export=sigmo_amrwb_decoder_destroy \
  -Wl,--export=sigmo_amrwb_decode \
  -Wl,--export=sigmo_amrwb_encoder_create \
  -Wl,--export=sigmo_amrwb_encoder_destroy \
  -Wl,--export=sigmo_amrwb_encode \
  -Wl,--export-memory \
  -o "${out_wasm}"

chmod 0644 "${out_wasm}"
echo "built ${out_wasm}"
