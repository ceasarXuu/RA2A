param(
    [string]$Pin,
    [string]$NodeId = $env:COMPUTERNAME,
    [string]$Name,
    [string]$Codex,
    [switch]$Uninstall
)

$ErrorActionPreference = 'Stop'
$TaskName = 'RA2A'
$InstallRoot = Join-Path $env:LOCALAPPDATA 'RA2A'
$BinDir = Join-Path $InstallRoot 'bin'
$BinaryPath = Join-Path $BinDir 'ra2a.exe'

if ($Uninstall) {
    $Mcp = $Codex
    if (-not $Mcp) { $Mcp = (Get-Command codex -ErrorAction SilentlyContinue).Source }
    if ($Mcp) { & $Mcp mcp remove ra2a 2>$null }
    Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath $InstallRoot -Recurse -Force -ErrorAction SilentlyContinue
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
$ExistingTask = Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
Stop-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
Move-Item -LiteralPath $BuildPath -Destination $BinaryPath -Force
if ($ExistingTask) { Start-ScheduledTask -TaskName $TaskName }
Write-Output 'RA2A command installed'
Write-Output "binary: $BinaryPath"

$SetupRequested = $PSBoundParameters.ContainsKey('Pin') -or $PSBoundParameters.ContainsKey('NodeId') -or $PSBoundParameters.ContainsKey('Name') -or $PSBoundParameters.ContainsKey('Codex')
if (-not $SetupRequested) {
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
