#!/usr/bin/env bash
set -euo pipefail

TARGET_ROOT="${TARGET_ROOT:-$(pwd)}"
REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
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
  (cd "$REPO_ROOT" && go build -o "$DST_BIN" ./cmd/mcp-semantic-search-zvec-go)
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
