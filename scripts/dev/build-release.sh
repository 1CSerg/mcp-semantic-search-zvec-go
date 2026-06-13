#!/usr/bin/env bash
# Production release build (same flags as .github/workflows/release.yml).
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$REPO_ROOT"

bash scripts/fetch/fetch-zvec-libs.sh > .deps/zvec-lib.env
bash scripts/fetch/fetch-onnx-runtime.sh > .deps/onnxruntime.env
# shellcheck disable=SC1091
. .deps/zvec-lib.env
# shellcheck disable=SC1091
. .deps/onnxruntime.env

mkdir -p bin
ext=""
case "$(uname -s)" in
  MINGW*|MSYS*|CYGWIN*)
    ext=".exe"
    cp -f "$ZVEC_LIB_DIR/zvec_c_api.dll" bin/
    ;;
  Darwin*)
    cp -f "$ZVEC_LIB_DIR/libzvec_c_api.dylib" bin/ 2>/dev/null || true
    ;;
  Linux*)
    cp -f "$ZVEC_LIB_DIR/libzvec_c_api.so" bin/ 2>/dev/null || true
    ;;
esac

out="bin/mcp-semantic-search-zvec-go${ext}"
CGO_ENABLED=1 LD_LIBRARY_PATH="${ZVEC_LIB_DIR}:${ORT_LIB_DIR}:${LD_LIBRARY_PATH:-}" \
  go build -tags "zvec,onnx" -ldflags="-s -w" -o "$out" ./cmd/mcp-semantic-search-zvec-go
cp -f "$ONNXRUNTIME_SHARED_LIBRARY_PATH" bin/ 2>/dev/null || true

"$out" --version
echo "Built $out (release: zvec,onnx; -ldflags -s -w)"
