#!/usr/bin/env bash
set -euo pipefail

# Usage:
#   uninstall.sh                    # remove this project's managed entries + registration
#   uninstall.sh --keep-data        # (legacy) keep index data in the project install dir
#   uninstall.sh --uninstall-app    # remove the user-level application
#   uninstall.sh --uninstall-app --force   # ... even while projects are registered
#
# In the global layout a project only holds project.yaml, so uninstalling it
# removes managed entries, rules and the registration. Collection data stays.

KEEP_DATA="${KEEP_DATA:-0}"
UNINSTALL_APP=0
FORCE=0
SERVER_KEY="semantic-search-zvec-go"
DEFAULT_CURSOR_RULE_REL=".cursor/rules/semantic-search-zvec-go.mdc"
DEFAULT_ROO_RULE_REL=".roo/rules/semantic-search-zvec-go.md"
DEFAULT_ROO_MCP_REL=".roo/mcp.json"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --keep-data) KEEP_DATA=1; shift ;;
    --uninstall-app) UNINSTALL_APP=1; shift ;;
    --force) FORCE=1; shift ;;
    --target-root) TARGET_ROOT="$2"; shift 2 ;;
    -h|--help) sed -n '2,12p' "$0"; exit 0 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
if [[ "$(basename "$SCRIPT_DIR")" == ".mcp-semantic-search-zvec-go" ]]; then
  INSTALL_DIR="$SCRIPT_DIR"
  TARGET_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
else
  TARGET_ROOT="${TARGET_ROOT:-$(pwd)}"
  INSTALL_DIR="$TARGET_ROOT/.mcp-semantic-search-zvec-go"
fi

REPO_ROOT="$(cd "$SCRIPT_DIR/../.." 2>/dev/null && pwd || echo "")"
# The helpers live in the clone (scripts/lib) when this runs from the repository,
# and in <userdir>/lib when it was installed with the application. Both must work:
# the documented app uninstall runs the installed copy.
GLOBAL_LIB=""
for candidate in \
  "$REPO_ROOT/scripts/lib/global-install.sh" \
  "$SCRIPT_DIR/../lib/global-install.sh" \
  "$SCRIPT_DIR/lib/global-install.sh" \
  "$INSTALL_DIR/bin/global-install.sh"; do
  if [[ -n "$candidate" && -f "$candidate" ]]; then
    GLOBAL_LIB="$candidate"
    break
  fi
done
if [[ -n "$GLOBAL_LIB" ]]; then
  # shellcheck source=/dev/null
  . "$GLOBAL_LIB"
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

# ---------------------------------------------------------------------------
# Global layout handling
# ---------------------------------------------------------------------------
GDIR=""
if declare -f global_dir >/dev/null 2>&1; then
  GDIR="$(global_dir 2>/dev/null || true)"
fi

if [[ "$UNINSTALL_APP" == "1" ]]; then
  [[ -n "$GDIR" ]] || { echo "cannot determine the user-level install directory (set MCP_GLOBAL_DIR or XDG_DATA_HOME)" >&2; exit 1; }
  global_paths "$GDIR"
  regs="$(global_registrations "$GDIR")"
  if [[ -n "$regs" && "$FORCE" != "1" ]]; then
    {
      echo "Refusing to remove the user-level application: projects are still registered:"
      printf '%s\n' "$regs" | sed 's/^/  /'
      echo
      echo "Uninstall those projects first, or re-run with --force. Collection data is preserved either way."
    } >&2
    exit 1
  fi
  if [[ -x "$G_BINARY" ]]; then
    "$G_BINARY" --stop-daemon >/dev/null 2>&1 || true
    sleep 0.5
  fi
  rm -rf "$G_BIN" "$G_RUNTIME" 2>/dev/null || true
  rm -f "$G_CONFIG" "$G_DAEMON_YAML" "$G_AUTH_TOKEN" "$GDIR/install-manifest.json" 2>/dev/null || true
  rm -f "$G_PROJECTS_DIR"/*.yaml "$G_PROJECTS_DIR"/*.yml 2>/dev/null || true
  echo "Removed the user-level application from $GDIR"
  echo "Collection data preserved: $G_COLLECTIONS"
  echo "Secrets preserved: $G_ENV_FILE"
  exit 0
fi

PROJECT_CONFIG="$(printf '%s' "$TARGET_ROOT/.mcp-semantic-search-zvec-go/project.yaml")"
if [[ -f "$PROJECT_CONFIG" && -n "$GDIR" ]] && ! legacy_project_install "$TARGET_ROOT"; then
  # Global project: only this project's managed artifacts are removed.
  global_paths "$GDIR"
  unregistered=0
  if [[ -x "$G_BINARY" ]]; then
    if run_step "unregister project" invoke_global_binary "$GDIR" --project-remove "$TARGET_ROOT"; then
      unregistered=1
    fi
  else
    # No binary: nothing can unregister, so the registry must keep its entry.
    unregistered=0
    echo "WARN: no installed binary at $G_BINARY; the registration is kept" >&2
  fi
  run_step "remove mcp.json entry" remove_mcp_json_entry "$TARGET_ROOT/.cursor/mcp.json" "$SERVER_KEY"
  run_step "remove Roo mcp.json entry" remove_mcp_json_entry "$TARGET_ROOT/$DEFAULT_ROO_MCP_REL" "$SERVER_KEY"
  for rel in "$DEFAULT_CURSOR_RULE_REL" "$DEFAULT_ROO_RULE_REL"; do
    rule_path="$TARGET_ROOT/$rel"
    if managed_rule_file "$rule_path"; then
      run_step "remove rule $rel" rm -f "$rule_path"
    elif [[ -f "$rule_path" ]]; then
      echo "Skipped rule (not install-managed): $rule_path"
    fi
  done
  # Deleting project.yaml while the registration survives leaves projects.d
  # pointing at a root with no config, which makes the whole registry invalid and
  # the daemon refuse to start.
  if [[ $unregistered -eq 1 ]]; then
    run_step "remove project config" rm -f "$PROJECT_CONFIG"
  else
    echo "WARN: kept $PROJECT_CONFIG: the project is still registered" >&2
    STEP_ERRORS+=("project config kept (still registered)")
  fi
  run_step "remove .gitignore block" remove_gitignore_block "$TARGET_ROOT/.gitignore"
  if [[ "${#STEP_ERRORS[@]}" -gt 0 ]]; then
    echo "Uninstall completed with ${#STEP_ERRORS[@]} error(s)." >&2
    exit 1
  fi
  echo "Uninstalled $SERVER_KEY from $TARGET_ROOT. Collection data preserved. Restart Cursor."
  exit 0
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
