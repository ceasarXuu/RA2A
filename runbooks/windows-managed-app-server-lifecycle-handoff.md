# Windows managed App Server 生命周期修复验收交接

## 目标

在 Windows/rog306 原生环境验收 commit `89e2555` 的进程管理修复，确认：

1. RA2A 不接管已有的 Codex App Server socket。
2. 每次 daemon 启动使用独立 socket，并生成 PID/Socket owner lease。
3. `ra2a stop`、`ra2a restart` 不留下 RA2A managed Codex 进程。
4. daemon 被强制终止后，计划任务自愈时能回收旧 managed 进程。
5. Codex Desktop 不可用时返回 `DESKTOP_OWNER_UNAVAILABLE`，不创建第二个 writer。

本次只做安装和验收，不修改源码、不创建分支、不发布版本。当前提交的版本字符串仍为 `v0.0.11`，以 Git commit 为准。

## 固定基线

```text
repository: https://github.com/ceasarXuu/RA2A.git
branch: main
required commit: 89e2555
change: reclaim orphaned managed App Servers and remove managed-writer fallback
```

## 1. 拉取并安装

保留开始前已有或来源不明的修改。如果工作区不干净且会影响安装，停止并回传 `git status --short`。

```powershell
Set-Location E:\RA2A
git status --short
git branch --show-current
git pull --ff-only
git merge-base --is-ancestor 89e2555 HEAD
if ($LASTEXITCODE -ne 0) { throw 'managed App Server lifecycle fix is missing' }
git log -1 --oneline
go version
go test ./...
go vet ./...
```

使用源码安装，不能用旧的 latest Release 替代：

```powershell
powershell -ExecutionPolicy Bypass -File .\install.ps1
$Ra2a = Join-Path ([Environment]::GetFolderPath('UserProfile')) '.local\bin\ra2a.exe'
& $Ra2a version
& $Ra2a restart
```

无参数执行 `install.ps1` 会保留现有 node ID、名称、PIN 和 Codex 路径。不要在本次验收中重新执行 `setup`，除非原配置确实不存在。

## 2. 记录服务和 owner lease

以下命令只读收集现场，不要结束任何 `codex.exe`：

```powershell
$UserHome = [Environment]::GetFolderPath('UserProfile')
$CodexRoot = if ($env:CODEX_HOME) { $env:CODEX_HOME } else { Join-Path $UserHome '.codex' }
$ControlDir = Join-Path $CodexRoot 'app-server-control'
$OwnerPath = Join-Path $ControlDir 'app-server-control.sock.ra2a-owner.json'

Get-ScheduledTask -TaskName RA2A | Select-Object TaskName, State
Get-ScheduledTaskInfo -TaskName RA2A | Select-Object LastRunTime, LastTaskResult
Get-CimInstance Win32_Process -Filter "Name='ra2a.exe'" |
  Select-Object ProcessId, ParentProcessId, ExecutablePath, CommandLine
Get-CimInstance Win32_Process -Filter "Name='codex.exe'" |
  Where-Object { $_.CommandLine -match 'app-server.*--listen' } |
  Select-Object ProcessId, ParentProcessId, ExecutablePath, CommandLine
if (Test-Path -LiteralPath $OwnerPath) { Get-Content -LiteralPath $OwnerPath -Raw }
Get-ChildItem -LiteralPath $ControlDir -Force -ErrorAction SilentlyContinue |
  Select-Object Name, Length, LastWriteTime
```

正确状态：

- 只有一个 `ra2a.exe daemon`；
- 至少有一个 RA2A managed `codex.exe app-server`，其命令行包含 `.ra2a-<daemon-pid>.sock`；
- `owner.json` 的 `pid` 等于 managed App Server PID，`socketPath` 与命令行一致；
- 官方 Desktop 的 App Server 命令行通常不含 `.ra2a-<pid>.sock`，不要将其当作 RA2A managed 进程。

## 3. stop/restart 回收验证

### 3.1 stop

先记录当前 managed PID，然后执行：

```powershell
$BeforeManaged = Get-CimInstance Win32_Process -Filter "Name='codex.exe'" |
  Where-Object { $_.CommandLine -match 'app-server.*\.ra2a-\d+\.sock' } |
  Select-Object -First 1
& $Ra2a stop
Start-Sleep -Seconds 2

Get-CimInstance Win32_Process -Filter "Name='ra2a.exe'" |
  Select-Object ProcessId, ParentProcessId, CommandLine
$AfterManaged = Get-CimInstance Win32_Process -Filter "Name='codex.exe'" |
  Where-Object { $_.CommandLine -match 'app-server.*\.ra2a-\d+\.sock' }
if ($AfterManaged) { throw 'RA2A managed App Server survived stop' }
if (Test-Path -LiteralPath $OwnerPath) { throw 'owner lease survived stop' }
```

官方 Codex Desktop App Server 可以继续运行；本检查只要求 RA2A 自己创建的 managed 进程消失。

### 3.2 restart

```powershell
& $Ra2a restart
Start-Sleep -Seconds 3
$Daemon = Get-CimInstance Win32_Process -Filter "Name='ra2a.exe'" |
  Where-Object { $_.CommandLine -match '\sdaemon(\s|$)' } |
  Select-Object -First 1
$Managed = Get-CimInstance Win32_Process -Filter "Name='codex.exe'" |
  Where-Object { $_.CommandLine -match 'app-server.*\.ra2a-\d+\.sock' } |
  Select-Object -First 1
if (-not $Daemon -or -not $Managed) { throw 'RA2A daemon or managed App Server did not restart' }
$Lease = Get-Content -LiteralPath $OwnerPath -Raw | ConvertFrom-Json
if ([int]$Lease.pid -ne [int]$Managed.ProcessId) { throw 'owner PID does not match managed App Server' }
if ($Lease.socketPath -notmatch '\.ra2a-\d+\.sock$') { throw 'socket is not per-launch isolated' }
```

