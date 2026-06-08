# Phase 5 gate smoke: shared daemon with 3 workspaces + MCP proxy test.
$ErrorActionPreference = "Stop"
$RepoRoot = Split-Path -Parent $PSScriptRoot
$SmokeRoot = Join-Path $env:TEMP "mcp-zvec-smoke-phase5"
$HttpPort = 18095
$MockPort = 9999
$Dims = 128

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

function Wait-IndexingIdle($port, $workspaceId) {
    $deadline = (Get-Date).AddSeconds(300)
    $headers = @{ "X-Workspace-ID" = $workspaceId }
    do {
        $status = Invoke-RestMethod -Uri "http://127.0.0.1:$port/v1/status?workspace_id=$workspaceId" -Headers $headers -TimeoutSec 15
        if (-not $status.indexing.running) { return $status }
        if ($status.indexing.state -eq "error") {
            throw "indexing failed for ${workspaceId}: $($status.indexing.message)"
        }
        if ((Get-Date) -gt $deadline) { throw "indexing did not finish for $workspaceId" }
        Start-Sleep -Milliseconds 800
    } while ($true)
}

function New-Workspace($id, $keyword) {
    $root = Join-Path $SmokeRoot $id
    $pkg = Join-Path $root "pkg"
    New-Item -ItemType Directory -Force -Path $pkg | Out-Null
    Set-Content -Path (Join-Path $pkg "main.go") -Value "package pkg`n`n// $keyword unique marker for workspace $id`nfunc Handler() {}`n" -Encoding ascii
    $install = Join-Path $root ".mcp-semantic-search-zvec-go"
    New-Item -ItemType Directory -Force -Path $install | Out-Null
    Copy-Item -Force (Join-Path $RepoRoot "scripts\smoke\daemon-workspace-config.yaml") (Join-Path $install "config.yaml")
    return @{
        Id = $id
        Root = $root
        IndexDir = Join-Path $install "data\index"
        ConfigPath = Join-Path $install "config.yaml"
        Keyword = $keyword
    }
}

