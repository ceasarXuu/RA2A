# RA2A

RA2A 是一个运行在局域网内的 MCP，目标是让多台设备上的多个 Codex Agent 能够互相发现，并向指定 Codex session 定向发送消息。

第一版仅面向 **Codex App**（OpenAI 桌面 App 中的 Codex 使用界面），不承诺兼容 Codex CLI、IDE 扩展、云任务或其他 MCP Host。

当前已进入最小实现阶段。极简模型与实验结果见：

- [RA2A 局域网 Agent 通信（极简闭环）](prd/2026-08-31-ra2a-minimal-model.md)
- [Codex App 最小可行性实验](experiments/README.md)
- [局域网发现与凭证握手开源方案实验](experiments/network/README.md)

## 设计约束

- 轻量、低依赖，优先交付单一可执行程序
- 网络波动后自动重新发现和连接，无需人工重启
- 每个用户运行一个 RA2A daemon，由操作系统负责登录启动和崩溃重启
- 支持 macOS、Linux、Windows，核心协议和行为一致
- 安装、验证和故障信息同时对人和 Agent 友好

## 当前范围

- 局域网节点自动发现
- Codex session 列举与寻址
- Agent 到指定 session 的文本消息投递
- 来源地址保留与双向回复

暂不包含中心服务、离线消息、持久队列、工作流编排和公网通信。

第一版使用一个长期共享的 6 位 PIN 形成信任组。首台设备生成 PIN，其他设备通过安装参数配置相同 PIN；daemon 将 PIN 原样作为 DTLS-PSK，相同 PIN 可以握手，不同 PIN 拒绝握手。第一版不做 KDF、证书、PAKE、轮换或锁定；该凭证只用于先跑通闭环，不承诺抵抗猜测、窃取或恶意局域网攻击。

## 当前架构与可行性

RA2A 采用单宿主模型：优先连接官方 canonical Unix control socket；若宿主尚未运行，则启动 `codex app-server --listen unix://<socket>` 并监管其生命周期。安装器把同一个 RA2A 二进制注册为 Codex stdio MCP；每次工具调用经 `127.0.0.1:47321` 访问常驻 daemon，不重复启动 LAN 节点或 App Server。

```text
Codex Agent ── stdio ──► ra2a mcp ── loopback ──► RA2A daemon
                                                   │
                         mDNS + CoAP/DTLS ◄────────┤
                                                   │
Codex App / remote client ──► managed App Server ──┘
```

2026-09-01 的 macOS 实机验证已经跑通：正式 MCP `send_message` 经 loopback → mDNS → DTLS → CoAP `/v1/messages` 投递到受管 session，连接同一宿主的官方 Codex 客户端实时显示来源、message ID 和正文，并回复 `RA2A_MCP_COMPLETE_OK`。

重要边界：普通 Desktop 本地模式使用私有 stdio App Server，不属于 V1 可写目标。`GET /v1/sessions` 可能从共享存储列出这类 thread，但这不代表它可写；尝试恢复时会明确返回 `active writer` 冲突。V1 只承诺由 RA2A 受管 App Server 创建或加载、且 Codex App 通过 Remote/SSH 连接使用的 session；不使用未公开的 Desktop 内部投递接口。

## Agent 工具

安装后 Codex 自动获得且仅获得两个正式工具：

- `list_targets()`：返回 mDNS 已发现的 RA2A 节点、节点可用状态，以及各节点未归档 session 的 `id`、标题和状态。节点状态为 `ready`、`degraded` 或 `unreachable`；`degraded` 会保留最近成功结果并设置 `sessionsStale=true`，网络波动不再让节点从列表中消失。
- `send_message(to, text)`：向 `ra2a://<node-id>/<session-id>` 创建一个远端 Codex 回合。工具从 MCP `_meta.threadId` 取得调用 session，自动生成可回复的 `from` 地址和唯一 message ID。

