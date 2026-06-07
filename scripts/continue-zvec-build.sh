#!/usr/bin/env bash
# DEPRECATED (2026-06): use scripts/fetch-zvec-libs.sh (zvec-ai vendor mode).
# Legacy: resume zvec native build on Windows (after build-zvec-deps-windows.sh configure).
set -euo pipefail
JOBS="${ZVEC_BUILD_JOBS:-2}"
export CCACHE_DISABLE=1
export CMAKE_BUILD_PARALLEL_LEVEL="$JOBS"
export NUMBER_OF_PROCESSORS="$JOBS"
export CXXFLAGS="${CXXFLAGS:-} -include cstdint"
export CFLAGS="${CFLAGS:-} -include stdint.h"
export PATH="/d/tools/winlibs/mingw64/bin:/c/Program Files/CMake/bin:/usr/bin:$PATH"
BUILD="$(go list -m -f '{{.Dir}}' github.com/danieleugenewilliams/zvec-go)/build/zvec-build"

echo "==> rocksdb (serial)"
cmake --build "$BUILD" --target rocksdb --parallel 1
echo "==> full zvec (jobs=$JOBS)"
rm -rf "$BUILD/thirdparty/arrow/arrow/src/ARROW.BUILD-stamp"
cmake --build "$BUILD" --parallel "$JOBS"
