param(
    [string]$Version = $env:RA2A_VERSION,
    [string]$ReleaseRoot = $env:RA2A_RELEASE_ROOT,
    [string]$Pin,
    [string]$NodeId = $env:COMPUTERNAME,
    [string]$Name,
    [string]$Codex,
    [switch]$Uninstall
)

$ErrorActionPreference = 'Stop'
if ($Uninstall) {
    $BinDir = Join-Path $HOME '.local\bin'
    $BinaryPath = Join-Path $BinDir 'ra2a.exe'
    if (Test-Path -LiteralPath $BinaryPath) {
        & $BinaryPath opencode-mcp-unregister
        if ($LASTEXITCODE -ne 0) { throw 'could not unregister OpenCode MCP' }
        & $BinaryPath opencode-server-cleanup
        if ($LASTEXITCODE -ne 0) { throw 'could not stop the shared OpenCode server' }
    }
    $CodexCmd = Get-Command codex -ErrorAction SilentlyContinue
    if ($CodexCmd) { & $CodexCmd.Source mcp remove ra2a 2>$null }
    if (Test-Path -LiteralPath (Join-Path $BinDir '.ra2a-codex-wrapper')) {
        $WrapperPath = Join-Path $BinDir 'codex.exe'
        if (Test-Path -LiteralPath $WrapperPath) {
            Move-Item -LiteralPath $WrapperPath -Destination "$WrapperPath.retired-$([Guid]::NewGuid().ToString('N'))"
        }
        Remove-Item -LiteralPath (Join-Path $BinDir 'codex.cmd') -Force -ErrorAction SilentlyContinue
        foreach ($Suffix in @('.exe', '.cmd')) {
            $Saved = Join-Path $BinDir "codex.bin$Suffix"
            if (Test-Path -LiteralPath $Saved) { Move-Item -LiteralPath $Saved -Destination (Join-Path $BinDir "codex$Suffix") }
        }
        Remove-Item -LiteralPath (Join-Path $BinDir '.ra2a-codex-wrapper'), (Join-Path $BinDir '.ra2a-codex-native-path') -Force -ErrorAction SilentlyContinue
    }
    if (Test-Path -LiteralPath (Join-Path $BinDir '.ra2a-opencode-wrapper')) {
        $WrapperPath = Join-Path $BinDir 'opencode.exe'
        if (Test-Path -LiteralPath $WrapperPath) {
            Move-Item -LiteralPath $WrapperPath -Destination "$WrapperPath.retired-$([Guid]::NewGuid().ToString('N'))"
        }
        $Saved = Join-Path $BinDir 'opencode.real.exe'
        if (Test-Path -LiteralPath $Saved) { Move-Item -LiteralPath $Saved -Destination $WrapperPath }
        Remove-Item -LiteralPath (Join-Path $BinDir '.ra2a-opencode-wrapper'), (Join-Path $BinDir '.ra2a-opencode-native-path') -Force -ErrorAction SilentlyContinue
    }
    Unregister-ScheduledTask -TaskName RA2A -Confirm:$false -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath $BinaryPath -Force -ErrorAction SilentlyContinue
    Write-Output 'RA2A uninstalled; native harness commands restored'
    exit 0
}
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
$BinDir = Join-Path $HOME '.local\bin'
$BinaryPath = Join-Path $BinDir 'ra2a.exe'
$CodexWrapperPath = Join-Path $BinDir 'codex.exe'
$CodexCmdPath = Join-Path $BinDir 'codex.cmd'
$CodexMarker = Join-Path $BinDir '.ra2a-codex-wrapper'
$CodexNativePath = Join-Path $BinDir '.ra2a-codex-native-path'
$OcWrapperPath = Join-Path $BinDir 'opencode.exe'
$OcRealPath = Join-Path $BinDir 'opencode.real.exe'
$OcMarker = Join-Path $BinDir '.ra2a-opencode-wrapper'
$OcNativePath = Join-Path $BinDir '.ra2a-opencode-native-path'
if ($Codex -and -not (Test-Path -LiteralPath $Codex -PathType Leaf)) { throw "Codex executable not found: $Codex" }
$CodexSource = $Codex
$RecordedCodexSource = if (Test-Path -LiteralPath $CodexNativePath) { (Get-Content -LiteralPath $CodexNativePath -Raw).Trim() } else { $null }
# The standalone managed install is the only layout Codex can update by itself:
# `codex update` answers "Could not detect the Codex installation method" through
# anything else, and the Codex App unpacks a fresh hash-versioned bin per update
# (…\AppData\Local\OpenAI\Codex\bin\<hash>\codex.exe), so a pin found there also
# goes stale silently. Both observed shapes are probed so an unknown layout keeps
# today's detection instead of silently pinning nothing.
$CodexHome = if ($env:CODEX_HOME) { $env:CODEX_HOME } else { Join-Path $HOME '.codex' }
$CodexStandalone = @(
    (Join-Path $CodexHome 'packages\standalone\current\bin\codex.exe'),
    (Join-Path $CodexHome 'packages\standalone\current\codex.exe'),
    (Join-Path $CodexHome 'packages\standalone\current\bin\codex'),
    (Join-Path $CodexHome 'packages\standalone\current\codex')
) | Where-Object { Test-Path -LiteralPath $_ -PathType Leaf } | Select-Object -First 1
# An explicit -Codex still wins, but replacing a recorded pin is a visible state
# change: name both paths so the swap can be reversed deliberately.
if (-not $CodexSource -and $CodexStandalone) {
    $CodexSource = $CodexStandalone
    if ($RecordedCodexSource -and $RecordedCodexSource -ne $CodexSource) {
        Write-Output "replacing recorded Codex pin ($RecordedCodexSource) with the self-updatable standalone install ($CodexSource)"
    }
}
if (-not $CodexSource -and (Test-Path -LiteralPath $CodexNativePath)) { $CodexSource = (Get-Content -LiteralPath $CodexNativePath -Raw).Trim() }
if ($CodexSource -and -not (Test-Path -LiteralPath $CodexSource -PathType Leaf)) { $CodexSource = $null }
if (-not $CodexSource -and (Test-Path -LiteralPath $CodexMarker)) {
    foreach ($Suffix in @('.exe', '.cmd')) {
        $Saved = Join-Path $BinDir "codex.bin$Suffix"
        if (Test-Path -LiteralPath $Saved) { $CodexSource = $Saved; break }
    }
}
if (-not $CodexSource) {
    $CodexSource = Get-Command codex.exe, codex.cmd, codex -All -ErrorAction SilentlyContinue |
        Where-Object { $_.CommandType -eq 'Application' -and (Test-Path -LiteralPath $_.Source -PathType Leaf) -and $_.Source -ne $CodexWrapperPath -and $_.Source -ne $CodexCmdPath } |
        Select-Object -ExpandProperty Source -First 1
}
if (-not $CodexSource -and -not (Test-Path -LiteralPath $CodexMarker) -and (Test-Path -LiteralPath $CodexWrapperPath)) {
    $CodexSource = $CodexWrapperPath
}
if (-not $CodexSource) {
    $CodexSource = Get-CimInstance Win32_Process -Filter "Name='codex.exe'" -ErrorAction SilentlyContinue |
        Where-Object { $_.CommandLine -match 'app-server' -and $_.CommandLine -notmatch '\.ra2a-' -and $_.ExecutablePath } |
        Select-Object -ExpandProperty ExecutablePath -First 1
}
if (-not $CodexSource -and (Test-Path -LiteralPath $CodexMarker)) {
    $Retired = [Guid]::NewGuid().ToString('N')
    foreach ($Path in @($CodexWrapperPath, $CodexCmdPath, $CodexMarker, $CodexNativePath)) {
        if (Test-Path -LiteralPath $Path) { Move-Item -LiteralPath $Path -Destination "$Path.retired-$Retired" }
    }
    Write-Output 'Codex executable is missing; stale RA2A launcher moved to backup'
}
$OcSource = $null
if (Test-Path -LiteralPath $OcNativePath) { $OcSource = (Get-Content -LiteralPath $OcNativePath -Raw).Trim() }
if ($OcSource -and -not (Test-Path -LiteralPath $OcSource -PathType Leaf)) { $OcSource = $null }
if (-not $OcSource -and (Test-Path -LiteralPath $OcRealPath)) { $OcSource = $OcRealPath }
if (-not $OcSource) {
    $OcSource = Get-Command opencode.exe, opencode.cmd, opencode -All -ErrorAction SilentlyContinue |
        Where-Object { $_.CommandType -eq 'Application' -and (Test-Path -LiteralPath $_.Source -PathType Leaf) -and $_.Source -ne $OcWrapperPath } |
        Select-Object -ExpandProperty Source -First 1
}
if (-not $OcSource -and -not (Test-Path -LiteralPath $OcMarker) -and (Test-Path -LiteralPath $OcWrapperPath)) {
    $OcSource = $OcWrapperPath
}
if (-not $OcSource -and (Test-Path -LiteralPath $OcMarker)) {
    $Retired = [Guid]::NewGuid().ToString('N')
    foreach ($Path in @($OcWrapperPath, $OcMarker, $OcNativePath)) {
        if (Test-Path -LiteralPath $Path) { Move-Item -LiteralPath $Path -Destination "$Path.retired-$Retired" }
    }
    Write-Output 'OpenCode executable is missing; stale RA2A launcher moved to backup'
}
if ($CodexSource) { Write-Output "detected harness: Codex ($CodexSource)" }
if ($OcSource) { Write-Output "detected harness: OpenCode ($OcSource)" }

