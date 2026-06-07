#!/usr/bin/env bash
set -euo pipefail
apt-get update -qq
apt-get install -y -qq git ca-certificates
bash scripts/fetch-zvec-libs.sh > /tmp/zvec-lib.env
# shellcheck disable=SC1091
source /tmp/zvec-lib.env
export LD_LIBRARY_PATH="${ZVEC_LIB_DIR}:${LD_LIBRARY_PATH:-}"
CGO_ENABLED=1 go test -tags "integration,zvec" -v -count=1 ./internal/store/zvec/...
