# Verify tree-sitter grammar CGO builds (grammars vendored via go modules).
$ErrorActionPreference = "Stop"
. (Join-Path (Split-Path $PSScriptRoot -Parent) 'lib\Stay-OpenOnError.ps1')
$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
Push-Location $RepoRoot
$failed = $false
try {
    $env:CGO_ENABLED = "1"
    . (Join-Path $RepoRoot "scripts\dev\ci-native-env.ps1")
    go build -tags "zvec,treesitter" -o NUL github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/chunk/ast
    if ($LASTEXITCODE -ne 0) { throw "go build ast package failed" }
    go test -tags "zvec,treesitter" -run "TestParseGoTree|TestExtractSymbol|TestASTChunker" -count=1 github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/chunk/ast
    if ($LASTEXITCODE -ne 0) { throw "go test ast package failed (check gcc on PATH for 0xc0000135)" }
    Write-Host "tree-sitter CGO spike: OK (go, python, javascript, typescript, tsx, bsl)"
} catch {
    $failed = $true
    Write-Error $_
} finally {
    Pop-Location
    if ($failed) { Wait-IfInteractiveOnError }
}
if ($failed) { exit 1 }
