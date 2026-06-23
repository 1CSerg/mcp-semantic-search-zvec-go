#!/usr/bin/env bash
# Export PATH (Windows) or LD_LIBRARY_PATH (Unix) for zvec / ONNX Runtime CGO tests.
# Usage: source scripts/dev/ci-native-env.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck disable=SC1091
source "$ROOT/scripts/dev/native-path.sh"

if [[ -f "$ROOT/.deps/zvec-lib.env" ]]; then
	# shellcheck disable=SC1091
	set -a
	source "$ROOT/.deps/zvec-lib.env"
	set +a
fi
if [[ -f "$ROOT/.deps/onnxruntime.env" ]]; then
	# shellcheck disable=SC1091
	set -a
	source "$ROOT/.deps/onnxruntime.env"
	set +a
fi

case "$(uname -s 2>/dev/null)" in
MINGW* | MSYS* | CYGWIN*)
	if [[ -n "${ZVEC_LIB_DIR:-}" ]]; then
		wp="$(native_path "$ZVEC_LIB_DIR")"
		export PATH="$wp${PATH:+;$PATH}"
	fi
	if [[ -n "${ORT_LIB_DIR:-}" ]]; then
		wp="$(native_path "$ORT_LIB_DIR")"
		export PATH="$wp${PATH:+;$PATH}"
	fi
	;;
*)
	if [[ -n "${ZVEC_LIB_DIR:-}" ]]; then
		export LD_LIBRARY_PATH="$ZVEC_LIB_DIR${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
	fi
	if [[ -n "${ORT_LIB_DIR:-}" ]]; then
		export LD_LIBRARY_PATH="$ORT_LIB_DIR${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
	fi
	;;
esac
