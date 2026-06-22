#!/usr/bin/env bash
# Verify tree-sitter grammar CGO builds (grammars vendored via go modules).
set -euo pipefail
REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$REPO_ROOT"
# shellcheck disable=SC1091
source "$REPO_ROOT/scripts/dev/ci-native-env.sh"
export CGO_ENABLED=1
go build -tags "zvec,treesitter" -o /dev/null ./internal/indexer/chunk/ast
go test -tags "zvec,treesitter" -run 'TestParseGoTree|TestExtractSymbol|TestASTChunker' -count=1 ./internal/indexer/chunk/ast/...
echo "tree-sitter CGO spike: OK (go, python, javascript, typescript, tsx, bsl)"
