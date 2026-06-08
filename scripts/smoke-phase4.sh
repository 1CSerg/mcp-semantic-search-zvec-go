#!/usr/bin/env bash
# Phase 4 gate smoke: local ONNX profile without external embedding API.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SMOKE_ROOT="${TMPDIR:-/tmp}/mcp-zvec-smoke-phase4"
SMOKE_INDEX="$SMOKE_ROOT/index"
SMOKE_MODELS="$SMOKE_ROOT/.mcp-semantic-search-zvec-go/models/paraphrase-multilingual-MiniLM-L12-v2"
HTTP_PORT=18092
BIN="$ROOT/bin/mcp-semantic-search-zvec-go"
SMOKE_PROCS=()

cleanup() {
  for pid in "${SMOKE_PROCS[@]:-}"; do
    kill "$pid" 2>/dev/null || true
  done
}
trap cleanup EXIT

wait_health() {
  local deadline=$((SECONDS + 30))
  while ! curl -sf "http://127.0.0.1:${HTTP_PORT}/health" >/dev/null; do
    if (( SECONDS > deadline )); then
      echo "HTTP server did not become ready on :${HTTP_PORT}" >&2
      exit 1
    fi
    sleep 0.4
  done
}

wait_indexing_idle() {
  local deadline=$((SECONDS + 180))
  while true; do
    local running
    running=$(curl -sf "http://127.0.0.1:${HTTP_PORT}/v1/status" | python3 -c 'import json,sys; print(json.load(sys.stdin)["indexing"]["running"])')
    if [[ "$running" == "False" || "$running" == "false" ]]; then
      return 0
    fi
    if (( SECONDS > deadline )); then
      echo "indexing did not finish" >&2
      exit 1
    fi
    sleep 0.8
  done
}

rm -rf "$SMOKE_ROOT"
mkdir -p "$SMOKE_ROOT/pkg" "$SMOKE_ROOT/.mcp-semantic-search-zvec-go"
printf 'package pkg\n\n// Auth middleware for semantic search smoke\nfunc Auth() {}\n' > "$SMOKE_ROOT/pkg/auth.go"

bash "$ROOT/scripts/fetch-onnx-model.sh" "$SMOKE_MODELS"
make -C "$ROOT" build-zvec

cp -f "$ROOT/scripts/smoke/onnx-config.yaml" "$SMOKE_ROOT/.mcp-semantic-search-zvec-go/config.yaml"
# shellcheck disable=SC1091
. "$ROOT/.deps/zvec-lib.env"
# shellcheck disable=SC1091
. "$ROOT/.deps/onnxruntime.env"

export CONFIG_PATH="$SMOKE_ROOT/.mcp-semantic-search-zvec-go/config.yaml"
export WORKSPACE_ROOT="$SMOKE_ROOT"
export INDEX_DIR="$SMOKE_INDEX"
export AUTO_INDEX_ON_START=false
export FILE_WATCHER_ENABLED=false
export CGO_ENABLED=1
export LD_LIBRARY_PATH="${ZVEC_LIB_DIR}:${ORT_LIB_DIR}:${LD_LIBRARY_PATH:-}"

"$BIN" --http --http-addr "127.0.0.1:${HTTP_PORT}" &
SMOKE_PROCS+=("$!")
wait_health

provider=$(curl -sf "http://127.0.0.1:${HTTP_PORT}/v1/status" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d["embedding_provider"])')
if [[ "$provider" != "onnx" ]]; then
  echo "expected onnx provider, got $provider" >&2
  exit 1
fi
phase=$(curl -sf "http://127.0.0.1:${HTTP_PORT}/v1/status" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("phase",""))')
if [[ "$phase" != "4" ]]; then
  echo "service not in phase 4 (stub mode?): phase=$phase" >&2
  exit 1
fi

curl -sf -X POST "http://127.0.0.1:${HTTP_PORT}/v1/reindex" \
  -H "Content-Type: application/json" \
  -d '{"force":true}' >/dev/null
wait_indexing_idle

curl -sf "http://127.0.0.1:${HTTP_PORT}/ready" | python3 -c 'import json,sys; d=json.load(sys.stdin); assert d.get("status")=="ready", d'

search_json=$(curl -sf -X POST "http://127.0.0.1:${HTTP_PORT}/v1/search" \
  -H "Content-Type: application/json" \
  -d '{"query":"authentication middleware","limit":5}')
python3 - <<PY
import json, sys
d = json.loads('''$search_json''')
assert len(d.get("results", [])) >= 1, d
assert "performance" in d, d
print("PASS Phase 4 smoke: local_multilingual reindex + search without external API")
PY
