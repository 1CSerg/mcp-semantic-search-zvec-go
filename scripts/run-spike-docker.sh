#!/usr/bin/env bash
# Run zvec integration tests inside Docker (vendor libs, no cmake).
# PowerShell: docker run --rm -v "${PWD}:/src" -w /src golang:1.26.3-bookworm bash /src/scripts/run-spike-docker-inner.sh
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
MOUNT="$ROOT"
case "$MOUNT" in
  /[a-zA-Z]/*) MOUNT="//${MOUNT:1:1}${MOUNT:2}" ;;
esac
exec docker run --rm -v "${MOUNT}:/src" -w /src golang:1.26.3-bookworm bash /src/scripts/run-spike-docker-inner.sh
