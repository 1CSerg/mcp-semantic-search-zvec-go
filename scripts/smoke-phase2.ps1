# Phase 2 gate smoke: empty project -> reindex -> HTTP search.
$ErrorActionPreference = "Stop"
$RepoRoot = Split-Path -Parent $PSScriptRoot
$SmokeRoot = Join-Path $env:TEMP "mcp-zvec-smoke-phase2"
$SmokeIndex = Join-Path $SmokeRoot "index"
$HttpPort = 18090
$MockPort = 9999
$Dims = 128

function Stop-SmokeProcs {
    foreach ($p in $script:SmokeProcs) {
        if ($p -and -not $p.HasExited) {
            Stop-Process -Id $p.Id -Force -ErrorAction SilentlyContinue
        }
    }
}

$script:SmokeProcs = @()
try {
    Get-Process -Name "mcp-semantic-search-zvec-go" -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue

    if (Test-Path $SmokeRoot) { Remove-Item -Recurse -Force $SmokeRoot }
    New-Item -ItemType Directory -Force -Path (Join-Path $SmokeRoot "pkg") | Out-Null
    Set-Content -Path (Join-Path $SmokeRoot "pkg\auth.go") -Value "package pkg`n`n// Auth middleware`nfunc Auth() {}`n" -Encoding UTF8

    & "$RepoRoot\scripts\build-zvec-windows.ps1" | Out-Null
    $LibDir = $env:ZVEC_LIB_DIR
    $env:CGO_ENABLED = "1"
    $env:Path = "$LibDir;" + $env:Path
    if (Test-Path "D:\tools\winlibs\mingw64\bin\gcc.exe") {
        $env:Path = "D:\tools\winlibs\mingw64\bin;" + $env:Path
        $env:CC = "gcc"
    }

    $env:CONFIG_PATH = Join-Path $RepoRoot "scripts\smoke\config.yaml"
    $env:WORKSPACE_ROOT = $SmokeRoot
    $env:INDEX_DIR = $SmokeIndex
    $env:AUTO_INDEX_ON_START = "false"

    $mock = Start-Process -FilePath "go" -ArgumentList @(
        "run", "$RepoRoot\scripts\smoke\mock-embed.go", "-port", $MockPort, "-dims", $Dims
    ) -PassThru -WindowStyle Hidden
    $script:SmokeProcs += $mock
    Start-Sleep -Seconds 2

    $bin = Join-Path $RepoRoot "bin\mcp-semantic-search-zvec-go.exe"
    $srv = Start-Process -FilePath $bin -ArgumentList @(
        "--http", "--http-addr", "127.0.0.1:$HttpPort"
    ) -PassThru -WindowStyle Hidden -WorkingDirectory (Join-Path $RepoRoot "bin")
    $script:SmokeProcs += $srv

    $deadline = (Get-Date).AddSeconds(15)
    do {
        try {
            Invoke-RestMethod -Uri "http://127.0.0.1:$HttpPort/health" -TimeoutSec 2 | Out-Null
            break
        } catch {
            if ((Get-Date) -gt $deadline) { throw "HTTP server did not become ready" }
            Start-Sleep -Milliseconds 500
        }
    } while ($true)

    $reindexBody = @{ force = $true } | ConvertTo-Json
    $reindex = Invoke-RestMethod -Method Post -Uri "http://127.0.0.1:$HttpPort/v1/reindex" `
        -ContentType "application/json" -Body $reindexBody
    if (-not $reindex.started) {
        throw "reindex not started: $($reindex | ConvertTo-Json -Depth 5)"
    }

    $pollDeadline = (Get-Date).AddSeconds(60)
    do {
        $status = Invoke-RestMethod -Uri "http://127.0.0.1:$HttpPort/v1/status" -TimeoutSec 5
        $running = $status.indexing.running
        if (-not $running) { break }
        if ((Get-Date) -gt $pollDeadline) { throw "indexing did not finish: $($status.indexing | ConvertTo-Json)" }
        Start-Sleep -Milliseconds 500
    } while ($true)

    if ($status.indexing.state -eq "error") {
        throw "indexing failed: $($status.indexing.error)"
    }

    $body = @{ query = "authentication middleware"; limit = 5 } | ConvertTo-Json
    $resp = Invoke-RestMethod -Method Post -Uri "http://127.0.0.1:$HttpPort/v1/search" `
        -ContentType "application/json" -Body $body
    $results = @($resp.results)
    if ($results.Count -lt 1) {
        throw "search returned no results: $($resp | ConvertTo-Json -Depth 5)"
    }

    Write-Host "PASS Phase 2 smoke: reindex + search ($($results.Count) results)"
    Write-Host "  path=$($results[0].path)"
} finally {
    Stop-SmokeProcs
}
