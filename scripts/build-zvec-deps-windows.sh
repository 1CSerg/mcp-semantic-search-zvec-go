#!/usr/bin/env bash
# DEPRECATED (2026-06): use scripts/fetch-zvec-libs.sh (zvec-ai vendor mode).
# Legacy: build danieleugenewilliams/zvec-go native deps on Windows (Git Bash + WinLibs MinGW + CMake).
# Prereqs: D:\tools\winlibs\mingw64\bin\gcc.exe (or WinLibs on PATH), CMake, patch, git.
set -euo pipefail

MINGW_BIN="${MINGW_BIN:-/d/tools/winlibs/mingw64/bin}"
CMAKE_BIN="${CMAKE_BIN:-/c/Program Files/CMake/bin}"
GIT_SSL="${GIT_SSL:-/c/Program Files/Git/usr/ssl/certs/ca-bundle.crt}"

# WinLibs cmake downloads deps via GnuTLS; point at Git's CA bundle.
export SSL_CERT_FILE="$GIT_SSL"
export SSL_CERT_DIR=""

# zvec lz4 ExternalProject invokes bare `make`; WinLibs ships mingw32-make only.
if [[ -x "$MINGW_BIN/mingw32-make.exe" && ! -x "$MINGW_BIN/make.exe" ]]; then
  cp -f "$MINGW_BIN/mingw32-make.exe" "$MINGW_BIN/make.exe" 2>/dev/null || true
fi

export PATH="$MINGW_BIN:$CMAKE_BIN:/usr/bin:$PATH"
# WinLibs GCC 16: rocksdb 8.1.1 relies on transitive <cstdint> includes.
export CXXFLAGS="${CXXFLAGS:-} -include cstdint"
export CFLAGS="${CFLAGS:-} -include stdint.h"

ZVEC_MOD="$(go list -m -f '{{.Dir}}' github.com/danieleugenewilliams/zvec-go)"
PROJECT_DIR="$ZVEC_MOD"
BUILD_DIR="$PROJECT_DIR/build"
DEPS_DIR="$PROJECT_DIR/deps"
ZVEC_DIR="$DEPS_DIR/zvec"
ZVEC_TAG="${ZVEC_TAG:-v0.2.0}"
ZVEC_REPO="${ZVEC_REPO:-https://github.com/alibaba/zvec.git}"
# High -j (e.g. 20) races Arrow/Boost subbuilds on Windows; cap parallelism.
NPROC="${ZVEC_BUILD_JOBS:-4}"
export CCACHE_DISABLE="${CCACHE_DISABLE:-1}"

CMAKE_GEN=( -G "MinGW Makefiles" -DCMAKE_C_COMPILER=gcc -DCMAKE_CXX_COMPILER=g++ )

echo "==> zvec-go build-deps (Windows MinGW)"
echo "    module: $ZVEC_MOD"

if [[ ! -d "$ZVEC_DIR" ]]; then
  mkdir -p "$DEPS_DIR"
  git clone --depth 1 --branch "$ZVEC_TAG" --recurse-submodules "$ZVEC_REPO" "$ZVEC_DIR"
fi

ANTLR4_CMAKE="$ZVEC_DIR/thirdparty/antlr/antlr4/runtime/Cpp/CMakeLists.txt"
if [[ -f "$ANTLR4_CMAKE" ]] && grep -q "CMP0054 OLD" "$ANTLR4_CMAKE"; then
  sed -i.bak -e '/CMP0054 OLD/d' -e '/CMP0045 OLD/d' -e '/CMP0042 OLD/d' -e '/CMP0059 OLD/d' "$ANTLR4_CMAKE"
fi

# GCC 16 (WinLibs): rocksdb blob_file_meta.h needs <cstdint> explicitly on Windows.
ROCKSDB_BLOB_H="$ZVEC_DIR/thirdparty/rocksdb/rocksdb-8.1.1/db/blob/blob_file_meta.h"
if [[ -f "$ROCKSDB_BLOB_H" ]] && ! grep -q '#include <cstdint>' "$ROCKSDB_BLOB_H"; then
  sed -i.bak '/rocksdb_namespace.h/a #include <cstdint>' "$ROCKSDB_BLOB_H"
fi

ZVEC_BUILD_DIR="$BUILD_DIR/zvec-build"
mkdir -p "$ZVEC_BUILD_DIR"

echo "==> Building libzvec (MinGW, jobs=$NPROC)..."
cmake -S "$ZVEC_DIR" -B "$ZVEC_BUILD_DIR" "${CMAKE_GEN[@]}" \
  -DCMAKE_BUILD_TYPE=Release \
  -DCMAKE_CXX_COMPILER_LAUNCHER= \
  -DCMAKE_C_COMPILER_LAUNCHER= \
  -DCMAKE_POSITION_INDEPENDENT_CODE=ON \
  -DCMAKE_POLICY_VERSION_MINIMUM=3.5 \
  -DBUILD_SHARED_LIBS=OFF \
  -DBUILD_TOOLS=OFF \
  -DBUILD_PYTHON_BINDINGS=OFF

cmake --build "$ZVEC_BUILD_DIR" --parallel "$NPROC"

WRAPPER_BUILD_DIR="$BUILD_DIR/wrapper-build"
mkdir -p "$WRAPPER_BUILD_DIR"

echo "==> Building zvec C wrapper..."
cmake -S "$PROJECT_DIR/c" -B "$WRAPPER_BUILD_DIR" "${CMAKE_GEN[@]}" \
  -DCMAKE_BUILD_TYPE=Release \
  -DZVEC_SRC_DIR="$ZVEC_DIR"

cmake --build "$WRAPPER_BUILD_DIR" --parallel "$NPROC"

LIB_DIR="$BUILD_DIR/lib"
mkdir -p "$LIB_DIR"

cp "$WRAPPER_BUILD_DIR/libzvec_c_wrapper.a" "$LIB_DIR/"
for lib in "$ZVEC_BUILD_DIR"/lib/*.a; do
  [[ -f "$lib" ]] && cp "$lib" "$LIB_DIR/"
done
for lib in "$ZVEC_BUILD_DIR"/external/usr/local/lib/*.a; do
  [[ -f "$lib" ]] && cp "$lib" "$LIB_DIR/"
done
ARROW_BUILD="$ZVEC_BUILD_DIR/thirdparty/arrow/arrow/src/ARROW.BUILD-build"
for dep in re2_ep-install/lib/libre2.a utf8proc_ep-install/lib/libutf8proc.a zlib_ep/src/zlib_ep-install/lib/libz.a; do
  [[ -f "$ARROW_BUILD/$dep" ]] && cp "$ARROW_BUILD/$dep" "$LIB_DIR/"
done

echo "==> Done. Libraries:"
ls -1 "$LIB_DIR/"
