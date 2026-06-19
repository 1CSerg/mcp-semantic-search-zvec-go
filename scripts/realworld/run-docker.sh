#!/usr/bin/env bash
# Realworld Docker smoke (D1/D2): build image, run HTTP reindex + search with corpus mount.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
HTTP_PORT="${REALWORLD_DOCKER_PORT:-19410}"
IMAGE="mcp-realworld-smoke:local"

if ! command -v docker >/dev/null 2>&1; then
  echo "SKIP: docker not found"
  exit 0
fi

CORPUS="$REPO_ROOT/tests/realworld/corpus"
INDEX_VOL="${TMPDIR:-/tmp}/mcp-realworld-docker-index"

cleanup() {
  docker rm -f mcp-realworld-smoke 2>/dev/null || true
}
trap cleanup EXIT

rm -rf "$INDEX_VOL"
mkdir -p "$INDEX_VOL"

echo "==> docker build"
docker build -f "$REPO_ROOT/docker/Dockerfile" -t "$IMAGE" "$REPO_ROOT"

echo "==> docker run"
docker rm -f mcp-realworld-smoke 2>/dev/null || true
docker run -d --name mcp-realworld-smoke \
  -p "127.0.0.1:${HTTP_PORT}:8080" \
  -v "$CORPUS:/workspace:ro" \
  -v "$INDEX_VOL:/data/index" \
  -e WORKSPACE_ROOT=/workspace \
  -e INDEX_DIR=/data/index \
  -e FILE_WATCHER_BACKEND=polling \
  -e AUTO_INDEX_ON_START=false \
  "$IMAGE" --http --http-addr :8080

deadline=$((SECONDS + 180))
until curl -sf "http://127.0.0.1:${HTTP_PORT}/health" >/dev/null; do
  if (( SECONDS > deadline )); then echo "container health timeout"; exit 1; fi
  sleep 2
done

curl -sf -X POST "http://127.0.0.1:${HTTP_PORT}/v1/reindex" \
  -H "Content-Type: application/json" \
  -d '{"force":true}' | grep -q '"started":true'

until curl -sf "http://127.0.0.1:${HTTP_PORT}/v1/status" | grep -q '"running":false'; do
  if curl -sf "http://127.0.0.1:${HTTP_PORT}/v1/status" | grep -q '"state":"error"'; then
    echo "indexing failed in container"; exit 1
  fi
  sleep 2
done

body=$(curl -sf -X POST "http://127.0.0.1:${HTTP_PORT}/v1/search" \
  -H "Content-Type: application/json" \
  -d '{"query":"REALWORLD_GO_AUTH_GATE","limit":5}')
echo "$body" | grep -q middleware.go

echo "PASS realworld Docker smoke (D1/D2)"
