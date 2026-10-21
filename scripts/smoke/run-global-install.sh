#!/usr/bin/env bash
# Global-install acceptance smoke: one application, N projects, isolated data.
#
# Runs entirely inside a temporary MCP_GLOBAL_DIR and temporary project roots.
# It never touches the real user-level install, real project configs or real
# indexes, and it never prints secrets.
#
# Usage:
#   bash scripts/smoke/run-global-install.sh [--binary <path>] [--keep-temp]
#
# Exit codes: 0 all checks passed, 1 a check failed, 2 bad invocation.

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
# shellcheck source=../lib/global-install.sh
. "$REPO_ROOT/scripts/lib/global-install.sh"

BINARY=""
KEEP_TEMP=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --binary) BINARY="$2"; shift 2 ;;
    --keep-temp) KEEP_TEMP=1; shift ;;
    -h|--help) sed -n '2,11p' "$0"; exit 0 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

CHECKS=0
FAILURES=()

check() {
  local name="$1"
  shift
  CHECKS=$((CHECKS + 1))
  if "$@" >/dev/null 2>&1; then
    echo "  PASS $name"
  else
    echo "  FAIL $name"
    FAILURES+=("$name")
  fi
}

# check_ok runs a command and prints its output when it fails, so a FAIL never
# hides the underlying error message.
check_ok() {
  local name="$1"
  shift
  CHECKS=$((CHECKS + 1))
  local out rc
  out="$("$@" 2>&1)"
  rc=$?
  if [[ $rc -eq 0 ]]; then
    echo "  PASS $name"
  else
    echo "  FAIL $name"
    printf '    output: %s\n' "${out//$'\n'/$'\n'    }"
    FAILURES+=("$name")
  fi
}

temp_dir() {
  mktemp -d "${TMPDIR:-/tmp}/$1-XXXXXX"
}

GLOBAL_TEMP="$(temp_dir mcp-global-smoke)"
PROJECT_A="$(temp_dir mcp-proj-a)"
PROJECT_B="$(temp_dir mcp-proj-b)"
PROJECT_C="$(temp_dir mcp-proj-c)"

cleanup() {
  # Stop any daemon this smoke started before deleting its directory: a stray
  # detached daemon would keep holding the port and the (now removed) index.
  if [[ -d "$GLOBAL_TEMP" ]]; then
    global_paths "$GLOBAL_TEMP"
    if [[ -x "$G_BINARY" ]]; then
      "$G_BINARY" --stop-daemon >/dev/null 2>&1 || true
      sleep 0.6
    fi
  fi
  if [[ "$KEEP_TEMP" == "1" ]]; then
    echo "Kept temp dirs: $GLOBAL_TEMP $PROJECT_A $PROJECT_B $PROJECT_C"
    return
  fi
  rm -rf "$GLOBAL_TEMP" "$PROJECT_A" "$PROJECT_B" "$PROJECT_C"
}
trap cleanup EXIT

fixture_project() {
  local root="$1" marker="$2"
  mkdir -p "$root"
  printf 'package main\n\n// %s\nfunc main() {}\n' "$marker" > "$root/main.go"
  printf '# %s\n' "$marker" > "$root/README.md"
}

if [[ -z "$BINARY" ]]; then
  # Build from the working tree: bin/ may hold a stale prebuilt without the flags
  # this smoke exercises.
  echo "Building a stub binary (no native tags) for the layout smoke..."
  BINARY="$GLOBAL_TEMP/$GLOBAL_BINARY_NAME"
  ( cd "$REPO_ROOT" && go build -o "$BINARY" ./cmd/mcp-semantic-search-zvec-go ) || {
    echo "go build failed" >&2
    exit 1
  }
fi
[[ -x "$BINARY" ]] || { echo "binary not runnable: $BINARY" >&2; exit 1; }
[[ "$("$BINARY" --version 2>&1)" =~ ^[0-9] ]] || { echo "binary does not report a version" >&2; exit 1; }

export MCP_GLOBAL_DIR="$GLOBAL_TEMP"
global_paths "$GLOBAL_TEMP"

echo "Global install smoke: $GLOBAL_TEMP"
echo "Binary: $BINARY"
echo ""

# --- 1. app install ---------------------------------------------------------
echo "1. Installing the user-level application"
global_init_layout "$GLOBAL_TEMP"
install_global_binary "$BINARY" "$GLOBAL_TEMP" >/dev/null 2>&1
check "binary installed under the user dir" test -x "$G_BINARY"
check "binary lives outside every project" test ! -e "$PROJECT_A/.mcp-semantic-search-zvec-go/bin/$GLOBAL_BINARY_NAME"
cp -f "$REPO_ROOT/config.yaml" "$G_CONFIG"
# A free port: the smoke must never bind the real install's 8080 or leave a
# daemon behind on the shared port.
SMOKE_PORT="$(free_loopback_port)"
write_daemon_yaml "$G_DAEMON_YAML" 10 "127.0.0.1:${SMOKE_PORT}"
check "daemon.yaml binds loopback only" grep -q 'http_addr: 127.0.0.1:' "$G_DAEMON_YAML"
check "daemon.yaml uses an isolated port" grep -q "http_addr: 127.0.0.1:${SMOKE_PORT}" "$G_DAEMON_YAML"
TOKEN="$(ensure_auth_token "$GLOBAL_TEMP")"
check "auth.token is 64 hex chars" bash -c "[[ '${TOKEN}' =~ ^[0-9a-f]{64}\$ ]]"
TOKEN_AGAIN="$(ensure_auth_token "$GLOBAL_TEMP")"
check "auth.token is not regenerated" test "$TOKEN_AGAIN" = "$TOKEN"

