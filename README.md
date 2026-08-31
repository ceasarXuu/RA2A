# RA2A

RA2A 是一个运行在局域网内的 MCP，目标是让多台设备上的多个 Codex Agent 能够互相发现，并向指定 Codex session 定向发送消息。

第一版仅面向 **Codex App**（OpenAI 桌面 App 中的 Codex 使用界面），不承诺兼容 Codex CLI、IDE 扩展、云任务或其他 MCP Host。

当前处于最小可行性实验阶段。极简模型与实验结果见：

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

## 安装

项目目前仍处于可行性实验阶段，尚无可安装版本。为避免误导，此处不提供尚不能执行的占位命令。

首个可运行版本必须同时提供：

- macOS/Linux：`install.sh`
- Windows：`install.ps1`

安装脚本将支持无交互和幂等执行，并输出明确的安装结果。交付脚本时，本节会同步补齐复制即可执行的安装、验证、升级、卸载及故障恢复命令。
