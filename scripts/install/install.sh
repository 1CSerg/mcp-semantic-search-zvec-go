#!/usr/bin/env bash
set -euo pipefail

# Usage:
#   install.sh                          # install/update the user app, add $PWD as a project
#   install.sh --target-root <dir>      # same, for another project
#   install.sh --install-app-only       # install/update the app, add no project
#   install.sh --migrate-legacy         # opt-in migration of a project-local install
#   install.sh --uninstall-app          # remove the user app (needs --force if projects remain)
#   install.sh --mcp-mode native        # legacy per-project install (unchanged behaviour)
#
# Default (global) mode: one application per user, one shared daemon, and one
# lightweight stdio proxy per MCP session. A project only stores project.yaml.

TARGET_ROOT="${TARGET_ROOT:-$(pwd)}"
FETCH_ONNX_MODEL="${FETCH_ONNX_MODEL:-0}"
REPLACE_CONFIG="${REPLACE_CONFIG:-0}"
MCP_MODE="${MCP_MODE:-global}"
INSTALL_APP_ONLY=0
MIGRATE_LEGACY=0
UNINSTALL_APP=0
FORCE=0
LEGACY_WORKSPACE_ID="${WORKSPACE_ID:-}"
LEGACY_DAEMON_URL="${DAEMON_URL:-http://127.0.0.1:8080}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --target-root) TARGET_ROOT="$2"; shift 2 ;;
    --install-app-only) INSTALL_APP_ONLY=1; shift ;;
    --migrate-legacy) MIGRATE_LEGACY=1; shift ;;
    --uninstall-app) UNINSTALL_APP=1; shift ;;
    --force) FORCE=1; shift ;;
    --mcp-mode) MCP_MODE="$2"; shift 2 ;;
    --fetch-onnx-model) FETCH_ONNX_MODEL=1; shift ;;
    --replace-config) REPLACE_CONFIG=1; shift ;;
    -h|--help) sed -n '2,14p' "$0"; exit 0 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
INSTALL_DIR="$TARGET_ROOT/.mcp-semantic-search-zvec-go"
BIN_DIR="$INSTALL_DIR/bin"
TEMPLATES="$REPO_ROOT/templates"
BINARY_NAME="mcp-semantic-search-zvec-go"
SERVER_KEY="semantic-search-zvec-go"
CURSOR_RULE_REL=".cursor/rules/semantic-search-zvec-go.mdc"
ROO_RULE_REL=".roo/rules/semantic-search-zvec-go.md"
ROO_MCP_REL=".roo/mcp.json"

# shellcheck source=../lib/global-install.sh
. "$REPO_ROOT/scripts/lib/global-install.sh"

version() {
  grep 'Version = ' "$REPO_ROOT/internal/version/version.go" | sed 's/.*"\(.*\)".*/\1/'
}

VERSION="$(version)"

copy_runtime_libs() {
  local bin_dir="$1"
  local install_version="$2"
  local repo_bin="$REPO_ROOT/bin"
  local zvec_tag
  zvec_tag="$(grep 'github.com/zvec-ai/zvec-go' "$REPO_ROOT/go.mod" | awk '{print $2}')"
  if [[ -z "$zvec_tag" ]]; then
    zvec_tag="v0.7.0"
  fi
  local runtime_marker="$bin_dir/.install-runtime-version"
  local expected_marker="${install_version}:${zvec_tag}"
  local need_refresh=1
  local lib_present=0
  case "$(uname -s)" in
    MINGW*|MSYS*|CYGWIN*) [[ -f "$bin_dir/zvec_c_api.dll" ]] && lib_present=1 ;;
    Linux*) [[ -f "$bin_dir/libzvec_c_api.so" ]] && lib_present=1 ;;
    Darwin*) [[ -f "$bin_dir/libzvec_c_api.dylib" ]] && lib_present=1 ;;
  esac
  if [[ -f "$runtime_marker" && "$lib_present" -eq 1 ]]; then
    marked="$(tr -d '\r\n' < "$runtime_marker")"
    if [[ "$marked" == "$expected_marker" ]]; then
      need_refresh=0
    fi
  fi
  if [[ "$need_refresh" -eq 0 ]]; then
    case "$(uname -s)" in
      MINGW*|MSYS*|CYGWIN*) [[ -f "$bin_dir/onnxruntime.dll" ]] && return 0 ;;
      Linux*) [[ -f "$bin_dir/libonnxruntime.so" || -f "$bin_dir/onnxruntime.dll" ]] && return 0 ;;
      Darwin*) [[ -f "$bin_dir/libonnxruntime.dylib" || -f "$bin_dir/onnxruntime.dll" ]] && return 0 ;;
    esac
  fi

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

  printf '%s' "$expected_marker" > "$runtime_marker"
}