`accepted` 只表示远端已创建回合。忙碌 session 返回 `SESSION_BUSY`；结果未知的超时返回 `DELIVERY_UNKNOWN` 且不会自动重发；调用上下文缺少 thread ID 时返回 `CALLER_SESSION_UNKNOWN`，不会伪造来源。

## 本地开发验证

需要 Go 1.24 或更高版本。在项目根目录执行：

```bash
go run ./cmd/ra2a selftest --pin A2B3C4 --id local-dev --name "Local Dev"
```

成功输出示例：

```text
discovered=ra2a://local-dev endpoint=127.0.0.1:<port>
sessions=<本机未归档 thread 数>
selftest=ok
```

启动常驻前台服务：

```bash
go run ./cmd/ra2a serve --pin A2B3C4 --id local-dev --name "Local Dev"
```

`Ctrl-C` 可正常停止。节点通过 `_ra2a._udp.local` 广播；同一条正式 LAN Node 代码同时用于 `serve` 和 `selftest`。

直接检查正式 MCP 协议（daemon 运行时）：

```bash
printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}' \
  | go run ./cmd/ra2a mcp
```

向指定节点的受管 session 发送消息：

```bash
go run ./cmd/ra2a send \
  --pin A2B3C4 \
  --id sender \
  --peer receiver \
  --session <thread-id> \
  --message "请检查当前任务状态"
```

发送端输出 `delivered=ra2a://receiver/<thread-id>` 只表示远端已创建新回合，不表示 Agent 已完成任务。

默认从 `PATH` 启动 `codex app-server`。也可以显式指定 Codex App 内置版本：

```bash
go run ./cmd/ra2a selftest \
  --pin A2B3C4 \
  --id local-app \
  --codex /Applications/ChatGPT.app/Contents/Resources/codex
```

本机最近一次受管宿主自检返回 61 个未归档 thread；数量会随本地会话变化。RA2A 使用 `thread/list` 和 `useStateDbOnly=true` 枚举摘要，并已通过 `thread/resume` + `turn/start` 向受管 session 完成真实消息注入。列表中的普通 Desktop 本地 thread 仍不保证可写。

默认 control socket 为 `$CODEX_HOME/app-server-control/app-server-control.sock`，未设置 `CODEX_HOME` 时使用 `~/.codex/app-server-control/app-server-control.sock`。可用 `--app-server-socket` 覆盖。官方客户端可使用 `codex --remote unix://` 连接；Codex App 应通过 Remote/SSH 使用同一宿主，而不是普通本地 session。

当前裁剪构建的开发期实测值：

| 目标 | 二进制大小 |
|---|---:|
| macOS arm64 | 9,020,274 B |
| Linux amd64 | 9,523,362 B |
| Windows amd64 | 9,764,864 B |

此前独立 stdio App Server 的资源测量已经过时；受管 Unix 宿主的新资源基线将在安装阶段重新测量。Codex App Server 仍是进程树的主要内存开销，但多个客户端共享同一宿主，不再为 RA2A 创建第二个 writer 实例。

## 安装

正式安装直接使用 GitHub latest Release 的预编译文件，自动选择系统和架构并校验 SHA-256。目标设备只需安装 Codex App；macOS/Linux 需要 `curl`，Windows 使用 PowerShell。无需 Git、无需 Go、无需源码目录，也不需要 root/管理员权限。

macOS / Linux：

```bash
curl -fsSL https://github.com/ceasarXuu/RA2A/releases/latest/download/install-ra2a.sh | sh
export PATH="$HOME/.local/bin:$PATH"
ra2a
```

`ra2a` 会要求输入本机名称（直接回车使用主机名），生成并显示一个长期 6 位 PIN，确认 `status: running` 后立即退出，不会占用终端。请保存该 PIN；其他设备执行安装后，用 `ra2a pin <PIN>` 加入同一信任组。

Agent 或自动化可以一次性无交互安装并启动：

