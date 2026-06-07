#!/usr/bin/env bash
set -euo pipefail

MIN="${COVERAGE_MIN:-80}"
PROFILE="${COVERAGE_PROFILE:-coverage.out}"
PACKAGES="${COVERAGE_PACKAGES:-./internal/...}"

go test -coverprofile="$PROFILE" $PACKAGES
total="$(go tool cover -func="$PROFILE" | awk '/^total:/ {gsub(/%/, "", $3); print $3}')"
echo "coverage: ${total}% (minimum ${MIN}%, packages: ${PACKAGES})"

awk -v total="$total" -v min="$MIN" 'BEGIN {
  if (total + 0 < min + 0) {
    printf "coverage %.1f%% is below minimum %d%%\n", total, min > "/dev/stderr"
    exit 1
  }
}'
