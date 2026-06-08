#!/usr/bin/env bash
# Phase 2 gate smoke: empty project -> reindex -> HTTP search.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SMOKE_ROOT="${TMPDIR:-/tmp}/mcp-zvec-smoke-phase2"
SMOKE_INDEX="${SMOKE_ROOT}/index"
HTTP_PORT=18090
MOCK_PORT=9999
DIMS=128

cleanup() {
  [[ -n "${MOCK_PID:-}" ]] && kill "$MOCK_PID" 2>/dev/null || true
  [[ -n "${SRV_PID:-}" ]] && kill "$SRV_PID" 2>/dev/null || true
}
trap cleanup EXIT

rm -rf "$SMOKE_ROOT"
mkdir -p "$SMOKE_ROOT/pkg"
printf 'package pkg\n\nfunc Auth() {}\n' >"$SMOKE_ROOT/pkg/auth.go"

bash "$ROOT/scripts/fetch-zvec-libs.sh" > "$ROOT/.deps/zvec-lib.env"
# shellcheck source=/dev/null
. "$ROOT/.deps/zvec-lib.env"

export CGO_ENABLED=1
export LD_LIBRARY_PATH="${ZVEC_LIB_DIR}:${LD_LIBRARY_PATH:-}"
export CONFIG_PATH="$ROOT/scripts/smoke/config.yaml"
export WORKSPACE_ROOT="$SMOKE_ROOT"
export INDEX_DIR="$SMOKE_INDEX"
export AUTO_INDEX_ON_START=false

go run "$ROOT/scripts/smoke/mock-embed.go" -port "$MOCK_PORT" -dims "$DIMS" &
MOCK_PID=$!
sleep 1

CGO_ENABLED=1 LD_LIBRARY_PATH="$ZVEC_LIB_DIR:${LD_LIBRARY_PATH:-}" \
  go build -tags zvec -o "$ROOT/bin/mcp-semantic-search-zvec-go" "$ROOT/cmd/mcp-semantic-search-zvec-go"
cp -f "$ZVEC_LIB_DIR"/libzvec_c_api.so "$ROOT/bin/" 2>/dev/null || true

"$ROOT/bin/mcp-semantic-search-zvec-go" --http --http-addr "127.0.0.1:$HTTP_PORT" &
SRV_PID=$!

for _ in $(seq 1 30); do
  curl -sf "http://127.0.0.1:$HTTP_PORT/health" >/dev/null && break
  sleep 0.5
done
curl -sf "http://127.0.0.1:$HTTP_PORT/health" >/dev/null

curl -sf -X POST "http://127.0.0.1:$HTTP_PORT/v1/reindex" \
  -H "Content-Type: application/json" \
  -d '{"force":true}' | grep -q '"started"[[:space:]]*:[[:space:]]*true'

for _ in $(seq 1 120); do
  status="$(curl -sf "http://127.0.0.1:$HTTP_PORT/v1/status")"
  echo "$status" | grep -q '"running"[[:space:]]*:[[:space:]]*false' && break
  sleep 0.5
done

resp="$(curl -sf -X POST "http://127.0.0.1:$HTTP_PORT/v1/search" \
  -H "Content-Type: application/json" \
  -d '{"query":"authentication","limit":5}')"
echo "$resp" | grep -q '"path"' || { echo "no search results: $resp"; exit 1; }
echo "PASS Phase 2 smoke: $resp"
