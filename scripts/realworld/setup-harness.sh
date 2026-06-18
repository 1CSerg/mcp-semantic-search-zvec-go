#!/usr/bin/env bash
# Setup ephemeral .realworld/ harness tree for manual E2E tests.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
PROFILE="onnx"
KEEP_INDEX=0

usage() {
  echo "Usage: $0 [--profile onnx|lmstudio|mock-fail|mock-dim-mismatch] [--keep-index]"
  exit 1
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --profile) PROFILE="$2"; shift 2 ;;
    --keep-index) KEEP_INDEX=1; shift ;;
    -h|--help) usage ;;
    *) echo "unknown arg: $1" >&2; usage ;;
  esac
done

REALWORLD="$REPO_ROOT/.realworld"
BIN_DIR="$REALWORLD/bin"
INDEX_DIR="$REALWORLD/data/index"
MODELS_DIR="$REALWORLD/models/paraphrase-multilingual-MiniLM-L12-v2"
CONFIG_SRC="$REPO_ROOT/tests/realworld/config/${PROFILE}.yaml"
if [[ ! -f "$CONFIG_SRC" ]]; then
  CONFIG_SRC="$REPO_ROOT/tests/realworld/config/onnx.yaml"
fi

copy_runtime_libs() {
  local dest="$1"
  local repo_bin="$REPO_ROOT/bin"
  case "$(uname -s)" in
    MINGW*|MSYS*|CYGWIN*)
      for f in zvec_c_api.dll onnxruntime.dll mcp-semantic-search-zvec-go.exe; do
        [[ -f "$repo_bin/$f" ]] && cp -f "$repo_bin/$f" "$dest/"
      done
      ;;
    Linux*)
      for f in libzvec_c_api.so mcp-semantic-search-zvec-go; do
        [[ -f "$repo_bin/$f" ]] && cp -f "$repo_bin/$f" "$dest/"
      done
      ;;
    Darwin*)
      for f in libzvec_c_api.dylib mcp-semantic-search-zvec-go; do
        [[ -f "$repo_bin/$f" ]] && cp -f "$repo_bin/$f" "$dest/"
      done
      ;;
  esac
  if [[ ! -f "$dest/zvec_c_api.dll" && ! -f "$dest/libzvec_c_api.so" && ! -f "$dest/libzvec_c_api.dylib" ]]; then
    bash "$REPO_ROOT/scripts/fetch/fetch-zvec-libs.sh" > "$REPO_ROOT/.deps/zvec-lib.env"
    # shellcheck disable=SC1091
    . "$REPO_ROOT/.deps/zvec-lib.env"
    case "$(uname -s)" in
      MINGW*|MSYS*|CYGWIN*) cp -f "$ZVEC_LIB_DIR/zvec_c_api.dll" "$dest/" 2>/dev/null || true ;;
      Linux*) cp -f "$ZVEC_LIB_DIR/libzvec_c_api.so" "$dest/" 2>/dev/null || true ;;
      Darwin*) cp -f "$ZVEC_LIB_DIR/libzvec_c_api.dylib" "$dest/" 2>/dev/null || true ;;
    esac
  fi
  if [[ ! -f "$dest/onnxruntime.dll" && ! -f "$dest/libonnxruntime.so" && ! -f "$dest/libonnxruntime.dylib" ]]; then
    bash "$REPO_ROOT/scripts/fetch/fetch-onnx-runtime.sh" > "$REPO_ROOT/.deps/onnxruntime.env"
    # shellcheck disable=SC1091
    . "$REPO_ROOT/.deps/onnxruntime.env"
    cp -f "$ONNXRUNTIME_SHARED_LIBRARY_PATH" "$dest/" 2>/dev/null || true
  fi
}

echo "==> build-zvec"
make -C "$REPO_ROOT" build-zvec

if [[ "$KEEP_INDEX" -eq 0 ]]; then
  echo "==> recreate $REALWORLD"
  rm -rf "$REALWORLD"
fi

mkdir -p "$BIN_DIR" "$INDEX_DIR" "$REALWORLD/logs" "$REALWORLD/models" "$REALWORLD/targets"

BIN_NAME="mcp-semantic-search-zvec-go"
case "$(uname -s)" in
  MINGW*|MSYS*|CYGWIN*) BIN_NAME="${BIN_NAME}.exe" ;;
esac
cp -f "$REPO_ROOT/bin/$BIN_NAME" "$BIN_DIR/$BIN_NAME"
chmod +x "$BIN_DIR/$BIN_NAME" 2>/dev/null || true
copy_runtime_libs "$BIN_DIR"

cp -f "$CONFIG_SRC" "$REALWORLD/config.yaml"
touch "$REALWORLD/.env"

if [[ "$PROFILE" == "onnx" ]] || grep -q 'active_profile: local_multilingual' "$REALWORLD/config.yaml" 2>/dev/null; then
  echo "==> fetch ONNX model"
  bash "$REPO_ROOT/scripts/fetch/fetch-onnx-model.sh" "$MODELS_DIR"
fi

echo "Harness ready: profile=$PROFILE"
echo "  WORKSPACE_ROOT=$REPO_ROOT/tests/realworld/corpus"
echo "  INDEX_DIR=$INDEX_DIR"
echo "  CONFIG_PATH=$REALWORLD/config.yaml"
echo "  BIN=$BIN_DIR/$BIN_NAME"
