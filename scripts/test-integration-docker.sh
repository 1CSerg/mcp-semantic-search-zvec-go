#!/usr/bin/env bash
# Run zvec integration tests inside Docker (fetch-zvec-libs + CGO).
# PowerShell: docker run --rm -v "${PWD}:/src" -w /src golang:1.26.3-bookworm bash /src/scripts/run-spike-docker-inner.sh
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
exec docker run --rm -v "$ROOT:/src" -w /src golang:1.26.3-bookworm bash /src/scripts/run-spike-docker-inner.sh