## 4. 强杀 daemon 后的自愈验证

不要使用 `Stop-Process -Name codex` 或 `taskkill /IM codex.exe`，它们会误伤官方 Desktop。只强杀明确的 RA2A daemon PID：

```powershell
$CrashDaemon = [int]$Daemon.ProcessId
$CrashManaged = [int]$Managed.ProcessId
Stop-Process -Id $CrashDaemon -Force

$Deadline = (Get-Date).AddSeconds(90)
$Recovered = $null
do {
  Start-Sleep -Seconds 2
  $Recovered = Get-CimInstance Win32_Process -Filter "Name='ra2a.exe'" |
    Where-Object { $_.CommandLine -match '\sdaemon(\s|$)' -and [int]$_.ProcessId -ne $CrashDaemon } |
    Select-Object -First 1
} while (-not $Recovered -and (Get-Date) -lt $Deadline)
if (-not $Recovered) { throw 'RA2A scheduled task did not recover within 90 seconds' }

$RecoveredManaged = Get-CimInstance Win32_Process -Filter "Name='codex.exe'" |
  Where-Object { $_.CommandLine -match 'app-server.*\.ra2a-\d+\.sock' } |
  Select-Object -First 1
if (-not $RecoveredManaged) { throw 'managed App Server did not recover' }
if (Get-Process -Id $CrashManaged -ErrorAction SilentlyContinue) {
  throw 'old managed App Server survived daemon crash recovery'
}
$RecoveredLease = Get-Content -LiteralPath $OwnerPath -Raw | ConvertFrom-Json
if ([int]$RecoveredLease.pid -ne [int]$RecoveredManaged.ProcessId) {
  throw 'recovered owner lease points to the wrong process'
}
```

若计划任务没有在 90 秒内恢复，回传 `Get-ScheduledTaskInfo -TaskName RA2A | Format-List *`，不要手动启动第二个 daemon。

## 5. Desktop owner 投递门禁

### 5.1 Desktop 正常运行

1. 在官方 Codex Desktop 打开专用测试 session，确认可手动输入。
2. 从 Mac 或另一台节点向该 session 发送一条带唯一 marker 的 RA2A 消息。
3. 确认消息在同一 Desktop UI 中实时出现，完成后仍可手动续聊。
4. 记录发送前后的 managed PID；不应出现第二个带 `.ra2a-<pid>.sock` 的 managed App Server。

### 5.2 Desktop 不可用

关闭或退出官方 Codex Desktop 后，从另一台节点发送一条唯一测试消息。期望：

- MCP/Agent 收到 `DESKTOP_OWNER_UNAVAILABLE`，并提示启动 Codex Desktop 后重试；
- 不产生第二个 RA2A managed App Server；
- 现有 owner lease 不被替换，不发生静默 writer 抢占。

不要为了验证失败路径而重试同一条消息；记录原始错误后重新启动 Desktop，再发送新的 marker 验证恢复路径。

## 6. 回传模板

```text
Windows version / architecture:
Codex Desktop version / PID:
RA2A node ID / name:
git HEAD:
required commit is ancestor: yes/no
ra2a version:
scheduled task state:
daemon PID before stop/restart:
managed App Server PID before stop/restart:
owner lease path:
owner PID matches managed PID: yes/no
owner socket path:
socket is per-launch isolated: yes/no
stop removed RA2A daemon/managed process: yes/no
restart created fresh owner lease/socket: yes/no
forced daemon PID:
old managed PID removed after recovery: yes/no
recovered daemon PID:
recovered managed PID:
Desktop owner message visible live: yes/no
manual continuation succeeded: yes/no
Desktop-unavailable result contains DESKTOP_OWNER_UNAVAILABLE: yes/no
second managed writer created: yes/no
result: PASS/FAIL
```

失败时附上以下只读输出，并保留错误原文；不要回传 PIN：

```powershell
git status --short
git rev-parse HEAD
& $Ra2a version
Get-ScheduledTaskInfo -TaskName RA2A | Format-List *
Get-CimInstance Win32_Process -Filter "Name='ra2a.exe'" | Select-Object ProcessId,ParentProcessId,ExecutablePath,CommandLine
Get-CimInstance Win32_Process -Filter "Name='codex.exe'" | Where-Object { $_.CommandLine -match 'app-server.*--listen' } | Select-Object ProcessId,ParentProcessId,ExecutablePath,CommandLine
if (Test-Path -LiteralPath $OwnerPath) { Get-Content -LiteralPath $OwnerPath -Raw }
$Config = Get-Content (Join-Path $UserHome '.config\ra2a\config.json') | ConvertFrom-Json
[pscustomobject]@{ nodeId=$Config.nodeId; name=$Config.name; pinConfigured=([string]$Config.pin).Length -eq 6; codex=$Config.codex }
```

## 不要做

- 不要 `taskkill /IM codex.exe /F`，不得误杀官方 Desktop App Server。
- 不要删除 rollout、session、writer lock 或 owner lease 伪造结果。
- 不要用第二个 App Server 代替 Desktop owner 投递。
- 不要在 `DELIVERY_UNKNOWN` 或 Desktop 不可用后重复发送同一条消息。
- 不要以 `v0.0.11` 版本字符串替代 `89e2555` 的源码祖先校验。
