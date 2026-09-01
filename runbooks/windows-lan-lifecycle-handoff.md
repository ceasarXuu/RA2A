# Windows 局域网发现生命周期验收交接

## 目标

在 rog306 原生验证提交 `94d7d26`：

1. 节点退出或不可见后，其他设备会删除 ghost peer。
2. 节点在相同 IP/端口恢复时，发送方能够重新进行 DTLS 握手。
3. 节点重启后端口变化时，发送方更新为新端点。

本次不修改 Desktop IPC、session 注入或 PIN 配置。

## 安装当前提交

在现有 RA2A 仓库的 PowerShell 中执行：

```powershell
git pull --ff-only
git merge-base --is-ancestor 94d7d263b79315a8fd2bdd528ce102bdea160180 HEAD
if ($LASTEXITCODE -ne 0) { throw 'LAN lifecycle fix is missing' }
powershell -ExecutionPolicy Bypass -File .\install.ps1
ra2a restart
ra2a version
Get-ScheduledTask -TaskName RA2A | Select-Object TaskName, State
Get-CimInstance Win32_Process -Filter "Name='ra2a.exe'" | Select-Object ProcessId, CommandLine
```

该修复自 `v0.0.7` 起进入正式 Release，`ra2a version` 应显示 `v0.0.7`；提交 `94d7d26` 仍作为修复祖先校验点。无参数运行 `install.ps1` 会保留现有 name、node ID、PIN 和 Codex 路径。

## 双机验收

1. rog306 启动后，在 Mac 确认只出现一个规范节点 ID，不再同时残留 `ROG306` 与 `rog306`。
2. 从 Mac 向 rog306 的专用测试 session 发送一条唯一消息，确认成功。
3. 在 Windows 执行 `ra2a restart`，随后从 Mac 再发送一条唯一消息；允许短暂发现窗口，但不应长期 `TARGET_UNREACHABLE`。
4. 停止 Windows RA2A 计划任务，Mac 应在 goodbye 后立即或最迟 30 秒移除该 peer：

```powershell
Stop-ScheduledTask -TaskName RA2A
```

5. 再次启动计划任务，Mac 应重新发现并恢复发送：

```powershell
Start-ScheduledTask -TaskName RA2A
```

## 通过标准

- Mac 目标列表没有离线 ghost peer 或大小写重复旧实例。
- Windows 重启前后都能接收消息。
- 相同端点恢复和端口变化恢复都不要求重启 Mac daemon。
- 消息只注入一次，没有 `DELIVERY_UNKNOWN` 或重复 turn。

## 失败时回传

请回传以下内容，不要先改代码：

```powershell
git rev-parse HEAD
ra2a version
Get-ScheduledTask -TaskName RA2A | Format-List *
Get-ScheduledTaskInfo -TaskName RA2A | Format-List *
Get-CimInstance Win32_Process -Filter "Name='ra2a.exe'" | Select-Object ProcessId, ExecutablePath, CommandLine
$c = Get-Content "$env:LOCALAPPDATA\RA2A\config.json" | ConvertFrom-Json
[pscustomobject]@{ nodeId=$c.nodeId; name=$c.name; pinConfigured=([string]$c.pin).Length -eq 6; codex=$c.codex }
```

保留 nodeId、name、进程路径和错误原文；不要回传 PIN 原文。
