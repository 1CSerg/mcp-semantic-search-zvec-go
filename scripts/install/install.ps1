param(
    [string]$TargetRoot = (Get-Location).Path,
    [switch]$FetchONNXModel
)

$ErrorActionPreference = "Stop"
$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
$InstallDir = Join-Path $TargetRoot ".mcp-semantic-search-zvec-go"
$BinDir = Join-Path $InstallDir "bin"
$Templates = Join-Path $RepoRoot "templates"
$Utf8NoBom = [System.Text.UTF8Encoding]::new($false)
$BinaryName = "mcp-semantic-search-zvec-go.exe"
$ServerKey = "semantic-search-zvec-go"

function Write-Utf8File {
    param([string]$Path, [string]$Content)
    $dir = Split-Path -Parent $Path
    if ($dir -and -not (Test-Path $dir)) { New-Item -ItemType Directory -Force -Path $dir | Out-Null }
    [System.IO.File]::WriteAllText($Path, $Content, $Utf8NoBom)
}

function Get-VersionFromRepo {
    $vf = Join-Path $RepoRoot "internal\version\version.go"
    if (-not (Test-Path $vf)) {
        throw "version file not found: $vf (expected internal/version/version.go in clone)"
    }
    $m = [regex]::Match((Get-Content -Raw $vf), 'Version\s*=\s*"([^"]+)"')
    if (-not $m.Success) {
        throw "could not parse Version from $vf"
    }
    return $m.Groups[1].Value
}

function Merge-McpJson {
    param([string]$Path, [string]$FragmentPath)
    $fragment = Get-Content -Raw $FragmentPath | ConvertFrom-Json
    $obj = @{ mcpServers = @{} }
    if (Test-Path $Path) {
        try { $obj = Get-Content -Raw $Path | ConvertFrom-Json } catch { }
    }
    if (-not $obj.mcpServers) { $obj | Add-Member -NotePropertyName mcpServers -NotePropertyValue @{} -Force }
    foreach ($p in $fragment.mcpServers.PSObject.Properties) {
        $obj.mcpServers | Add-Member -NotePropertyName $p.Name -NotePropertyValue $p.Value -Force
    }
    Write-Utf8File $Path ($obj | ConvertTo-Json -Depth 10)
}

$version = Get-VersionFromRepo
Write-Host "Installing mcp-semantic-search-zvec-go v$version into $InstallDir"

@(
    $InstallDir,
    $BinDir,
    (Join-Path $InstallDir "data\index"),
    (Join-Path $InstallDir "data\logs"),
    (Join-Path $InstallDir "models")
) | ForEach-Object {
    if (-not (Test-Path $_)) { New-Item -ItemType Directory -Force -Path $_ | Out-Null }
}

Copy-Item -Force (Join-Path $RepoRoot "config.yaml") (Join-Path $InstallDir "config.yaml")

$modelDir = Join-Path $InstallDir "models\paraphrase-multilingual-MiniLM-L12-v2"
$configText = Get-Content -Raw (Join-Path $InstallDir "config.yaml")
if ($FetchONNXModel -or ($configText -match 'active_profile:\s*local_multilingual')) {
    & "$RepoRoot\scripts\fetch\fetch-onnx-model.ps1" -DestDir $modelDir
}

$envFile = Join-Path $InstallDir ".env"
$envExample = Join-Path $Templates "env.example"
if (-not (Test-Path $envFile) -and (Test-Path $envExample)) {
    Copy-Item -Force $envExample $envFile
    Write-Host "Created secrets file: $envFile"
}

# Build from clone or copy prebuilt
$srcBin = Join-Path $RepoRoot "bin\mcp-semantic-search-zvec-go.exe"
$dstBin = Join-Path $BinDir $BinaryName
if (Test-Path $srcBin) {
    Copy-Item -Force $srcBin $dstBin
    Write-Host "Copied binary from clone: $dstBin"
} else {
    Push-Location $RepoRoot
    try {
        & "$RepoRoot\scripts\dev\build-zvec-windows.ps1"
        Copy-Item -Force (Join-Path $RepoRoot "bin\mcp-semantic-search-zvec-go.exe") $dstBin
        $dllSrc = Join-Path $env:ZVEC_LIB_DIR "zvec_c_api.dll"
        if (Test-Path $dllSrc) {
            Copy-Item -Force $dllSrc (Join-Path $BinDir "zvec_c_api.dll")
        }
        $ortSrc = Join-Path $env:ORT_LIB_DIR "onnxruntime.dll"
        if (Test-Path $ortSrc) {
            Copy-Item -Force $ortSrc (Join-Path $BinDir "onnxruntime.dll")
        }
        Write-Host "Built binary: $dstBin"
    } finally {
        Pop-Location
    }
}

$manifest = @{
    mode = "native"
    runtime = "go"
    version = $version
    installed_at = (Get-Date).ToUniversalTime().ToString("o")
} | ConvertTo-Json
Write-Utf8File (Join-Path $InstallDir "install-manifest.json") $manifest
Write-Utf8File (Join-Path $InstallDir "installed-version.txt") $version

$cursorDir = Join-Path $TargetRoot ".cursor"
if (-not (Test-Path $cursorDir)) { New-Item -ItemType Directory -Force -Path $cursorDir | Out-Null }
Merge-McpJson (Join-Path $cursorDir "mcp.json") (Join-Path $Templates "cursor-mcp.fragment.json")

# gitignore block
$gitignore = Join-Path $TargetRoot ".gitignore"
$block = @"
# BEGIN mcp-semantic-search-zvec-go
.mcp-semantic-search-zvec-go/
# END mcp-semantic-search-zvec-go
"@
if (Test-Path $gitignore) {
    $content = Get-Content -Raw $gitignore
    if ($content -notmatch "BEGIN mcp-semantic-search-zvec-go") {
        Add-Content -Path $gitignore -Value "`n$block"
    }
} else {
    Write-Utf8File $gitignore $block
}

Write-Host "Done. Restart Cursor. MCP server key: $ServerKey"
Write-Host "Fill in $envFile for cloud embedding profiles (RouterAI, DashScope, etc.)."
Write-Host "For offline ONNX: set active_profile: local_multilingual and run reindex with force=true."
