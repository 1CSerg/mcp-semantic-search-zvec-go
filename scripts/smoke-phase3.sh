#!/usr/bin/env bash
# Phase 3 gate smoke: reconnect resilience + watcher + /ready.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SMOKE_ROOT="${TMPDIR:-/tmp}/mcp-zvec-smoke-phase3"
SMOKE_INDEX="${SMOKE_ROOT}/index"
HTTP_PORT=18091
MOCK_PORT=9998
DIMS=128
RECONNECTS="${SMOKE_PHASE3_RECONNECTS:-10}"
if [[ $# -gt 0 ]]; then
  RECONNECTS="$1"
fi

cleanup() {
  if [[ -n "${MOCK_PID:-}" ]]; then kill "$MOCK_PID" 2>/dev/null || true; fi
  if [[ -n "${SRV_PID:-}" ]]; then kill "$SRV_PID" 2>/dev/null || true; fi
}
trap cleanup EXIT

rm -rf "$SMOKE_ROOT"
mkdir -p "$SMOKE_ROOT/pkg"
cat >"$SMOKE_ROOT/pkg/auth.go" <<'EOF'
package pkg

// Auth middleware
func Auth() {}
EOF

make -C "$REPO_ROOT" build-zvec >/dev/null
# shellcheck disable=SC1091
source "$REPO_ROOT/.deps/zvec-lib.env"
export LD_LIBRARY_PATH="${ZVEC_LIB_DIR}:${LD_LIBRARY_PATH:-}"
export CGO_ENABLED=1

export CONFIG_PATH="$REPO_ROOT/scripts/smoke/config.yaml"
export WORKSPACE_ROOT="$SMOKE_ROOT"
export INDEX_DIR="$SMOKE_INDEX"
export AUTO_INDEX_ON_START=false
export FILE_WATCHER_ENABLED=true
export FILE_WATCHER_BACKEND=polling
export FILE_WATCHER_POLL_INTERVAL_SECONDS=1

go run "$REPO_ROOT/scripts/smoke/mock-embed.go" -port "$MOCK_PORT" -dims "$DIMS" &
MOCK_PID=$!
sleep 2

BIN="$REPO_ROOT/bin/mcp-semantic-search-zvec-go"
wait_health() {
  local port=$1
  for _ in $(seq 1 50); do
    if curl -sf "http://127.0.0.1:${port}/health" >/dev/null; then
      return 0
    fi
    sleep 0.3
  done
  echo "HTTP server did not become ready on :${port}" >&2
  exit 1
}

wait_index_idle() {
  local port=$1
  for _ in $(seq 1 150); do
    local running
    running=$(curl -sf "http://127.0.0.1:${port}/v1/status" | python3 -c 'import json,sys; print(json.load(sys.stdin)["indexing"]["running"])')
    if [[ "$running" == "False" || "$running" == "false" ]]; then
      return 0
    fi
    sleep 0.4
  done
  echo "indexing did not finish" >&2
  exit 1
}

for i in $(seq 1 "$RECONNECTS"); do
  "$BIN" --http --http-addr "127.0.0.1:${HTTP_PORT}" &
  SRV_PID=$!
  wait_health "$HTTP_PORT"

  if [[ "$i" -eq 1 ]]; then
    curl -sf -X POST "http://127.0.0.1:${HTTP_PORT}/v1/reindex" \
      -H "Content-Type: application/json" -d '{"force":true}' >/dev/null
    wait_index_idle "$HTTP_PORT"
  fi

  ready=$(curl -sf "http://127.0.0.1:${HTTP_PORT}/ready")
  echo "$ready" | grep -q '"status": "ready"' || {
    echo "not ready on cycle $i: $ready" >&2
    exit 1
  }

  kill "$SRV_PID" 2>/dev/null || true
  wait "$SRV_PID" 2>/dev/null || true
  SRV_PID=""
  sleep 0.4
  if [[ -f "${SMOKE_INDEX}/index.lock" ]]; then
    echo "stale index.lock after reconnect cycle $i" >&2
    exit 1
  fi
done

"$BIN" --http --http-addr "127.0.0.1:${HTTP_PORT}" &
SRV_PID=$!
wait_health "$HTTP_PORT"
wait_index_idle "$HTTP_PORT"

echo "// watcher touch" >>"$SMOKE_ROOT/pkg/auth.go"
sleep 4
wait_index_idle "$HTTP_PORT"

status=$(curl -sf "http://127.0.0.1:${HTTP_PORT}/v1/status")
echo "$status" | grep -q '"enabled_in_config": true' || {
  echo "file watcher not enabled in status" >&2
  exit 1
}

search=$(curl -sf -X POST "http://127.0.0.1:${HTTP_PORT}/v1/search" \
  -H "Content-Type: application/json" \
  -d '{"query":"authentication middleware","limit":5}')
echo "$search" | grep -q '"performance"' || {
  echo "missing search performance metrics" >&2
  exit 1
}

echo "PASS Phase 3 smoke: ${RECONNECTS} reconnects + watcher + /ready"
