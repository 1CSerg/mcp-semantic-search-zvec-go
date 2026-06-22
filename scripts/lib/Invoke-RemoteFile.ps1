function Invoke-RemoteFile {
    param(
        [Parameter(Mandatory)]
        [string]$Uri,
        [Parameter(Mandatory)]
        [string]$OutFile
    )
    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
    $iwrError = $null
    try {
        Invoke-WebRequest -Uri $Uri -OutFile $OutFile -UseBasicParsing
        return
    } catch {
        $iwrError = $_
        Write-Host "Invoke-WebRequest failed, retrying with curl.exe..."
    }
    $curl = Get-Command curl.exe -ErrorAction SilentlyContinue
    if (-not $curl) {
        throw "Download failed: $iwrError (curl.exe not found)"
    }
    & curl.exe --ssl-no-revoke -fsSL -o $OutFile $Uri
    if ($LASTEXITCODE -ne 0) {
        throw "curl.exe download failed with exit code $LASTEXITCODE (after IWR: $iwrError)"
    }
}
