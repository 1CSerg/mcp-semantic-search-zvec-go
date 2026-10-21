# Native end-to-end acceptance for the global layout (Windows wrapper).
#
# Runs scripts/smoke/global-daemon-check.go against the production binary
# (tags zvec,onnx,treesitter): one shared daemon, several projects, real MCP
# stdio proxies performing initialize/tools-call, and per-collection isolation.
#
# Everything happens in a temporary MCP_GLOBAL_DIR and temporary project roots;
# the driver stops every process it started.
#
# Usage:
#   pwsh -File scripts/smoke/run-global-daemon.ps1 [-Binary <path>] [-KeepWork]
#
# Exit codes: 0 all checks passed, 1 a check failed, 2 bad invocation.

[CmdletBinding()]
param(
    [string]$Binary = "",
    [switch]$KeepWork,
    [int]$TimeoutSeconds = 120
)

$ErrorActionPreference = "Stop"
$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path

$work = Join-Path ([System.IO.Path]::GetTempPath()) ("mcp-global-daemon-{0}" -f [guid]::NewGuid().ToString('N').Substring(0, 8))
New-Item -ItemType Directory -Force -Path $work | Out-Null

$cleanup = {
    # The driver stops its own daemon; this is a safety net for a hard failure.
    # The daemon's command line carries no MCP_GLOBAL_DIR (it travels in the
    # environment), so match it by the user dir it is serving: every daemon whose
    # runtime lock lives under our work dir is ours.
    $lockPath = Join-Path $work "run\userdir\runtime\daemon.lock"
    if (Test-Path $lockPath) {
        try {
            $pid = ((Get-Content -Raw $lockPath).Split()[0] -replace '^pid=', '')
            if ($pid -match '^\d+$') {
                Stop-Process -Id ([int]$pid) -Force -ErrorAction SilentlyContinue
            }
        } catch { }
    }
    if (-not $KeepWork) { Remove-Item -Recurse -Force $work -ErrorAction SilentlyContinue }
    else { Write-Host "Kept work dir: $work" }
}

try {
    if (-not $Binary) {
        $Binary = Join-Path $RepoRoot "bin\mcp-semantic-search-zvec-go.exe"
    }
    if (-not (Test-Path $Binary)) {
        Write-Host "Binary not found at $Binary; building with native tags..."
        Push-Location $RepoRoot
        try {
            & "$RepoRoot\scripts\dev\build-zvec-windows.ps1"
            if ($LASTEXITCODE -ne 0) { throw "native build failed with exit code $LASTEXITCODE" }
        } finally {
            Pop-Location
        }
        $Binary = Join-Path $RepoRoot "bin\mcp-semantic-search-zvec-go.exe"
    }

    # A native build must report a version; a stub build is rejected because it
    # cannot open zvec and would make the isolation checks meaningless.
    $version = & $Binary --version 2>&1
    if ($LASTEXITCODE -ne 0) {
        throw "binary $Binary does not run: $version"
    }
    Write-Host "Native end-to-end smoke: $Binary (v$((($version | Out-String).Trim())))"

    Push-Location $RepoRoot
    try {
        $driver = Join-Path $work "global-daemon-check.exe"
        & go build -o $driver ./scripts/smoke/global-daemon-check.go
        if ($LASTEXITCODE -ne 0) { throw "driver build failed with exit code $LASTEXITCODE" }

        $args = @("-binary", $Binary, "-work", (Join-Path $work "run"), "-timeout", "$($TimeoutSeconds)s")
        if ($KeepWork) { $args += "-keep" }
        & $driver @args
        $code = $LASTEXITCODE
    } finally {
        Pop-Location
    }
    exit $code
} catch {
    Write-Error $_
    exit 1
} finally {
    & $cleanup
}
