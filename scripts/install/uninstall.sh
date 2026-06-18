#!/usr/bin/env bash
set -euo pipefail

KEEP_DATA="${KEEP_DATA:-0}"
SERVER_KEY="semantic-search-zvec-go"
DEFAULT_CURSOR_RULE_REL=".cursor/rules/semantic-search-zvec-go.mdc"
DEFAULT_ROO_RULE_REL=".roo/rules/semantic-search-zvec-go.md"
DEFAULT_ROO_MCP_REL=".roo/mcp.json"

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
if [[ "$(basename "$SCRIPT_DIR")" == ".mcp-semantic-search-zvec-go" ]]; then
  INSTALL_DIR="$SCRIPT_DIR"
  TARGET_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
else
  TARGET_ROOT="${TARGET_ROOT:-$(pwd)}"
  INSTALL_DIR="$TARGET_ROOT/.mcp-semantic-search-zvec-go"
fi

STEP_ERRORS=()

run_step() {
  local name="$1"
  shift
  if "$@"; then
    echo "OK: $name"
  else
    echo "WARN: $name failed" >&2
    STEP_ERRORS+=("$name")
  fi
}

stop_mcp_processes() {
  local workspace="$1"
  local install_dir="$2"
  local index_dir="$install_dir/data/index"
  local exe="$install_dir/bin/mcp-semantic-search-zvec-go"

  if [[ -x "$exe" ]]; then
    if ! "$exe" --stop-stdio-for-workspace "$workspace" --index-dir "$index_dir"; then
      return 1
    fi
    return 0
  fi

  echo "WARN: binary not found ($exe); using pgrep fallback" >&2
  stop_stale_processes_fallback "$workspace" "$install_dir"
}

stop_stale_processes_fallback() {
  local workspace="$1"
  local install_dir="$2"
  if ! command -v pgrep >/dev/null 2>&1; then
    echo "WARN: pgrep not available; close IDE before uninstall if files are locked" >&2
    return 0
  fi
  local stopped=0
  while IFS= read -r pid; do
    [[ -z "$pid" ]] && continue
    local cmdline
    cmdline="$(ps -p "$pid" -o args= 2>/dev/null || true)"
    if [[ "$cmdline" == *"mcp-semantic-search-zvec-go"* && ( "$cmdline" == *"$workspace"* || "$cmdline" == *"$install_dir"* ) ]]; then
      echo "Stopping PID $pid"
      kill -TERM "$pid" 2>/dev/null || true
      stopped=1
    fi
  done < <(pgrep -f 'mcp-semantic-search-zvec-go' 2>/dev/null || true)
  if [[ "$stopped" == "0" ]]; then
    echo "WARN: no MCP processes matched workspace" >&2
  fi
  sleep 0.4
  local stdio_lock="$install_dir/data/index/stdio.lock"
  if [[ -f "$stdio_lock" ]]; then
    read -r holder _ < "$stdio_lock" || true
    if [[ "$holder" =~ ^[0-9]+$ ]]; then
      kill -TERM "$holder" 2>/dev/null || true
      sleep 0.4
    fi
    rm -f "$stdio_lock" 2>/dev/null || true
  fi
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

remove_roo_rule() {
  local target_root="$1"
  local install_dir="$2"
  local rel="$DEFAULT_ROO_RULE_REL"
  local manifest="$install_dir/install-manifest.json"
  if [[ -f "$manifest" ]]; then
    rel="$(python3 - <<'PY' "$manifest"
import json, sys
path = sys.argv[1]
try:
    with open(path, encoding="utf-8") as f:
        obj = json.load(f)
    print(obj.get("roo_rule") or ".roo/rules/semantic-search-zvec-go.md")
except Exception:
    print(".roo/rules/semantic-search-zvec-go.md")
PY
)"
  fi
  local rule_path="$target_root/$rel"
  [[ -f "$rule_path" ]] || return 0
  if ! grep -q "managedBy: mcp-semantic-search-zvec-go" "$rule_path"; then
    echo "Skipped Roo rule (not install-managed): $rule_path"
    return 0
  fi
  rm -f "$rule_path"
  echo "Removed Roo rule: $rule_path"
}