# ---------------------------------------------------------------------------
# Global (one app per user) mode
# ---------------------------------------------------------------------------

resolve_repo_binary() {
  local src="$REPO_ROOT/bin/$BINARY_NAME"
  if [[ -x "$src" ]]; then
    printf '%s\n' "$src"
    return 0
  fi
  (
    cd "$REPO_ROOT"
    bash "$REPO_ROOT/scripts/fetch/fetch-zvec-libs.sh" > "$REPO_ROOT/.deps/zvec-lib.env"
    bash "$REPO_ROOT/scripts/fetch/fetch-onnx-runtime.sh" > "$REPO_ROOT/.deps/onnxruntime.env"
    # shellcheck disable=SC1091
    . "$REPO_ROOT/.deps/zvec-lib.env"
    # shellcheck disable=SC1091
    . "$REPO_ROOT/.deps/onnxruntime.env"
    CGO_ENABLED=1 LD_LIBRARY_PATH="${ZVEC_LIB_DIR}:${ORT_LIB_DIR}:${LD_LIBRARY_PATH:-}" \
      go build -tags "zvec,onnx,treesitter" -o "$src" ./cmd/mcp-semantic-search-zvec-go
  )
  [[ -x "$src" ]] || { echo "build did not produce $src" >&2; return 1; }
  printf '%s\n' "$src"
}

