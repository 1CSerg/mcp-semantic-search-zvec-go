#!/usr/bin/env bash
# Phase 1 gate smoke: seed-index -> HTTP /v1/search with ranked results.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
SMOKE_INDEX="${TMPDIR:-/tmp}/mcp-zvec-smoke-index"
HTTP_PORT=18089
MOCK_PORT=9999
DIMS=128

cleanup() {
  [[ -n "${MOCK_PID:-}" ]] && kill "$MOCK_PID" 2>/dev/null || true
  [[ -n "${SRV_PID:-}" ]] && kill "$SRV_PID" 2>/dev/null || true
}
trap cleanup EXIT

bash "$ROOT/scripts/fetch/fetch-zvec-libs.sh" > "$ROOT/.deps/zvec-lib.env"
# shellcheck source=/dev/null
. "$ROOT/.deps/zvec-lib.env"

export CGO_ENABLED=1
export LD_LIBRARY_PATH="${ZVEC_LIB_DIR}:${LD_LIBRARY_PATH:-}"
export CONFIG_PATH="$SCRIPT_DIR/config.yaml"
export WORKSPACE_ROOT="$ROOT"
export INDEX_DIR="$SMOKE_INDEX"
rm -rf "$SMOKE_INDEX"
mkdir -p "$SMOKE_INDEX"

go run "$SCRIPT_DIR/mock-embed.go" -port "$MOCK_PORT" -dims "$DIMS" &
MOCK_PID=$!
sleep 1

cd "$ROOT"
go run -tags zvec ./cmd/seed-index -n 100

CGO_ENABLED=1 LD_LIBRARY_PATH="$ZVEC_LIB_DIR:${LD_LIBRARY_PATH:-}" \
  go build -tags zvec -o "$ROOT/bin/mcp-semantic-search-zvec-go" ./cmd/mcp-semantic-search-zvec-go
cp -f "$ZVEC_LIB_DIR"/libzvec_c_api.so "$ROOT/bin/" 2>/dev/null || true

"$ROOT/bin/mcp-semantic-search-zvec-go" --http --http-addr "127.0.0.1:$HTTP_PORT" &
SRV_PID=$!

for _ in $(seq 1 30); do
  curl -sf "http://127.0.0.1:$HTTP_PORT/health" >/dev/null && break
  sleep 0.5
done
curl -sf "http://127.0.0.1:$HTTP_PORT/health" >/dev/null

resp="$(curl -sf -X POST "http://127.0.0.1:$HTTP_PORT/v1/search" \
  -H "Content-Type: application/json" \
  -d '{"query":"authentication","limit":5}')"
echo "$resp" | grep -q '"results"' || { echo "no results key: $resp"; exit 1; }
echo "$resp" | grep -q '"path"' || { echo "empty results: $resp"; exit 1; }
echo "PASS Phase 1 smoke: $resp"
