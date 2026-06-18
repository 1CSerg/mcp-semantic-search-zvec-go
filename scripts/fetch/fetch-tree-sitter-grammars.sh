#!/usr/bin/env bash
# Verify tree-sitter-go CGO build (grammar is vendored via go module).
set -euo pipefail
REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$REPO_ROOT"
export CGO_ENABLED=1
go build -tags "zvec,treesitter" -o /dev/null ./internal/indexer/chunk/ast
go test -tags "zvec,treesitter" -run TestParseGoTree -count=1 ./internal/indexer/chunk/ast/...
echo "tree-sitter CGO spike: OK"