install_global_app() {
  local gdir
  gdir="$(global_dir)"
  global_init_layout "$gdir"
  echo "Installing the user-level application into ${gdir}"

  local src_bin
  src_bin="$(resolve_repo_binary)"
  install_global_binary "$src_bin" "$gdir"
  # Native libraries first, then the version gate: a cgo build cannot start
  # without them, so verifying the bare binary would fail on a clean machine.
  copy_runtime_libs "$G_BIN" "$VERSION"
  if ! verify_global_binary "$gdir"; then
    rollback_global_binary "$gdir"
    return 1
  fi
  printf '%s\n' "$VERSION" > "$G_VERSION_FILE"

  local merge_args=("$REPO_ROOT/scripts/install/merge-config.py" "$REPO_ROOT/config.yaml" "$G_CONFIG")
  if [[ "$REPLACE_CONFIG" == "1" ]]; then
    merge_args+=(--replace)
  fi
  local merge_rc=0
  set +e
  "$(python_bin)" "${merge_args[@]}"
  merge_rc=$?
  set -e
  if [[ $merge_rc -eq 2 ]]; then
    if [[ ! -f "$G_CONFIG" ]]; then
      cp -f "$REPO_ROOT/config.yaml" "$G_CONFIG"
      echo "created"
    else
      echo "WARNING: config.yaml preserved (install merge requires ruamel.yaml): pip install -r scripts/install/requirements.txt" >&2
    fi
  elif [[ $merge_rc -ne 0 ]]; then
    echo "merge-config.py failed with exit code $merge_rc" >&2
    return 1
  fi

  # daemon.yaml is created once; an existing one is a user decision.
  if [[ ! -f "$G_DAEMON_YAML" ]]; then
    write_daemon_yaml "$G_DAEMON_YAML"
    echo "Created daemon config: $G_DAEMON_YAML"
  fi

  if [[ "$FETCH_ONNX_MODEL" == "1" ]] || grep -q 'active_profile: local_multilingual' "$G_CONFIG" 2>/dev/null; then
    bash "$REPO_ROOT/scripts/fetch/fetch-onnx-model.sh" "$G_MODELS/paraphrase-multilingual-MiniLM-L12-v2"
  fi

  if [[ ! -f "$G_ENV_FILE" && -f "$TEMPLATES/env.example" ]]; then
    ( umask 077; cp -f "$TEMPLATES/env.example" "$G_ENV_FILE" )
    echo "Created secrets file: $G_ENV_FILE (global; never per project)"
  fi

  ensure_auth_token "$gdir" >/dev/null

  cat > "$gdir/install-manifest.json" <<EOF
{
  "mode": "global",
  "runtime": "go",
  "version": "${VERSION}",
  "installed_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "global_dir": "${gdir}",
  "server_key": "${SERVER_KEY}",
  "cursor_rule": "${CURSOR_RULE_REL}",
  "roo_rule": "${ROO_RULE_REL}",
  "roo_mcp": "${ROO_MCP_REL}"
}
EOF
  cp -f "$REPO_ROOT/scripts/install/uninstall.sh" "$gdir/uninstall.sh"
  chmod +x "$gdir/uninstall.sh"
  # The uninstall script needs the shared helpers; without them it cannot even
  # resolve the user-level directory (see resolve_lib_dir in uninstall.sh).
  mkdir -p "$gdir/lib"
  cp -f "$REPO_ROOT/scripts/lib/global-install.sh" "$gdir/lib/global-install.sh"
  printf '%s\n' "$gdir"
}

install_global_project_entry() {
  local root="$1" gdir="$2"
  global_paths "$gdir"
  root="$(cd "$root" && pwd)"

  local out rc=0
  set +e
  out="$(invoke_global_binary "$gdir" --project-add "$root" 2>&1)"
  rc=$?
  set -e
  echo "$out"
  if [[ $rc -ne 0 ]]; then
    echo "project-add failed for $root" >&2
    return 1
  fi

  local collection_id
  collection_id="$(project_collection_id "$root")"
  if [[ -z "$collection_id" ]]; then
    echo "project.yaml for $root has no collection_id after --project-add" >&2
    return 1
  fi

  local project_config
  project_config="$(project_config_path "$root")"
  mkdir -p "$root/.cursor" "$root/.roo"
  set_managed_mcp_entry "$root/.cursor/mcp.json" "$(native_path "$G_BINARY")" "$(native_path "$project_config")"
  set_managed_mcp_entry "$root/.roo/mcp.json" "$(native_path "$G_BINARY")" "$(native_path "$project_config")"

  local rule_src="$TEMPLATES/cursor-rules/semantic-search-zvec-go.mdc"
  [[ -f "$rule_src" ]] || { echo "cursor rule template not found: $rule_src" >&2; return 1; }
  mkdir -p "$root/.cursor/rules"
  cp -f "$rule_src" "$root/$CURSOR_RULE_REL"
  echo "Installed Cursor rule: $root/$CURSOR_RULE_REL"

  local roo_rule_src="$TEMPLATES/roo-rules/semantic-search-zvec-go.md"
  [[ -f "$roo_rule_src" ]] || { echo "roo rule template not found: $roo_rule_src" >&2; return 1; }
  mkdir -p "$root/.roo/rules"
  cp -f "$roo_rule_src" "$root/$ROO_RULE_REL"
  echo "Installed Roo rule: $root/$ROO_RULE_REL"

  local gitignore="$root/.gitignore"
  local block="# BEGIN mcp-semantic-search-zvec-go
.mcp-semantic-search-zvec-go/
# END mcp-semantic-search-zvec-go"
  if [[ -f "$gitignore" ]]; then
    grep -q "BEGIN mcp-semantic-search-zvec-go" "$gitignore" || printf '\n%s\n' "$block" >> "$gitignore"
  else
    printf '%s\n' "$block" > "$gitignore"
  fi

  echo "Project collection: $collection_id"
}