# --- 2. add two projects ----------------------------------------------------
echo "2. Adding two projects to the one application"
fixture_project "$PROJECT_A" "PROJECT-A-MARKER"
fixture_project "$PROJECT_B" "PROJECT-B-MARKER"
check_ok "project-add A exits 0" invoke_global_binary "$GLOBAL_TEMP" --project-add "$PROJECT_A"
check_ok "project-add B exits 0" invoke_global_binary "$GLOBAL_TEMP" --project-add "$PROJECT_B"
ID_A="$(project_collection_id "$PROJECT_A")"
ID_B="$(project_collection_id "$PROJECT_B")"
check "A has a collection_id" bash -c "[[ '${ID_A}' =~ ^[0-9a-f-]{36}\$ ]]"
check "B has a collection_id" bash -c "[[ '${ID_B}' =~ ^[0-9a-f-]{36}\$ ]]"
check "A and B have different collection ids" test "$ID_A" != "$ID_B"

# --- 3. per-project isolation ----------------------------------------------
echo "3. Checking per-project isolation"
for root in "$PROJECT_A" "$PROJECT_B"; do
  install_dir="$root/.mcp-semantic-search-zvec-go"
  label="$(basename "$root")"
  files="$(cd "$install_dir" && find . -type f | sed 's|^\./||' | sort | tr '\n' ' ')"
  check "$label: only project.yaml is stored" test "$files" = "project.yaml "
  check "$label: no bin/models/index/.env" bash -c \
    "[[ ! -e '$install_dir/bin' && ! -e '$install_dir/models' && ! -e '$install_dir/data' && ! -e '$install_dir/.env' ]]"
  check "$label: project.yaml has no absolute path" bash -c "! grep -qF '${root}' '$(project_config_path "$root")'"
  check "$label: project.yaml declares schema_version 1" grep -q '^schema_version: 1$' "$(project_config_path "$root")"
done

# --- 4. registrations ------------------------------------------------------
echo "4. Checking the local registry"
REG_COUNT="$(global_registrations "$GLOBAL_TEMP" | wc -l | tr -d ' ')"
check "two registrations exist" test "$REG_COUNT" = "2"
check "registration files carry no index_dir/config_path" bash -c \
  "! grep -qE 'index_dir|config_path' '$G_PROJECTS_DIR'/*.yaml"

# --- 5. managed IDE entries ------------------------------------------------
echo "5. Writing managed IDE entries"
PROJECT_CONFIG_A="$(project_config_path "$PROJECT_A")"
mkdir -p "$PROJECT_A/.cursor" "$PROJECT_A/.roo"
printf '{"mcpServers":{"other_server":{"command":"other.exe"}}}\n' > "$PROJECT_A/.cursor/mcp.json"
set_managed_mcp_entry "$PROJECT_A/.cursor/mcp.json" "$(native_path "$G_BINARY")" "$(native_path "$PROJECT_CONFIG_A")"
check "unrelated mcp.json entry preserved" grep -q 'other_server' "$PROJECT_A/.cursor/mcp.json"
check "managed mcp.json entry written" grep -q -- '--stdio-proxy' "$PROJECT_A/.cursor/mcp.json"
check "entry carries no secrets or INDEX_DIR" bash -c \
  "! grep -qE 'API_TOKEN|INDEX_DIR|WORKSPACE_ROOT' '$PROJECT_A/.cursor/mcp.json'"
# The JSON escapes backslashes, so compare the parsed value, not the raw text.
entry_command() {
  "$(python_bin)" - "$1" <<'PY'
import json, sys
with open(sys.argv[1], encoding="utf-8") as fh:
    entry = json.load(fh)["mcpServers"]["semantic-search-zvec-go"]
print(entry["command"])
PY
}
check "entry points at the shared binary" test "$(entry_command "$PROJECT_A/.cursor/mcp.json")" = "$(native_path "$G_BINARY")"

# --- 6. idempotent re-install ----------------------------------------------
echo "6. Re-running install (idempotency)"
BEFORE_A="$(cat "$PROJECT_CONFIG_A")"
check_ok "re-add exits 0" invoke_global_binary "$GLOBAL_TEMP" --project-add "$PROJECT_A"
check "collection id unchanged on re-add" test "$(project_collection_id "$PROJECT_A")" = "$ID_A"
check "project.yaml unchanged on re-add" test "$(cat "$PROJECT_CONFIG_A")" = "$BEFORE_A"
check "still exactly two registrations" test "$(global_registrations "$GLOBAL_TEMP" | wc -l | tr -d ' ')" = "2"
check "daemon.yaml not overwritten" grep -q "127.0.0.1:${SMOKE_PORT}" "$G_DAEMON_YAML"

