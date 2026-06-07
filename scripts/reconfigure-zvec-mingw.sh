#!/usr/bin/env bash
# DEPRECATED (2026-06): use scripts/fetch-zvec-libs.sh (zvec-ai vendor mode).
# Legacy: reconfigure danieleugenewilliams/zvec-go MinGW build.
set -euo pipefail
export PATH="/d/tools/winlibs/mingw64/bin:/c/Program Files/CMake/bin:/usr/bin:$PATH"
export CCACHE_DISABLE=1
ZVEC_MOD="$(go list -m -f '{{.Dir}}' github.com/danieleugenewilliams/zvec-go)"
BUILD="$ZVEC_MOD/build/zvec-build"
ZVEC_DIR="$ZVEC_MOD/deps/zvec"
cmake -S "$ZVEC_DIR" -B "$BUILD" \
  -G "MinGW Makefiles" \
  -DCMAKE_C_COMPILER=gcc \
  -DCMAKE_CXX_COMPILER=g++ \
  -DCMAKE_BUILD_TYPE=Release \
  -DCMAKE_CXX_FLAGS="-include cstdint" \
  -DCMAKE_C_FLAGS="-include stdint.h" \
  -DCMAKE_POSITION_INDEPENDENT_CODE=ON \
  -DCMAKE_POLICY_VERSION_MINIMUM=3.5 \
  -DBUILD_SHARED_LIBS=OFF \
  -DBUILD_TOOLS=OFF \
  -DBUILD_PYTHON_BINDINGS=OFF \
  -DCMAKE_CXX_COMPILER_LAUNCHER= \
  -DCMAKE_C_COMPILER_LAUNCHER=
echo "==> reconfigured $BUILD"
