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

ZVEC_ENV="$REPO_ROOT/.deps/zvec-lib.env"
if [[ ! -f "$ZVEC_ENV" ]]; then
  bash "$REPO_ROOT/scripts/fetch/fetch-zvec-libs.sh" > "$ZVEC_ENV"
fi
# shellcheck disable=SC1091
. "$ZVEC_ENV"
if [[ -z "${ZVEC_LIB_DIR:-}" ]]; then
  echo "run-all.sh: ZVEC_LIB_DIR is not set after sourcing $ZVEC_ENV" >&2
  exit 1
fi
if [[ ! -d "$ZVEC_LIB_DIR" ]]; then
  echo "run-all.sh: ZVEC_LIB_DIR does not exist: $ZVEC_LIB_DIR" >&2
  exit 1
fi

ORT_ENV="$REPO_ROOT/.deps/onnxruntime.env"
if [[ -f "$ORT_ENV" ]]; then
  # shellcheck disable=SC1091
  . "$ORT_ENV"
  if [[ -z "${ORT_LIB_DIR:-}" ]]; then
    echo "run-all.sh: ORT_LIB_DIR is not set after sourcing $ORT_ENV" >&2
    exit 1
  fi
  if [[ ! -d "$ORT_LIB_DIR" ]]; then
    echo "run-all.sh: ORT_LIB_DIR does not exist: $ORT_LIB_DIR" >&2
    exit 1
  fi
fi

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
