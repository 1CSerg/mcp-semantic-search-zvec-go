function Wait-IfInteractiveOnError {
    param([int]$ExitCode = 1)
    if ($ExitCode -eq 0) { return }
    if ($env:STAY_OPEN_DISABLE -eq 'true') { return }
    if ($env:STAY_OPEN_SUPPRESS -eq '1') { return }
    if ($env:CI -in @('true','1')) { return }
    if ($env:GITHUB_ACTIONS -eq 'true') { return }
    if ($Host.Name -ne 'ConsoleHost') { return }
    try { if ([Console]::IsInputRedirected) { return } } catch { return }
    if (-not [Environment]::UserInteractive) { return }
    if ($Error.Count -gt 0) {
        Write-Host ''
        Write-Host 'Ошибка:' -ForegroundColor Red
        Write-Host ($Error[0].ToString()) -ForegroundColor Red
    }
    Write-Host ''
    Read-Host 'Нажмите Enter для закрытия'
}
