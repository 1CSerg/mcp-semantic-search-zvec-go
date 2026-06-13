param(
    [string]$TargetRoot = (Get-Location).Path,
    [switch]$FetchONNXModel,
    [switch]$ReplaceConfig,
    [ValidateSet("Native", "Proxy")]
    [string]$McpMode = "Native",
    [string]$WorkspaceId = "",
    [string]$DaemonUrl = "http://127.0.0.1:8080"
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

    $obj = [ordered]@{}
    $mcpServers = @{}
    if (Test-Path $Path) {
        try {
            $existing = Get-Content -Raw $Path | ConvertFrom-Json
            foreach ($p in $existing.PSObject.Properties) {
                if ($p.Name -eq "mcpServers") {
                    if ($null -ne $p.Value) {
                        foreach ($sp in $p.Value.PSObject.Properties) {
                            $mcpServers[$sp.Name] = $sp.Value
                        }
                    }
                } else {
                    $obj[$p.Name] = $p.Value
                }
            }
        } catch { }
    }

    $fragment = Get-Content -Raw $FragmentPath | ConvertFrom-Json
    foreach ($p in $fragment.mcpServers.PSObject.Properties) {
        $mcpServers[$p.Name] = $p.Value
    }
    $obj["mcpServers"] = $mcpServers
    Write-Utf8File $Path ($obj | ConvertTo-Json -Depth 10)
}

