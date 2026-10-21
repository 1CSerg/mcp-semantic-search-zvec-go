param(
    [switch]$KeepData,
    # Remove the user-level application as well (requires no registrations or -Force).
    [switch]$UninstallApp,
    [switch]$Force
)

$ErrorActionPreference = "Stop"

# The shared helpers live in the clone (scripts/lib) when this script runs from the
# repository, and in <userdir>/lib when it was installed with the application. Both
# locations must work: the documented app uninstall runs the installed copy.
function Resolve-InstallLib {
    param([string]$ScriptDir)
    $candidates = @()
    if ((Split-Path -Leaf $ScriptDir) -eq '.mcp-semantic-search-zvec-go') {
        # A project-local install: the clone helpers are two levels up.
        $candidates += Join-Path (Split-Path $ScriptDir -Parent) 'scripts\lib\global-install.ps1'
        $candidates += Join-Path $ScriptDir 'bin\global-install.ps1'
    } else {
        # The repository's scripts\install, or <userdir> with its lib\ directory.
        $candidates += Join-Path (Split-Path $ScriptDir -Parent) 'lib\global-install.ps1'
        $candidates += Join-Path $ScriptDir 'lib\global-install.ps1'
    }
    foreach ($candidate in $candidates) {
        if (Test-Path $candidate) { return $candidate }
    }
    return $null
}

$libPath = Resolve-InstallLib -ScriptDir $PSScriptRoot
if (-not $libPath) {
    throw "cannot find global-install.ps1 for $PSScriptRoot; re-run install.ps1 to restore the helpers"
}
$libDir = Split-Path $libPath -Parent
. $libPath
$stayOpen = Join-Path $libDir 'Stay-OpenOnError.ps1'
if (-not (Test-Path $stayOpen)) { $stayOpen = Join-Path $PSScriptRoot 'bin\Stay-OpenOnError.ps1' }
if (Test-Path $stayOpen) { . $stayOpen }

if ((Split-Path -Leaf $PSScriptRoot) -eq '.mcp-semantic-search-zvec-go') {
    $InstallDir = $PSScriptRoot
    $TargetRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
} else {
    $TargetRoot = (Get-Location).Path
    $InstallDir = Join-Path $TargetRoot ".mcp-semantic-search-zvec-go"
}
$ServerKey = "semantic-search-zvec-go"
$DefaultCursorRuleRel = ".cursor/rules/semantic-search-zvec-go.mdc"
$DefaultRooRuleRel = ".roo/rules/semantic-search-zvec-go.md"
$DefaultRooMcpRel = ".roo/mcp.json"
$Utf8NoBom = [System.Text.UTF8Encoding]::new($false)
$script:StepErrors = @()

function Invoke-UninstallStep {
    param(
        [string]$Name,
        [scriptblock]$Action
    )

    try {
        & $Action
        Write-Host "OK: $Name"
    } catch {
        $msg = "$Name`: $($_.Exception.Message)"
        $script:StepErrors += $msg
        Write-Warning $msg
    }
}

function Remove-ItemWithRetry {
    param(
        [Parameter(Mandatory = $true)]
        [string]$LiteralPath,
        [switch]$Recurse
    )

    for ($attempt = 0; $attempt -lt 3; $attempt++) {
        try {
            if ($Recurse) {
                Remove-Item -LiteralPath $LiteralPath -Recurse -Force -ErrorAction Stop
            } else {
                Remove-Item -LiteralPath $LiteralPath -Force -ErrorAction Stop
            }
            return
        } catch {
            if ($attempt -ge 2) { throw }
            Start-Sleep -Milliseconds 500
        }
    }
}

function Stop-McpProcesses {
    param(
        [string]$Workspace,
        [string]$InstallDir
    )

    $exe = Join-Path $InstallDir "bin\mcp-semantic-search-zvec-go.exe"
    $indexDir = Join-Path $InstallDir "data\index"
    if (Test-Path $exe) {
        $args = @(
            "--stop-stdio-for-workspace", $Workspace,
            "--index-dir", $indexDir
        )
        & $exe @args
        if ($LASTEXITCODE -ne 0) {
            throw "binary stop-stdio exited with code $LASTEXITCODE"
        }
        return
    }

    Write-Warning "Binary not found ($exe); using PowerShell fallback process stop"
    Stop-McpProcessesFallback -Workspace $Workspace -InstallDir $InstallDir
}

