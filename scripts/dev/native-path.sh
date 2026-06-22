#!/usr/bin/env bash
# native_path: MSYS/Git-Bash path -> Windows path for native PE DLL search.
native_path() {
	local p="$1"
	case "$(uname -s 2>/dev/null)" in
	MINGW* | MSYS* | CYGWIN*)
		if command -v cygpath >/dev/null 2>&1; then
			cygpath -w "$p"
			return
		fi
		if [[ "$p" =~ ^/([a-zA-Z])/(.*)$ ]]; then
			local drive rest
			drive="$(printf '%s' "${BASH_REMATCH[1]}" | tr '[:lower:]' '[:upper:]')"
			rest="${BASH_REMATCH[2]}"
			rest="${rest//\\//}"
			rest="${rest//\//\\}"
			printf '%s:\\%s' "$drive" "$rest"
			return
		fi
		;;
	esac
	printf '%s' "$p"
}
