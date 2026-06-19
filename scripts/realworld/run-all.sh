#!/usr/bin/env bash
# Run realworld manual E2E scenarios (not CI).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
PROFILE="onnx"
KEEP_INDEX=0
GO_RUN=""
RUN_DOCKER=0
EXTRA_ARGS=()

usage() {
  echo "Usage: $0 [--profile onnx|lmstudio] [--keep-index] [--run TestName] [--docker]"
  exit 1
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --profile) PROFILE="$2"; shift 2 ;;
    --keep-index) KEEP_INDEX=1; shift ;;
    --run) GO_RUN="$2"; shift 2 ;;
    --docker) RUN_DOCKER=1; shift ;;
    -h|--help) usage ;;
    *) EXTRA_ARGS+=("$1"); shift ;;
  esac
done

SETUP_ARGS=(--profile "$PROFILE")
[[ "$KEEP_INDEX" -eq 1 ]] && SETUP_ARGS+=(--keep-index)
bash "$SCRIPT_DIR/setup-harness.sh" "${SETUP_ARGS[@]}"

if [[ "$PROFILE" == "lmstudio" ]]; then
  if ! curl -sf "http://127.0.0.1:1234/v1/models" >/dev/null 2>&1; then
    echo "SKIP: LM Studio not reachable at http://127.0.0.1:1234 — start LM Studio and load text-embedding-qwen3-embedding-0.6b"
    exit 0
  fi
fi

ZVEC_ENV="$REPO_ROOT/.deps/zvec-lib.env"
if [[ ! -f "$ZVEC_ENV" ]]; then
  bash "$REPO_ROOT/scripts/fetch/fetch-zvec-libs.sh" > "$ZVEC_ENV"
fi
# shellcheck disable=SC1091
. "$ZVEC_ENV"
# shellcheck disable=SC1091
. "$REPO_ROOT/.deps/onnxruntime.env" 2>/dev/null || true

export CGO_ENABLED=1
export LD_LIBRARY_PATH="${ZVEC_LIB_DIR:-}:${ORT_LIB_DIR:-}:${LD_LIBRARY_PATH:-}"
export REALWORLD_REPO_ROOT="$REPO_ROOT"
export REALWORLD_PROFILE="$PROFILE"

TEST_ARGS=(-tags "realworld,zvec" -count=1 -timeout 30m -v ./tests/realworld/...)
[[ -n "$GO_RUN" ]] && TEST_ARGS=(-run "$GO_RUN" "${TEST_ARGS[@]}")

cd "$REPO_ROOT"
go test "${TEST_ARGS[@]}"

if [[ "$RUN_DOCKER" -eq 1 ]]; then
  bash "$SCRIPT_DIR/run-docker.sh"
fi