# --- 7. conflicts ----------------------------------------------------------
echo "7. Checking id/root conflict safety"
COPY_DIR="$(temp_dir mcp-proj-copy)"
cp -R "$PROJECT_A/.mcp-semantic-search-zvec-go" "$COPY_DIR/.mcp-semantic-search-zvec-go"
check "copying a project with the same id is refused" bash -c \
  "! MCP_GLOBAL_DIR='$GLOBAL_TEMP' '$G_BINARY' --project-add '$COPY_DIR' >/dev/null 2>&1"
check "the copy did not register" test "$(global_registrations "$GLOBAL_TEMP" | wc -l | tr -d ' ')" = "2"
check_ok "--project-new-collection succeeds" invoke_global_binary "$GLOBAL_TEMP" --project-new-collection "$COPY_DIR"
ID_COPY="$(project_collection_id "$COPY_DIR")"
check "the copy got a new id" bash -c "[[ '${ID_COPY}' != '${ID_A}' && '${ID_COPY}' =~ ^[0-9a-f-]{36}\$ ]]"
check "the original id is unchanged" test "$(project_collection_id "$PROJECT_A")" = "$ID_A"
check "three registrations now exist" test "$(global_registrations "$GLOBAL_TEMP" | wc -l | tr -d ' ')" = "3"
# Unregister the copy before deleting it: a registration whose root is gone is an
# invalid snapshot, and reload would (correctly) refuse to apply it.
check_ok "project-remove for the copy exits 0" invoke_global_binary "$GLOBAL_TEMP" --project-remove "$COPY_DIR"
rm -rf "$COPY_DIR"

# --- 8. paths with spaces and Cyrillic -------------------------------------
echo "8. Checking paths with spaces and Cyrillic"
UNICODE_ROOT="$GLOBAL_TEMP/Проект с пробелами"
fixture_project "$UNICODE_ROOT" "UNICODE-MARKER"
check_ok "unicode path add exits 0" invoke_global_binary "$GLOBAL_TEMP" --project-add "$UNICODE_ROOT"
ID_U="$(project_collection_id "$UNICODE_ROOT")"
check "unicode project has a collection id" bash -c "[[ '${ID_U}' =~ ^[0-9a-f-]{36}\$ ]]"
check "unicode project registered under the user dir" bash -c \
  "grep -qF '${ID_U}' '$G_PROJECTS_DIR/${ID_U}.yaml'"

# --- 9. project uninstall keeps data --------------------------------------
echo "9. Uninstalling project B"
mkdir -p "$G_COLLECTIONS/$ID_B"
printf 'keep me' > "$G_COLLECTIONS/$ID_B/marker"
check_ok "project-remove exits 0" invoke_global_binary "$GLOBAL_TEMP" --project-remove "$PROJECT_B"
check "B registration removed" test ! -e "$G_PROJECTS_DIR/$ID_B.yaml"
check "B collection data preserved" test -f "$G_COLLECTIONS/$ID_B/marker"
check "A still registered" test -f "$G_PROJECTS_DIR/$ID_A.yaml"

# --- 10. daemon lifecycle --------------------------------------------------
echo "10. Checking the daemon lifecycle"
ENSURE_OUT="$(invoke_global_binary "$GLOBAL_TEMP" --ensure-daemon 2>&1)"
check "ensure-daemon exits 0" test $? -eq 0
check "ensure-daemon reports the isolated listener" bash -c \
  "printf '%s' \"\$1\" | grep -q '127.0.0.1:${SMOKE_PORT}'" _ "$ENSURE_OUT"
STOP_OUT="$(invoke_global_binary "$GLOBAL_TEMP" --stop-daemon 2>&1)"
check "stop-daemon exits 0" test $? -eq 0
sleep 0.8
check "daemon stopped" bash -c \
  "! pgrep -f 'mcp-semantic-search-zvec-go.*--daemon' >/dev/null 2>&1 || ! pgrep -af 'mcp-semantic-search-zvec-go' | grep -qF '$GLOBAL_TEMP'"

# --- 11. relative MCP_GLOBAL_DIR is rejected ------------------------------
echo "11. Checking MCP_GLOBAL_DIR validation"
check "relative MCP_GLOBAL_DIR is rejected" bash -c \
  "! MCP_GLOBAL_DIR=relative/dir bash -c '. \"$REPO_ROOT/scripts/lib/global-install.sh\"; global_dir' >/dev/null 2>&1"

echo ""
if [[ ${#FAILURES[@]} -gt 0 ]]; then
  echo "FAILED ${#FAILURES[@]}/${CHECKS} checks:"
  printf '  - %s\n' "${FAILURES[@]}"
  exit 1
fi
echo "PASSED all ${CHECKS} checks."
exit 0
