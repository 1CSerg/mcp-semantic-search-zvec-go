#!/usr/bin/env bash
# Shared helpers for the user-level ("global") install layout.
#
# One application per user owns every collection; each project only stores a
# project.yaml with its collection_id. Sourced by install.sh, uninstall.sh and the
# smoke scripts so the layout is defined exactly once.
#
# Layout under $MCP_GLOBAL_DIR (default $XDG_DATA_HOME/mcp-semantic-search-zvec-go):
#   bin/                      the single installed binary + native libraries
#   config.yaml               embeddings, defaults, logging
#   daemon.yaml               loopback listener, resource caps
#   .env                      user secrets (never per project)
#   auth.token                local Bearer token for proxy <-> daemon
#   projects.d/<id>.yaml      local registry: id, root, description
#   runtime/daemon.lock       singleton daemon lock
#   data/collections/<id>/    index per collection
#   data/models/              shared model files
#   data/logs/                daemon/proxy/crash logs

GLOBAL_APP_NAME="mcp-semantic-search-zvec-go"
GLOBAL_SERVER_KEY="semantic-search-zvec-go"
GLOBAL_MANAGED_MARKER="managedBy: mcp-semantic-search-zvec-go"
GLOBAL_CURSOR_RULE_REL=".cursor/rules/semantic-search-zvec-go.mdc"
GLOBAL_ROO_RULE_REL=".roo/rules/semantic-search-zvec-go.md"

# On Windows (Git Bash / MSYS / Cygwin) the binary must carry the .exe suffix:
# exec with an absolute path does not append PATHEXT, so an extensionless file
# would exist but never start.
case "$(uname -s 2>/dev/null || echo unknown)" in
  MINGW*|MSYS*|CYGWIN*) GLOBAL_BINARY_NAME="mcp-semantic-search-zvec-go.exe" ;;
  *) GLOBAL_BINARY_NAME="mcp-semantic-search-zvec-go" ;;
esac

