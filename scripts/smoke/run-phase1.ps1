# Phase 1 gate smoke: seed-index -> HTTP /v1/status -> /v1/search (plain + path_glob).
#
# The run is fully isolated: a throw-away workspace under %TEMP% holds the config,
# the index and the seeded collection, so the repo root index is never touched and
# INDEX_DIR/CONFIG_PATH never violate path containment (no warning noise).
# Ports are allocated dynamically and both helper processes are built binaries, so
# a leftover process can neither block the run nor survive the cleanup.
# Failures print the server log tail instead of a bare "no results".
$ErrorActionPreference = "Stop"
. (Join-Path (Split-Path $PSScriptRoot -Parent) 'lib\Stay-OpenOnError.ps1')

$ScriptDir = $PSScriptRoot
$RepoRoot = (Resolve-Path (Join-Path $ScriptDir "..\..")).Path
$Dims = 128
$SmokeRoot = Join-Path $env:TEMP "mcp-zvec-smoke-phase1"
$WorkspaceRoot = $SmokeRoot
$IndexDir = Join-Path $SmokeRoot "index"
$ConfigPath = Join-Path $SmokeRoot "config.yaml"
$LogFile = Join-Path $SmokeRoot "logs\server.log"
# Seeded chunk paths are internal/module/file_<n>.go (see cmd/seed-index).
$SeedGlob = "internal/module/*.go"
$MissGlob = "does/not/match/*.go"

function Stop-SmokeProcs {
    foreach ($p in $script:SmokeProcs) {
        if ($p -and -not $p.HasExited) {
            Stop-Process -Id $p.Id -Force -ErrorAction SilentlyContinue
        }
    }
}

function Get-FreePort {
    param([string]$CheckBin)
    $port = & $CheckBin free-port
    if ($LASTEXITCODE -ne 0 -or -not $port) { throw "cannot allocate a free TCP port" }
    return [int]$port
}

function Assert-Status {
    param([string]$BaseUrl, [string]$CheckBin)
    $json = Invoke-RestMethod -Uri "$BaseUrl/v1/status" -TimeoutSec 10 | ConvertTo-Json -Depth 6
    $json | & $CheckBin status
    if ($LASTEXITCODE -ne 0) { throw "seeded index is not visible to the server" }
}

function Assert-Results {
    param([string]$Json, [int]$Min, [string]$Glob, [string]$CheckBin, [string]$FailMessage)
    $Json | & $CheckBin results "$Min" $Glob
    if ($LASTEXITCODE -ne 0) { throw $FailMessage }
}

function Wait-HttpReady {
    param([string]$BaseUrl)
    $deadline = (Get-Date).AddSeconds(20)
    do {
        try {
            Invoke-RestMethod -Uri "$BaseUrl/health" -TimeoutSec 2 | Out-Null
            return
        } catch {
            if ((Get-Date) -gt $deadline) { throw "HTTP server did not become ready on $BaseUrl" }
            Start-Sleep -Milliseconds 500
        }
    } while ($true)
}