assert_no_legacy_install() {
  local root="$1"
  if ! legacy_project_install "$root"; then return 0; fi
  if [[ "$MIGRATE_LEGACY" == "1" ]]; then return 0; fi
  cat >&2 <<EOF
A legacy project-local install was found at $root/.mcp-semantic-search-zvec-go/bin.
The global install would create a second owner of this project's index.

Either:
  - re-run with --migrate-legacy to move this project to the user-level app
    (the old install is kept as a backup until you remove it), or
  - uninstall the project-local install first.
EOF
  return 1
}

invoke_legacy_migration() {
  local root="$1" gdir="$2"
  echo "Migrating legacy project install for $root"
  local legacy_bin="$INSTALL_DIR/bin/$BINARY_NAME"
  if [[ -x "$legacy_bin" ]]; then
    "$legacy_bin" --stop-stdio-for-workspace "$root" 2>&1 | sed 's/^/  /' || true
  fi
  local legacy_config="$INSTALL_DIR/config.yaml"
  global_paths "$gdir"
  if [[ -f "$legacy_config" ]]; then
    local merge_rc=0
    set +e
    "$(python_bin)" "$REPO_ROOT/scripts/install/merge-config.py" "$legacy_config" "$G_CONFIG" --no-overwrite-profiles
    merge_rc=$?
    set -e
    if [[ $merge_rc -eq 3 ]]; then
      echo "legacy profiles conflict with the global config; rename them in $legacy_config and re-run with --migrate-legacy" >&2
      return 1
    fi
  fi
  local legacy_env="$INSTALL_DIR/.env"
  if [[ -f "$legacy_env" ]]; then
    local added=0 key
    while IFS= read -r line; do
      key="$(printf '%s' "$line" | sed -n 's/^[[:space:]]*\([A-Za-z_][A-Za-z0-9_]*\)[[:space:]]*=.*/\1/p')"
      [[ -n "$key" ]] || continue
      if ! grep -qE "^[[:space:]]*${key}[[:space:]]*=" "$G_ENV_FILE" 2>/dev/null; then
        printf '%s\n' "$line" >> "$G_ENV_FILE"
        added=$((added + 1))
      fi
    done < "$legacy_env"
    echo "  merged $added secret name(s) into the global .env (values not printed)"
  fi
  echo "Legacy install kept as a backup at $INSTALL_DIR"
}

