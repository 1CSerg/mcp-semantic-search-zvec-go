$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'Stay-OpenOnError.ps1')

$failed = $false
$exitCode = 1
try {
    Set-Location -LiteralPath $PSScriptRoot
    & (Join-Path $PSScriptRoot 'mcp-semantic-search-zvec-go.exe') @args
    if ($LASTEXITCODE -ne 0) {
        $exitCode = $LASTEXITCODE
        throw "mcp-semantic-search-zvec-go.exe exited with code $LASTEXITCODE"
    }
} catch {
    $failed = $true
    Write-Error $_
} finally {
    if ($failed) { Wait-IfInteractiveOnError -ExitCode $exitCode }
}
if ($failed) { exit $exitCode }
