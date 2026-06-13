#!/usr/bin/env bash
# Phase 5 Docker smoke: shared daemon container with 2 workspaces + isolation checks.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
SMOKE_ROOT="${TMPDIR:-/tmp}/mcp-zvec-smoke-phase5-docker"
HTTP_PORT=18096
MOCK_PORT=9999
COMPOSE_PROJECT="mcp-smoke-daemon"
COMPOSE_FILE="$REPO_ROOT/docker/docker-compose.daemon.yml"

if ! command -v docker >/dev/null 2>&1; then
  echo "docker not found; skipping Phase 5 Docker smoke"
  exit 0
fi

cleanup() {
  if [[ -n "${MOCK_PID:-}" ]]; then kill "$MOCK_PID" 2>/dev/null || true; fi
  docker compose -f "$COMPOSE_FILE" --project-name "$COMPOSE_PROJECT" down -v 2>/dev/null || true
}
trap cleanup EXIT

docker compose -f "$COMPOSE_FILE" --project-name "$COMPOSE_PROJECT" down -v 2>/dev/null || true
rm -rf "$SMOKE_ROOT"
mkdir -p "$SMOKE_ROOT"

make_workspace() {
  local id="$1" keyword="$2"
  local root="$SMOKE_ROOT/$id"
  mkdir -p "$root/pkg" "$root/.mcp-semantic-search-zvec-go/data/index"
  printf 'package pkg\n\n// %s unique marker for workspace %s\nfunc Handler() {}\n' "$keyword" "$id" >"$root/pkg/main.go"
  cp "$SCRIPT_DIR/daemon-workspace-config-docker.yaml" "$root/.mcp-semantic-search-zvec-go/config.yaml"
  printf '%s\n' "$root"
}

WS_ALPHA="$(make_workspace ws-alpha "alpha authentication gateway")"
WS_BETA="$(make_workspace ws-beta "beta database repository layer")"
IDX_ALPHA="$WS_ALPHA/.mcp-semantic-search-zvec-go/data/index"
IDX_BETA="$WS_BETA/.mcp-semantic-search-zvec-go/data/index"

cat >"$SMOKE_ROOT/daemon.yaml" <<EOF
max_open_workspaces: 4
workspaces:
  - id: ws-alpha
    root: /workspaces/ws-alpha
    index_dir: /workspaces/ws-alpha-index
    config_path: /workspaces/ws-alpha/.mcp-semantic-search-zvec-go/config.yaml
  - id: ws-beta
    root: /workspaces/ws-beta
    index_dir: /workspaces/ws-beta-index
    config_path: /workspaces/ws-beta/.mcp-semantic-search-zvec-go/config.yaml
EOF

go run "$SCRIPT_DIR/mock-embed.go" -port "$MOCK_PORT" -dims 128 &
MOCK_PID=$!
sleep 2

export HTTP_PORT="$HTTP_PORT"
export WS_ALPHA_ROOT="$WS_ALPHA"
export WS_ALPHA_INDEX="$IDX_ALPHA"
export WS_BETA_ROOT="$WS_BETA"
export WS_BETA_INDEX="$IDX_BETA"
export DAEMON_CONFIG_PATH="$SMOKE_ROOT/daemon.yaml"

cd "$REPO_ROOT"
docker compose -f "$COMPOSE_FILE" --project-name "$COMPOSE_PROJECT" up --build -d

deadline=$((SECONDS + 120))
until curl -sf "http://127.0.0.1:$HTTP_PORT/health" >/dev/null; do
  if (( SECONDS > deadline )); then echo "daemon HTTP not ready"; exit 1; fi
  sleep 2
done

curl -sf "http://127.0.0.1:$HTTP_PORT/v1/workspaces" | grep -q ws-alpha
curl -sf "http://127.0.0.1:$HTTP_PORT/v1/workspaces" | grep -q ws-beta

for id in ws-alpha ws-beta; do
  curl -sf -X POST "http://127.0.0.1:$HTTP_PORT/v1/reindex" \
    -H "Content-Type: application/json" \
    -d "{\"force\":true,\"workspace_id\":\"$id\"}" | grep -q '"started":true'
  until curl -sf "http://127.0.0.1:$HTTP_PORT/v1/status?workspace_id=$id" | grep -q '"running":false'; do
    if curl -sf "http://127.0.0.1:$HTTP_PORT/v1/status?workspace_id=$id" | grep -q '"state":"error"'; then
      echo "indexing failed for $id"; exit 1
    fi
    sleep 1
  done
done

ROOT_A="$(curl -sf "http://127.0.0.1:$HTTP_PORT/v1/status?workspace_id=ws-alpha" | python3 -c "import sys,json; print(json.load(sys.stdin).get('workspace_root',''))")"
ROOT_B="$(curl -sf "http://127.0.0.1:$HTTP_PORT/v1/status?workspace_id=ws-beta" | python3 -c "import sys,json; print(json.load(sys.stdin).get('workspace_root',''))")"
if [[ "$ROOT_A" == "$ROOT_B" ]]; then echo "same workspace_root for both ids"; exit 1; fi

go test "$REPO_ROOT/internal/transport/mcp/" -run TestMCPOverHTTPProxy -count=1
echo "PASS Phase 5 Docker smoke: daemon container, 2 workspaces, isolation"
