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

## 可行性状态

依据 OpenAI 官方文档，Codex App 可安装本地 MCP，App Server 也提供 thread 枚举、恢复、状态读取和启动新回合所需的方法，因此接收端主链路具备协议基础。

目前整体判定为 **条件可行**：当前 macOS Codex App 自带版本的实机探针已观察到 MCP 调用携带匹配目标的 thread ID，独立 App Server 也能读取共享会话存储并启动临时回合；但官方文档未承诺这些内部元数据稳定，也未承诺外部启动的持久化回合会与正在运行的桌面 App UI 实时同步。进入完整实现前仍需完成可见持久化 session 的 Go/No-Go 探针。详细证据和验收门槛见 [PRD 的官方文档可行性判定](prd/2026-08-31-ra2a-minimal-model.md#13-官方文档可行性判定)。

局域网读取闭环已经可运行：RA2A 启动本机 Codex App Server，通过 mDNS/DNS-SD 发现本机节点，再使用共享 PIN 经 CoAP/DTLS 调用 `/v1/sessions`。该接口现在返回本机真实的未归档 Codex thread 摘要。

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

默认从 `PATH` 启动 `codex app-server`。也可以显式指定 Codex App 内置版本：

```bash
go run ./cmd/ra2a selftest \
  --pin A2B3C4 \
  --id local-app \
  --codex /Applications/ChatGPT.app/Contents/Resources/codex
```

本机实测全局 Codex 0.146.0 和 Codex App 内置 0.151.0-alpha.7.2 均返回 60 个 thread。RA2A 使用只读的 `thread/list` 和 `useStateDbOnly=true`；本阶段没有向任何既有 session 写入消息。

当前成功只证明独立 App Server 能读取 Codex App 共享的会话存储。它没有附着正在运行的桌面 App Server 进程，也尚未证明外部 `turn/start` 会在桌面 App UI 中实时出现。

当前裁剪构建的开发期实测值：

| 目标 | 二进制大小 |
|---|---:|
| macOS arm64 | 6,717,106 B |
| Linux amd64 | 7,106,722 B |
| Windows amd64 | 7,287,296 B |

macOS arm64 前台常驻实测中，RA2A 自身空闲 RSS 为 12,704 KiB，它启动的 Codex App Server 子进程为 55,904 KiB；一次读取 60 个 thread 的自检进程树观测峰值为 94,470,144 B。Codex 子进程是当前主要内存开销，后续需验证能否安全复用桌面 App 的本地端点。以上均为开发期单次测量，不代表长期稳定值。

## 安装

项目目前处于早期开发阶段，已有可从源码运行的 LAN Node，但尚无可安装发布版本。为避免误导，此处不提供尚不能执行的占位安装命令。

首个可运行版本必须同时提供：

- macOS/Linux：`install.sh`
- Windows：`install.ps1`

安装脚本将支持无交互和幂等执行，并输出明确的安装结果。交付脚本时，本节会同步补齐复制即可执行的安装、验证、升级、卸载及故障恢复命令。
