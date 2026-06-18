#!/usr/bin/env bash
set -euo pipefail

TARGET_ROOT="${TARGET_ROOT:-$(pwd)}"
FETCH_ONNX_MODEL="${FETCH_ONNX_MODEL:-0}"
REPLACE_CONFIG="${REPLACE_CONFIG:-0}"
REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
INSTALL_DIR="$TARGET_ROOT/.mcp-semantic-search-zvec-go"
BIN_DIR="$INSTALL_DIR/bin"
TEMPLATES="$REPO_ROOT/templates"
BINARY_NAME="mcp-semantic-search-zvec-go"
SERVER_KEY="semantic-search-zvec-go"
CURSOR_RULE_REL=".cursor/rules/semantic-search-zvec-go.mdc"
ROO_RULE_REL=".roo/rules/semantic-search-zvec-go.md"
ROO_MCP_REL=".roo/mcp.json"

version() {
  grep 'Version = ' "$REPO_ROOT/internal/version/version.go" | sed 's/.*"\(.*\)".*/\1/'
}

VERSION="$(version)"
echo "Installing mcp-semantic-search-zvec-go v${VERSION} into ${INSTALL_DIR}"

copy_runtime_libs() {
  local bin_dir="$1"
  local repo_bin="$REPO_ROOT/bin"
  case "$(uname -s)" in
    MINGW*|MSYS*|CYGWIN*)
      for f in zvec_c_api.dll onnxruntime.dll; do
        if [[ -f "$repo_bin/$f" ]]; then
          cp -f "$repo_bin/$f" "$bin_dir/"
        fi
      done
      ;;
    Linux*)
      for f in libzvec_c_api.so; do
        if [[ -f "$repo_bin/$f" ]]; then
          cp -f "$repo_bin/$f" "$bin_dir/"
        fi
      done
      ;;
    Darwin*)
      for f in libzvec_c_api.dylib; do
        if [[ -f "$repo_bin/$f" ]]; then
          cp -f "$repo_bin/$f" "$bin_dir/"
        fi
      done
      ;;
  esac

  if [[ ! -f "$bin_dir/zvec_c_api.dll" && ! -f "$bin_dir/libzvec_c_api.so" && ! -f "$bin_dir/libzvec_c_api.dylib" ]]; then
    bash "$REPO_ROOT/scripts/fetch/fetch-zvec-libs.sh" > "$REPO_ROOT/.deps/zvec-lib.env"
    # shellcheck disable=SC1091
    . "$REPO_ROOT/.deps/zvec-lib.env"
    case "$(uname -s)" in
      MINGW*|MSYS*|CYGWIN*) cp -f "$ZVEC_LIB_DIR/zvec_c_api.dll" "$bin_dir/" 2>/dev/null || true ;;
      Linux*) cp -f "$ZVEC_LIB_DIR/libzvec_c_api.so" "$bin_dir/" 2>/dev/null || true ;;
      Darwin*) cp -f "$ZVEC_LIB_DIR/libzvec_c_api.dylib" "$bin_dir/" 2>/dev/null || true ;;
    esac
  fi

  if [[ ! -f "$bin_dir/onnxruntime.dll" && ! -f "$bin_dir/libonnxruntime.so" && ! -f "$bin_dir/libonnxruntime.dylib" ]]; then
    bash "$REPO_ROOT/scripts/fetch/fetch-onnx-runtime.sh" > "$REPO_ROOT/.deps/onnxruntime.env"
    # shellcheck disable=SC1091
    . "$REPO_ROOT/.deps/onnxruntime.env"
    cp -f "$ONNXRUNTIME_SHARED_LIBRARY_PATH" "$bin_dir/" 2>/dev/null || true
  fi
}

mkdir -p "$BIN_DIR" "$INSTALL_DIR/data/index" "$INSTALL_DIR/data/logs" "$INSTALL_DIR/models"

merge_config_args=("$REPO_ROOT/scripts/install/merge-config.py" "$REPO_ROOT/config.yaml" "$INSTALL_DIR/config.yaml")
if [[ "$REPLACE_CONFIG" == "1" ]]; then
  merge_config_args+=(--replace)
