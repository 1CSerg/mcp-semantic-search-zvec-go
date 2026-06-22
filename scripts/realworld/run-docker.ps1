# Realworld Docker smoke (D1/D2): build image, run HTTP reindex + search with corpus mount.
param(
    [int]$Port = 19410
)

$ErrorActionPreference = "Stop"
. (Join-Path (Split-Path $PSScriptRoot -Parent) 'lib\Stay-OpenOnError.ps1')
$ScriptDir = $PSScriptRoot
$RepoRoot = (Resolve-Path (Join-Path $ScriptDir "..\..")).Path
$Image = "mcp-realworld-smoke:local"
$Corpus = Join-Path $RepoRoot "tests\realworld\corpus"
$IndexVol = Join-Path $env:TEMP "mcp-realworld-docker-index"

if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
    Write-Host "SKIP: docker not found"
    exit 0
}

$failed = $false
try {
    if (Test-Path $IndexVol) { Remove-Item -Recurse -Force $IndexVol }
    New-Item -ItemType Directory -Path $IndexVol | Out-Null

    Write-Host "==> docker build"
    docker build -f (Join-Path $RepoRoot "docker\Dockerfile") -t $Image $RepoRoot

    docker rm -f mcp-realworld-smoke 2>$null | Out-Null
    Write-Host "==> docker run"
    docker run -d --name mcp-realworld-smoke `
        -p "127.0.0.1:${Port}:8080" `
        -v "${Corpus}:/workspace:ro" `
        -v "${IndexVol}:/data/index" `
        -e WORKSPACE_ROOT=/workspace `
        -e INDEX_DIR=/data/index `
        -e FILE_WATCHER_BACKEND=polling `
        -e AUTO_INDEX_ON_START=false `
        $Image --http --http-addr :8080

    $deadline = (Get-Date).AddMinutes(3)
    while ((Get-Date) -lt $deadline) {
        try {
            Invoke-RestMethod -Uri "http://127.0.0.1:$Port/health" -TimeoutSec 3 | Out-Null
            break
        } catch { Start-Sleep -Seconds 2 }
    }

    Invoke-RestMethod -Method Post -Uri "http://127.0.0.1:$Port/v1/reindex" `
        -ContentType "application/json" -Body '{"force":true}' | Out-Null

    while ($true) {
        $st = Invoke-RestMethod -Uri "http://127.0.0.1:$Port/v1/status"
        if (-not $st.indexing.running) { break }
        if ($st.indexing.state -eq "error") { throw "indexing failed in container" }
        Start-Sleep -Seconds 2
    }

    $search = Invoke-RestMethod -Method Post -Uri "http://127.0.0.1:$Port/v1/search" `
        -ContentType "application/json" -Body '{"query":"REALWORLD_GO_AUTH_GATE","limit":5}'
    if (-not ($search.results | Where-Object { $_.path -match "middleware.go" })) {
        throw "search miss in container"
    }
    Write-Host "PASS realworld Docker smoke (D1/D2)"
} catch {
    $failed = $true
    Write-Error $_
} finally {
    docker rm -f mcp-realworld-smoke 2>$null | Out-Null
    if ($failed) { Wait-IfInteractiveOnError }
}
if ($failed) { exit 1 }
