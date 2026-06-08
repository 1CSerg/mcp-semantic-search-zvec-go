# Phase 4 gate smoke: local ONNX profile without external embedding API.
$ErrorActionPreference = "Stop"
$RepoRoot = Split-Path -Parent $PSScriptRoot
$SmokeRoot = Join-Path $env:TEMP "mcp-zvec-smoke-phase4"
$SmokeIndex = Join-Path $SmokeRoot "index"
$SmokeModels = Join-Path $SmokeRoot ".mcp-semantic-search-zvec-go\models\paraphrase-multilingual-MiniLM-L12-v2"
$HttpPort = 18092

function Stop-SmokeProcs {
    foreach ($p in $script:SmokeProcs) {
        if ($p -and -not $p.HasExited) {
            Stop-Process -Id $p.Id -Force -ErrorAction SilentlyContinue
        }
    }
}

function Wait-Health($port) {
    $deadline = (Get-Date).AddSeconds(30)
    do {
        try {
            Invoke-RestMethod -Uri "http://127.0.0.1:$port/health" -TimeoutSec 2 | Out-Null
            return
        } catch {
            if ((Get-Date) -gt $deadline) { throw "HTTP server did not become ready on :$port" }
            Start-Sleep -Milliseconds 400
        }
    } while ($true)
}

function Wait-IndexingIdle($port) {
    $deadline = (Get-Date).AddSeconds(300)
    do {
        $status = Invoke-RestMethod -Uri "http://127.0.0.1:$port/v1/status" -TimeoutSec 10
        if (-not $status.indexing.running) { return $status }
        if ($status.indexing.state -eq "error") {
            throw "indexing failed: $($status.indexing.message)"
        }
        if ((Get-Date) -gt $deadline) { throw "indexing did not finish" }
        Start-Sleep -Milliseconds 800
    } while ($true)
}

$script:SmokeProcs = @()
try {
    Get-Process mcp-semantic-search-zvec-go -ErrorAction SilentlyContinue | Stop-Process -Force
    if (Test-Path $SmokeRoot) { Remove-Item -Recurse -Force $SmokeRoot }
    New-Item -ItemType Directory -Force -Path (Join-Path $SmokeRoot "pkg") | Out-Null
    Set-Content -Path (Join-Path $SmokeRoot "pkg\auth.go") -Value "package pkg`n`n// Auth middleware for semantic search smoke`nfunc Auth() {}`n" -Encoding ascii

    & "$RepoRoot\scripts\fetch-onnx-model.ps1" -DestDir $SmokeModels | Out-Null
    & "$RepoRoot\scripts\fetch-onnx-runtime.ps1" | Out-Null
    & "$RepoRoot\scripts\build-zvec-windows.ps1" | Out-Null

    $LibDir = $env:ZVEC_LIB_DIR
    $env:ONNXRUNTIME_SHARED_LIBRARY_PATH = Join-Path $env:ORT_LIB_DIR "onnxruntime.dll"
    $env:CGO_ENABLED = "1"
    $env:Path = "$LibDir;$env:ORT_LIB_DIR;" + $env:Path
    if (Test-Path "D:\tools\winlibs\mingw64\bin\gcc.exe") {
        $env:Path = "D:\tools\winlibs\mingw64\bin;" + $env:Path
        $env:CC = "gcc"
    }

    $configDir = Join-Path $SmokeRoot ".mcp-semantic-search-zvec-go"
    New-Item -ItemType Directory -Force -Path $configDir | Out-Null
    Copy-Item -Force (Join-Path $RepoRoot "scripts\smoke\onnx-config.yaml") (Join-Path $configDir "config.yaml")

    $env:CONFIG_PATH = Join-Path $configDir "config.yaml"
    $env:WORKSPACE_ROOT = $SmokeRoot
    $env:INDEX_DIR = $SmokeIndex
    $env:AUTO_INDEX_ON_START = "false"
    $env:FILE_WATCHER_ENABLED = "false"

    $bin = Join-Path $RepoRoot "bin\mcp-semantic-search-zvec-go.exe"
    $srv = Start-Process -FilePath $bin -ArgumentList @(
        "--http", "--http-addr", "127.0.0.1:$HttpPort"
    ) -PassThru -WindowStyle Hidden -WorkingDirectory (Join-Path $RepoRoot "bin")
    $script:SmokeProcs += $srv
    Wait-Health $HttpPort

    $status = Invoke-RestMethod -Uri "http://127.0.0.1:$HttpPort/v1/status" -TimeoutSec 10
    if ($status.embedding_provider -ne "onnx") {
        throw "expected onnx provider, got $($status.embedding_provider)"
    }
    if ($status.bootstrap -eq $true -or $status.phase -ne "4") {
        throw "service running in stub/bootstrap mode: $($status | ConvertTo-Json -Compress)"
    }

    $reindex = Invoke-RestMethod -Method Post -Uri "http://127.0.0.1:$HttpPort/v1/reindex" `
        -ContentType "application/json" -Body (@{ force = $true } | ConvertTo-Json)
    if (-not $reindex.started) { throw "reindex not started: $($reindex.message)" }
    $status = Wait-IndexingIdle $HttpPort
    if ($status.indexed_chunks_manifest -lt 1) { throw "expected indexed chunks" }

    $ready = Invoke-RestMethod -Uri "http://127.0.0.1:$HttpPort/ready" -TimeoutSec 15
    if ($ready.status -ne "ready") { throw "not ready: $($ready | ConvertTo-Json -Compress)" }

    $body = @{ query = "authentication middleware"; limit = 5 } | ConvertTo-Json
    $resp = Invoke-RestMethod -Method Post -Uri "http://127.0.0.1:$HttpPort/v1/search" `
        -ContentType "application/json" -Body $body
    if ($resp.results.Count -lt 1) { throw "expected search results, got $($resp | ConvertTo-Json -Compress)" }
    if (-not $resp.performance) { throw "missing search performance metrics" }

    Write-Host "PASS Phase 4 smoke: local_multilingual reindex + search without external API"
} finally {
    Stop-SmokeProcs
}
