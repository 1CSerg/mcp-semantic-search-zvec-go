#!/usr/bin/env bash
set -euo pipefail

KEEP_DATA="${KEEP_DATA:-0}"
SERVER_KEY="semantic-search-zvec-go"
DEFAULT_CURSOR_RULE_REL=".cursor/rules/semantic-search-zvec-go.mdc"

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
if [[ "$(basename "$SCRIPT_DIR")" == ".mcp-semantic-search-zvec-go" ]]; then
  INSTALL_DIR="$SCRIPT_DIR"
  TARGET_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
else
  TARGET_ROOT="${TARGET_ROOT:-$(pwd)}"
  INSTALL_DIR="$TARGET_ROOT/.mcp-semantic-search-zvec-go"
fi

stop_stale_processes() {
  local workspace="$1"
  if ! command -v pgrep >/dev/null 2>&1; then
    return 0
  fi
  while IFS= read -r pid; do
    [[ -z "$pid" ]] && continue
    local cmdline
    cmdline="$(ps -p "$pid" -o args= 2>/dev/null || true)"
    if [[ "$cmdline" == *"$workspace"* && "$cmdline" == *"mcp-semantic-search-zvec-go"* ]]; then
      echo "Stopping PID $pid"
      kill -TERM "$pid" 2>/dev/null || true
    fi
  done < <(pgrep -f 'mcp-semantic-search-zvec-go' 2>/dev/null || true)
}

remove_gitignore_block() {
  local path="$1"
  [[ -f "$path" ]] || return 0
  grep -q "BEGIN mcp-semantic-search-zvec-go" "$path" || return 0

  python3 - <<'PY' "$path"
import re, sys
path = sys.argv[1]
with open(path, encoding="utf-8") as f:
    content = f.read()
block = r"(?:\n)?# BEGIN mcp-semantic-search-zvec-go\n\.mcp-semantic-search-zvec-go/\n# END mcp-semantic-search-zvec-go\n?"
new_content = re.sub(block, "", content, count=1, flags=re.MULTILINE).rstrip()
if not new_content.strip():
    import os
    os.remove(path)
    print(f"Removed .gitignore (contained only mcp-semantic-search-zvec-go block)")
else:
    with open(path, "w", encoding="utf-8", newline="\n") as f:
        f.write(new_content + "\n")
    print(f"Removed mcp-semantic-search-zvec-go block from {path}")
PY
}

remove_cursor_rule() {
  local target_root="$1"
  local install_dir="$2"
  local rel="$DEFAULT_CURSOR_RULE_REL"
  local manifest="$install_dir/install-manifest.json"
  if [[ -f "$manifest" ]]; then
    rel="$(python3 - <<'PY' "$manifest"
import json, sys
path = sys.argv[1]
try:
    with open(path, encoding="utf-8") as f:
        obj = json.load(f)
    print(obj.get("cursor_rule") or ".cursor/rules/semantic-search-zvec-go.mdc")
except Exception:
    print(".cursor/rules/semantic-search-zvec-go.mdc")
PY
)"
  fi
  local rule_path="$target_root/$rel"
  [[ -f "$rule_path" ]] || return 0
  if ! grep -q "managedBy: mcp-semantic-search-zvec-go" "$rule_path"; then
    echo "Skipped Cursor rule (not install-managed): $rule_path"
    return 0
  fi
  rm -f "$rule_path"
  echo "Removed Cursor rule: $rule_path"
}

stop_stale_processes "$TARGET_ROOT"

MCP_JSON="$TARGET_ROOT/.cursor/mcp.json"
if [[ -f "$MCP_JSON" ]]; then
  python3 - <<'PY' "$MCP_JSON" "$SERVER_KEY"
import json, sys
mcp_path, key = sys.argv[1:3]
with open(mcp_path, encoding="utf-8") as f:
    obj = json.load(f)
servers = obj.get("mcpServers") or {}
if key in servers:
    del servers[key]
    obj["mcpServers"] = servers
    with open(mcp_path, "w", encoding="utf-8") as f:
        json.dump(obj, f, indent=2)
        f.write("\n")
PY
fi

remove_cursor_rule "$TARGET_ROOT" "$INSTALL_DIR"

if [[ -d "$INSTALL_DIR" ]]; then
  if [[ "$KEEP_DATA" == "1" ]]; then
    find "$INSTALL_DIR" -mindepth 1 -maxdepth 1 \
      ! -name data ! -name models -exec rm -rf {} +
  else
    rm -rf "$INSTALL_DIR"
  fi
fi

remove_gitignore_block "$TARGET_ROOT/.gitignore"

echo "Uninstalled $SERVER_KEY. Restart Cursor."
