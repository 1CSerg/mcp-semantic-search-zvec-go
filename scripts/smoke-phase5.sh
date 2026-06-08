#!/usr/bin/env bash
# Phase 5 gate smoke: shared daemon with 3 workspaces + MCP proxy test.
set -euo pipefail
REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SMOKE_ROOT="${TMPDIR:-/tmp}/mcp-zvec-smoke-phase5"
HTTP_PORT=18095
MOCK_PORT=9999
DIMS=128

cleanup() {
  jobs -p 2>/dev/null | xargs -r kill 2>/dev/null || true
  pkill -f "mock-embed.go" 2>/dev/null || true
  pkill -f "mcp-semantic-search-zvec-go" 2>/dev/null || true
}
trap cleanup EXIT

rm -rf "$SMOKE_ROOT"
mkdir -p "$SMOKE_ROOT"

make_workspace() {
  local id="$1" keyword="$2"
  local root="$SMOKE_ROOT/$id"
  mkdir -p "$root/pkg" "$root/.mcp-semantic-search-zvec-go"
  printf 'package pkg\n\n// %s unique marker for workspace %s\nfunc Handler() {}\n' "$keyword" "$id" >"$root/pkg/main.go"
  cp "$REPO_ROOT/scripts/smoke/daemon-workspace-config.yaml" "$root/.mcp-semantic-search-zvec-go/config.yaml"
  echo "$id|$root|$root/.mcp-semantic-search-zvec-go/data/index|$root/.mcp-semantic-search-zvec-go/config.yaml|$keyword"
}

WS_ALPHA=$(make_workspace ws-alpha "alpha authentication gateway")
WS_BETA=$(make_workspace ws-beta "beta database repository layer")
WS_GAMMA=$(make_workspace ws-gamma "gamma middleware router pipeline")

DAEMON_PATH="$SMOKE_ROOT/daemon.yaml"
cat >"$DAEMON_PATH" <<EOF
max_open_workspaces: 2
workspaces:
EOF
for ws in "$WS_ALPHA" "$WS_BETA" "$WS_GAMMA"; do
  IFS='|' read -r id root idx cfg _ <<<"$ws"
  cat >>"$DAEMON_PATH" <<EOF
  - id: $id
    root: $root
    index_dir: $idx
    config_path: $cfg
EOF
done

make -C "$REPO_ROOT" build-zvec
source "$REPO_ROOT/.deps/zvec-lib.env"
export CGO_ENABLED=1
export LD_LIBRARY_PATH="${ZVEC_LIB_DIR:-}:${LD_LIBRARY_PATH:-}"

go run "$REPO_ROOT/scripts/smoke/mock-embed.go" -port "$MOCK_PORT" -dims "$DIMS" &
sleep 2

BIN="$REPO_ROOT/bin/mcp-semantic-search-zvec-go"
"$BIN" --daemon --daemon-config "$DAEMON_PATH" --http-addr "127.0.0.1:$HTTP_PORT" &
sleep 2

for i in $(seq 1 30); do
  curl -sf "http://127.0.0.1:$HTTP_PORT/health" >/dev/null && break
  sleep 0.4
done

curl -sf "http://127.0.0.1:$HTTP_PORT/v1/workspaces" | grep -q ws-alpha

for ws in "$WS_ALPHA" "$WS_BETA" "$WS_GAMMA"; do
  IFS='|' read -r id _ _ _ keyword <<<"$ws"
  curl -sf -X POST "http://127.0.0.1:$HTTP_PORT/v1/reindex" \
    -H "Content-Type: application/json" -H "X-Workspace-ID: $id" \
    -d "{\"force\":true,\"workspace_id\":\"$id\"}" | grep -q '"started":true'
  for j in $(seq 1 300); do
    st=$(curl -sf "http://127.0.0.1:$HTTP_PORT/v1/status?workspace_id=$id")
    echo "$st" | grep -q '"running":false' && break
    sleep 0.8
  done
  body=$(curl -sf -X POST "http://127.0.0.1:$HTTP_PORT/v1/search" \
    -H "Content-Type: application/json" -H "X-Workspace-ID: $id" \
    -d "{\"query\":\"$keyword\",\"limit\":3,\"workspace_id\":\"$id\"}")
  echo "$body" | grep -q main.go
done

go test "$REPO_ROOT/internal/transport/mcp/" -run TestMCPOverHTTPProxy -count=1

echo "PASS Phase 5 smoke: shared daemon with 3 workspaces"