$script:SmokeProcs = @()
try {
    Get-Process mcp-semantic-search-zvec-go -ErrorAction SilentlyContinue | Stop-Process -Force
    if (Test-Path $SmokeRoot) { Remove-Item -Recurse -Force $SmokeRoot }
    New-Item -ItemType Directory -Force -Path $SmokeRoot | Out-Null

    $wsA = New-Workspace "ws-alpha" "alpha authentication gateway"
    $wsB = New-Workspace "ws-beta" "beta database repository layer"
    $wsC = New-Workspace "ws-gamma" "gamma middleware router pipeline"
    $workspaces = @($wsA, $wsB, $wsC)

    $daemonYaml = @"
max_open_workspaces: 2
workspaces:
"@
    foreach ($ws in $workspaces) {
        $root = ($ws.Root -replace '\\', '/')
        $idx = ($ws.IndexDir -replace '\\', '/')
        $cfg = ($ws.ConfigPath -replace '\\', '/')
        $daemonYaml += "`n  - id: $($ws.Id)`n    root: $root`n    index_dir: $idx`n    config_path: $cfg"
    }
    $daemonPath = Join-Path $SmokeRoot "daemon.yaml"
    Set-Content -Path $daemonPath -Value $daemonYaml -Encoding ascii

    & "$RepoRoot\scripts\build-zvec-windows.ps1" | Out-Null
    $LibDir = $env:ZVEC_LIB_DIR
    $env:CGO_ENABLED = "1"
    $env:Path = "$LibDir;" + $env:Path
    if (Test-Path "D:\tools\winlibs\mingw64\bin\gcc.exe") {
        $env:Path = "D:\tools\winlibs\mingw64\bin;" + $env:Path
        $env:CC = "gcc"
    }

    $mock = Start-Process -FilePath "go" -ArgumentList @(
        "run", "$RepoRoot\scripts\smoke\mock-embed.go", "-port", $MockPort, "-dims", $Dims
    ) -PassThru -WindowStyle Hidden
    $script:SmokeProcs += $mock
    Start-Sleep -Seconds 2

    $bin = Join-Path $RepoRoot "bin\mcp-semantic-search-zvec-go.exe"
    $srv = Start-Process -FilePath $bin -ArgumentList @(
        "--daemon", "--daemon-config", $daemonPath, "--http-addr", "127.0.0.1:$HttpPort"
    ) -PassThru -WindowStyle Hidden -WorkingDirectory (Join-Path $RepoRoot "bin")
    $script:SmokeProcs += $srv
    Wait-Health $HttpPort

    $list = Invoke-RestMethod -Uri "http://127.0.0.1:$HttpPort/v1/workspaces"
    if (@($list.workspaces).Count -ne 3) {
        throw "expected 3 workspaces, got $($list | ConvertTo-Json -Compress)"
    }

    foreach ($ws in $workspaces) {
        $headers = @{ "X-Workspace-ID" = $ws.Id }
        $reindex = Invoke-RestMethod -Method Post -Uri "http://127.0.0.1:$HttpPort/v1/reindex" `
            -Headers $headers -ContentType "application/json" -Body (@{ force = $true; workspace_id = $ws.Id } | ConvertTo-Json)
        if (-not $reindex.started) { throw "reindex not started for $($ws.Id): $($reindex.message)" }
        Wait-IndexingIdle $HttpPort $ws.Id | Out-Null
    }

    foreach ($ws in $workspaces) {
        $headers = @{ "X-Workspace-ID" = $ws.Id }
        $body = @{ query = $ws.Keyword; limit = 3; workspace_id = $ws.Id } | ConvertTo-Json
        $resp = Invoke-RestMethod -Method Post -Uri "http://127.0.0.1:$HttpPort/v1/search" `
            -Headers $headers -ContentType "application/json" -Body $body
        if ($resp.results.Count -lt 1) { throw "no results for $($ws.Id)" }
        $path = [string]$resp.results[0].path
        if ($path -notmatch "main.go") { throw "unexpected path for $($ws.Id): $path" }
    }

    # Cross-workspace isolation: alpha query on beta workspace should not return alpha-specific path reliably
    $cross = Invoke-RestMethod -Method Post -Uri "http://127.0.0.1:$HttpPort/v1/search" `
        -Headers @{ "X-Workspace-ID" = "ws-beta" } -ContentType "application/json" `
        -Body (@{ query = "alpha authentication gateway"; limit = 3; workspace_id = "ws-beta" } | ConvertTo-Json)
    foreach ($r in @($cross.results)) {
        if ($r.snippet -match "alpha authentication") {
            throw "cross-workspace leak: beta returned alpha snippet"
        }
    }

    # Concurrent load
    $jobs = @()
    foreach ($ws in $workspaces) {
        $jobs += Start-Job -ScriptBlock {
            param($Port, $WsId)
            1..5 | ForEach-Object {
                Invoke-RestMethod -Uri "http://127.0.0.1:$Port/v1/status?workspace_id=$WsId" -TimeoutSec 30 | Out-Null
            }
        } -ArgumentList $HttpPort, $ws.Id
    }
    Wait-Job -Job $jobs -Timeout 120 | Out-Null
    foreach ($j in $jobs) {
        if ((Receive-Job $j 2>&1) -match "fail") { throw "concurrent load job failed" }
        Remove-Job $j
    }

    Push-Location $RepoRoot
    go test ./internal/transport/mcp/ -run TestMCPOverHTTPProxy -count=1
    if ($LASTEXITCODE -ne 0) { throw "MCP proxy test failed" }
    Pop-Location

    Write-Host "PASS Phase 5 smoke: daemon with 3 workspaces, search isolation, concurrent status, MCP proxy test"
} finally {
    Stop-SmokeProcs
    Get-Job -ErrorAction SilentlyContinue | Remove-Job -Force -ErrorAction SilentlyContinue
}