function Normalize-WindowsPath {
    param([string]$Path)
    if ([string]::IsNullOrWhiteSpace($Path)) { return $Path }
    return ([System.IO.Path]::GetFullPath($Path)).Replace('/', '\')
}

function Get-WindowsPowerShellExe {
    $cmd = Get-Command powershell.exe -ErrorAction Stop
    return (Normalize-WindowsPath $cmd.Source)
}

function Build-WindowsMcpEnv {
    param([string]$TargetRoot)
    $root = Normalize-WindowsPath $TargetRoot
    $installDir = Normalize-WindowsPath (Join-Path $root ".mcp-semantic-search-zvec-go")
    return [ordered]@{
        WORKSPACE_ROOT      = $root
        WORKSPACE_ID        = $root
        INDEX_DIR           = Normalize-WindowsPath (Join-Path $installDir "data\index")
        CONFIG_PATH         = Normalize-WindowsPath (Join-Path $installDir "config.yaml")
        AUTO_INDEX_ON_START = "true"
    }
}

function Install-WindowsLaunchers {
    param([string]$DestBinDir)
    $srcDir = Join-Path $Templates "bin"
    foreach ($name in @("run-mcp-stdio.ps1", "run-mcp-proxy.ps1", "run-mcp-stdio.cmd")) {
        $src = Join-Path $srcDir $name
        if (-not (Test-Path $src)) {
            throw "launcher template not found: $src"
        }
        Copy-Item -Force $src (Join-Path $DestBinDir $name)
    }
}

function Read-McpJsonObject {
    param([string]$Path)
    $obj = [ordered]@{}
    $mcpServers = @{}
    if (Test-Path $Path) {
        try {
            $existing = Get-Content -Raw $Path | ConvertFrom-Json
            foreach ($p in $existing.PSObject.Properties) {
                if ($p.Name -eq "mcpServers") {
                    if ($null -ne $p.Value) {
                        foreach ($sp in $p.Value.PSObject.Properties) {
                            $mcpServers[$sp.Name] = $sp.Value
                        }
                    }
                } else {
                    $obj[$p.Name] = $p.Value
                }
            }
        } catch { }
    }
    return @($obj, $mcpServers)
}

function Merge-WindowsMcpJson {
    param(
        [string]$Path,
        [string]$TargetRoot,
        [string]$BinDir
    )

    $read = Read-McpJsonObject -Path $Path
    $obj = $read[0]
    $mcpServers = $read[1]

    $launcher = Normalize-WindowsPath (Join-Path $BinDir "run-mcp-stdio.ps1")
    $mcpServers[$ServerKey] = [ordered]@{
        command = (Get-WindowsPowerShellExe)
        args    = @(
            "-NoProfile",
            "-ExecutionPolicy", "Bypass",
            "-File", $launcher
        )
        env     = (Build-WindowsMcpEnv -TargetRoot $TargetRoot)
    }
    $obj["mcpServers"] = $mcpServers
    Write-Utf8File $Path ($obj | ConvertTo-Json -Depth 10)
}

function Merge-WindowsMcpJsonProxy {
    param(
        [string]$Path,
        [string]$TargetRoot,
        [string]$BinDir,
        [string]$WorkspaceId,
        [string]$DaemonUrl
    )

    $read = Read-McpJsonObject -Path $Path
    $obj = $read[0]
    $mcpServers = $read[1]

    $launcher = Normalize-WindowsPath (Join-Path $BinDir "run-mcp-proxy.ps1")
    $mcpServers[$ServerKey] = [ordered]@{
        command = (Get-WindowsPowerShellExe)
        args    = @(
            "-NoProfile",
            "-ExecutionPolicy", "Bypass",
            "-File", $launcher,
            "--stdio-proxy",
            "--workspace-id", $WorkspaceId,
            "--daemon-url", $DaemonUrl
        )
        env     = (Build-WindowsMcpEnv -TargetRoot $TargetRoot)
    }
    $obj["mcpServers"] = $mcpServers
    Write-Utf8File $Path ($obj | ConvertTo-Json -Depth 10)
}

function Copy-RuntimeLibs {
    param([string]$DestBinDir)

    $repoBin = Join-Path $RepoRoot "bin"
    foreach ($name in @("zvec_c_api.dll", "onnxruntime.dll")) {
        $src = Join-Path $repoBin $name
        if (Test-Path $src) {
            Copy-Item -Force $src (Join-Path $DestBinDir $name)
        }
    }

    if (-not (Test-Path (Join-Path $DestBinDir "zvec_c_api.dll"))) {
        if (-not $env:ZVEC_LIB_DIR) {
            & "$RepoRoot\scripts\fetch\fetch-zvec-libs.ps1" | Out-Null
        }
        $dllSrc = Join-Path $env:ZVEC_LIB_DIR "zvec_c_api.dll"
        if (Test-Path $dllSrc) {
            Copy-Item -Force $dllSrc (Join-Path $DestBinDir "zvec_c_api.dll")
        }
    }
    if (-not (Test-Path (Join-Path $DestBinDir "onnxruntime.dll"))) {
        if (-not $env:ORT_LIB_DIR) {
            & "$RepoRoot\scripts\fetch\fetch-onnx-runtime.ps1" | Out-Null
        }
        $ortSrc = Join-Path $env:ORT_LIB_DIR "onnxruntime.dll"
        if (Test-Path $ortSrc) {
            Copy-Item -Force $ortSrc (Join-Path $DestBinDir "onnxruntime.dll")
        }
    }
}

function Install-ConfigYaml {
    param(
        [string]$TemplatePath,
        [string]$DestPath,
        [switch]$Replace
    )

    $mergeScript = Join-Path $RepoRoot "scripts\install\merge-config.py"
    $py = Get-Command python -ErrorAction SilentlyContinue
    if (-not $py) { $py = Get-Command python3 -ErrorAction SilentlyContinue }

    if ($py) {
        $args = @($mergeScript, $TemplatePath, $DestPath)
        if ($Replace) { $args += "--replace" }
        & $py.Source @args
        if ($LASTEXITCODE -eq 0) { return }
        if ($LASTEXITCODE -eq 2) {
            if (Test-Path $DestPath) {
                Write-Warning "config.yaml preserved (install merge requires ruamel.yaml): pip install -r scripts/install/requirements.txt"
                return
            }
        } else {
            throw "merge-config.py failed with exit code $LASTEXITCODE"
        }
    }

    if (Test-Path $DestPath) {
        if ($Replace) {
            Copy-Item -Force $TemplatePath $DestPath
            Write-Host "Replaced config.yaml (Python merge unavailable)"
        } else {
            Write-Warning "config.yaml preserved (Python not found for merge)"
        }
        return
    }

    Copy-Item -Force $TemplatePath $DestPath
    Write-Host "Created config.yaml (Python merge unavailable)"
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

Install-ConfigYaml `
    -TemplatePath (Join-Path $RepoRoot "config.yaml") `
    -DestPath (Join-Path $InstallDir "config.yaml") `
    -Replace:$ReplaceConfig

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
    try {
        Copy-Item -Force $srcBin $dstBin
        Write-Host "Copied binary from clone: $dstBin"
    } catch {
        Write-Warning "Could not update project binary (in use?): $($_.Exception.Message)"
    }
} else {
    Push-Location $RepoRoot
    try {
        & "$RepoRoot\scripts\dev\build-zvec-windows.ps1"
        Copy-Item -Force (Join-Path $RepoRoot "bin\mcp-semantic-search-zvec-go.exe") $dstBin
        Write-Host "Built binary: $dstBin"
    } finally {
        Pop-Location
    }
}
Copy-RuntimeLibs $BinDir
Install-WindowsLaunchers -DestBinDir $BinDir
Copy-Item -Force (Join-Path $PSScriptRoot "uninstall.ps1") (Join-Path $InstallDir "uninstall.ps1")

if (-not (Test-Path $dstBin)) {
    throw "MCP binary not found in project install dir: $dstBin"
}
foreach ($name in @($BinaryName, "run-mcp-stdio.ps1", "zvec_c_api.dll")) {
    $path = Join-Path $BinDir $name
    if (-not (Test-Path $path)) {
        throw "missing install artifact: $path"
    }
}
if (-not (Test-Path (Join-Path $InstallDir "uninstall.ps1"))) {
    throw "missing uninstall.ps1 in $InstallDir"
}
Write-Host "Project binary: $dstBin"

$manifest = @{
    mode = if ($McpMode -eq "Proxy") { "proxy" } else { "native" }
    runtime = "go"
    version = $version
    installed_at = (Get-Date).ToUniversalTime().ToString("o")
    mcp_mode = $McpMode
    workspace_id = $WorkspaceId
    daemon_url = if ($McpMode -eq "Proxy") { $DaemonUrl } else { $null }
} | ConvertTo-Json
Write-Utf8File (Join-Path $InstallDir "install-manifest.json") $manifest
Write-Utf8File (Join-Path $InstallDir "installed-version.txt") $version

$cursorDir = Join-Path $TargetRoot ".cursor"
if (-not (Test-Path $cursorDir)) { New-Item -ItemType Directory -Force -Path $cursorDir | Out-Null }
$mcpJsonPath = Join-Path $cursorDir "mcp.json"
if ($McpMode -eq "Proxy") {
    if ([string]::IsNullOrWhiteSpace($WorkspaceId)) {
        throw "-WorkspaceId is required when -McpMode Proxy"
    }
    Merge-WindowsMcpJsonProxy -Path $mcpJsonPath -TargetRoot $TargetRoot -BinDir $BinDir `
        -WorkspaceId $WorkspaceId -DaemonUrl $DaemonUrl
} else {
    Merge-WindowsMcpJson -Path $mcpJsonPath -TargetRoot $TargetRoot -BinDir $BinDir
}

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
