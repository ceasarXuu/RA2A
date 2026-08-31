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

RA2A 采用单宿主模型：优先连接官方 canonical Unix control socket；若宿主尚未运行，则启动 `codex app-server --listen unix://<socket>` 并监管其生命周期。RA2A 和 Codex 客户端连接同一个 App Server，因此一个 thread 只有一个 writer 宿主。

```text
Codex App / codex --remote ─┐
                            ├── managed Codex App Server（唯一 writer）
RA2A daemon ────────────────┘                │
                                             └── sessions
```

2026-09-01 的 macOS 实机验证已经跑通：两个 RA2A 节点经 mDNS → DTLS → CoAP `/v1/messages` 投递到受管 session，连接同一宿主的官方 Codex 客户端实时显示消息并回复 `RA2A_MANAGED_HOST_OK`。

重要边界：普通 Desktop 本地模式使用私有 stdio App Server，不属于 V1 可写目标。`GET /v1/sessions` 可能从共享存储列出这类 thread，但这不代表它可写；尝试恢复时会明确返回 `active writer` 冲突。V1 只承诺由 RA2A 受管 App Server 创建或加载、且 Codex App 通过 Remote/SSH 连接使用的 session；不使用未公开的 Desktop 内部投递接口。

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
| macOS arm64 | 7,967,234 B |
| Linux amd64 | 8,466,594 B |
| Windows amd64 | 8,674,304 B |

此前独立 stdio App Server 的资源测量已经过时；受管 Unix 宿主的新资源基线将在安装阶段重新测量。Codex App Server 仍是进程树的主要内存开销，但多个客户端共享同一宿主，不再为 RA2A 创建第二个 writer 实例。

## 安装

项目目前处于早期开发阶段，已有可从源码运行的 LAN Node，但尚无可安装发布版本。为避免误导，此处不提供尚不能执行的占位安装命令。

首个可运行版本必须同时提供：

- macOS/Linux：`install.sh`
- Windows：`install.ps1`

安装脚本将支持无交互和幂等执行，并输出明确的安装结果。交付脚本时，本节会同步补齐复制即可执行的安装、验证、升级、卸载及故障恢复命令。