function Search-Smoke {
    param([string]$BaseUrl, [string]$Query, [int]$Limit, [string]$PathGlob)
    $payload = @{ query = $Query; limit = $Limit }
    if ($PathGlob) { $payload.path_glob = $PathGlob }
    $body = $payload | ConvertTo-Json
    return Invoke-RestMethod -Method Post -Uri "$BaseUrl/v1/search" `
        -ContentType "application/json" -Body $body -TimeoutSec 30
}

function Show-ServerLogTail {
    if (Test-Path $LogFile) {
        Write-Host "--- server.log (tail) ---"
        Get-Content $LogFile -Tail 25 | ForEach-Object { Write-Host $_ }
    }
}

$script:SmokeProcs = @()
$failed = $false
try {
    & "$RepoRoot\scripts\dev\build-zvec-windows.ps1" | Out-Null

    $LibDir = $env:ZVEC_LIB_DIR
    $env:CGO_ENABLED = "1"
    $env:Path = "$LibDir;" + $env:Path
    if (Test-Path "D:\tools\winlibs\mingw64\bin\gcc.exe") {
        $env:Path = "D:\tools\winlibs\mingw64\bin;" + $env:Path
        $env:CC = "gcc"
    }

    if (Test-Path $SmokeRoot) { Remove-Item -Recurse -Force $SmokeRoot }
    New-Item -ItemType Directory -Force -Path $WorkspaceRoot, $IndexDir | Out-Null

    $binDir = Join-Path $RepoRoot "bin"
    New-Item -ItemType Directory -Force -Path $binDir | Out-Null
    $checkBin = Join-Path $binDir "smoke-check.exe"
    go build -o $checkBin (Join-Path $ScriptDir "smoke-check.go")
    if ($LASTEXITCODE -ne 0) { throw "smoke-check build failed with exit code $LASTEXITCODE" }

    $httpPort = Get-FreePort -CheckBin $checkBin
    $mockPort = Get-FreePort -CheckBin $checkBin
    $BaseUrl = "http://127.0.0.1:$httpPort"

    # The smoke profile points at the mock embedding server, so the generated
    # config carries the dynamically allocated mock port.
    $configText = Get-Content -Raw (Join-Path $ScriptDir "config.yaml")
    $configText = $configText -replace 'http://127\.0\.0\.1:\d+/v1', "http://127.0.0.1:$mockPort/v1"
    Set-Content -Path $ConfigPath -Value $configText -Encoding utf8

    # Everything (config, index, logs) lives under $WorkspaceRoot: no containment warnings.
    $env:CONFIG_PATH = $ConfigPath
    $env:WORKSPACE_ROOT = $WorkspaceRoot
    $env:INDEX_DIR = $IndexDir

    $mockBin = Join-Path $binDir "mock-embed.exe"
    go build -o $mockBin (Join-Path $ScriptDir "mock-embed.go")
    if ($LASTEXITCODE -ne 0) { throw "mock-embed build failed with exit code $LASTEXITCODE" }

    $mock = Start-Process -FilePath $mockBin -ArgumentList @(
        "-port", "$mockPort", "-dims", "$Dims"
    ) -PassThru -WindowStyle Hidden
    $script:SmokeProcs += $mock
    Start-Sleep -Seconds 1

    Push-Location $RepoRoot
    try {
        go run -tags zvec ./cmd/seed-index -n 100
        if ($LASTEXITCODE -ne 0) { throw "seed-index failed with exit code $LASTEXITCODE" }
    } finally {
        Pop-Location
    }

    $bin = Join-Path $binDir "mcp-semantic-search-zvec-go.exe"
    $srv = Start-Process -FilePath $bin -ArgumentList @(
        "--http", "--http-addr", "127.0.0.1:$httpPort"
    ) -PassThru -WindowStyle Hidden -WorkingDirectory $binDir
    $script:SmokeProcs += $srv

    Wait-HttpReady -BaseUrl $BaseUrl

    # The seeded collection must be visible: zvec_open_ok / zvec_doc_count prove the
    # server opened the collection the seed created instead of resetting it.
    Assert-Status -BaseUrl $BaseUrl -CheckBin $checkBin

    $plain = Search-Smoke -BaseUrl $BaseUrl -Query "authentication" -Limit 5 | ConvertTo-Json -Depth 6
    Assert-Results -Json $plain -Min 1 -Glob "-" -CheckBin $checkBin -FailMessage "plain search returned no usable results"

    # path_glob must filter without losing recall (regression guard for the
    # topK-expansion defect: the seeded collection holds exactly 100 chunks).
    $globbed = Search-Smoke -BaseUrl $BaseUrl -Query "authentication" -Limit 5 -PathGlob $SeedGlob | ConvertTo-Json -Depth 6
    Assert-Results -Json $globbed -Min 1 -Glob $SeedGlob -CheckBin $checkBin `
        -FailMessage "path_glob '$SeedGlob' lost recall or returned a non-matching path"

    $missed = Search-Smoke -BaseUrl $BaseUrl -Query "authentication" -Limit 5 -PathGlob $MissGlob | ConvertTo-Json -Depth 6
    Assert-Results -Json $missed -Min 0 -Glob $MissGlob -CheckBin $checkBin `
        -FailMessage "path_glob '$MissGlob' returned results for a non-matching glob"

    Write-Host "PASS path_glob filter: matching glob returns only matching paths, non-matching glob returns 0"
    Write-Host "  smoke workspace: $SmokeRoot"
} catch {
    $failed = $true
    Write-Error $_
} finally {
    Stop-SmokeProcs
    if ($failed) {
        Show-ServerLogTail
        Wait-IfInteractiveOnError
    }
}
if ($failed) { exit 1 }