```bash
curl -fsSL https://github.com/ceasarXuu/RA2A/releases/latest/download/install-ra2a.sh \
  | sh -s -- \
  --pin A2B3C4 \
  --node-id device-b \
  --name "Device B" \
  --codex /Applications/ChatGPT.app/Contents/Resources/codex
```

如果 `codex` 不在 `PATH`，首次引导会自动检查 macOS Codex App 内置路径；无交互安装需通过 `--codex` 明确指定。

Windows PowerShell：

```powershell
irm https://github.com/ceasarXuu/RA2A/releases/latest/download/install-ra2a.ps1 | iex
$env:Path = "$env:LOCALAPPDATA\RA2A\bin;$env:Path"
ra2a
```

Windows 无交互安装：

```powershell
$Installer = [scriptblock]::Create((irm https://github.com/ceasarXuu/RA2A/releases/latest/download/install-ra2a.ps1))
& $Installer -Pin A2B3C4 -NodeId device-b -Name "Device B" -Codex C:\absolute\path\to\codex.exe
```

首次引导或无交互安装成功必须以 `status: running` 结束。可用 `codex mcp get ra2a` 检查 MCP 注册。服务管理方式如下：

| 平台 | 用户级保活机制 | 状态检查 |
|---|---|---|
| macOS | LaunchAgent `com.ra2a.daemon` | `launchctl print gui/$(id -u)/com.ra2a.daemon` |
| Linux | systemd user unit `ra2a.service` | `systemctl --user status ra2a.service` |
| Windows | 当前用户计划任务 `RA2A` | `Get-ScheduledTask -TaskName RA2A` |

macOS 日志位于 `~/.config/ra2a/logs/`；Linux 可使用 `journalctl --user -u ra2a.service`。安装失败时，先确认下载工具、Codex 路径和对应用户级服务管理器可用，再原样重跑安装命令。校验或下载失败不会覆盖已有二进制。

## 日常命令

| 命令 | 行为 |
|---|---|
| `ra2a` | 首次运行进入引导；后续只确认服务正在运行并退出 |
| `ra2a restart` | 重启后台服务 |
| `ra2a name [名称]` | 设置设备名称；省略参数时交互输入 |
| `ra2a pin [6位PIN]` | 设置长期共享 PIN；省略参数时交互输入 |
| `ra2a version` | 输出当前版本，当前为 `v0.0.3` |
| `ra2a update` | 从 GitHub 最新正式 Release 下载当前平台产物，校验 SHA-256 后更新并重启 |

正常升级直接执行：

```bash
ra2a update
```

名称、PIN 和节点 ID 在安装与升级时保持不变。开发者如需从源码安装，需要 Git 与 Go 1.24+：

```bash
git clone https://github.com/ceasarXuu/RA2A.git
cd RA2A
./install.sh
```

源码安装的卸载命令：

```bash
./install.sh --uninstall
```

```powershell
powershell -ExecutionPolicy Bypass -File .\install.ps1 -Uninstall
```

## GitHub Release 发版

GitHub Release 是正式发版主流程。程序版本由 `ra2a version` 输出；推送同名 `v*` tag 后，[Release 工作流](.github/workflows/release.yml) 会先校验 tag 与程序版本完全一致，再构建并发布 macOS、Linux、Windows 的 amd64/arm64 产物和逐文件 SHA-256 校验。版本不一致或任一构建失败时不会发布。

维护者发布当前版本的标准命令：

```bash
git tag v0.0.3
git push origin v0.0.3
```

不要重复使用或移动已发布 tag；下一版应先修改程序版本并通过测试，再创建对应的新 tag。`ra2a update` 只消费 GitHub 的 latest 正式 Release，不使用草稿或预发布版本。

Windows 安装脚本和 Windows socket URL 已完成静态契约及交叉编译验证，但尚未在真实 Windows Codex App 环境完成端到端验收；当前已实机通过的平台仍是 macOS。