function Stop-McpProcessesFallback {
    param(
        [string]$Workspace,
        [string]$InstallDir
    )

    $stopped = 0
    $markerPath = Join-Path $InstallDir "bin\workspace-root.txt"
    $markerRoot = $null
    if (Test-Path $markerPath) {
        $markerRoot = (Get-Content -Raw $markerPath).Trim()
    }

    Get-CimInstance Win32_Process -ErrorAction SilentlyContinue |
        Where-Object {
            $name = $_.Name.ToLowerInvariant()
            $cmd = [string]$_.CommandLine
            if ($name -eq 'mcp-semantic-search-zvec-go.exe' -and $cmd -like '*--stdio*') {
                if ($cmd -like "*$Workspace*" -or $cmd -like "*$InstallDir*") { return $true }
                if ($markerRoot -and $cmd -like "*$markerRoot*") { return $true }
            }
            if (($name -like '*powershell*' -or $name -like '*pwsh*') -and $cmd -like '*run-mcp-stdio.ps1*') {
                if ($cmd -like "*$InstallDir*" -or $cmd -like "*$Workspace*") { return $true }
            }
            return $false
        } |
        ForEach-Object {
            Write-Host "Stopping PID $($_.ProcessId) ($($_.Name))"
            Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue
            $stopped++
        }

    if ($stopped -eq 0) {
        Write-Warning "No MCP processes matched workspace; close Cursor or disable MCP if uninstall fails on locked files"
    }

    Start-Sleep -Milliseconds 400

    $stdioLock = Join-Path $InstallDir "data\index\stdio.lock"
    if (Test-Path $stdioLock) {
        try {
            $holder = (Get-Content -Raw $stdioLock).Split()[0]
            if ($holder -match '^\d+$') {
                Stop-Process -Id ([int]$holder) -Force -ErrorAction SilentlyContinue
                Start-Sleep -Milliseconds 400
            }
            Remove-Item -LiteralPath $stdioLock -Force -ErrorAction SilentlyContinue
        } catch { }
    }
}

