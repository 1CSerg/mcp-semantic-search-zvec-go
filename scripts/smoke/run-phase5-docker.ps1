# Phase 5 Docker smoke: shared daemon container with 2 workspaces + isolation checks.
$ErrorActionPreference = "Stop"
. (Join-Path (Split-Path $PSScriptRoot -Parent) 'lib\Stay-OpenOnError.ps1')
$ScriptDir = $PSScriptRoot
$RepoRoot = (Resolve-Path (Join-Path $ScriptDir "..\..")).Path
$SmokeRoot = Join-Path $env:TEMP "mcp-zvec-smoke-phase5-docker"
$HttpPort = 18096
$MockPort = 9999
$ComposeProject = "mcp-smoke-daemon"
$ComposeFile = Join-Path $RepoRoot "docker\docker-compose.daemon.yml"

function Invoke-Docker {
    param([switch]$Quiet)
    $cmdArgs = $args
    $prevEap = $ErrorActionPreference
    $ErrorActionPreference = "Continue"
    try {
        if ($Quiet) {
            & docker @cmdArgs *>$null
        } else {
            $lines = & docker @cmdArgs 2>&1
            foreach ($line in @($lines)) {
                if ($line -is [System.Management.Automation.ErrorRecord]) {
                    Write-Host $line.ToString()
                } else {
                    Write-Host $line
                }
            }
        }
        $code = $LASTEXITCODE
        if ($null -eq $code) { $code = 0 }
        return $code
    } finally {
        $ErrorActionPreference = $prevEap
    }
}

function Wait-MockEmbed {
    param([int]$Port)
    $deadline = (Get-Date).AddSeconds(30)
    do {
        try {
            Invoke-RestMethod -Uri "http://127.0.0.1:$Port/v1/models" -TimeoutSec 2 | Out-Null
            return
        } catch {
            if ((Get-Date) -gt $deadline) {
                throw "mock embeddings not ready on :$Port (is port in use?)"
            }
            Start-Sleep -Milliseconds 500
        }
    } while ($true)
}

function Wait-Health {
    param([int]$Port)
    $deadline = (Get-Date).AddSeconds(120)
    do {
        try {
            Invoke-RestMethod -Uri "http://127.0.0.1:$Port/health" -TimeoutSec 3 | Out-Null
            return
        } catch {
            if ((Get-Date) -gt $deadline) { throw "daemon HTTP not ready on :$Port" }
            Start-Sleep -Seconds 2
        }
    } while ($true)
}

function Wait-IndexingIdle {
    param([int]$Port, [string]$WorkspaceId)
    $deadline = (Get-Date).AddSeconds(300)
    do {
        $status = Invoke-RestMethod -Uri "http://127.0.0.1:$Port/v1/status?workspace_id=$WorkspaceId" -TimeoutSec 15
        if (-not $status.indexing.running) { return $status }
        if ($status.indexing.state -eq "error") {
            throw "indexing failed for ${WorkspaceId}: $($status.indexing.message)"
        }
        if ((Get-Date) -gt $deadline) { throw "indexing did not finish for $WorkspaceId" }
        Start-Sleep -Milliseconds 800
    } while ($true)
}

function New-Workspace {
    param([string]$Id, [string]$Keyword, [int]$EmbedPort)
    $root = Join-Path $SmokeRoot $Id
    $pkg = Join-Path $root "pkg"
    New-Item -ItemType Directory -Force -Path $pkg | Out-Null
    Set-Content -Path (Join-Path $pkg "main.go") -Value "package pkg`n`n// $Keyword unique marker for workspace $Id`nfunc Handler() {}`n" -Encoding ascii
    $install = Join-Path $root ".mcp-semantic-search-zvec-go"
    New-Item -ItemType Directory -Force -Path (Join-Path $install "data\index") | Out-Null
    $config = Get-Content -Raw (Join-Path $ScriptDir "daemon-workspace-config-docker.yaml")
    $config = $config -replace "http://host\.docker\.internal:9999/v1", "http://host.docker.internal:$EmbedPort/v1"
    Set-Content -Path (Join-Path $install "config.yaml") -Value $config -Encoding ascii
    return @{
        Id = $Id
        Root = (Resolve-Path $root).Path
        IndexDir = Join-Path $install "data\index"
    }
}

$mock = $null
$savedEnv = @{}
$composeStarted = $false
$failed = $false
try {
    $docker = Get-Command docker.exe -ErrorAction SilentlyContinue
    if (-not $docker) { $docker = Get-Command docker -ErrorAction SilentlyContinue }
    if (-not $docker) {
        Write-Warning "docker not found; skipping Phase 5 Docker smoke"
        return
    }
    try {
        if ((Invoke-Docker -Quiet info) -ne 0) { throw "daemon unavailable" }
    } catch {
        Write-Warning "docker daemon not running; skipping Phase 5 Docker smoke"
        return
    }

    Invoke-Docker -Quiet rm -f mcp-semantic-search-zvec-go-daemon | Out-Null
    if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
        throw "go not found in PATH (required for mock embeddings on :$MockPort)"
    }
    if (Test-Path $SmokeRoot) { Remove-Item -Recurse -Force $SmokeRoot }
    New-Item -ItemType Directory -Force -Path $SmokeRoot | Out-Null

    $wsA = New-Workspace "ws-alpha" "alpha authentication gateway" $MockPort
    $wsB = New-Workspace "ws-beta" "beta database repository layer" $MockPort

    $daemonYaml = @"
