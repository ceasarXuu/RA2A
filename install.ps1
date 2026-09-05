param(
    [string]$Pin,
    [string]$NodeId = $env:COMPUTERNAME,
    [string]$Name,
    [string]$Codex,
    [switch]$CodexWrapper,
    [switch]$Uninstall
)

$ErrorActionPreference = 'Stop'
$TaskName = 'RA2A'
$BinDir = Join-Path $HOME '.local\bin'
$BinaryPath = Join-Path $BinDir 'ra2a.exe'
$WrapperPath = Join-Path $BinDir 'codex.exe'
$WrapperCmdPath = Join-Path $BinDir 'codex.cmd'
$WrapperMarker = Join-Path $BinDir '.ra2a-codex-wrapper'
$ConfigPath = Join-Path $HOME '.config\ra2a\config.json'
$LegacyInstallRoot = Join-Path $env:LOCALAPPDATA 'RA2A'
$LegacyConfigPath = Join-Path $LegacyInstallRoot 'config.json'
$LegacyBinaryPath = Join-Path $LegacyInstallRoot 'bin\ra2a.exe'

if ($Uninstall) {
    $Mcp = $Codex
    if (-not $Mcp) { $Mcp = (Get-Command codex -ErrorAction SilentlyContinue).Source }
    # Run MCP cleanup while a wrapper can still pass `mcp` through.
    if ($Mcp -and (Test-Path -LiteralPath $Mcp -PathType Leaf)) { & $Mcp mcp remove ra2a 2>$null }
    if (Test-Path -LiteralPath $WrapperMarker) {
        Remove-Item -LiteralPath $WrapperPath, $WrapperCmdPath, $WrapperMarker -Force -ErrorAction SilentlyContinue
        Write-Output 'RA2A codex wrapper removed; the native codex command is restored.'
    }
    Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath $BinaryPath -Force -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath (Split-Path -Parent $ConfigPath) -Recurse -Force -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath $LegacyInstallRoot -Recurse -Force -ErrorAction SilentlyContinue
    Write-Output 'RA2A uninstalled for current user'
    exit 0
}

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    throw 'Go 1.24 or newer is required to build from source'
}
$SourceRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
$BuildPath = Join-Path $env:TEMP ("ra2a-install-{0}.exe" -f ([Guid]::NewGuid().ToString('N')))
Push-Location $SourceRoot
try {
    & go build -trimpath -ldflags '-s -w' -o $BuildPath ./cmd/ra2a
    if ($LASTEXITCODE -ne 0) { throw 'go build failed' }
} finally {
    Pop-Location
}
New-Item -ItemType Directory -Force -Path $BinDir | Out-Null
Stop-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
$RetiredPath = $null
Get-ChildItem -LiteralPath $BinDir -Filter 'ra2a.exe.retired-*' -ErrorAction SilentlyContinue |
    Remove-Item -Force -ErrorAction SilentlyContinue
if (Test-Path -LiteralPath $BinaryPath) {
    # Desktop-owned MCP processes may still map the old executable after the
    # daemon task stops. Windows permits renaming that image, but not replacing
    # it in place, so retire it before publishing the new command path.
    $RetiredPath = "$BinaryPath.retired-$([Guid]::NewGuid().ToString('N'))"
    Move-Item -LiteralPath $BinaryPath -Destination $RetiredPath
}
try {
    Move-Item -LiteralPath $BuildPath -Destination $BinaryPath
} catch {
    if ($RetiredPath -and -not (Test-Path -LiteralPath $BinaryPath)) {
        Move-Item -LiteralPath $RetiredPath -Destination $BinaryPath -ErrorAction SilentlyContinue
    }
    throw
}
Write-Output 'RA2A command installed'
Write-Output "binary: $BinaryPath"

if ($CodexWrapper) {
    if ((Test-Path -LiteralPath $WrapperPath) -and -not (Test-Path -LiteralPath $WrapperMarker)) {
        throw "codex.exe already exists at $WrapperPath without the RA2A marker; refusing to overwrite it"
    }
    $WrapperBuildPath = Join-Path $env:TEMP ("codex-wrapper-{0}.exe" -f ([Guid]::NewGuid().ToString('N')))
    Push-Location $SourceRoot
    try {
        & go build -trimpath -ldflags '-s -w' -o $WrapperBuildPath ./cmd/codex-wrapper
        if ($LASTEXITCODE -ne 0) { throw 'codex wrapper build failed' }
    } finally {
        Pop-Location
    }
    New-Item -ItemType Directory -Force -Path $BinDir | Out-Null
    Move-Item -LiteralPath $WrapperBuildPath -Destination $WrapperPath -Force
    @("@echo off", "`"%~dp0codex.exe`" %*") | Set-Content -LiteralPath $WrapperCmdPath -Encoding Ascii
    New-Item -ItemType File -Path $WrapperMarker -Force | Out-Null
    Write-Output 'RA2A codex wrapper installed (plain codex TUI sessions are proxied when RA2A is available)'
} elseif (Test-Path -LiteralPath $WrapperMarker) {
    # Wrapper was previously installed but this run did not request it again;
    # keep the existing wrapper so the user's environment stays stable.
    Write-Output 'RA2A codex wrapper already installed (kept)'
}

$SetupRequested = $PSBoundParameters.ContainsKey('Pin') -or $PSBoundParameters.ContainsKey('NodeId') -or $PSBoundParameters.ContainsKey('Name') -or $PSBoundParameters.ContainsKey('Codex')
if (-not $SetupRequested) {
    if ((Test-Path -LiteralPath $ConfigPath) -or (Test-Path -LiteralPath $LegacyConfigPath)) {
        & $BinaryPath restart
        if ($LASTEXITCODE -ne 0) { throw 'RA2A restart failed' }
        if (Test-Path -LiteralPath $ConfigPath) {
            Remove-Item -LiteralPath $LegacyBinaryPath -Force -ErrorAction SilentlyContinue
            Remove-Item -LiteralPath $LegacyConfigPath -Force -ErrorAction SilentlyContinue
        }
        exit 0
    }
    Write-Output 'Run ra2a to finish setup.'
    exit 0
}
if ($Pin -notmatch '^[A-Za-z0-9]{6}$') {
    throw 'PIN must be exactly 6 letters or digits'
}
if (-not $Name) { $Name = $NodeId }
if (-not $Codex) {
    $Command = Get-Command codex.exe -ErrorAction SilentlyContinue
    if (-not $Command) { $Command = Get-Command codex -ErrorAction SilentlyContinue }
    if ($Command) { $Codex = $Command.Source }
}
if (-not $Codex -or -not (Test-Path -LiteralPath $Codex -PathType Leaf)) {
    throw 'Codex executable not found; pass -Codex C:\absolute\path\to\codex.exe'
}
& $BinaryPath setup --pin $Pin --node-id $NodeId --name $Name --codex $Codex
if ($LASTEXITCODE -ne 0) { throw 'RA2A setup failed' }
if (Test-Path -LiteralPath $ConfigPath) {
    Remove-Item -LiteralPath $LegacyBinaryPath -Force -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath $LegacyConfigPath -Force -ErrorAction SilentlyContinue
}