# global_dir prints the resolved user-level install directory.
# MCP_GLOBAL_DIR must be absolute so derived paths do not depend on the cwd.
global_dir() {
  if [[ -n "${MCP_GLOBAL_DIR:-}" ]]; then
    if [[ "${MCP_GLOBAL_DIR}" != /* ]]; then
      echo "MCP_GLOBAL_DIR must be an absolute path, got '${MCP_GLOBAL_DIR}'" >&2
      return 1
    fi
    printf '%s\n' "${MCP_GLOBAL_DIR}"
    return 0
  fi
  if [[ -n "${XDG_DATA_HOME:-}" ]]; then
    printf '%s\n' "${XDG_DATA_HOME}/${GLOBAL_APP_NAME}"
    return 0
  fi
  printf '%s\n' "${HOME}/.local/share/${GLOBAL_APP_NAME}"
}

# global_paths sets G_* variables for the given (or resolved) global dir.
global_paths() {
  local dir="${1:-$(global_dir)}"
  G_ROOT="$dir"
  G_BIN="$dir/bin"
  G_CONFIG="$dir/config.yaml"
  G_DAEMON_YAML="$dir/daemon.yaml"
  G_ENV_FILE="$dir/.env"
  G_AUTH_TOKEN="$dir/auth.token"
  G_PROJECTS_DIR="$dir/projects.d"
  G_RUNTIME="$dir/runtime"
  G_COLLECTIONS="$dir/data/collections"
  G_MODELS="$dir/data/models"
  G_LOGS="$dir/data/logs"
  G_BINARY="$dir/bin/${GLOBAL_BINARY_NAME}"
  G_VERSION_FILE="$dir/bin/installed-version.txt"
}

global_init_layout() {
  global_paths "${1:-}"
  local dir
  for dir in "$G_ROOT" "$G_BIN" "$G_PROJECTS_DIR" "$G_RUNTIME" "$G_COLLECTIONS" "$G_MODELS" "$G_LOGS"; do
    mkdir -p "$dir"
    chmod 700 "$dir" 2>/dev/null || true
  done
}

project_config_path() {
  printf '%s\n' "$1/.mcp-semantic-search-zvec-go/project.yaml"
}

project_collection_id() {
  local path
  path="$(project_config_path "$1")"
  [[ -f "$path" ]] || return 0
  sed -n 's/^[[:space:]]*collection_id:[[:space:]]*"\{0,1\}\([A-Za-z0-9][A-Za-z0-9._-]*\)"\{0,1\}[[:space:]]*$/\1/p' "$path" | head -n1
}

# legacy_project_install reports whether a project still holds its own binary.
legacy_project_install() {
  local root="$1"
  [[ -x "$root/.mcp-semantic-search-zvec-go/bin/${GLOBAL_BINARY_NAME}" ]]
}

write_daemon_yaml() {
  # Loopback only: the user-level daemon is local and its token never leaves the host.
  # A caller may pass an explicit loopback address (smoke tests use a free port so
  # they never collide with the real install or leave a stray daemon).
  local path="$1"
  local max_open="${2:-10}"
  local http_addr="${3:-127.0.0.1:8080}"
  if [[ ! "$http_addr" =~ ^(127\.0\.0\.1|\[::1\]|localhost):[0-9]+$ ]]; then
    echo "write_daemon_yaml: HTTPAddr must be loopback, got '$http_addr'" >&2
    return 1
  fi
  cat > "$path" <<EOF
# User-level daemon for ${GLOBAL_APP_NAME}.
# One daemon owns every project collection. Loopback only.
http_addr: ${http_addr}
max_open_workspaces: ${max_open}
auto_index_on_open: true
EOF
}

# free_loopback_port prints an unused loopback TCP port for isolated smoke runs.
# Dependency-free: a failed /dev/tcp connect means nothing is listening there.
free_loopback_port() {
  local attempt port
  for attempt in $(seq 1 200); do
    port=$(( 20000 + (RANDOM * 32768 + RANDOM) % 40000 ))
    if ! (exec 3<>"/dev/tcp/127.0.0.1/${port}") 2>/dev/null; then
      printf '%s\n' "$port"
      return 0
    fi
    exec 3>&- 2>/dev/null || true
  done
  echo "free_loopback_port: could not find a free port" >&2
  return 1
}

ensure_auth_token() {
  global_paths "${1:-}"
  if [[ -f "$G_AUTH_TOKEN" ]]; then
    local existing
    existing="$(tr -d '\r\n' < "$G_AUTH_TOKEN")"
    if [[ ${#existing} -ge 32 ]]; then
      printf '%s\n' "$existing"
      return 0
    fi
    echo "auth.token at $G_AUTH_TOKEN is corrupt; delete it and re-run install" >&2
    return 1
  fi
  local token
  token="$(head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n')"
  ( umask 077; printf '%s\n' "$token" > "$G_AUTH_TOKEN" )
  chmod 600 "$G_AUTH_TOKEN" 2>/dev/null || true
  printf '%s\n' "$token"
}

# install_global_binary installs or replaces the single application binary with
# staging, backup and rollback.
#
# The new binary is NOT executed here. A native build cannot start without its
# shared libraries, and on a first install $G_BIN is still empty, so a version
# gate at this point would fail on every clean machine. The caller verifies with
# verify_global_binary after the libraries are in place, and only then calls
# finalize_global_binary (which drops the backup).
#
# Sets G_INSTALL_BACKUP to the previous binary path ("" when there was none) so
# the caller can roll back if a later step fails.
install_global_binary() {
  local source_bin="$1" dir="${2:-$(global_dir)}"
  global_paths "$dir"
  [[ -f "$source_bin" ]] || { echo "source binary not found: $source_bin" >&2; return 1; }
  mkdir -p "$G_BIN"

  local staging="${G_BINARY}.staging"
  cp -f "$source_bin" "$staging"
  chmod +x "$staging"

  # Ask a running daemon to stop gracefully; a busy proxy would keep the file
  # mapped and must be closed by the user, never force-killed.
  if [[ -f "$G_BINARY" ]]; then
    "$G_BINARY" --stop-daemon >/dev/null 2>&1 || true
    sleep 0.5
  fi

  local backup="${G_BINARY}.previous"
  G_INSTALL_BACKUP=""
  if [[ -f "$G_BINARY" ]]; then
    rm -f "$backup"
    mv -f "$G_BINARY" "$backup"
    G_INSTALL_BACKUP="$backup"
  fi
  if ! mv -f "$staging" "$G_BINARY"; then
    rollback_global_binary "$dir"
    echo "could not move the new binary into place" >&2
    return 1
  fi
  chmod +x "$G_BINARY"
}

# verify_global_binary runs --version on the installed binary. Call it only after
# the native libraries are beside the EXE.
verify_global_binary() {
  local dir="${1:-$(global_dir)}"
  global_paths "$dir"
  [[ -x "$G_BINARY" ]] || { echo "installed binary not found: $G_BINARY" >&2; return 1; }
  if ! "$G_BINARY" --version >/dev/null 2>&1; then
    echo "installed binary $G_BINARY does not start (missing native libraries?)" >&2
    return 1
  fi
}

# rollback_global_binary restores the previous binary (if any) and removes the
# broken one. Safe to call when no backup exists: it just removes the new binary.
rollback_global_binary() {
  local dir="${1:-$(global_dir)}"
  global_paths "$dir"
  local backup="${G_INSTALL_BACKUP:-${G_BINARY}.previous}"
  rm -f "$G_BINARY"
  if [[ -n "$backup" && -f "$backup" ]]; then
    mv -f "$backup" "$G_BINARY"
    chmod +x "$G_BINARY"
    echo "install rolled back to the previous binary" >&2
    G_INSTALL_BACKUP=""
  fi
}

# finalize_global_binary drops the backup once every post-install check passed.
finalize_global_binary() {
  local dir="${1:-$(global_dir)}"
  global_paths "$dir"
  rm -f "${G_BINARY}.previous"
  G_INSTALL_BACKUP=""
}

write_project_config() {
  # Portable project.yaml: no root, no index path, no secret.
  local root="$1" collection_id="$2"
  local path dir
  path="$(project_config_path "$root")"
  dir="$(dirname "$path")"
  mkdir -p "$dir"
  cat > "$path" <<EOF
schema_version: 1
collection_id: ${collection_id}
EOF
  printf '%s\n' "$path"
}

# set_managed_mcp_entry writes only the managed server key in a mcp.json.
set_managed_mcp_entry() {
  local mcp_path="$1" binary="$2" project_config="$3"
  "$(python_bin)" - "$mcp_path" "$binary" "$project_config" "$GLOBAL_SERVER_KEY" <<'PY'
import json, os, sys
mcp_path, binary, project_config, key = sys.argv[1:5]
obj = {}
if os.path.exists(mcp_path):
    try:
        with open(mcp_path, encoding="utf-8") as fh:
            obj = json.load(fh)
    except json.JSONDecodeError as exc:
        raise SystemExit(f"cannot parse {mcp_path} as JSON; fix or remove it first ({exc})")
servers = obj.get("mcpServers") or {}
servers[key] = {"command": binary, "args": ["--stdio-proxy", "--project-config", project_config]}
obj["mcpServers"] = servers
os.makedirs(os.path.dirname(mcp_path) or ".", exist_ok=True)
with open(mcp_path, "w", encoding="utf-8") as fh:
    json.dump(obj, fh, indent=2)
    fh.write("\n")
PY
}

# remove_managed_mcp_entry removes only the managed server key.
remove_managed_mcp_entry() {
  local mcp_path="$1"
  [[ -f "$mcp_path" ]] || return 0
  "$(python_bin)" - "$mcp_path" "$GLOBAL_SERVER_KEY" <<'PY'
import json, sys
mcp_path, key = sys.argv[1:3]
try:
    with open(mcp_path, encoding="utf-8") as fh:
        obj = json.load(fh)
except json.JSONDecodeError:
    raise SystemExit(0)
servers = obj.get("mcpServers") or {}
servers.pop(key, None)
obj["mcpServers"] = servers
with open(mcp_path, "w", encoding="utf-8") as fh:
    json.dump(obj, fh, indent=2)
    fh.write("\n")
PY
}

# managed_rule_file reports whether a rule file carries the install marker.
managed_rule_file() {
  local path="$1"
  [[ -f "$path" ]] || return 1
  head -n 12 "$path" | grep -qF "$GLOBAL_MANAGED_MARKER"
}

# global_registrations prints "id<TAB>root" for each projects.d entry.
global_registrations() {
  local dir="${1:-$(global_dir)}/projects.d"
  [[ -d "$dir" ]] || return 0
  local file id root
  for file in "$dir"/*.yaml "$dir"/*.yml; do
    [[ -f "$file" ]] || continue
    id="$(sed -n 's/^id:[[:space:]]*//p' "$file" | head -n1 | tr -d '\r')"
    root="$(sed -n 's/^root:[[:space:]]*//p' "$file" | head -n1 | tr -d '\r')"
    [[ -n "$id" ]] && printf '%s\t%s\n' "$id" "$root"
  done
}

# python_bin prints a usable Python interpreter (python3, else python).
# Git Bash on Windows often ships python3 as a failing Store alias.
python_bin() {
  local candidate
  for candidate in python3 python; do
    if command -v "$candidate" >/dev/null 2>&1 && "$candidate" -c 'print(1)' >/dev/null 2>&1; then
      printf '%s\n' "$candidate"
      return 0
    fi
  done
  return 1
}

# native_path converts a POSIX path to the form the Go binary understands.
# Under Git Bash/MSYS the binary is a Windows executable, so /tmp/x is not a path
# it can resolve; cygpath -w translates it. On Linux this is a no-op.
native_path() {
  local path="$1"
  case "$(uname -s 2>/dev/null || echo unknown)" in
    MINGW*|MSYS*|CYGWIN*)
      if command -v cygpath >/dev/null 2>&1; then
        cygpath -w "$path"
        return 0
      fi
      ;;
  esac
  printf '%s\n' "$path"
}

# invoke_global_binary runs the installed binary with MCP_GLOBAL_DIR pinned to
# the user-level install.
invoke_global_binary() {
  local dir="$1"; shift
  global_paths "$dir"
  [[ -x "$G_BINARY" ]] || { echo "installed binary not found: $G_BINARY" >&2; return 1; }
  MCP_GLOBAL_DIR="$(native_path "$dir")" "$G_BINARY" "$@"
}
