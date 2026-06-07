param(
    [switch]$KeepData
)

$ErrorActionPreference = "Stop"
$TargetRoot = (Get-Location).Path
$InstallDir = Join-Path $TargetRoot ".mcp-semantic-search-zvec-go"
$ServerKey = "semantic-search-zvec-go"

function Stop-StaleProcesses {
    param([string]$Workspace)
    Get-CimInstance Win32_Process -Filter "Name = 'mcp-semantic-search-zvec-go.exe'" -ErrorAction SilentlyContinue |
        Where-Object { $_.CommandLine -like "*$Workspace*" } |
        ForEach-Object {
            Write-Host "Stopping PID $($_.ProcessId)"
            Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue
        }
}

function Remove-GitignoreBlock {
    param([string]$Path)
    if (-not (Test-Path $Path)) { return }

    $content = Get-Content -Raw $Path
    if ($content -notmatch "BEGIN mcp-semantic-search-zvec-go") { return }

    $block = '(?ms)(?:\r?\n)?# BEGIN mcp-semantic-search-zvec-go\r?\n\.mcp-semantic-search-zvec-go/\r?\n# END mcp-semantic-search-zvec-go\r?\n?'
    $newContent = ($content -replace $block, '').TrimEnd()

    if ([string]::IsNullOrWhiteSpace($newContent)) {
        Remove-Item -Force $Path
        Write-Host "Removed .gitignore (contained only mcp-semantic-search-zvec-go block)"
        return
    }

    $utf8NoBom = [System.Text.UTF8Encoding]::new($false)
    [System.IO.File]::WriteAllText($Path, $newContent + [Environment]::NewLine, $utf8NoBom)
    Write-Host "Removed mcp-semantic-search-zvec-go block from $Path"
}

Stop-StaleProcesses -Workspace $TargetRoot

$mcpJson = Join-Path $TargetRoot ".cursor\mcp.json"
if (Test-Path $mcpJson) {
    $obj = Get-Content -Raw $mcpJson | ConvertFrom-Json
    if ($obj.mcpServers.PSObject.Properties.Name -contains $ServerKey) {
        $obj.mcpServers.PSObject.Properties.Remove($ServerKey)
        [System.IO.File]::WriteAllText($mcpJson, ($obj | ConvertTo-Json -Depth 10))
    }
}

if (Test-Path $InstallDir) {
    if ($KeepData) {
        Get-ChildItem $InstallDir -Exclude data, models | Remove-Item -Recurse -Force
    } else {
        Remove-Item -Recurse -Force $InstallDir
    }
}

Remove-GitignoreBlock (Join-Path $TargetRoot ".gitignore")

Write-Host "Uninstalled $ServerKey. Restart Cursor."
