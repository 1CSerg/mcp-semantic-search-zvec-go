#!/usr/bin/env bash
set -euo pipefail

TARGET_ROOT="${TARGET_ROOT:-$(pwd)}"
FETCH_ONNX_MODEL="${FETCH_ONNX_MODEL:-0}"
REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
INSTALL_DIR="$TARGET_ROOT/.mcp-semantic-search-zvec-go"
BIN_DIR="$INSTALL_DIR/bin"
TEMPLATES="$REPO_ROOT/templates"
BINARY_NAME="mcp-semantic-search-zvec-go"
SERVER_KEY="semantic-search-zvec-go"

version() {
  grep 'Version = ' "$REPO_ROOT/internal/version/version.go" | sed 's/.*"\(.*\)".*/\1/'
}

VERSION="$(version)"
echo "Installing mcp-semantic-search-zvec-go v${VERSION} into ${INSTALL_DIR}"

mkdir -p "$BIN_DIR" "$INSTALL_DIR/data/index" "$INSTALL_DIR/data/logs" "$INSTALL_DIR/models"
cp -f "$REPO_ROOT/config.yaml" "$INSTALL_DIR/config.yaml"

MODEL_DIR="$INSTALL_DIR/models/paraphrase-multilingual-MiniLM-L12-v2"
if [[ "$FETCH_ONNX_MODEL" == "1" ]] || grep -q 'active_profile: local_multilingual' "$INSTALL_DIR/config.yaml" 2>/dev/null; then
  bash "$REPO_ROOT/scripts/fetch/fetch-onnx-model.sh" "$MODEL_DIR"
fi

ENV_FILE="$INSTALL_DIR/.env"
if [[ ! -f "$ENV_FILE" && -f "$TEMPLATES/env.example" ]]; then
  cp -f "$TEMPLATES/env.example" "$ENV_FILE"
  echo "Created secrets file: $ENV_FILE"
fi

DST_BIN="$BIN_DIR/$BINARY_NAME"
if [[ -x "$REPO_ROOT/bin/$BINARY_NAME" ]]; then
  cp -f "$REPO_ROOT/bin/$BINARY_NAME" "$DST_BIN"
  chmod +x "$DST_BIN"
else
  bash "$REPO_ROOT/scripts/fetch/fetch-zvec-libs.sh" > "$REPO_ROOT/.deps/zvec-lib.env"
  bash "$REPO_ROOT/scripts/fetch/fetch-onnx-runtime.sh" > "$REPO_ROOT/.deps/onnxruntime.env"
  # shellcheck disable=SC1091
  . "$REPO_ROOT/.deps/zvec-lib.env"
  # shellcheck disable=SC1091
  . "$REPO_ROOT/.deps/onnxruntime.env"
  (
    cd "$REPO_ROOT"
    CGO_ENABLED=1 LD_LIBRARY_PATH="${ZVEC_LIB_DIR}:${ORT_LIB_DIR}:${LD_LIBRARY_PATH:-}" \
      go build -tags "zvec,onnx" -o "$DST_BIN" ./cmd/mcp-semantic-search-zvec-go
    case "$(uname -s)" in
      Linux*)
        if [[ -f "$ZVEC_LIB_DIR/libzvec_c_api.so" ]]; then
          cp -f "$ZVEC_LIB_DIR/libzvec_c_api.so" "$BIN_DIR/"
        fi
        if [[ -f "$ONNXRUNTIME_SHARED_LIBRARY_PATH" ]]; then
          cp -f "$ONNXRUNTIME_SHARED_LIBRARY_PATH" "$BIN_DIR/"
        fi
        ;;
      Darwin*)
        if [[ -f "$ZVEC_LIB_DIR/libzvec_c_api.dylib" ]]; then
          cp -f "$ZVEC_LIB_DIR/libzvec_c_api.dylib" "$BIN_DIR/"
        fi
        if [[ -f "$ONNXRUNTIME_SHARED_LIBRARY_PATH" ]]; then
          cp -f "$ONNXRUNTIME_SHARED_LIBRARY_PATH" "$BIN_DIR/"
        fi
        ;;
    esac
  )
fi

cat > "$INSTALL_DIR/install-manifest.json" <<EOF
{
  "mode": "native",
  "runtime": "go",
  "version": "${VERSION}",
  "installed_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
}
EOF
echo "$VERSION" > "$INSTALL_DIR/installed-version.txt"

CURSOR_DIR="$TARGET_ROOT/.cursor"
mkdir -p "$CURSOR_DIR"
MCP_JSON="$CURSOR_DIR/mcp.json"
FRAGMENT="$TEMPLATES/cursor-mcp-linux.fragment.json"
python3 - <<'PY' "$MCP_JSON" "$FRAGMENT" "$SERVER_KEY"
import json, sys
mcp_path, frag_path, key = sys.argv[1:4]
frag = json.load(open(frag_path, encoding="utf-8"))
try:
    obj = json.load(open(mcp_path, encoding="utf-8"))
except FileNotFoundError:
    obj = {"mcpServers": {}}
obj.setdefault("mcpServers", {}).update(frag["mcpServers"])
json.dump(obj, open(mcp_path, "w", encoding="utf-8"), indent=2)
PY

GITIGNORE="$TARGET_ROOT/.gitignore"
BLOCK="# BEGIN mcp-semantic-search-zvec-go
.mcp-semantic-search-zvec-go/
# END mcp-semantic-search-zvec-go"
if [[ -f "$GITIGNORE" ]]; then
  grep -q "BEGIN mcp-semantic-search-zvec-go" "$GITIGNORE" || printf '\n%s\n' "$BLOCK" >> "$GITIGNORE"
else
  printf '%s\n' "$BLOCK" > "$GITIGNORE"
fi

echo "Done. Restart Cursor. MCP server key: $SERVER_KEY"
echo "Fill in $ENV_FILE for cloud embedding profiles (RouterAI, DashScope, etc.)."
echo "For offline ONNX: set active_profile: local_multilingual and run reindex with force=true."