fi
set +e
merge_out="$(python3 "${merge_config_args[@]}" 2>&1)"
merge_rc=$?
set -e
if [[ $merge_rc -eq 0 ]]; then
  echo "$merge_out"
elif [[ $merge_rc -eq 2 ]]; then
  if [[ -f "$INSTALL_DIR/config.yaml" ]]; then
    echo "WARNING: config.yaml preserved (install merge requires ruamel.yaml): pip install -r scripts/install/requirements.txt" >&2
  else
    cp -f "$REPO_ROOT/config.yaml" "$INSTALL_DIR/config.yaml"
    echo "created"
  fi
else
  echo "$merge_out" >&2
  exit 1
fi

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
      go build -tags "zvec,onnx,treesitter" -o "$DST_BIN" ./cmd/mcp-semantic-search-zvec-go
  )
fi
copy_runtime_libs "$BIN_DIR"

printf '%s\n' "$(cd "$TARGET_ROOT" && pwd)" > "$BIN_DIR/workspace-root.txt"

cp -f "$REPO_ROOT/scripts/install/uninstall.sh" "$INSTALL_DIR/uninstall.sh"
chmod +x "$INSTALL_DIR/uninstall.sh"

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

RULE_TEMPLATE="$TEMPLATES/cursor-rules/semantic-search-zvec-go.mdc"
if [[ ! -f "$RULE_TEMPLATE" ]]; then
  echo "cursor rule template not found: $RULE_TEMPLATE" >&2
  exit 1
fi
RULES_DIR="$CURSOR_DIR/rules"
mkdir -p "$RULES_DIR"
cp -f "$RULE_TEMPLATE" "$TARGET_ROOT/$CURSOR_RULE_REL"
echo "Installed Cursor rule: $TARGET_ROOT/$CURSOR_RULE_REL"

ROO_DIR="$TARGET_ROOT/.roo"
mkdir -p "$ROO_DIR"
ROO_MCP_JSON="$ROO_DIR/mcp.json"
python3 - <<'PY' "$ROO_MCP_JSON" "$FRAGMENT" "$SERVER_KEY"
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
echo "Updated Roo MCP config: $ROO_MCP_JSON"

ROO_RULE_TEMPLATE="$TEMPLATES/roo-rules/semantic-search-zvec-go.md"
if [[ ! -f "$ROO_RULE_TEMPLATE" ]]; then
  echo "roo rule template not found: $ROO_RULE_TEMPLATE" >&2
  exit 1
fi
ROO_RULES_DIR="$ROO_DIR/rules"
mkdir -p "$ROO_RULES_DIR"
cp -f "$ROO_RULE_TEMPLATE" "$TARGET_ROOT/$ROO_RULE_REL"
echo "Installed Roo rule: $TARGET_ROOT/$ROO_RULE_REL"

cat > "$INSTALL_DIR/install-manifest.json" <<EOF
{
  "mode": "native",
  "runtime": "go",
  "version": "${VERSION}",
  "installed_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "cursor_rule": "${CURSOR_RULE_REL}",
  "roo_rule": "${ROO_RULE_REL}",
  "roo_mcp": "${ROO_MCP_REL}"
}
EOF
echo "$VERSION" > "$INSTALL_DIR/installed-version.txt"

GITIGNORE="$TARGET_ROOT/.gitignore"
BLOCK="# BEGIN mcp-semantic-search-zvec-go
.mcp-semantic-search-zvec-go/
# END mcp-semantic-search-zvec-go"
if [[ -f "$GITIGNORE" ]]; then
  grep -q "BEGIN mcp-semantic-search-zvec-go" "$GITIGNORE" || printf '\n%s\n' "$BLOCK" >> "$GITIGNORE"
else
  printf '%s\n' "$BLOCK" > "$GITIGNORE"
fi

echo "WARNING: Chunking strategy updated. You must run MCP 'reindex' with force: true after starting the IDE."
echo "Done. Restart Cursor. MCP server key: $SERVER_KEY"
echo "Roo/Zoo Code: .roo/mcp.json and $ROO_RULE_REL updated — restart Roo Code if used."
echo "Fill in $ENV_FILE for cloud embedding profiles (RouterAI, DashScope, etc.)."
echo "For offline ONNX: set active_profile: local_multilingual and run reindex with force=true."
