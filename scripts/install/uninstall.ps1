param(
    [switch]$KeepData
)

$ErrorActionPreference = "Stop"
if ((Split-Path -Leaf $PSScriptRoot) -eq '.mcp-semantic-search-zvec-go') {
    $InstallDir = $PSScriptRoot
    $TargetRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
} else {
    $TargetRoot = (Get-Location).Path
    $InstallDir = Join-Path $TargetRoot ".mcp-semantic-search-zvec-go"
}
$ServerKey = "semantic-search-zvec-go"
$DefaultCursorRuleRel = ".cursor/rules/semantic-search-zvec-go.mdc"
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

Invoke-UninstallStep "stop MCP processes" {
    Stop-McpProcesses -Workspace $TargetRoot -InstallDir $InstallDir
}

Invoke-UninstallStep "remove mcp.json entry" {
    Remove-McpJsonEntry -TargetRoot $TargetRoot -ServerKey $ServerKey
}

Invoke-UninstallStep "remove Cursor rule" {
    Remove-CursorRule -TargetRoot $TargetRoot -InstallDir $InstallDir
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
    exit 1
}

Write-Host "Uninstalled $ServerKey. Restart Cursor."
exit 0