max_open_workspaces: 4
workspaces:
  - id: ws-alpha
    root: /workspaces/ws-alpha
    index_dir: /workspaces/ws-alpha-index
    config_path: /workspaces/ws-alpha/.mcp-semantic-search-zvec-go/config.yaml
  - id: ws-beta
    root: /workspaces/ws-beta
    index_dir: /workspaces/ws-beta-index
    config_path: /workspaces/ws-beta/.mcp-semantic-search-zvec-go/config.yaml
"@
    $daemonPath = Join-Path $SmokeRoot "daemon.yaml"
    Set-Content -Path $daemonPath -Value $daemonYaml -Encoding ascii

    $mock = Start-Process -FilePath "go" -ArgumentList @(
        "run", "$ScriptDir\mock-embed.go", "-port", $MockPort, "-dims", 128
    ) -PassThru -WindowStyle Hidden
    Wait-MockEmbed $MockPort

    $env:HTTP_PORT = [string]$HttpPort
    $env:WS_ALPHA_ROOT = $wsA.Root
    $env:WS_ALPHA_INDEX = $wsA.IndexDir
    $env:WS_BETA_ROOT = $wsB.Root
    $env:WS_BETA_INDEX = $wsB.IndexDir
    $env:DAEMON_CONFIG_PATH = $daemonPath
    $savedEnv = @{
        HTTP_PORT = $env:HTTP_PORT
        WS_ALPHA_ROOT = $env:WS_ALPHA_ROOT
        WS_ALPHA_INDEX = $env:WS_ALPHA_INDEX
        WS_BETA_ROOT = $env:WS_BETA_ROOT
        WS_BETA_INDEX = $env:WS_BETA_INDEX
        DAEMON_CONFIG_PATH = $env:DAEMON_CONFIG_PATH
    }

    Push-Location $RepoRoot
    try {
        Write-Host "Building and starting Docker daemon (first run may take several minutes)..."
        if ((Invoke-Docker compose -f $ComposeFile --project-name $ComposeProject up --build -d) -ne 0) {
            throw "docker compose up failed"
        }
        $composeStarted = $true
    } finally {
        Pop-Location
    }

    Wait-Health $HttpPort

    $list = Invoke-RestMethod -Uri "http://127.0.0.1:$HttpPort/v1/workspaces"
    if (@($list.workspaces).Count -ne 2) {
        throw "expected 2 workspaces, got $($list | ConvertTo-Json -Compress)"
    }

    foreach ($ws in @($wsA, $wsB)) {
        $body = @{ force = $true; workspace_id = $ws.Id } | ConvertTo-Json
        $reindex = Invoke-RestMethod -Method Post -Uri "http://127.0.0.1:$HttpPort/v1/reindex" `
            -ContentType "application/json" -Body $body
        if (-not $reindex.started) { throw "reindex not started for $($ws.Id)" }
        Wait-IndexingIdle $HttpPort $ws.Id | Out-Null
    }

    $statusA = Invoke-RestMethod -Uri "http://127.0.0.1:$HttpPort/v1/status?workspace_id=ws-alpha"
    $statusB = Invoke-RestMethod -Uri "http://127.0.0.1:$HttpPort/v1/status?workspace_id=ws-beta"
    if ([string]$statusA.workspace_root -eq [string]$statusB.workspace_root) {
        throw "daemon reports same workspace_root for both ids"
    }

    $cross = Invoke-RestMethod -Method Post -Uri "http://127.0.0.1:$HttpPort/v1/search" `
        -ContentType "application/json" `
        -Body (@{ query = "alpha authentication gateway"; limit = 3; workspace_id = "ws-beta" } | ConvertTo-Json)
    foreach ($r in @($cross.results)) {
        if ([string]$r.snippet -match "alpha authentication") {
            throw "cross-workspace leak in Docker daemon"
        }
    }

    Push-Location $RepoRoot
    go test ./internal/transport/mcp/ -run TestMCPOverHTTPProxy -count=1
    if ($LASTEXITCODE -ne 0) { throw "MCP proxy unit test failed" }
    Pop-Location

    Write-Host "PASS Phase 5 Docker smoke: daemon container, 2 workspaces, isolation"
} catch {
    $failed = $true
    Write-Error $_
} finally {
    if ($mock -and -not $mock.HasExited) { Stop-Process -Id $mock.Id -Force -ErrorAction SilentlyContinue }
    if ($composeStarted) {
        if ($savedEnv.Count -gt 0) {
            foreach ($k in $savedEnv.Keys) { Set-Item -Path "Env:$k" -Value $savedEnv[$k] }
        }
        Invoke-Docker -Quiet compose -f $ComposeFile --project-name $ComposeProject down -v | Out-Null
    }
    Remove-Item Env:HTTP_PORT, Env:WS_ALPHA_ROOT, Env:WS_ALPHA_INDEX, Env:WS_BETA_ROOT, Env:WS_BETA_INDEX, Env:DAEMON_CONFIG_PATH -ErrorAction SilentlyContinue
    if ($failed) { Wait-IfInteractiveOnError }
}

if ($failed) { exit 1 }
