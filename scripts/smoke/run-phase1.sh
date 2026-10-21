#!/usr/bin/env bash
# Phase 1 gate smoke: seed-index -> HTTP /v1/status -> /v1/search (plain + path_glob).
#
# The run is fully isolated: a throw-away workspace under ${TMPDIR:-/tmp} holds the
# config, the index and the seeded collection, so the repo root index is never
# touched and INDEX_DIR/CONFIG_PATH never violate path containment (no warnings).
# Ports are allocated dynamically and both helper processes are built binaries, so
# a leftover process can neither block the run nor survive the cleanup.
# Failures print the server log tail instead of a bare "no results".
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
SMOKE_ROOT="${TMPDIR:-/tmp}/mcp-zvec-smoke-phase1"
SMOKE_WORKSPACE="$SMOKE_ROOT"
SMOKE_INDEX="$SMOKE_ROOT/index"
SMOKE_CONFIG="$SMOKE_ROOT/config.yaml"
LOG_FILE="$SMOKE_ROOT/logs/server.log"
DIMS=128
# Seeded chunk paths are internal/module/file_<n>.go (see cmd/seed-index).
SEED_GLOB="internal/module/*.go"
MISS_GLOB="does/not/match/*.go"

MOCK_PID=""
SRV_PID=""

cleanup() {
  [[ -n "$MOCK_PID" ]] && kill "$MOCK_PID" 2>/dev/null || true
  [[ -n "$SRV_PID" ]] && kill "$SRV_PID" 2>/dev/null || true
}

show_log_tail() {
  if [[ -f "$LOG_FILE" ]]; then
    echo "--- server.log (tail) ---" >&2
    tail -n 25 "$LOG_FILE" >&2
  fi
}

on_exit() {
  local code=$?
  cleanup
  if [[ $code -ne 0 ]]; then
    show_log_tail
  fi
  exit $code
}
trap on_exit EXIT

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

bash "$ROOT/scripts/fetch/fetch-zvec-libs.sh" > "$ROOT/.deps/zvec-lib.env"
# shellcheck source=/dev/null
. "$ROOT/.deps/zvec-lib.env"

export CGO_ENABLED=1
# Windows (Git Bash/MSYS) resolves native DLLs via PATH, Unix via LD_LIBRARY_PATH.
# shellcheck source=/dev/null
source "$ROOT/scripts/dev/ci-native-env.sh"

mkdir -p "$ROOT/bin"
go build -o "$ROOT/bin/smoke-check" "$SCRIPT_DIR/smoke-check.go"
CHECK="$ROOT/bin/smoke-check"

rm -rf "$SMOKE_ROOT"
mkdir -p "$SMOKE_INDEX"

HTTP_PORT="$("$CHECK" free-port)" || fail "cannot allocate a free HTTP port"
MOCK_PORT="$("$CHECK" free-port)" || fail "cannot allocate a free mock port"
BASE_URL="http://127.0.0.1:$HTTP_PORT"

# The smoke profile points at the mock embedding server, so the generated config
# carries the dynamically allocated mock port.
sed "s#http://127\.0\.0\.1:[0-9][0-9]*/v1#http://127.0.0.1:$MOCK_PORT/v1#" \
  "$SCRIPT_DIR/config.yaml" > "$SMOKE_CONFIG"

# Everything (config, index, logs) lives under $SMOKE_WORKSPACE: no containment warnings.
export CONFIG_PATH="$SMOKE_CONFIG"
export WORKSPACE_ROOT="$SMOKE_WORKSPACE"
export INDEX_DIR="$SMOKE_INDEX"

go build -o "$ROOT/bin/mock-embed" "$SCRIPT_DIR/mock-embed.go"

"$ROOT/bin/mock-embed" -port "$MOCK_PORT" -dims "$DIMS" &
MOCK_PID=$!
sleep 1

cd "$ROOT"
go run -tags zvec ./cmd/seed-index -n 100

CGO_ENABLED=1 LD_LIBRARY_PATH="${ZVEC_LIB_DIR}:${LD_LIBRARY_PATH:-}" \
  go build -tags zvec -o "$ROOT/bin/mcp-semantic-search-zvec-go" ./cmd/mcp-semantic-search-zvec-go
cp -f "$ZVEC_LIB_DIR"/libzvec_c_api.so "$ROOT/bin/" 2>/dev/null || true

"$ROOT/bin/mcp-semantic-search-zvec-go" --http --http-addr "127.0.0.1:$HTTP_PORT" &
SRV_PID=$!

ready=0
for _ in $(seq 1 40); do
  if curl -sf "$BASE_URL/health" >/dev/null; then ready=1; break; fi
  sleep 0.5
done
[[ "$ready" -eq 1 ]] || fail "HTTP server did not become ready on $BASE_URL"

# The seeded collection must be visible: zvec_open_ok / zvec_doc_count prove the
# server opened the collection the seed created instead of resetting it.
curl -sf "$BASE_URL/v1/status" | "$CHECK" status || fail "seeded index is not visible to the server"

search() {
  local body="$1"
  curl -sf -X POST "$BASE_URL/v1/search" -H "Content-Type: application/json" -d "$body"
}

plain_json="$(search '{"query":"authentication","limit":5}')"
echo "$plain_json" | "$CHECK" results 1 - || fail "plain search returned no usable results"

# path_glob must filter without losing recall (regression guard for the
# topK-expansion defect: the seeded collection holds exactly 100 chunks).
glob_json="$(search "{\"query\":\"authentication\",\"limit\":5,\"path_glob\":\"$SEED_GLOB\"}")"
echo "$glob_json" | "$CHECK" results 1 "$SEED_GLOB" \
  || fail "path_glob '$SEED_GLOB' lost recall or returned a non-matching path"

miss_json="$(search "{\"query\":\"authentication\",\"limit\":5,\"path_glob\":\"$MISS_GLOB\"}")"
echo "$miss_json" | "$CHECK" results 0 "$MISS_GLOB" \
  || fail "path_glob '$MISS_GLOB' returned results for a non-matching glob"
echo "PASS path_glob filter: matching glob returns only matching paths, non-matching glob returns 0"

echo "  smoke workspace: $SMOKE_ROOT"