remove_mcp_json_entry() {
  local mcp_json="$1"
  local key="$2"
  [[ -f "$mcp_json" ]] || return 0
  python3 - <<'PY' "$mcp_json" "$key"
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
    print(f"Removed {key} from {mcp_path}")
PY
}

remove_install_dir() {
  local install_dir="$1"
  [[ -d "$install_dir" ]] || return 0
  if [[ "$KEEP_DATA" == "1" ]]; then
    find "$install_dir" -mindepth 1 -maxdepth 1 \
      ! -name data ! -name models -exec rm -rf {} +
    return 0
  fi
  if [[ "$SCRIPT_DIR" == "$install_dir"* ]]; then
    find "$install_dir" -mindepth 1 -maxdepth 1 ! -name "$(basename "$0")" -exec rm -rf {} +
    (
      sleep 1
      rm -f "$SCRIPT_DIR/$(basename "$0")" 2>/dev/null || true
      rm -rf "$install_dir" 2>/dev/null || true
    ) &
    echo "Scheduled install directory cleanup: $install_dir"
    return 0
  fi
  rm -rf "$install_dir"
  echo "Removed install directory: $install_dir"
}

LEGACY_STAGING_DIR=""
MANIFEST="$INSTALL_DIR/install-manifest.json"
if [[ -f "$MANIFEST" ]]; then
  LEGACY_STAGING_DIR="$(python3 - <<'PY' "$MANIFEST"
import json, sys
path = sys.argv[1]
try:
    with open(path, encoding="utf-8") as f:
        obj = json.load(f)
    print(obj.get("cursor_staging_dir") or "")
except Exception:
    print("")
PY
)"
fi

run_step "stop MCP processes" stop_mcp_processes "$TARGET_ROOT" "$INSTALL_DIR"

MCP_JSON="$TARGET_ROOT/.cursor/mcp.json"
run_step "remove mcp.json entry" remove_mcp_json_entry "$MCP_JSON" "$SERVER_KEY"

run_step "remove Cursor rule" remove_cursor_rule "$TARGET_ROOT" "$INSTALL_DIR"

ROO_MCP_JSON="$TARGET_ROOT/$DEFAULT_ROO_MCP_REL"
if [[ -f "$MANIFEST" ]]; then
  ROO_MCP_JSON="$TARGET_ROOT/$(python3 - <<'PY' "$MANIFEST"
import json, sys
path = sys.argv[1]
try:
    with open(path, encoding="utf-8") as f:
        obj = json.load(f)
    print(obj.get("roo_mcp") or ".roo/mcp.json")
except Exception:
    print(".roo/mcp.json")
PY
)"
fi
run_step "remove Roo mcp.json entry" remove_mcp_json_entry "$ROO_MCP_JSON" "$SERVER_KEY"

run_step "remove Roo rule" remove_roo_rule "$TARGET_ROOT" "$INSTALL_DIR"

run_step "remove install directory" remove_install_dir "$INSTALL_DIR"

if [[ -n "$LEGACY_STAGING_DIR" && -d "$LEGACY_STAGING_DIR" ]]; then
  run_step "remove legacy staging" rm -rf "$LEGACY_STAGING_DIR"
fi

run_step "remove .gitignore block" remove_gitignore_block "$TARGET_ROOT/.gitignore"

if [[ "${#STEP_ERRORS[@]}" -gt 0 ]]; then
  echo "Uninstall completed with ${#STEP_ERRORS[@]} error(s). Close IDE and retry." >&2
  exit 1
fi

echo "Uninstalled $SERVER_KEY. Restart Cursor."
