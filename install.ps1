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
$LogDir = Join-Path $InstallRoot 'logs'

if ($Uninstall) {
	$McpCodex = $Codex
	if (-not $McpCodex) {
		$McpCommand = Get-Command codex -ErrorAction SilentlyContinue
		if ($McpCommand) { $McpCodex = $McpCommand.Source }
	}
	if ($McpCodex) { & $McpCodex mcp remove ra2a 2>$null }
    Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath $InstallRoot -Recurse -Force -ErrorAction SilentlyContinue
    Write-Output 'RA2A uninstalled for current user'
    exit 0
}

if (-not $Pin) {
    $Pin = ([Guid]::NewGuid().ToString('N').Substring(0, 6)).ToUpperInvariant()
}
if ($Pin -notmatch '^[A-Za-z0-9]{6}$') {
    throw 'PIN must be exactly 6 letters or digits'
}
if (-not $NodeId) { throw 'node ID cannot be empty' }
if (-not $Name) { $Name = $NodeId }
foreach ($Value in @($NodeId, $Name, $Codex)) {
    if ($Value -and ($Value.Contains('"') -or $Value.Contains("`r") -or $Value.Contains("`n"))) {
        throw 'node ID, name, and Codex path cannot contain quotes or newlines'
    }
}

if (-not $Codex) {
    $Command = Get-Command codex.exe -ErrorAction SilentlyContinue
    if (-not $Command) { $Command = Get-Command codex -ErrorAction SilentlyContinue }
    if ($Command) { $Codex = $Command.Source }
}
if (-not $Codex -or -not (Test-Path -LiteralPath $Codex -PathType Leaf)) {
    throw 'Codex executable not found; pass -Codex C:\absolute\path\to\codex.exe'
}
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    throw 'Go 1.24 or newer is required to build from source'
}

$SourceRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
New-Item -ItemType Directory -Force -Path $BinDir, $LogDir | Out-Null
Push-Location $SourceRoot
try {
    & go build -trimpath -ldflags '-s -w' -o $BinaryPath ./cmd/ra2a
    if ($LASTEXITCODE -ne 0) { throw 'go build failed' }
} finally {
    Pop-Location
}

& $Codex mcp remove ra2a 2>$null
& $Codex mcp add ra2a -- $BinaryPath mcp
if ($LASTEXITCODE -ne 0) { throw 'failed to register RA2A MCP with Codex' }

$Arguments = 'serve --pin "{0}" --id "{1}" --name "{2}" --codex "{3}"' -f $Pin, $NodeId, $Name, $Codex
$Action = New-ScheduledTaskAction -Execute $BinaryPath -Argument $Arguments
$Trigger = New-ScheduledTaskTrigger -AtLogOn
$Settings = New-ScheduledTaskSettingsSet `
    -RestartCount 999 `
    -RestartInterval (New-TimeSpan -Minutes 1) `
    -ExecutionTimeLimit ([TimeSpan]::Zero) `
    -StartWhenAvailable `
    -AllowStartIfOnBatteries `
    -DontStopIfGoingOnBatteries
$CurrentUser = [System.Security.Principal.WindowsIdentity]::GetCurrent().Name
$Principal = New-ScheduledTaskPrincipal -UserId $CurrentUser -LogonType Interactive -RunLevel Limited

Register-ScheduledTask `
    -TaskName $TaskName `
    -Action $Action `
    -Trigger $Trigger `
    -Settings $Settings `
    -Principal $Principal `
    -Description 'RA2A LAN agent daemon' `
    -Force | Out-Null
Start-ScheduledTask -TaskName $TaskName
$Task = $null
for ($Attempt = 0; $Attempt -lt 10; $Attempt++) {
    $Task = Get-ScheduledTask -TaskName $TaskName
    if ($Task.State -eq 'Running') { break }
    Start-Sleep -Milliseconds 500
}
if ($Task.State -ne 'Running') { throw "RA2A task did not stay running; state: $($Task.State)" }

Write-Output 'RA2A installed'
Write-Output "binary: $BinaryPath"
Write-Output "node: $NodeId"
Write-Output "PIN: $Pin"
Write-Output 'status: running'