uninstall_global_app() {
  local gdir="$1"
  global_paths "$gdir"
  local regs
  regs="$(global_registrations "$gdir")"
  if [[ -n "$regs" && "$FORCE" != "1" ]]; then
    {
      echo "Refusing to remove the user-level application: projects are still registered:"
      printf '%s\n' "$regs" | sed 's/^/  /'
      echo
      echo "Uninstall those projects first, or re-run with --force. Collection data is preserved either way."
    } >&2
    return 1
  fi
  if [[ -x "$G_BINARY" ]]; then
    "$G_BINARY" --stop-daemon >/dev/null 2>&1 || true
    sleep 0.5
  fi
  rm -rf "$G_BIN" "$G_RUNTIME" 2>/dev/null || true
  rm -f "$G_CONFIG" "$G_DAEMON_YAML" "$G_AUTH_TOKEN" "$gdir/install-manifest.json" 2>/dev/null || true
  rm -f "$G_PROJECTS_DIR"/*.yaml "$G_PROJECTS_DIR"/*.yml 2>/dev/null || true
  echo "Removed the user-level application from $gdir"
  echo "Collection data preserved: $G_COLLECTIONS"
  echo "Secrets preserved: $G_ENV_FILE"
}

# ---------------------------------------------------------------------------
# Dispatch
# ---------------------------------------------------------------------------

if [[ "$UNINSTALL_APP" == "1" ]]; then
  uninstall_global_app "$(global_dir)"
  exit 0
fi

if [[ "$MCP_MODE" == "global" ]]; then
  GDIR="$(global_dir)"
  assert_no_legacy_install "$TARGET_ROOT"
  install_global_app >/dev/null
  # The previous binary is kept until every post-install step succeeded, so a
  # failure here leaves the working version in place.
  install_failed=0
  if [[ "$MIGRATE_LEGACY" == "1" ]]; then
    invoke_legacy_migration "$TARGET_ROOT" "$GDIR" || install_failed=1
  fi
  global_paths "$GDIR"
  if [[ $install_failed -eq 0 ]]; then
    if [[ "$INSTALL_APP_ONLY" == "1" ]]; then
      set +e
      out="$(invoke_global_binary "$GDIR" --ensure-daemon 2>&1)"
      rc=$?
      set -e
      if [[ $rc -ne 0 ]]; then
        echo "WARNING: the daemon did not start: $out" >&2
      else
        echo "$out"
      fi
      echo "Installed the user-level application only (no project added)."
      echo "Add a project later: install.sh --target-root <project>"
    else
      install_global_project_entry "$TARGET_ROOT" "$GDIR" || install_failed=1
    fi
  fi
  if [[ $install_failed -ne 0 ]]; then
    rollback_global_binary "$GDIR"
    echo "install failed; the previous application version (if any) was restored" >&2
    exit 1
  fi
  finalize_global_binary "$GDIR"
  echo ""
  echo "Application (one per user): $GDIR"
  echo "Global config:  $G_CONFIG"
  echo "Global secrets: $G_ENV_FILE  (never per project)"
  echo "Collections:    $G_COLLECTIONS"
  echo "Restart Cursor / Roo Code (MCP has no hot reload). Server key: $SERVER_KEY"
  echo "Fill in the global .env for cloud embedding profiles; then run MCP 'reindex' with force: true."
  exit 0
fi

# ---------------------------------------------------------------------------
# Legacy per-project mode (explicit --mcp-mode native)
# ---------------------------------------------------------------------------
echo "Installing mcp-semantic-search-zvec-go v${VERSION} into ${INSTALL_DIR} (legacy ${MCP_MODE} mode)"

mkdir -p "$BIN_DIR" "$INSTALL_DIR/data/index" "$INSTALL_DIR/data/logs" "$INSTALL_DIR/models"

merge_config_args=("$REPO_ROOT/scripts/install/merge-config.py" "$REPO_ROOT/config.yaml" "$INSTALL_DIR/config.yaml")
if [[ "$REPLACE_CONFIG" == "1" ]]; then
  merge_config_args+=(--replace)
fi
set +e
merge_out="$("$(python_bin)" "${merge_config_args[@]}" 2>&1)"
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
copy_runtime_libs "$BIN_DIR" "$VERSION"

printf '%s\n' "$(cd "$TARGET_ROOT" && pwd)" > "$BIN_DIR/workspace-root.txt"

cp -f "$REPO_ROOT/scripts/install/uninstall.sh" "$INSTALL_DIR/uninstall.sh"
chmod +x "$INSTALL_DIR/uninstall.sh"

CURSOR_DIR="$TARGET_ROOT/.cursor"
mkdir -p "$CURSOR_DIR"
MCP_JSON="$CURSOR_DIR/mcp.json"
FRAGMENT="$TEMPLATES/cursor-mcp-linux.fragment.json"
"$(python_bin)" - <<'PY' "$MCP_JSON" "$FRAGMENT" "$SERVER_KEY"
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
"$(python_bin)" - <<'PY' "$ROO_MCP_JSON" "$FRAGMENT" "$SERVER_KEY"
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