try {
    New-Item -ItemType Directory -Force -Path $TemporaryRoot | Out-Null
    Invoke-WebRequest "$DownloadRoot/$Asset" -OutFile $TemporaryBinary
    Invoke-WebRequest "$DownloadRoot/$Asset.sha256" -OutFile $TemporaryChecksum
    $Expected = ((Get-Content -LiteralPath $TemporaryChecksum -Raw).Trim() -split '\s+')[0]
    $Actual = (Get-FileHash -LiteralPath $TemporaryBinary -Algorithm SHA256).Hash
    if (-not $Expected -or $Expected -ine $Actual) { throw 'Release checksum verification failed' }

    foreach ($Wrapper in @(@('codex-wrapper', $CodexSource), @('opencode-wrapper', $OcSource))) {
        if (-not $Wrapper[1]) { continue }
        $WrapperAsset = "$($Wrapper[0])-$Version-windows-$Architecture.exe"
        $WrapperBinary = Join-Path $TemporaryRoot $WrapperAsset
        Invoke-WebRequest "$DownloadRoot/$WrapperAsset" -OutFile $WrapperBinary
        Invoke-WebRequest "$DownloadRoot/$WrapperAsset.sha256" -OutFile "$WrapperBinary.sha256"
        $WrapperExpected = ((Get-Content -LiteralPath "$WrapperBinary.sha256" -Raw).Trim() -split '\s+')[0]
        $WrapperActual = (Get-FileHash -LiteralPath $WrapperBinary -Algorithm SHA256).Hash
        if (-not $WrapperExpected -or $WrapperExpected -ine $WrapperActual) { throw "Release checksum verification failed: $WrapperAsset" }
    }

    $ConfigPath = Join-Path $HOME '.config\ra2a\config.json'
    $LegacyInstallRoot = Join-Path $env:LOCALAPPDATA 'RA2A'
    $LegacyConfigPath = Join-Path $LegacyInstallRoot 'config.json'
    $LegacyBinaryPath = Join-Path $LegacyInstallRoot 'bin\ra2a.exe'
    $ExistingTask = Get-ScheduledTask -TaskName RA2A -ErrorAction SilentlyContinue
    $OldDaemon = Get-CimInstance Win32_Process -Filter "Name='ra2a.exe'" -ErrorAction SilentlyContinue |
        Where-Object { $_.CommandLine -match '\sdaemon(\s|$)' } | Select-Object -First 1
    if ($ExistingTask) { Stop-ScheduledTask -TaskName RA2A -ErrorAction SilentlyContinue }
    if ($OldDaemon) {
        for ($Attempt = 0; $Attempt -lt 20 -and (Get-Process -Id $OldDaemon.ProcessId -ErrorAction SilentlyContinue); $Attempt++) {
            Start-Sleep -Milliseconds 500
        }
        if (Get-Process -Id $OldDaemon.ProcessId -ErrorAction SilentlyContinue) { throw 'old RA2A daemon did not exit' }
    }
    New-Item -ItemType Directory -Force -Path $BinDir | Out-Null
    $RetiredPath = $null
    if (Test-Path -LiteralPath $BinaryPath) {
        $RetiredPath = "$BinaryPath.retired-$([Guid]::NewGuid().ToString('N'))"
        Move-Item -LiteralPath $BinaryPath -Destination $RetiredPath
    }
    try { Move-Item -LiteralPath $TemporaryBinary -Destination $BinaryPath }
    catch {
        if ($RetiredPath -and -not (Test-Path -LiteralPath $BinaryPath)) { Move-Item -LiteralPath $RetiredPath -Destination $BinaryPath -ErrorAction SilentlyContinue }
        throw
    }

    if ($CodexSource) {
        if (-not (Test-Path -LiteralPath $CodexMarker)) {
            foreach ($Suffix in @('.exe', '.cmd')) {
                $Original = Join-Path $BinDir "codex$Suffix"
                if (Test-Path -LiteralPath $Original) {
                    $Saved = Join-Path $BinDir "codex.bin$Suffix"
                    if (Test-Path -LiteralPath $Saved) { throw "refusing to overwrite $Saved" }
                    Move-Item -LiteralPath $Original -Destination $Saved
                    if ($CodexSource -eq $Original) { $CodexSource = $Saved }
                }
            }
        } elseif (Test-Path -LiteralPath $CodexWrapperPath) {
            Move-Item -LiteralPath $CodexWrapperPath -Destination "$CodexWrapperPath.retired-$([Guid]::NewGuid().ToString('N'))"
        }
        Move-Item -LiteralPath (Join-Path $TemporaryRoot "codex-wrapper-$Version-windows-$Architecture.exe") -Destination $CodexWrapperPath
        @('@echo off', '"%~dp0codex.exe" %*') | Set-Content -LiteralPath $CodexCmdPath -Encoding Ascii
        Set-Content -LiteralPath $CodexNativePath -Value $CodexSource -NoNewline
        New-Item -ItemType File -Path $CodexMarker -Force | Out-Null
        Write-Output 'Codex CLI launcher automatically installed'
    }
    if ($OcSource) {
        if (-not (Test-Path -LiteralPath $OcMarker) -and (Test-Path -LiteralPath $OcWrapperPath)) {
            if (Test-Path -LiteralPath $OcRealPath) { throw "refusing to overwrite $OcRealPath" }
            Move-Item -LiteralPath $OcWrapperPath -Destination $OcRealPath
            if ($OcSource -eq $OcWrapperPath) { $OcSource = $OcRealPath }
        } elseif (Test-Path -LiteralPath $OcWrapperPath) {
            Move-Item -LiteralPath $OcWrapperPath -Destination "$OcWrapperPath.retired-$([Guid]::NewGuid().ToString('N'))"
        }
        Move-Item -LiteralPath (Join-Path $TemporaryRoot "opencode-wrapper-$Version-windows-$Architecture.exe") -Destination $OcWrapperPath
        Set-Content -LiteralPath $OcNativePath -Value $OcSource -NoNewline
        New-Item -ItemType File -Path $OcMarker -Force | Out-Null
        Write-Output 'OpenCode launcher automatically installed'
    }
    if (($env:Path -split ';') -notcontains $BinDir) {
        Write-Output "RA2A launcher directory is not on PATH: add $BinDir before other harness binaries when opening a new terminal."
    }
    Write-Output "RA2A $Version installed"
    Write-Output "binary: $BinaryPath"
    if ($Pin) {
        if (-not $Name) { $Name = $NodeId }
        if (-not $CodexSource -and -not $OcSource) { throw 'No supported harness found; install Codex or OpenCode before setup' }
        $SetupArgs = @('setup', '--pin', $Pin, '--node-id', $NodeId, '--name', $Name)
        if ($CodexSource) { $SetupArgs += @('--codex', $CodexSource) }
        if ($OcSource) { $SetupArgs += @('--opencode', $OcSource) }
        & $BinaryPath @SetupArgs
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
