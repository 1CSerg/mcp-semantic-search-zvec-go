#!/usr/bin/env bash
set -euo pipefail

MIN="${COVERAGE_MIN:-85}"
PKG_MIN="${COVERAGE_PKG_MIN:-50}"
PROFILE="${COVERAGE_PROFILE:-coverage.out}"
PACKAGES="${COVERAGE_PACKAGES:-./internal/...}"

set +e
output="$(go test -coverprofile="$PROFILE" $PACKAGES 2>&1)"
status=$?
set -e

printf '%s\n' "$output"
if [ "$status" -ne 0 ]; then
	exit "$status"
fi

total="$(go tool cover -func="$PROFILE" | awk '/^total:/ {gsub(/%/, "", $3); print $3}')"
echo "coverage: ${total}% (project minimum ${MIN}%, packages: ${PACKAGES})"

fail=0
while IFS= read -r line; do
	pkg="$(echo "$line" | awk '{print $2}')"
	pct="$(echo "$line" | sed -n 's/.*coverage: \([0-9.]*\)%.*/\1/p')"
	if [ -z "$pct" ]; then
		continue
	fi
	echo "  package ${pkg}: ${pct}% (module minimum ${PKG_MIN}%)"
	awk -v pct="$pct" -v pkg_min="$PKG_MIN" -v pkg="$pkg" 'BEGIN {
		if (pct + 0 < pkg_min + 0) {
			printf "package %s: %.1f%% is below module minimum %d%%\n", pkg, pct, pkg_min > "/dev/stderr"
			exit 1
		}
	}' || fail=1
done < <(printf '%s\n' "$output" | grep 'coverage:.*% of statements')

awk -v total="$total" -v min="$MIN" 'BEGIN {
	if (total + 0 < min + 0) {
		printf "coverage %.1f%% is below project minimum %d%%\n", total, min > "/dev/stderr"
		exit 1
	}
}' || fail=1

exit "$fail"
