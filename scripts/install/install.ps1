param(
    [string]$TargetRoot = (Get-Location).Path,
    [switch]$FetchONNXModel,
    [switch]$ReplaceConfig,
    [ValidateSet("Global", "Native", "Proxy")]
    [string]$McpMode = "Global",
    [string]$WorkspaceId = "",
    [string]$DaemonUrl = "http://127.0.0.1:8080",
    # Install or update the user-level application only; no project is added.
    [switch]$InstallAppOnly,
    # Explicitly migrate a legacy project-local install into the global layout.
    [switch]$MigrateLegacy,
    # Remove the user-level application (requires no registrations or -Force).
    [switch]$UninstallApp,
    [switch]$Force
)

$ErrorActionPreference = "Stop"
. (Join-Path (Split-Path $PSScriptRoot -Parent) 'lib\Stay-OpenOnError.ps1')
. (Join-Path (Split-Path $PSScriptRoot -Parent) 'lib\global-install.ps1')
$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
$InstallDir = Join-Path $TargetRoot ".mcp-semantic-search-zvec-go"
$BinDir = Join-Path $InstallDir "bin"
$Templates = Join-Path $RepoRoot "templates"
$Utf8NoBom = [System.Text.UTF8Encoding]::new($false)
$BinaryName = "mcp-semantic-search-zvec-go.exe"
$ServerKey = "semantic-search-zvec-go"
$CursorRuleRelPath = ".cursor/rules/semantic-search-zvec-go.mdc"
$RooRuleRelPath = ".roo/rules/semantic-search-zvec-go.md"
$RooMcpRelPath = ".roo/mcp.json"

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
    foreach ($name in @("run-mcp-stdio.ps1", "run-mcp-proxy.ps1", "run-mcp-stdio.cmd", "Stay-OpenOnError.ps1")) {
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

function Get-ZvecGoTagFromRepo {
    $gm = Join-Path $RepoRoot "go.mod"
    if (-not (Test-Path $gm)) {
        return "v0.7.0"
    }
    $m = [regex]::Match((Get-Content -Raw $gm), 'github.com/zvec-ai/zvec-go\s+(v[^\s\r\n]+)')
    if ($m.Success) {
        return $m.Groups[1].Value.Trim()
    }
    return "v0.7.0"
}

function Copy-RuntimeLibs {
    param(
        [string]$DestBinDir,
        [string]$InstallVersion
    )

    $zvecTag = Get-ZvecGoTagFromRepo
    $runtimeMarker = Join-Path $DestBinDir ".install-runtime-version"
    $expectedMarker = "${InstallVersion}:${zvecTag}"
    $needRefresh = $true
    if ((Test-Path $runtimeMarker) -and (Test-Path (Join-Path $DestBinDir "zvec_c_api.dll"))) {
        $marked = (Get-Content $runtimeMarker -Raw).Trim()
        if ($marked -eq $expectedMarker) {
            $needRefresh = $false
        }
    }
    if (-not $needRefresh -and (Test-Path (Join-Path $DestBinDir "onnxruntime.dll"))) {
        return
    }

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

    Write-Utf8File $runtimeMarker $expectedMarker
}

function Install-CursorRule {
    param(
        [string]$TargetRoot,
        [string]$TemplatesDir
    )

    $src = Join-Path $TemplatesDir "cursor-rules\semantic-search-zvec-go.mdc"
    if (-not (Test-Path $src)) {
        throw "cursor rule template not found: $src"
    }

    $rulesDir = Join-Path $TargetRoot ".cursor\rules"
    if (-not (Test-Path $rulesDir)) {
        New-Item -ItemType Directory -Force -Path $rulesDir | Out-Null
    }

    $dest = Join-Path $TargetRoot ($CursorRuleRelPath -replace '/', '\')
    Copy-Item -Force $src $dest
    Write-Host "Installed Cursor rule: $dest"
}

function Merge-RooMcpJson {
    param(
        [string]$TargetRoot,
        [string]$BinDir
    )

    $rooDir = Join-Path $TargetRoot ".roo"
    if (-not (Test-Path $rooDir)) { New-Item -ItemType Directory -Force -Path $rooDir | Out-Null }
    $path = Join-Path $rooDir "mcp.json"
    Merge-WindowsMcpJson -Path $path -TargetRoot $TargetRoot -BinDir $BinDir
}

function Merge-RooMcpJsonProxy {
    param(
        [string]$TargetRoot,
        [string]$BinDir,
        [string]$WorkspaceId,
        [string]$DaemonUrl
    )

    $rooDir = Join-Path $TargetRoot ".roo"
    if (-not (Test-Path $rooDir)) { New-Item -ItemType Directory -Force -Path $rooDir | Out-Null }
    $path = Join-Path $rooDir "mcp.json"
    Merge-WindowsMcpJsonProxy -Path $path -TargetRoot $TargetRoot -BinDir $BinDir `
        -WorkspaceId $WorkspaceId -DaemonUrl $DaemonUrl
}

function Install-RooCodeRule {
    param(
        [string]$TargetRoot,
        [string]$TemplatesDir
    )

    $src = Join-Path $TemplatesDir "roo-rules\semantic-search-zvec-go.md"
    if (-not (Test-Path $src)) {
        throw "roo rule template not found: $src"
    }

    $rulesDir = Join-Path $TargetRoot ".roo\rules"
    if (-not (Test-Path $rulesDir)) {
        New-Item -ItemType Directory -Force -Path $rulesDir | Out-Null
    }

    $dest = Join-Path $TargetRoot ($RooRuleRelPath -replace '/', '\')
    Copy-Item -Force $src $dest
    Write-Host "Installed Roo rule: $dest"
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

# ---------------------------------------------------------------------------
# Global (one app per user) mode
# ---------------------------------------------------------------------------

function Resolve-RepoBinary {
    <#
    Returns the path to a buildable/copyable binary from this clone, building it
    when no prebuilt one exists. Shared by both install modes.
    #>
    $srcBin = Join-Path $RepoRoot "bin\$BinaryName"
    if (Test-Path $srcBin) { return $srcBin }
    Push-Location $RepoRoot
    try {
        & "$RepoRoot\scripts\dev\build-zvec-windows.ps1"
    } finally {
        Pop-Location
    }
    $built = Join-Path $RepoRoot "bin\$BinaryName"
    if (-not (Test-Path $built)) {
        throw "build did not produce $built"
    }
    return $built
}

function Install-GlobalRuntimeLibs {
    param([Parameter(Mandatory = $true)][string]$GlobalDir, [Parameter(Mandatory = $true)][string]$Version)
    $p = Get-GlobalPaths -GlobalDir $GlobalDir
    Copy-RuntimeLibs -DestBinDir $p.Bin -InstallVersion $Version
}

function Install-GlobalApp {
    <#
    Installs or updates the single user-level application: binary, native
    libraries, config.yaml, daemon.yaml, .env template. No project is touched and
    no project registration is created.
    #>
    param(
        [Parameter(Mandatory = $true)][string]$Version,
        [switch]$SkipVersionCheck
    )
    $globalDir = Get-GlobalDir
    $p = Initialize-GlobalLayout -GlobalDir $globalDir
    Write-Host "Installing the user-level application into $globalDir"

    $srcBin = Resolve-RepoBinary
    # The previous binary is kept as a backup until the caller completes the
    # install: a failed library copy, version check or daemon start must leave a
    # working application behind.
    $null = Install-GlobalBinary -SourceBinary $srcBin -GlobalDir $globalDir -SkipVersionCheck:$SkipVersionCheck
    Install-GlobalRuntimeLibs -GlobalDir $globalDir -Version $Version
    # Verify after the native libraries are beside the EXE: a native build cannot
    # start without them, and a broken install must not be reported as good.
    if (-not $SkipVersionCheck) {
        $reported = Test-GlobalBinaryRuns -Path $p.Binary
        if (-not $reported) {
            Restore-GlobalBinary -GlobalDir $globalDir
            throw "the installed binary at $($p.Binary) does not start (missing native libraries?)"
        }
        Write-Host "Installed binary version: $reported"
    }
    Write-Utf8File -Path $p.VersionFile -Content $Version

    Install-ConfigYaml `
        -TemplatePath (Join-Path $RepoRoot "config.yaml") `
        -DestPath $p.Config `
        -Replace:$ReplaceConfig

    # daemon.yaml is created once: an existing one is a user decision (ports,
    # caps) and is never overwritten silently.
    if (-not (Test-Path $p.DaemonYaml)) {
        Write-GlobalDaemonYaml -Path $p.DaemonYaml
        Write-Host "Created daemon config: $($p.DaemonYaml)"
    }

    if ($FetchONNXModel -or ((Get-Content -Raw $p.Config) -match 'active_profile:\s*local_multilingual')) {
        $modelDir = Join-Path $p.Models "paraphrase-multilingual-MiniLM-L12-v2"
        & "$RepoRoot\scripts\fetch\fetch-onnx-model.ps1" -DestDir $modelDir
    }

    $envExample = Join-Path $Templates "env.example"
    if (-not (Test-Path $p.EnvFile) -and (Test-Path $envExample)) {
        Copy-Item -Force $envExample $p.EnvFile
        Write-Host "Created secrets file: $($p.EnvFile) (global; never per project)"
    }

    $null = Get-GlobalAuthToken -GlobalDir $globalDir

    $manifest = [ordered]@{
        mode         = "global"
        runtime      = "go"
        version      = $Version
        installed_at = (Get-Date).ToUniversalTime().ToString("o")
        global_dir   = $globalDir
        server_key   = $ServerKey
        cursor_rule  = $CursorRuleRelPath
        roo_rule     = $RooRuleRelPath
        roo_mcp      = $RooMcpRelPath
    } | ConvertTo-Json -Depth 5
    Write-Utf8File -Path (Join-Path $globalDir "install-manifest.json") -Content $manifest

    # Uninstall for the app itself lives with the app, not in a project. It needs
    # the shared helpers, so they are installed next to it: without them the
    # script cannot even resolve the user-level directory.
    Copy-Item -Force (Join-Path $PSScriptRoot "uninstall.ps1") (Join-Path $globalDir "uninstall.ps1")
    $libDir = Join-Path $globalDir "lib"
    if (-not (Test-Path $libDir)) { New-Item -ItemType Directory -Force -Path $libDir | Out-Null }
    foreach ($helper in @("global-install.ps1", "Stay-OpenOnError.ps1")) {
        $src = Join-Path (Split-Path $PSScriptRoot -Parent) "lib\$helper"
        if (Test-Path $src) { Copy-Item -Force $src (Join-Path $libDir $helper) }
    }
    return $globalDir
}

function Install-GlobalProjectEntry {
    <#
    Adds one project: project.yaml + registration + managed IDE entries. The
    binary is never copied into the project.
    #>
    param(
        [Parameter(Mandatory = $true)][string]$TargetRoot,
        [Parameter(Mandatory = $true)][string]$GlobalDir
    )
    $root = [System.IO.Path]::GetFullPath($TargetRoot)
    $result = Invoke-GlobalBinary -GlobalDir $GlobalDir -Arguments @("--project-add", $root)
    if ($result.ExitCode -ne 0) {
        throw "project-add failed for ${root}:`n$($result.Output)"
    }
    Write-Host $result.Output

    $collectionId = Get-ProjectCollectionID -TargetRoot $root
    if (-not $collectionId) {
        throw "project.yaml for $root has no collection_id after --project-add"
    }

    $entry = New-GlobalMcpEntry -GlobalDir $GlobalDir -TargetRoot $root
    $cursorDir = Join-Path $root ".cursor"
    if (-not (Test-Path $cursorDir)) { New-Item -ItemType Directory -Force -Path $cursorDir | Out-Null }
    Set-ManagedMcpEntry -Path (Join-Path $cursorDir "mcp.json") -Entry $entry

    Install-CursorRule -TargetRoot $root -TemplatesDir $Templates

    $rooDir = Join-Path $root ".roo"
    if (-not (Test-Path $rooDir)) { New-Item -ItemType Directory -Force -Path $rooDir | Out-Null }
    Set-ManagedMcpEntry -Path (Join-Path $rooDir "mcp.json") -Entry $entry
    Install-RooCodeRule -TargetRoot $root -TemplatesDir $Templates

    $gitignore = Join-Path $root ".gitignore"
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
        Write-Utf8File -Path $gitignore -Content $block
    }

    Write-Host "Project collection: $collectionId"
    return $collectionId
}

function Assert-NoLegacyInstall {
    <#
    A legacy project-local install is a second owner of the index. Without
    -MigrateLegacy the install stops with instructions instead of creating two
    owners for the same project.
    #>
    param([Parameter(Mandatory = $true)][string]$TargetRoot)
    if (-not (Test-LegacyProjectInstall -TargetRoot $TargetRoot)) { return }
    if ($MigrateLegacy) { return }
    throw @"
A legacy project-local install was found at $(Join-Path $TargetRoot '.mcp-semantic-search-zvec-go\bin').
The global install would create a second owner of this project's index.

Either:
  - re-run with -MigrateLegacy to move this project to the user-level app
    (the old install is kept as a backup until you remove it), or
  - uninstall the project-local install first.
"@
}

function Invoke-LegacyMigration {
    <#
    Opt-in migration: stop the old per-project MCP processes with the existing
    lifecycle command, copy profiles/secrets into the user-level config, then add
    the project. Nothing is deleted and no secret is printed.
    #>
    param([Parameter(Mandatory = $true)][string]$TargetRoot, [Parameter(Mandatory = $true)][string]$GlobalDir)
    Write-Host "Migrating legacy project install for $TargetRoot"
    $legacyBin = Join-Path $InstallDir "bin\$BinaryName"
    if (Test-Path $legacyBin) {
        & $legacyBin --stop-stdio-for-workspace $TargetRoot 2>&1 | ForEach-Object { Write-Host "  $_" }
    }
    $legacyConfig = Join-Path $InstallDir "config.yaml"
    $p = Get-GlobalPaths -GlobalDir $GlobalDir
    if (Test-Path $legacyConfig) {
        # Merge only profiles/secrets the global config does not already have.
        # A profile name that exists with a different model/dimensions is a
        # conflict: the user must rename it, never "last one wins".
        $mergeArgs = @(
            (Join-Path $RepoRoot "scripts\install\merge-config.py"),
            $legacyConfig, $p.Config, "--no-overwrite-profiles"
        )
        $py = Get-Command python -ErrorAction SilentlyContinue
        if (-not $py) { $py = Get-Command python3 -ErrorAction SilentlyContinue }
        if ($py) {
            & $py.Source @mergeArgs
            if ($LASTEXITCODE -eq 3) {
                throw "legacy profiles conflict with the global config; rename them in $legacyConfig and re-run with -MigrateLegacy"
            }
        } else {
            Write-Warning "Python not found: profiles from $legacyConfig were not merged"
        }
    }
    $legacyEnv = Join-Path $InstallDir ".env"
    if (Test-Path $legacyEnv) {
        $globalEnv = @{}
        if (Test-Path $p.EnvFile) {
            foreach ($line in Get-Content $p.EnvFile) {
                $m = [regex]::Match($line, '^\s*([A-Za-z_][A-Za-z0-9_]*)\s*=')
                if ($m.Success) { $globalEnv[$m.Groups[1].Value] = $true }
            }
        }
        $added = 0
        foreach ($line in Get-Content $legacyEnv) {
            $m = [regex]::Match($line, '^\s*([A-Za-z_][A-Za-z0-9_]*)\s*=')
            if ($m.Success -and -not $globalEnv.ContainsKey($m.Groups[1].Value)) {
                Add-Content -Path $p.EnvFile -Value $line
                $added++
            }
        }
        Write-Host "  merged $added secret name(s) into the global .env (values not printed)"
    }
    Write-Host "Legacy install kept as a backup at $InstallDir"
}

function Uninstall-GlobalApp {
    <#
    Removes the user-level application. Collection data is preserved by default
    and is never deleted for another project.
    #>
    param([Parameter(Mandatory = $true)][string]$GlobalDir)
    $p = Get-GlobalPaths -GlobalDir $GlobalDir
    $regs = @(Get-GlobalRegistrations -GlobalDir $GlobalDir)
    if ($regs.Count -gt 0 -and -not $Force) {
        $list = ($regs | ForEach-Object { "  $($_.ID) -> $($_.Root)" }) -join "`n"
        throw @"
Refusing to remove the application: $($regs.Count) project(s) are still registered.
$list

Remove them first (uninstall.ps1 -TargetRoot <root>, or --project-remove <root>).
Re-run with -Force to remove the application anyway; its registrations are then
deleted too, because they would point at a collection the application no longer
serves. Collection data and .env are preserved in every case.
"@
    }
    if (Test-Path $p.Binary) {
        $null = Invoke-GlobalBinary -GlobalDir $GlobalDir -Arguments @("--stop-daemon")
        Start-Sleep -Milliseconds 500
    }
    foreach ($path in @($p.Bin, $p.Runtime)) {
        if (Test-Path $path) { Remove-Item -Recurse -Force $path -ErrorAction SilentlyContinue }
    }
    foreach ($file in @($p.Config, $p.DaemonYaml, $p.AuthToken, (Join-Path $GlobalDir "install-manifest.json"), (Join-Path $GlobalDir "uninstall.ps1"))) {
        if (Test-Path $file) { Remove-Item -Force $file -ErrorAction SilentlyContinue }
    }
    # Registrations are stale once the app is gone; the collection data stays.
    if (Test-Path $p.ProjectsDir) {
        foreach ($file in Get-ChildItem -Path $p.ProjectsDir -Filter "*.yaml" -File) {
            Remove-Item -Force $file.FullName -ErrorAction SilentlyContinue
        }
    }
    Write-Host "Removed the user-level application from $GlobalDir"
    Write-Host "Collection data preserved under $($p.Collections)"
    Write-Host "Secrets preserved at $($p.EnvFile)"
}

function Uninstall-ProjectEntries {
    <#
    Removes only this project's managed entries, rules and registration. Other
    projects and all collection data are untouched.
    #>
    param([Parameter(Mandatory = $true)][string]$TargetRoot, [Parameter(Mandatory = $true)][string]$GlobalDir)
    $root = [System.IO.Path]::GetFullPath($TargetRoot)
    $collectionId = Get-ProjectCollectionID -TargetRoot $root
    $result = Invoke-GlobalBinary -GlobalDir $GlobalDir -Arguments @("--project-remove", $root)
    if ($result.ExitCode -ne 0) {
        Write-Warning "project-remove failed for $root`: $($result.Output)"
    }
    $null = Remove-ManagedMcpEntry -Path (Join-Path $root ".cursor\mcp.json")
    $null = Remove-ManagedMcpEntry -Path (Join-Path $root ".roo\mcp.json")
    foreach ($rel in @($CursorRuleRelPath, $RooRuleRelPath)) {
        $path = Join-Path $root ($rel -replace '/', '\')
        if (Test-ManagedRuleFile -Path $path) { Remove-Item -Force $path }
    }
    if ($collectionId) {
        Write-Host "Project $root unregistered; collection $collectionId data preserved"
    } else {
        Write-Host "Project $root had no managed install"
    }
}

$failed = $false
try {

$version = Get-VersionFromRepo

# ---------------------------------------------------------------------------
# Global mode (default): one application per user, N projects with collection ids
# ---------------------------------------------------------------------------
if ($UninstallApp) {
    Uninstall-GlobalApp -GlobalDir (Get-GlobalDir)
    Write-Host "Uninstall complete."
    return
}

if ($McpMode -eq "Global") {
    $globalDir = Get-GlobalDir
    Assert-NoLegacyInstall -TargetRoot $TargetRoot
    $null = Install-GlobalApp -Version $version

    # From here on any failure must restore the previous application version:
    # the backup is only dropped once every step succeeded.
    try {
        if ($MigrateLegacy) {
            Invoke-LegacyMigration -TargetRoot $TargetRoot -GlobalDir $globalDir
        }

        if ($InstallAppOnly) {
            # Install the application and start the daemon; no project is added.
            $ensured = Invoke-GlobalBinary -GlobalDir $globalDir -Arguments @("--ensure-daemon")
            if ($ensured.ExitCode -ne 0) {
                Write-Warning "the daemon did not start: $($ensured.Output)"
            } else {
                Write-Host $ensured.Output
            }
            Write-Host "Installed the user-level application only (no project added)."
            Write-Host "Add a project later: install.ps1 -TargetRoot <project>"
        } else {
            # Adding the project also ensures the daemon and reloads the registry, so
            # the new collection is available without restarting anything.
            $null = Install-GlobalProjectEntry -TargetRoot $TargetRoot -GlobalDir $globalDir
        }
    } catch {
        Restore-GlobalBinary -GlobalDir $globalDir
        throw
    }
    Complete-GlobalBinaryInstall -GlobalDir $globalDir

    Write-Host ""
    Write-Host "Application (one per user): $globalDir"
    Write-Host "Global config:  $(Join-Path $globalDir 'config.yaml')"
    Write-Host "Global secrets: $(Join-Path $globalDir '.env')  (never per project)"
    Write-Host "Collections:    $(Join-Path $globalDir 'data\collections')"
    Write-Host "Restart Cursor / Roo Code (MCP has no hot reload). Server key: $ServerKey"
    Write-Host "Fill in the global .env for cloud embedding profiles; then run MCP 'reindex' with force: true."
    return
}

# ---------------------------------------------------------------------------
# Legacy modes (explicit -McpMode Native / Proxy): unchanged behaviour
# ---------------------------------------------------------------------------
Write-Host "Installing mcp-semantic-search-zvec-go v$version into $InstallDir (legacy $McpMode mode)"

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
Copy-RuntimeLibs $BinDir $version
Install-WindowsLaunchers -DestBinDir $BinDir
$workspaceRootPath = Normalize-WindowsPath $TargetRoot
Write-Utf8File (Join-Path $BinDir "workspace-root.txt") $workspaceRootPath
Copy-Item -Force (Join-Path $PSScriptRoot "uninstall.ps1") (Join-Path $InstallDir "uninstall.ps1")

if (-not (Test-Path $dstBin)) {
    throw "MCP binary not found in project install dir: $dstBin"
}
foreach ($name in @($BinaryName, "run-mcp-stdio.ps1", "workspace-root.txt", "zvec_c_api.dll", "onnxruntime.dll")) {
    $path = Join-Path $BinDir $name
    if (-not (Test-Path $path)) {
        throw "missing install artifact: $path"
    }
}
if (-not (Test-Path (Join-Path $InstallDir "uninstall.ps1"))) {
    throw "missing uninstall.ps1 in $InstallDir"
}
Write-Host "Project binary: $dstBin"

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

Install-CursorRule -TargetRoot $TargetRoot -TemplatesDir $Templates

$rooDir = Join-Path $TargetRoot ".roo"
if (-not (Test-Path $rooDir)) { New-Item -ItemType Directory -Force -Path $rooDir | Out-Null }
if ($McpMode -eq "Proxy") {
    Merge-RooMcpJsonProxy -TargetRoot $TargetRoot -BinDir $BinDir `
        -WorkspaceId $WorkspaceId -DaemonUrl $DaemonUrl
} else {
    Merge-RooMcpJson -TargetRoot $TargetRoot -BinDir $BinDir
}
Install-RooCodeRule -TargetRoot $TargetRoot -TemplatesDir $Templates

$manifest = @{
    mode = if ($McpMode -eq "Proxy") { "proxy" } else { "native" }
    runtime = "go"
    version = $version
    installed_at = (Get-Date).ToUniversalTime().ToString("o")
    mcp_mode = $McpMode
    workspace_id = $WorkspaceId
    daemon_url = if ($McpMode -eq "Proxy") { $DaemonUrl } else { $null }
    cursor_rule = $CursorRuleRelPath
    roo_rule = $RooRuleRelPath
    roo_mcp = $RooMcpRelPath
} | ConvertTo-Json
Write-Utf8File (Join-Path $InstallDir "install-manifest.json") $manifest

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

Write-Host "WARNING: Chunking strategy updated. You must run MCP 'reindex' with force: true after starting the IDE."
Write-Host "Done. Restart Cursor. MCP server key: $ServerKey"
Write-Host "Roo/Zoo Code: .roo/mcp.json and $RooRuleRelPath updated — restart Roo Code if used."
Write-Host "Fill in $envFile for cloud embedding profiles (RouterAI, DashScope, etc.)."
Write-Host "For offline ONNX: set active_profile: local_multilingual and run reindex with force=true."

} catch {
    $failed = $true
    Write-Error $_
} finally {
    if ($failed) { Wait-IfInteractiveOnError }
}
if ($failed) { exit 1 }
