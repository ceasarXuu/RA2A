param(
    [string]$Pin,
    [string]$NodeId = $env:COMPUTERNAME,
    [string]$Name,
    [string]$Codex,
    # Legacy aliases; launchers are now detected and installed automatically.
    [switch]$CodexWrapper,
    [switch]$OpenCodeWrapper,
    [switch]$Uninstall
)

$ErrorActionPreference = 'Stop'
$TaskName = 'RA2A'
$BinDir = Join-Path $HOME '.local\bin'
$BinaryPath = Join-Path $BinDir 'ra2a.exe'
$WrapperPath = Join-Path $BinDir 'codex.exe'
$WrapperCmdPath = Join-Path $BinDir 'codex.cmd'
$WrapperMarker = Join-Path $BinDir '.ra2a-codex-wrapper'
$WrapperNativePath = Join-Path $BinDir '.ra2a-codex-native-path'
$OcWrapperPath = Join-Path $BinDir 'opencode.exe'
$OcWrapperReal = Join-Path $BinDir 'opencode.real.exe'
$OcWrapperMarker = Join-Path $BinDir '.ra2a-opencode-wrapper'
$OcNativePath = Join-Path $BinDir '.ra2a-opencode-native-path'
$ConfigPath = Join-Path $HOME '.config\ra2a\config.json'
$LegacyInstallRoot = Join-Path $env:LOCALAPPDATA 'RA2A'
$LegacyConfigPath = Join-Path $LegacyInstallRoot 'config.json'
$LegacyBinaryPath = Join-Path $LegacyInstallRoot 'bin\ra2a.exe'

