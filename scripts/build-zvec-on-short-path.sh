#!/usr/bin/env bash
# DEPRECATED (2026-06): use scripts/fetch-zvec-libs.sh (zvec-ai vendor mode).
# Legacy: build zvec in a short path (Z: via subst) to avoid Windows MAX_PATH in Arrow/Boost.
set -euo pipefail
JOBS="${ZVEC_BUILD_JOBS:-2}"
export CCACHE_DISABLE=1
export NUMBER_OF_PROCESSORS="$JOBS"
export CMAKE_BUILD_PARALLEL_LEVEL="$JOBS"
export PATH="/d/tools/winlibs/mingw64/bin:/c/Program Files/CMake/bin:/usr/bin:$PATH"
export SSL_CERT_FILE="${GIT_SSL:-/c/Program Files/Git/usr/ssl/certs/ca-bundle.crt}"

ZVEC_MOD="$(go list -m -f '{{.Dir}}' github.com/danieleugenewilliams/zvec-go)"
# Map module dir to Z: if not already (subst must be run as admin on some systems).
if [[ ! -d /z/deps/zvec ]]; then
  echo "ERROR: run first (elevated if needed):"
  echo "  subst Z: \"$(cygpath -w "$ZVEC_MOD")\""
  exit 1
fi

ROOT=/z
ZVEC_DIR="$ROOT/deps/zvec"
BUILD="$ROOT/build/zvec-build"
CMAKE_GEN=( -G "MinGW Makefiles" -DCMAKE_C_COMPILER=gcc -DCMAKE_CXX_COMPILER=g++ )

if [[ -x /d/tools/winlibs/mingw64/bin/mingw32-make.exe && ! -x /d/tools/winlibs/mingw64/bin/make.exe ]]; then
  cp -f /d/tools/winlibs/mingw64/bin/mingw32-make.exe /d/tools/winlibs/mingw64/bin/make.exe 2>/dev/null || true
fi

echo "==> clean $BUILD"
rm -rf "$BUILD"

echo "==> configure $BUILD (short path)"
cmake -S "$ZVEC_DIR" -B "$BUILD" "${CMAKE_GEN[@]}" \
  -DCMAKE_BUILD_TYPE=Release \
  -DCMAKE_CXX_FLAGS="-include cstdint" \
  -DCMAKE_C_FLAGS="-include stdint.h" \
  -DCMAKE_CXX_COMPILER_LAUNCHER= \
  -DCMAKE_C_COMPILER_LAUNCHER= \
  -DCMAKE_POSITION_INDEPENDENT_CODE=ON \
  -DCMAKE_POLICY_VERSION_MINIMUM=3.5 \
  -DBUILD_SHARED_LIBS=OFF \
  -DBUILD_TOOLS=OFF \
  -DBUILD_PYTHON_BINDINGS=OFF

echo "==> build (jobs=$JOBS)"
cmake --build "$BUILD" --parallel "$JOBS"
