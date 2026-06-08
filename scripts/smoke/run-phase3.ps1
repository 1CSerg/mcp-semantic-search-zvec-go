# Phase 3 gate smoke: reconnect resilience + watcher + /ready.
param(
    [int]$Reconnects = $(if ($env:SMOKE_PHASE3_RECONNECTS) { [int]$env:SMOKE_PHASE3_RECONNECTS } else { 10 })
)

$ErrorActionPreference = "Stop"
$ScriptDir = $PSScriptRoot
$RepoRoot = (Resolve-Path (Join-Path $ScriptDir "..\..")).Path
$SmokeRoot = Join-Path $env:TEMP "mcp-zvec-smoke-phase3"
$SmokeIndex = Join-Path $SmokeRoot "index"
$HttpPort = 18091
$MockPort = 9998
$Dims = 128

function Stop-SmokeProcs {
    foreach ($p in $script:SmokeProcs) {
        if ($p -and -not $p.HasExited) {
            Stop-Process -Id $p.Id -Force -ErrorAction SilentlyContinue
        }
    }
}

function Wait-Health($port) {
    $deadline = (Get-Date).AddSeconds(15)
    do {
        try {
            Invoke-RestMethod -Uri "http://127.0.0.1:$port/health" -TimeoutSec 2 | Out-Null
            return
        } catch {
            if ((Get-Date) -gt $deadline) { throw "HTTP server did not become ready on :$port" }
            Start-Sleep -Milliseconds 300
        }
    } while ($true)
}

function Wait-IndexingIdle($port) {
    $deadline = (Get-Date).AddSeconds(60)
    do {
        $status = Invoke-RestMethod -Uri "http://127.0.0.1:$port/v1/status" -TimeoutSec 5
        if (-not $status.indexing.running) { return $status }
        if ((Get-Date) -gt $deadline) { throw "indexing did not finish" }
        Start-Sleep -Milliseconds 400
    } while ($true)
}

$script:SmokeProcs = @()
try {
    if (Test-Path $SmokeRoot) { Remove-Item -Recurse -Force $SmokeRoot }
    New-Item -ItemType Directory -Force -Path (Join-Path $SmokeRoot "pkg") | Out-Null
    Set-Content -Path (Join-Path $SmokeRoot "pkg\auth.go") -Value "package pkg`n`n// Auth middleware`nfunc Auth() {}`n" -Encoding UTF8

    & "$RepoRoot\scripts\dev\build-zvec-windows.ps1" | Out-Null
    $LibDir = $env:ZVEC_LIB_DIR
    $env:CGO_ENABLED = "1"
    $env:Path = "$LibDir;" + $env:Path
    if (Test-Path "D:\tools\winlibs\mingw64\bin\gcc.exe") {
        $env:Path = "D:\tools\winlibs\mingw64\bin;" + $env:Path
        $env:CC = "gcc"
    }

    $env:CONFIG_PATH = Join-Path $ScriptDir "config.yaml"
    $env:WORKSPACE_ROOT = $SmokeRoot
    $env:INDEX_DIR = $SmokeIndex
    $env:AUTO_INDEX_ON_START = "false"
    $env:FILE_WATCHER_ENABLED = "true"
    $env:FILE_WATCHER_BACKEND = "polling"
    $env:FILE_WATCHER_POLL_INTERVAL_SECONDS = "1"

    $mock = Start-Process -FilePath "go" -ArgumentList @(
        "run", "$ScriptDir\mock-embed.go", "-port", $MockPort, "-dims", $Dims
    ) -PassThru -WindowStyle Hidden
    $script:SmokeProcs += $mock
    Start-Sleep -Seconds 2

    $bin = Join-Path $RepoRoot "bin\mcp-semantic-search-zvec-go.exe"
    for ($i = 1; $i -le $Reconnects; $i++) {
        $srv = Start-Process -FilePath $bin -ArgumentList @(
            "--http", "--http-addr", "127.0.0.1:$HttpPort"
        ) -PassThru -WindowStyle Hidden -WorkingDirectory (Join-Path $RepoRoot "bin")
        $script:SmokeProcs += $srv
        Wait-Health $HttpPort

        if ($i -eq 1) {
            $reindex = Invoke-RestMethod -Method Post -Uri "http://127.0.0.1:$HttpPort/v1/reindex" `
                -ContentType "application/json" -Body (@{ force = $true } | ConvertTo-Json)
            if (-not $reindex.started) { throw "reindex not started" }
            $status = Wait-IndexingIdle $HttpPort
            if ($status.indexing.state -eq "error") { throw "indexing failed" }
        }

        $ready = Invoke-RestMethod -Uri "http://127.0.0.1:$HttpPort/ready" -TimeoutSec 5
        if ($ready.status -ne "ready") { throw "not ready on cycle $i : $($ready | ConvertTo-Json)" }

        Stop-Process -Id $srv.Id -Force
        Start-Sleep -Milliseconds 400
        if (Test-Path (Join-Path $SmokeIndex "index.lock")) {
            throw "stale index.lock after reconnect cycle $i"
        }
    }

    $srv = Start-Process -FilePath $bin -ArgumentList @(
        "--http", "--http-addr", "127.0.0.1:$HttpPort"
    ) -PassThru -WindowStyle Hidden -WorkingDirectory (Join-Path $RepoRoot "bin")
    $script:SmokeProcs += $srv
    Wait-Health $HttpPort
    Wait-IndexingIdle $HttpPort | Out-Null

    Add-Content -Path (Join-Path $SmokeRoot "pkg\auth.go") -Value "`n// watcher touch`n" -Encoding UTF8
    Start-Sleep -Seconds 4
    $status = Wait-IndexingIdle $HttpPort
    if (-not $status.file_watcher.enabled_in_config) {
        throw "file watcher not enabled in status"
    }

    $body = @{ query = "authentication middleware"; limit = 5 } | ConvertTo-Json
    $resp = Invoke-RestMethod -Method Post -Uri "http://127.0.0.1:$HttpPort/v1/search" `
        -ContentType "application/json" -Body $body
    if (-not $resp.performance) { throw "missing search performance metrics" }

    Write-Host "PASS Phase 3 smoke: $Reconnects reconnects + watcher + /ready"
} finally {
    Stop-SmokeProcs
}
