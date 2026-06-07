#!/usr/bin/env bash
# DEPRECATED (2026-06): use scripts/fetch-zvec-libs.sh (zvec-ai vendor mode).
# Legacy: resume danieleugenewilliams/zvec-go cmake build.
set -uo pipefail
export CCACHE_DISABLE=1
export NUMBER_OF_PROCESSORS="${ZVEC_BUILD_JOBS:-2}"
export CMAKE_BUILD_PARALLEL_LEVEL="$NUMBER_OF_PROCESSORS"
export CXXFLAGS="-include cstdint"
export PATH="/d/tools/winlibs/mingw64/bin:/c/Program Files/CMake/bin:/usr/bin:$PATH"
BUILD="$(go list -m -f '{{.Dir}}' github.com/danieleugenewilliams/zvec-go)/build/zvec-build"
LOG="/d/root/clouds/YandexDisk/30_Dev/repo/MCP/mcp-semantic-search-zvec-go/zvec-build.log"
cmake --build "$BUILD" --parallel "$NUMBER_OF_PROCESSORS" 2>&1 | tee "$LOG"
echo "EXIT:${PIPESTATUS[0]}"
