#!/usr/bin/env bash
# Native end-to-end acceptance for the global layout (Unix wrapper).
#
# Runs scripts/smoke/global-daemon-check.go against the production binary
# (tags zvec,onnx,treesitter): one shared daemon, several projects, real MCP
# stdio proxies performing initialize/tools-call, and per-collection isolation.
#
# Everything happens in a temporary MCP_GLOBAL_DIR and temporary project roots;
# the driver stops every process it started.
#
# Usage:
#   bash scripts/smoke/run-global-daemon.sh [--binary <path>] [--keep-work]
#
# Exit codes: 0 all checks passed, 1 a check failed, 2 bad invocation.

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
BINARY=""
KEEP_WORK=0
TIMEOUT_SECONDS=120

while [[ $# -gt 0 ]]; do
  case "$1" in
    --binary) BINARY="$2"; shift 2 ;;
    --keep-work) KEEP_WORK=1; shift ;;
    --timeout) TIMEOUT_SECONDS="$2"; shift 2 ;;
    -h|--help) sed -n '2,16p' "$0"; exit 0 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

WORK="$(mktemp -d "${TMPDIR:-/tmp}/mcp-global-daemon-XXXXXX")"

cleanup() {
  # The driver stops its own processes; this is a safety net for a hard failure.
  pkill -f "$WORK" >/dev/null 2>&1 || true
  if [[ "$KEEP_WORK" == "1" ]]; then
    echo "Kept work dir: $WORK"
  else
    rm -rf "$WORK"
  fi
}
trap cleanup EXIT

if [[ -z "$BINARY" ]]; then
  BINARY="$REPO_ROOT/bin/mcp-semantic-search-zvec-go"
fi
if [[ ! -x "$BINARY" ]]; then
  echo "Binary not found or not executable: $BINARY" >&2
  echo "Build it first with native tags (zvec,onnx,treesitter), then pass --binary." >&2
  exit 2
fi

VERSION_OUT="$("$BINARY" --version 2>&1)"
if [[ $? -ne 0 ]]; then
  echo "binary $BINARY does not run: $VERSION_OUT" >&2
  exit 1
fi
echo "Native end-to-end smoke: $BINARY (v${VERSION_OUT})"

DRIVER="$WORK/global-daemon-check"
if ! ( cd "$REPO_ROOT" && go build -o "$DRIVER" ./scripts/smoke/global-daemon-check.go ); then
  echo "driver build failed" >&2
  exit 1
fi

DRIVER_ARGS=(-binary "$BINARY" -work "$WORK/run" -timeout "${TIMEOUT_SECONDS}s")
if [[ "$KEEP_WORK" == "1" ]]; then
  DRIVER_ARGS+=(-keep)
fi
"$DRIVER" "${DRIVER_ARGS[@]}"
exit $?