if ($Uninstall) {
    if (Test-Path -LiteralPath $BinaryPath) {
        & $BinaryPath opencode-mcp-unregister
        if ($LASTEXITCODE -ne 0) { throw 'could not unregister OpenCode MCP' }
        & $BinaryPath opencode-server-cleanup
        if ($LASTEXITCODE -ne 0) { throw 'could not stop the shared OpenCode server' }
    }
    $Mcp = $Codex
    if (-not $Mcp) { $Mcp = (Get-Command codex -ErrorAction SilentlyContinue).Source }
    # Run MCP cleanup while a wrapper can still pass `mcp` through.
    if ($Mcp -and (Test-Path -LiteralPath $Mcp -PathType Leaf)) { & $Mcp mcp remove ra2a 2>$null }
    if (Test-Path -LiteralPath $WrapperMarker) {
        if (Test-Path -LiteralPath $WrapperPath) {
            Move-Item -LiteralPath $WrapperPath -Destination "$WrapperPath.retired-$([Guid]::NewGuid().ToString('N'))"
        }
        Remove-Item -LiteralPath $WrapperCmdPath -Force -ErrorAction SilentlyContinue
        foreach ($Suffix in @('.exe', '.cmd')) {
            $Saved = Join-Path $BinDir "codex.bin$Suffix"
            if (Test-Path -LiteralPath $Saved) {
                Move-Item -LiteralPath $Saved -Destination (Join-Path $BinDir "codex$Suffix")
            }
        }
        Remove-Item -LiteralPath $WrapperMarker, $WrapperNativePath -Force -ErrorAction SilentlyContinue
        Write-Output 'RA2A codex wrapper removed; the native codex command is restored.'
    }
    if (Test-Path -LiteralPath $OcWrapperMarker) {
        $OcUninstallRetired = $null
        if (Test-Path -LiteralPath $OcWrapperPath) {
            $OcUninstallRetired = "$OcWrapperPath.retired-$([Guid]::NewGuid().ToString('N'))"
            Move-Item -LiteralPath $OcWrapperPath -Destination $OcUninstallRetired
        }
        try {
            if (Test-Path -LiteralPath $OcWrapperReal) {
                Move-Item -LiteralPath $OcWrapperReal -Destination $OcWrapperPath
            }
        } catch {
            if ($OcUninstallRetired -and -not (Test-Path -LiteralPath $OcWrapperPath)) {
                Move-Item -LiteralPath $OcUninstallRetired -Destination $OcWrapperPath -ErrorAction SilentlyContinue
            }
            throw
        }
        Remove-Item -LiteralPath $OcWrapperMarker -Force
        Remove-Item -LiteralPath $OcNativePath -Force -ErrorAction SilentlyContinue
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
if ($Codex -and -not (Test-Path -LiteralPath $Codex -PathType Leaf)) { throw "Codex executable not found: $Codex" }
$CodexNative = $Codex
$OpenCodeNative = $null
if (-not $CodexNative -and (Test-Path -LiteralPath $WrapperMarker)) {
    if (Test-Path -LiteralPath $WrapperNativePath) { $CodexNative = (Get-Content -LiteralPath $WrapperNativePath -Raw).Trim() }
    if ($CodexNative -and -not (Test-Path -LiteralPath $CodexNative -PathType Leaf)) { $CodexNative = $null }
    if (-not $CodexNative) {
        foreach ($Suffix in @('.exe', '.cmd')) {
            $Saved = Join-Path $BinDir "codex.bin$Suffix"
            if (Test-Path -LiteralPath $Saved) { $CodexNative = $Saved; break }
        }
    }
}
if (-not $CodexNative) {
    $CodexNative = Get-Command codex.exe, codex.cmd, codex -All -ErrorAction SilentlyContinue |
        Where-Object { $_.CommandType -eq 'Application' -and (Test-Path -LiteralPath $_.Source -PathType Leaf) -and $_.Source -ne $WrapperPath -and $_.Source -ne $WrapperCmdPath } |
        Select-Object -ExpandProperty Source -First 1
}
if (-not $CodexNative -and -not (Test-Path -LiteralPath $WrapperMarker) -and (Test-Path -LiteralPath $WrapperPath)) {
    $CodexNative = $WrapperPath
}
if (-not $CodexNative) {
    # Codex Desktop can bundle a usable native CLI without putting it on PATH.
    $CodexNative = Get-CimInstance Win32_Process -Filter "Name='codex.exe'" -ErrorAction SilentlyContinue |
        Where-Object { $_.CommandLine -match 'app-server' -and $_.CommandLine -notmatch '\.ra2a-' -and $_.ExecutablePath } |
        Select-Object -ExpandProperty ExecutablePath -First 1
}
if (-not $CodexNative -and (Test-Path -LiteralPath $WrapperMarker)) {
    $Retired = [Guid]::NewGuid().ToString('N')
    foreach ($Path in @($WrapperPath, $WrapperCmdPath, $WrapperMarker, $WrapperNativePath)) {
        if (Test-Path -LiteralPath $Path) { Move-Item -LiteralPath $Path -Destination "$Path.retired-$Retired" }
    }
    Write-Output 'Codex executable is missing; stale RA2A launcher moved to backup'
}
if (Test-Path -LiteralPath $OcWrapperMarker) {
    if (Test-Path -LiteralPath $OcNativePath) { $OpenCodeNative = (Get-Content -LiteralPath $OcNativePath -Raw).Trim() }
    if ($OpenCodeNative -and -not (Test-Path -LiteralPath $OpenCodeNative -PathType Leaf)) { $OpenCodeNative = $null }
    if (-not $OpenCodeNative -and (Test-Path -LiteralPath $OcWrapperReal)) { $OpenCodeNative = $OcWrapperReal }
}
if (-not $OpenCodeNative) {
    $OpenCodeNative = Get-Command opencode.exe, opencode.cmd, opencode -All -ErrorAction SilentlyContinue |
        Where-Object { $_.CommandType -eq 'Application' -and (Test-Path -LiteralPath $_.Source -PathType Leaf) -and $_.Source -ne $OcWrapperPath } |
        Select-Object -ExpandProperty Source -First 1
}
if (-not $OpenCodeNative -and -not (Test-Path -LiteralPath $OcWrapperMarker) -and (Test-Path -LiteralPath $OcWrapperPath)) {
    $OpenCodeNative = $OcWrapperPath
}
if (-not $OpenCodeNative -and (Test-Path -LiteralPath $OcWrapperMarker)) {
    $Retired = [Guid]::NewGuid().ToString('N')
    foreach ($Path in @($OcWrapperPath, $OcWrapperMarker, $OcNativePath)) {
        if (Test-Path -LiteralPath $Path) { Move-Item -LiteralPath $Path -Destination "$Path.retired-$Retired" }
    }
    Write-Output 'OpenCode executable is missing; stale RA2A launcher moved to backup'
}
if ($CodexNative) { Write-Output "detected harness: Codex ($CodexNative)" }
if ($OpenCodeNative) { Write-Output "detected harness: OpenCode ($OpenCodeNative)" }
if (-not $CodexNative -and -not $OpenCodeNative) { Write-Output 'no supported harness detected; RA2A command only will be installed' }
$BuildPath = Join-Path $env:TEMP ("ra2a-install-{0}.exe" -f ([Guid]::NewGuid().ToString('N')))
Push-Location $SourceRoot
try {
    & go build -trimpath -ldflags '-s -w' -o $BuildPath ./cmd/ra2a
    if ($LASTEXITCODE -ne 0) { throw 'go build failed' }
} finally {
    Pop-Location
}
$WrapperBuildPath = $null
$OcBuildPath = $null
if ($CodexNative -or $OpenCodeNative) {
    Push-Location $SourceRoot
    try {
        if ($CodexNative) {
            $WrapperBuildPath = Join-Path $env:TEMP ("codex-wrapper-{0}.exe" -f ([Guid]::NewGuid().ToString('N')))
            & go build -trimpath -ldflags '-s -w' -o $WrapperBuildPath ./cmd/codex-wrapper
            if ($LASTEXITCODE -ne 0) { throw 'codex wrapper build failed' }
        }
        if ($OpenCodeNative) {
            $OcBuildPath = Join-Path $env:TEMP ("oc-wrapper-{0}.exe" -f ([Guid]::NewGuid().ToString('N')))
            & go build -trimpath -ldflags '-s -w' -o $OcBuildPath ./cmd/oc-wrapper
            if ($LASTEXITCODE -ne 0) { throw 'opencode wrapper build failed' }
        }
    } finally {
        Pop-Location
    }
}
New-Item -ItemType Directory -Force -Path $BinDir | Out-Null
$OldDaemon = Get-CimInstance Win32_Process -Filter "Name='ra2a.exe'" -ErrorAction SilentlyContinue |
    Where-Object { $_.CommandLine -match '\sdaemon(\s|$)' } | Select-Object -First 1
Stop-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
if ($OldDaemon) {
    for ($Attempt = 0; $Attempt -lt 20 -and (Get-Process -Id $OldDaemon.ProcessId -ErrorAction SilentlyContinue); $Attempt++) {
        Start-Sleep -Milliseconds 500
    }
    if (Get-Process -Id $OldDaemon.ProcessId -ErrorAction SilentlyContinue) { throw 'old RA2A daemon did not exit; refusing to publish a new binary' }
}
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

if ($CodexNative) {
    New-Item -ItemType Directory -Force -Path $BinDir | Out-Null
    if (-not (Test-Path -LiteralPath $WrapperMarker)) {
        foreach ($Suffix in @('.exe', '.cmd')) {
            $Original = Join-Path $BinDir "codex$Suffix"
            if (Test-Path -LiteralPath $Original) {
                $Saved = Join-Path $BinDir "codex.bin$Suffix"
                if (Test-Path -LiteralPath $Saved) { throw "refusing to overwrite existing $Saved" }
                Move-Item -LiteralPath $Original -Destination $Saved
                if ($CodexNative -eq $Original) { $CodexNative = $Saved }
            }
        }
    } elseif (Test-Path -LiteralPath $WrapperPath) {
        Move-Item -LiteralPath $WrapperPath -Destination "$WrapperPath.retired-$([Guid]::NewGuid().ToString('N'))"
    }
    Move-Item -LiteralPath $WrapperBuildPath -Destination $WrapperPath
    @("@echo off", "`"%~dp0codex.exe`" %*") | Set-Content -LiteralPath $WrapperCmdPath -Encoding Ascii
    Set-Content -LiteralPath $WrapperNativePath -Value $CodexNative -NoNewline
    New-Item -ItemType File -Path $WrapperMarker -Force | Out-Null
    Write-Output 'RA2A codex wrapper installed (plain codex TUI sessions are proxied when RA2A is available)'
}

if ($OpenCodeNative) {
    New-Item -ItemType Directory -Force -Path $BinDir | Out-Null
    $OcNativeMoved = $false
    if ((Test-Path -LiteralPath $OcWrapperPath) -and -not (Test-Path -LiteralPath $OcWrapperMarker)) {
        if (Test-Path -LiteralPath $OcWrapperReal) {
            throw "opencode.real.exe already exists at $OcWrapperReal; refusing to overwrite it"
        }
        Move-Item -LiteralPath $OcWrapperPath -Destination $OcWrapperReal -Force
        if ($OpenCodeNative -eq $OcWrapperPath) { $OpenCodeNative = $OcWrapperReal }
        $OcNativeMoved = $true
    }
    $OcRetired = $null
    if (Test-Path -LiteralPath $OcWrapperMarker) {
        if (-not (Test-Path -LiteralPath $OcWrapperPath)) {
            throw "OpenCode wrapper marker exists but $OcWrapperPath is missing; refusing to replace an unknown installation"
        }
        $OcRetired = "$OcWrapperPath.retired-$([Guid]::NewGuid().ToString('N'))"
        Move-Item -LiteralPath $OcWrapperPath -Destination $OcRetired
    }
    try {
        Move-Item -LiteralPath $OcBuildPath -Destination $OcWrapperPath
    } catch {
        if ($OcRetired -and -not (Test-Path -LiteralPath $OcWrapperPath)) {
            Move-Item -LiteralPath $OcRetired -Destination $OcWrapperPath -ErrorAction SilentlyContinue
        }
        if ($OcNativeMoved -and -not (Test-Path -LiteralPath $OcWrapperPath)) {
            Move-Item -LiteralPath $OcWrapperReal -Destination $OcWrapperPath -ErrorAction SilentlyContinue
        }
        throw
    }
    New-Item -ItemType File -Path $OcWrapperMarker -Force | Out-Null
    Set-Content -LiteralPath $OcNativePath -Value $OpenCodeNative -NoNewline
    Write-Output 'RA2A opencode wrapper installed; interactive opencode attaches automatically.'
}

if (($env:Path -split ';') -notcontains $BinDir) {
    Write-Output "RA2A launcher directory is not on PATH: add $BinDir before other harness binaries when opening a new terminal."
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
    $Codex = $CodexNative
}
if (-not $Codex -and -not $OpenCodeNative) {
    throw 'No supported harness found; install Codex or OpenCode before setup'
}
$SetupArgs = @('setup', '--pin', $Pin, '--node-id', $NodeId, '--name', $Name)
if ($Codex) { $SetupArgs += @('--codex', $Codex) }
if ($OpenCodeNative) { $SetupArgs += @('--opencode', $OpenCodeNative) }
& $BinaryPath @SetupArgs
if ($LASTEXITCODE -ne 0) { throw 'RA2A setup failed' }
if (Test-Path -LiteralPath $ConfigPath) {
    Remove-Item -LiteralPath $LegacyBinaryPath -Force -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath $LegacyConfigPath -Force -ErrorAction SilentlyContinue
}