function Remove-CursorRule {
    param(
        [string]$TargetRoot,
        [string]$InstallDir
    )

    $rel = $DefaultCursorRuleRel
    $manifestPath = Join-Path $InstallDir "install-manifest.json"
    if (Test-Path $manifestPath) {
        try {
            $manifest = Get-Content -Raw $manifestPath | ConvertFrom-Json
            if ($manifest.PSObject.Properties.Name -contains "cursor_rule" -and $manifest.cursor_rule) {
                $rel = [string]$manifest.cursor_rule
            }
        } catch { }
    }

    $rulePath = Join-Path $TargetRoot ($rel -replace '/', '\')
    if (-not (Test-Path $rulePath)) { return }

    $content = Get-Content -Raw $rulePath
    if ($content -notmatch "managedBy:\s*mcp-semantic-search-zvec-go") {
        Write-Host "Skipped Cursor rule (not install-managed): $rulePath"
        return
    }

    Remove-ItemWithRetry -LiteralPath $rulePath
    Write-Host "Removed Cursor rule: $rulePath"
}

function Remove-RooRule {
    param(
        [string]$TargetRoot,
        [string]$InstallDir
    )

    $rel = $DefaultRooRuleRel
    $manifestPath = Join-Path $InstallDir "install-manifest.json"
    if (Test-Path $manifestPath) {
        try {
            $manifest = Get-Content -Raw $manifestPath | ConvertFrom-Json
            if ($manifest.PSObject.Properties.Name -contains "roo_rule" -and $manifest.roo_rule) {
                $rel = [string]$manifest.roo_rule
            }
        } catch { }
    }

    $rulePath = Join-Path $TargetRoot ($rel -replace '/', '\')
    if (-not (Test-Path $rulePath)) { return }

    $content = Get-Content -Raw $rulePath
    if ($content -notmatch "managedBy:\s*mcp-semantic-search-zvec-go") {
        Write-Host "Skipped Roo rule (not install-managed): $rulePath"
        return
    }

    Remove-ItemWithRetry -LiteralPath $rulePath
    Write-Host "Removed Roo rule: $rulePath"
}

function Remove-GitignoreBlock {
    param([string]$Path)
    if (-not (Test-Path $Path)) { return }

    $content = Get-Content -Raw $Path
    if ($content -notmatch "BEGIN mcp-semantic-search-zvec-go") { return }

    $block = '(?ms)(?:\r?\n)?# BEGIN mcp-semantic-search-zvec-go\r?\n\.mcp-semantic-search-zvec-go/\r?\n# END mcp-semantic-search-zvec-go\r?\n?'
    $newContent = ($content -replace $block, '').TrimEnd()

    if ([string]::IsNullOrWhiteSpace($newContent)) {
        Remove-ItemWithRetry -LiteralPath $Path
        Write-Host "Removed .gitignore (contained only mcp-semantic-search-zvec-go block)"
        return
    }

    [System.IO.File]::WriteAllText($Path, $newContent + [Environment]::NewLine, $Utf8NoBom)
    Write-Host "Removed mcp-semantic-search-zvec-go block from $Path"
}

function Remove-McpJsonEntry {
    param(
        [string]$TargetRoot,
        [string]$ServerKey
    )

    $mcpJson = Join-Path $TargetRoot ".cursor\mcp.json"
    if (-not (Test-Path $mcpJson)) { return }

    $obj = Get-Content -Raw $mcpJson | ConvertFrom-Json
    if ($obj.mcpServers.PSObject.Properties.Name -notcontains $ServerKey) { return }

    $obj.mcpServers.PSObject.Properties.Remove($ServerKey)
    [System.IO.File]::WriteAllText($mcpJson, ($obj | ConvertTo-Json -Depth 10) + [Environment]::NewLine, $Utf8NoBom)
    Write-Host "Removed $ServerKey from $mcpJson"
}

function Remove-RooMcpJsonEntry {
    param(
        [string]$TargetRoot,
        [string]$ServerKey,
        [string]$InstallDir
    )

    $rel = $DefaultRooMcpRel
    $manifestPath = Join-Path $InstallDir "install-manifest.json"
    if (Test-Path $manifestPath) {
        try {
            $manifest = Get-Content -Raw $manifestPath | ConvertFrom-Json
            if ($manifest.PSObject.Properties.Name -contains "roo_mcp" -and $manifest.roo_mcp) {
                $rel = [string]$manifest.roo_mcp
            }
        } catch { }
    }

    $mcpJson = Join-Path $TargetRoot ($rel -replace '/', '\')
    if (-not (Test-Path $mcpJson)) { return }

    $obj = Get-Content -Raw $mcpJson | ConvertFrom-Json
    if ($obj.mcpServers.PSObject.Properties.Name -notcontains $ServerKey) { return }

    $obj.mcpServers.PSObject.Properties.Remove($ServerKey)
    [System.IO.File]::WriteAllText($mcpJson, ($obj | ConvertTo-Json -Depth 10) + [Environment]::NewLine, $Utf8NoBom)
    Write-Host "Removed $ServerKey from $mcpJson"
}

function Remove-InstallDirectory {
    param(
        [string]$InstallDir,
        [switch]$KeepData
    )

    if (-not (Test-Path $InstallDir)) { return }

    $scriptPath = $PSCommandPath
    $runningFromInstallDir = $scriptPath -like "$InstallDir*"

    if ($KeepData) {
        $exclude = @('data', 'models')
        Get-ChildItem -LiteralPath $InstallDir -Force |
            Where-Object { $exclude -notcontains $_.Name } |
            ForEach-Object {
                if ($runningFromInstallDir -and $_.FullName -eq $scriptPath) { return }
                Remove-ItemWithRetry -LiteralPath $_.FullName -Recurse:($_.PSIsContainer)
            }
        return
    }

    if (-not $runningFromInstallDir) {
        Remove-ItemWithRetry -LiteralPath $InstallDir -Recurse
        Write-Host "Removed install directory: $InstallDir"
        return
    }

    Get-ChildItem -LiteralPath $InstallDir -Force |
        Where-Object { $_.FullName -ne $scriptPath } |
        ForEach-Object {
            Remove-ItemWithRetry -LiteralPath $_.FullName -Recurse:($_.PSIsContainer)
        }

    $tempScript = Join-Path ([System.IO.Path]::GetTempPath()) ("mcp-sszg-uninstall-{0}.ps1" -f [guid]::NewGuid().ToString('N'))
    Copy-Item -LiteralPath $scriptPath -Destination $tempScript -Force
    $cleanup = @"
Remove-Item -LiteralPath '$scriptPath' -Force -ErrorAction SilentlyContinue
Remove-Item -LiteralPath '$InstallDir' -Recurse -Force -ErrorAction SilentlyContinue
Remove-Item -LiteralPath '$tempScript' -Force -ErrorAction SilentlyContinue
"@
    Start-Process -FilePath "powershell.exe" -ArgumentList @(
        "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", $cleanup
    ) -WindowStyle Hidden | Out-Null
    Write-Host "Scheduled install directory cleanup: $InstallDir"
}

$legacyStagingDir = $null
$manifestPath = Join-Path $InstallDir "install-manifest.json"
if (Test-Path $manifestPath) {
    try {
        $manifest = Get-Content -Raw $manifestPath | ConvertFrom-Json
        if ($manifest.PSObject.Properties.Name -contains "cursor_staging_dir") {
            $legacyStagingDir = $manifest.cursor_staging_dir
        }
    } catch { }
}

$failed = $false
try {

# ---------------------------------------------------------------------------
# Global layout: the project only holds a project.yaml. Uninstalling the project
# removes its managed entries, rules and registration; collection data stays.
# ---------------------------------------------------------------------------
$globalDir = $null
try { $globalDir = Get-GlobalDir } catch { $globalDir = $null }

if ($UninstallApp) {
    if (-not $globalDir) {
        throw "cannot determine the user-level install directory (set MCP_GLOBAL_DIR or LOCALAPPDATA)"
    }
    $regs = @(Get-GlobalRegistrations -GlobalDir $globalDir)
    if ($regs.Count -gt 0 -and -not $Force) {
        $list = ($regs | ForEach-Object { "  $($_.ID) -> $($_.Root)" }) -join "`n"
        throw @"
Refusing to remove the user-level application: $($regs.Count) project(s) are still registered.
$list

Uninstall those projects first, or re-run with -Force. Collection data is
preserved either way.
"@
    }
    $p = Get-GlobalPaths -GlobalDir $globalDir
    if (Test-Path $p.Binary) {
        $null = Invoke-GlobalBinary -GlobalDir $globalDir -Arguments @("--stop-daemon")
        Start-Sleep -Milliseconds 500
    }
    foreach ($dir in @($p.Bin, $p.Runtime)) {
        if (Test-Path $dir) { Remove-ItemWithRetry -LiteralPath $dir -Recurse }
    }
    foreach ($file in @($p.Config, $p.DaemonYaml, $p.AuthToken, (Join-Path $globalDir "install-manifest.json"))) {
        if (Test-Path $file) { Remove-ItemWithRetry -LiteralPath $file }
    }
    if (Test-Path $p.ProjectsDir) {
        Get-ChildItem -LiteralPath $p.ProjectsDir -Filter "*.yaml" -File | ForEach-Object {
            Remove-ItemWithRetry -LiteralPath $_.FullName
        }
    }
    Write-Host "Removed the user-level application from $globalDir"
    Write-Host "Collection data preserved: $($p.Collections)"
    Write-Host "Secrets preserved: $($p.EnvFile)"
    exit 0
}

$projectConfigPath = Get-ProjectConfigPath -TargetRoot $TargetRoot
$isGlobalProject = (Test-Path $projectConfigPath) -and -not (Test-LegacyProjectInstall -TargetRoot $TargetRoot)
if ($isGlobalProject) {
    if (-not $globalDir) {
        throw "cannot determine the user-level install directory (set MCP_GLOBAL_DIR or LOCALAPPDATA)"
    }
    $unregistered = $false
    Invoke-UninstallStep "unregister project" {
        $result = Invoke-GlobalBinary -GlobalDir $globalDir -Arguments @("--project-remove", $TargetRoot)
        if ($result.ExitCode -ne 0) { throw $result.Output }
        Write-Host $result.Output
        $unregistered = $true
    }
    Invoke-UninstallStep "remove mcp.json entry" {
        $null = Remove-ManagedMcpEntry -Path (Join-Path $TargetRoot ".cursor\mcp.json") -ServerKey $ServerKey
        Write-Host "Removed $ServerKey from .cursor/mcp.json"
    }
    Invoke-UninstallStep "remove Roo mcp.json entry" {
        $null = Remove-ManagedMcpEntry -Path (Join-Path $TargetRoot ".roo\mcp.json") -ServerKey $ServerKey
        Write-Host "Removed $ServerKey from .roo/mcp.json"
    }
    Invoke-UninstallStep "remove managed rules" {
        foreach ($rel in @($DefaultCursorRuleRel, $DefaultRooRuleRel)) {
            $path = Join-Path $TargetRoot ($rel -replace '/', '\')
            if (Test-ManagedRuleFile -Path $path) {
                Remove-ItemWithRetry -LiteralPath $path
                Write-Host "Removed rule: $path"
            } elseif (Test-Path $path) {
                Write-Host "Skipped rule (not install-managed): $path"
            }
        }
    }
    # Deleting project.yaml while the registration survives leaves projects.d
    # pointing at a root with no config, which makes the whole registry invalid and
    # the daemon refuse to start. Keep the file unless the unregister succeeded.
    if ($unregistered) {
        Invoke-UninstallStep "remove project config" {
            if (Test-Path $projectConfigPath) { Remove-ItemWithRetry -LiteralPath $projectConfigPath }
        }
    } else {
        Write-Warning "kept ${projectConfigPath}: the project is still registered, so removing it would leave the registry inconsistent"
    }
    Invoke-UninstallStep "remove .gitignore block" {
        Remove-GitignoreBlock (Join-Path $TargetRoot ".gitignore")
    }
    if ($script:StepErrors.Count -gt 0) {
        Write-Warning "Uninstall completed with $($script:StepErrors.Count) error(s)."
        foreach ($err in $script:StepErrors) { Write-Warning $err }
        $failed = $true
    } else {
        Write-Host "Uninstalled $ServerKey from $TargetRoot. Collection data preserved. Restart Cursor."
    }
    exit 0
}

Invoke-UninstallStep "stop MCP processes" {
    Stop-McpProcesses -Workspace $TargetRoot -InstallDir $InstallDir
}

Invoke-UninstallStep "remove mcp.json entry" {
    Remove-McpJsonEntry -TargetRoot $TargetRoot -ServerKey $ServerKey
}

Invoke-UninstallStep "remove Cursor rule" {
    Remove-CursorRule -TargetRoot $TargetRoot -InstallDir $InstallDir
}

Invoke-UninstallStep "remove Roo mcp.json entry" {
    Remove-RooMcpJsonEntry -TargetRoot $TargetRoot -ServerKey $ServerKey -InstallDir $InstallDir
}

Invoke-UninstallStep "remove Roo rule" {
    Remove-RooRule -TargetRoot $TargetRoot -InstallDir $InstallDir
}

Invoke-UninstallStep "remove install directory" {
    Remove-InstallDirectory -InstallDir $InstallDir -KeepData:$KeepData
}

if ($legacyStagingDir -and (Test-Path $legacyStagingDir)) {
    Invoke-UninstallStep "remove legacy staging" {
        Remove-ItemWithRetry -LiteralPath $legacyStagingDir -Recurse
        Write-Host "Removed legacy Cursor staging: $legacyStagingDir"
    }
}

Invoke-UninstallStep "remove .gitignore block" {
    Remove-GitignoreBlock (Join-Path $TargetRoot ".gitignore")
}

if ($script:StepErrors.Count -gt 0) {
    Write-Warning "Uninstall completed with $($script:StepErrors.Count) error(s). Close Cursor and retry, or remove artifacts manually."
    foreach ($err in $script:StepErrors) {
        Write-Warning $err
    }
    $failed = $true
} else {
    Write-Host "Uninstalled $ServerKey. Restart Cursor."
}

} catch {
    $failed = $true
    Write-Error $_
} finally {
    if ($failed) { Wait-IfInteractiveOnError }
}
if ($failed) { exit 1 }
exit 0
