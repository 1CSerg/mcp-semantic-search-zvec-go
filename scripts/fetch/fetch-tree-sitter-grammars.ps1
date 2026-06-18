# Verify tree-sitter grammar CGO builds (grammars vendored via go modules).
$ErrorActionPreference = "Stop"
$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
Push-Location $RepoRoot
try {
    $env:CGO_ENABLED = "1"
    $ZvecEnv = Join-Path $RepoRoot ".deps\zvec-lib.env"
    if (Test-Path $ZvecEnv) {
        Get-Content $ZvecEnv | ForEach-Object {
            if ($_ -match '^ZVEC_LIB_DIR=(.+)$') {
                $env:PATH = "$($Matches[1]);$env:PATH"
            }
        }
    }
    go build -tags "zvec,treesitter" -o NUL github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/chunk/ast
    if ($LASTEXITCODE -ne 0) { throw "go build ast package failed" }
    go test -tags "zvec,treesitter" -run "TestParseGoTree|TestExtractSymbol|TestASTChunker" -count=1 github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/chunk/ast
    if ($LASTEXITCODE -ne 0) { throw "go test ast package failed (check gcc on PATH for 0xc0000135)" }
    Write-Host "tree-sitter CGO spike: OK (go, python, javascript, typescript, tsx)"
} finally {
    Pop-Location
}
