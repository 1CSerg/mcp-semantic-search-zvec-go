# Phase 1 gate smoke: seed-index -> HTTP /v1/search with ranked results.
$ErrorActionPreference = "Stop"
$ScriptDir = $PSScriptRoot
$RepoRoot = (Resolve-Path (Join-Path $ScriptDir "..\..")).Path
$SmokeIndex = Join-Path $env:TEMP "mcp-zvec-smoke-index"
$HttpPort = 18089
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

    & "$RepoRoot\scripts\dev\build-zvec-windows.ps1" | Out-Null

    $LibDir = $env:ZVEC_LIB_DIR
    $env:CGO_ENABLED = "1"
    $env:Path = "$LibDir;" + $env:Path
    if (Test-Path "D:\tools\winlibs\mingw64\bin\gcc.exe") {
        $env:Path = "D:\tools\winlibs\mingw64\bin;" + $env:Path
        $env:CC = "gcc"
    }

    $env:CONFIG_PATH = Join-Path $ScriptDir "config.yaml"
    $env:WORKSPACE_ROOT = $RepoRoot
    $env:INDEX_DIR = $SmokeIndex
    if (Test-Path $SmokeIndex) { Remove-Item -Recurse -Force $SmokeIndex }
    New-Item -ItemType Directory -Force -Path $SmokeIndex | Out-Null

    $mock = Start-Process -FilePath "go" -ArgumentList @(
        "run", "$ScriptDir\mock-embed.go", "-port", $MockPort, "-dims", $Dims
    ) -PassThru -WindowStyle Hidden
    $script:SmokeProcs += $mock
    Start-Sleep -Seconds 2

    Push-Location $RepoRoot
    go run -tags zvec ./cmd/seed-index -n 100
    if ($LASTEXITCODE -ne 0) { throw "seed-index failed" }

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

    $body = @{ query = "authentication"; limit = 5 } | ConvertTo-Json
    $resp = Invoke-RestMethod -Method Post -Uri "http://127.0.0.1:$HttpPort/v1/search" `
        -ContentType "application/json" -Body $body

    $results = @($resp.results)
    if ($results.Count -lt 1) {
        throw "search returned no results: $($resp | ConvertTo-Json -Depth 5)"
    }
    foreach ($r in $results) {
        if (-not $r.path -or $null -eq $r.score) {
            throw "result missing path/score: $($r | ConvertTo-Json -Depth 3)"
        }
    }

    Write-Host "PASS Phase 1 smoke: $($results.Count) results, top score=$($results[0].score)"
    Write-Host "  path=$($results[0].path) snippet=$($results[0].snippet)"
} finally {
    Stop-SmokeProcs
    Pop-Location -ErrorAction SilentlyContinue
}
