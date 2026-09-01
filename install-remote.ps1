param(
    [string]$Version = $env:RA2A_VERSION,
    [string]$ReleaseRoot = $env:RA2A_RELEASE_ROOT,
    [string]$Pin,
    [string]$NodeId = $env:COMPUTERNAME,
    [string]$Name,
    [string]$Codex
)

$ErrorActionPreference = 'Stop'
if (-not $ReleaseRoot) { $ReleaseRoot = 'https://github.com/ceasarXuu/RA2A/releases' }
if (-not $Version) {
    $Release = Invoke-RestMethod 'https://api.github.com/repos/ceasarXuu/RA2A/releases/latest'
    $Version = $Release.tag_name
}
if ($Version -notmatch '^v[0-9]') { throw "Invalid release version: $Version" }

$RuntimeArchitecture = [Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString().ToLowerInvariant()
$Architecture = switch ($RuntimeArchitecture) {
    'x64' { 'amd64' }
    'arm64' { 'arm64' }
    default { throw "Unsupported architecture: $RuntimeArchitecture" }
}
$Asset = "ra2a-$Version-windows-$Architecture.exe"
$DownloadRoot = "$ReleaseRoot/download/$Version"
$TemporaryRoot = Join-Path $env:TEMP ("ra2a-release-{0}" -f ([Guid]::NewGuid().ToString('N')))
$TemporaryBinary = Join-Path $TemporaryRoot $Asset
$TemporaryChecksum = "$TemporaryBinary.sha256"

try {
    New-Item -ItemType Directory -Force -Path $TemporaryRoot | Out-Null
    Invoke-WebRequest "$DownloadRoot/$Asset" -OutFile $TemporaryBinary
    Invoke-WebRequest "$DownloadRoot/$Asset.sha256" -OutFile $TemporaryChecksum
    $Expected = ((Get-Content -LiteralPath $TemporaryChecksum -Raw).Trim() -split '\s+')[0]
    $Actual = (Get-FileHash -LiteralPath $TemporaryBinary -Algorithm SHA256).Hash
    if (-not $Expected -or $Expected -ine $Actual) { throw 'Release checksum verification failed' }

    $BinDir = Join-Path $HOME '.local\bin'
    $BinaryPath = Join-Path $BinDir 'ra2a.exe'
    $ConfigPath = Join-Path $HOME '.config\ra2a\config.json'
    $LegacyInstallRoot = Join-Path $env:LOCALAPPDATA 'RA2A'
    $LegacyConfigPath = Join-Path $LegacyInstallRoot 'config.json'
    $LegacyBinaryPath = Join-Path $LegacyInstallRoot 'bin\ra2a.exe'
    $ExistingTask = Get-ScheduledTask -TaskName RA2A -ErrorAction SilentlyContinue
    if ($ExistingTask) { Stop-ScheduledTask -TaskName RA2A -ErrorAction SilentlyContinue }
    New-Item -ItemType Directory -Force -Path $BinDir | Out-Null
    for ($Attempt = 0; $Attempt -lt 10; $Attempt++) {
        try {
            Move-Item -LiteralPath $TemporaryBinary -Destination $BinaryPath -Force
            break
        } catch {
            if ($Attempt -eq 9) { throw }
            Start-Sleep -Milliseconds 500
        }
    }
    Write-Output "RA2A $Version installed"
    Write-Output "binary: $BinaryPath"
    if ($Pin) {
        if (-not $Name) { $Name = $NodeId }
        if (-not $Codex) { $Codex = (Get-Command codex -ErrorAction SilentlyContinue).Source }
        if (-not $Codex) { throw 'Codex executable not found; pass -Codex C:\absolute\path\to\codex.exe' }
        & $BinaryPath setup --pin $Pin --node-id $NodeId --name $Name --codex $Codex
        if ($LASTEXITCODE -ne 0) { throw 'RA2A setup failed' }
    } elseif ((Test-Path -LiteralPath $ConfigPath) -or (Test-Path -LiteralPath $LegacyConfigPath)) {
        & $BinaryPath restart
        if ($LASTEXITCODE -ne 0) { throw 'RA2A restart failed' }
    } else {
        Write-Output "Run $BinaryPath to finish setup."
    }
    if (Test-Path -LiteralPath $ConfigPath) {
        Remove-Item -LiteralPath $LegacyBinaryPath -Force -ErrorAction SilentlyContinue
        Remove-Item -LiteralPath $LegacyConfigPath -Force -ErrorAction SilentlyContinue
    }
} finally {
    Remove-Item -LiteralPath $TemporaryRoot -Recurse -Force -ErrorAction SilentlyContinue
}
