# PRD：RA2A 局域网 Agent 通信（极简闭环）

- 状态：Draft
- 创建日期：2026-08-31
- 更新日期：2026-08-31
- 发起人：用户
- 原始诉求：让局域网内多台设备上的多个 Codex Agent 能发现彼此，并向指定 session 发送消息。
- 产品权威：`Confirmed Product Decisions` 章节

## 请求方评审摘要

- 已确认：局域网运行、多设备安装 Codex App 与本 MCP、自动发现、定向向指定 session 注入消息；服务轻量、低依赖、自愈，并支持 macOS、Linux、Windows 与 Agent 友好安装。
- 建议的最小闭环：单一信任组、仅处理在线且空闲的 session、无离线队列、回复也是一条新消息。
- 实施前必须确认：信任边界、忙碌 session 的处理、session 暴露范围、平台版本与架构基线、轻量资源预算。
- 当前状态原因：目标已清楚，但上述事项会直接改变安全性、兼容范围和发布门槛。

## 1. 一句话模型

RA2A 是一个局域网内的“定向消息投递器”：Agent 发现可达 session，向其中发送一段文本，接收 session 将它作为新的 Codex 用户回合开始执行。

RA2A 不是任务平台、消息队列或多 Agent 编排框架。

## 2. 目标与成功标准

### 目标

在两台或多台运行 Codex App 的局域网设备之间，完成以下闭环：

1. Agent A 能列出设备 B 上可达的 Codex session。
2. Agent A 能向指定 session B 发送文本消息。
3. session B 能看到来源地址和正文，并启动一个 Codex 回合处理它。
4. Agent B 如需回应，可向来源地址再发送一条消息。

### 成功标准

两台设备无需中心服务器，仅共享最少配置，即可完成一次 A → B → A 的消息往返；短暂网络中断恢复后，无需重启或重新配置即可再次通信。

## 3. 极简领域模型

系统只有三个对象：

| 对象 | 最小字段 | 含义 |
|---|---|---|
| Node | `id`、`name`、`endpoint` | 一台运行 RA2A 的设备 |
| Session | `id`、`title`、`status` | 一个 Codex thread；对外统一称 session |
| Message | `id`、`from`、`to`、`text` | 一次定向文本投递 |

地址统一表示为：

```text
ra2a://<node-id>/<session-id>
```

不新增 Agent 实体。第一版中，一个 session 就是一个可寻址 Agent。

## 4. 极简系统结构

每台设备只运行一个逻辑 RA2A Node：

```text
Codex Session A
      │ MCP tool call
      ▼
RA2A Node A ── LAN HTTP ──► RA2A Node B
      ▲                         │
      │ mDNS discovery          │ Codex App Server
      └─────────────────────────▼
                            Codex Session B
```

RA2A Node 只有三个职责：

1. 对本机 Codex 暴露 MCP 工具。
2. 发现局域网内其他 RA2A Node，并收发消息。
3. 通过本机 Codex App Server 列出 session、恢复 session、启动新回合。

不设置中心节点。所有节点对等。

核心服务必须保持轻量：不引入数据库，不依赖常驻的第三方基础服务，优先交付单一可执行程序。除 Codex 本身外，应尽量避免要求用户预装语言运行时或包管理器。

## 5. 极简交互面

### MCP 工具

仅提供两个工具：

```text
list_targets() -> Node[] + Session[]
send_message(to, text) -> accepted | error
```

- `list_targets` 返回当前已验证且在线的节点及 session。
- `send_message` 从 MCP 调用元数据中取得当前调用者 session，自动形成 `from` 地址；调用者只需要提供 `to` 和 `text`。

### 节点间接口

仅提供两个接口：

```text
GET  /v1/sessions
POST /v1/messages
```

- `GET /v1/sessions` 返回本机可达 session 的最小摘要。
- `POST /v1/messages` 验证请求并向目标 session 启动一个 Codex 回合。
- 节点发现使用 mDNS 服务 `_ra2a._tcp.local`，广播内容只包含节点 ID、名称、端口和协议版本。

## 6. 消息语义

发送到 Codex 的实际文本必须保留来源：

```text
[RA2A message]
from: ra2a://node-a/session-a
message-id: <uuid>

<message text>
```

接收规则：

1. 目标节点不存在或鉴权失败：拒绝。
2. 目标 session 不存在、已归档或不可加载：拒绝。
3. 目标 session 空闲：恢复该 session，并通过 `turn/start` 启动新回合。
4. 目标 session 忙碌：第一版建议返回 `SESSION_BUSY`，不排队、不插入正在执行的回合。
5. 接收端成功创建回合后返回 `accepted`；这只代表已投递，不代表任务已完成。
6. 处理结果不通过原 HTTP 请求等待返回。接收 Agent 需要回复时，调用 `send_message` 向 `from` 地址发送新消息。

## 7. 最小状态模型

### Node

```text
unknown → online → reconnecting
             ▲          │
             └──────────┘
```

- mDNS 发现并鉴权成功后为 `online`。
- 广播过期或连接失败后进入 `reconnecting`，暂时从可选目标中移除。
- 服务持续重新发现和重连；网络恢复后自动回到 `online`，无需人工重启、重新安装或重新配置。
- 本机 Codex App Server 连接中断时遵循同样规则；恢复前报告明确的本地不可用状态。

### Message

```text
sending → accepted
        ↘ rejected
        ↘ unknown
```

- `accepted`：接收端已创建 Codex 回合。
- `rejected`：明确的鉴权、目标或忙碌错误。
- `unknown`：超时导致发送端无法判断是否已投递。

第一版不自动重试，避免超时后产生重复消息。

这里的“不自动重试”只约束结果未知的消息投递，不限制节点发现、健康检查和连接的自动恢复。

## 8. 安装与平台体验

第一版必须支持：

- macOS；
- Linux；
- Windows。

各平台协议、配置语义和 MCP 工具行为必须一致。平台差异仅允许存在于安装、服务托管和路径表达上。

安装体验要求：

- macOS/Linux 提供 `install.sh`，Windows 提供 `install.ps1`；
- 脚本支持无交互执行、重复执行和明确退出码，便于人和 Agent 调用；
- 脚本失败时说明失败原因、已完成的操作以及可执行的恢复步骤；
- 脚本不得把密钥写入日志，不得静默修改与 RA2A 无关的系统配置；
- README 必须给出复制即可执行的安装、验证、升级和卸载步骤，并明确平台前置条件；
- 若某个平台不得不增加额外依赖，README 必须显式说明，不得在安装过程中隐式拉取不相关运行时。

## 9. 最小安全模型

建议所有节点配置同一个随机共享密钥，形成一个完全互信的 RA2A 信任组：

- mDNS 发现可以公开，但 session 列表和消息接口必须鉴权。
- 持有共享密钥的节点可查看可暴露 session，并可向其发送消息。
- 密钥不通过 mDNS 或消息正文传播。
- 第一版不做账号、角色、逐设备授权和逐 session ACL。

共享密钥不是可省略的装饰：向 Codex session 注入文本可能触发文件、网络或终端操作，因此“同一局域网”不能直接等同于“可信”。

## 10. 明确不做

第一版不包含：

- 中心服务器或云服务；
- 数据库；
- 离线消息和持久队列；
- 自动重试、顺序保证、恰好一次投递；
- 群发、广播和群聊；
- 任务拆分、Agent 调度和工作流编排；
- 文件、图片或二进制附件；
- 消息历史 UI；
- 跨局域网、NAT 穿透或公网中继；
- 用户体系、复杂配对流程和细粒度权限；
- 自动等待远端任务完成。

## 11. 验收标准

1. Given 两台设备运行 RA2A 且共享密钥一致，when Agent A 调用 `list_targets`，then 能看到设备 B 及其可达 session。
2. Given session B 在线且空闲，when Agent A 向其地址调用 `send_message`，then session B 出现包含来源地址和正文的新用户回合并开始处理。
3. Given Agent B 收到消息，when 它向消息中的 `from` 地址发送回复，then 原 session A 收到新回合。
4. Given 两台设备密钥不同，when 任一方请求 session 列表或发送消息，then 请求被拒绝且不泄露 session 数据。
5. Given 目标 session 正在运行，when 收到新消息，then 返回明确的 `SESSION_BUSY`，且不修改当前回合。
6. Given 目标设备离线，when 广播过期，then 该节点不再出现在 `list_targets` 的可用目标中。
7. Given 发送请求超时，then 返回 `DELIVERY_UNKNOWN`，且发送端不自动重试。
8. Given 节点因短暂网络波动失联，when 网络恢复，then 节点自动重新发现并恢复通信，无需重启服务或修改配置。
9. Given 一台受支持平台的新设备已安装 Codex，when 用户或 Agent 按 README 执行对应安装脚本，then 能以无交互方式完成安装并得到可验证的成功或明确失败结果。
10. Given 同一版本分别运行在 macOS、Linux 和 Windows，when 执行发现与消息投递，then 对外协议和 MCP 工具行为一致。
11. Given 发布构建完成，then README 或发布说明记录产物大小、空闲内存和空闲 CPU 的实测值，使“轻量”可以持续比较而非仅凭主观判断。

## 12. Confirmed Product Decisions

> PROTECTED USER-AUTHORITY SECTION
> 本节中的行，未经用户对具体决策变更的明确批准，不得创建、修改、删除、重新解释或替代。Agent 不得自行批准。

| ID | Confirmed Decision | Must Do | Must Not Do | Rationale | Violation Signal | Confirmation | Status |
|---|---|---|---|---|---|---|---|
| PD1 | 产品运行在局域网内 | 节点在局域网内通信 | 第一版不得依赖公网中心服务 | 用户明确限定运行环境 | 基本通信必须访问公网服务 | user-confirmed-direct: “运行在局域网内的 mcp” | active |
| PD2 | 安装本 MCP 与 Codex App 的设备能够互相发现 | 提供节点发现能力 | 不要求用户手填每个对端地址才能完成基本发现 | 用户明确要求双方互相发现 | 两台同网设备默认不可见 | user-confirmed-direct: “双方能够通过这个 mcp 互相发现对方” | active |
| PD3 | 可以向指定 session 注入一条消息 | session 必须可寻址，消息必须定向投递 | 不得只支持设备级广播或不可选择目标 | 这是核心交互闭环 | 只能发给设备或随机 session | user-confirmed-direct: “向指定的 session 注入一条消息” | active |
| PD4 | 目标是让多设备、多 Agent 协作 | 保留消息来源和可回复地址 | 不得把产品退化为仅供人阅读的局域网聊天 | 回复寻址是最小协作闭环的一部分 | 接收 Agent 无法回复来源 Agent | user-confirmed-direct: “局域网内的多个设备的多个 agent 能互相协作起来” | active |
| PD5 | 服务要尽可能轻量 | 控制运行资源和交付体积，并公开实测结果 | 不得无证据引入重型运行时或常驻基础服务 | 局域网多端常驻需要低开销 | 空闲服务持续产生明显负载且无测量数据 | user-confirmed-direct: “该服务要尽可能的轻量” | active |
| PD6 | 服务具备网络自愈能力 | 网络恢复后自动重新发现和连接 | 不得要求用户因暂时断线重启或重新配置 | 网络波动不应造成永久失联 | 网络恢复后节点仍不可达直至人工干预 | user-confirmed-direct: “服务具备自愈能力避免网络波动导致的断线后不可恢复” | active |
| PD7 | 提供 Agent 友好的安装脚本和 README 安装说明 | 提供跨平台脚本、无交互模式、明确结果与恢复说明 | 不得只提供人工拼装步骤或隐式失败 | 人和 Agent 都应可靠完成部署 | README 命令不可复制执行，或脚本必须依赖人工问答 | user-confirmed-direct: “提供友好的安装脚本，并在 readme 中介绍安装方式，agent 友好” | active |
| PD8 | 尽量低依赖 | 优先单一产物并最小化外部运行依赖 | 不得为便利随意叠加语言运行时、数据库或基础服务 | 降低安装、维护和跨平台成本 | 基础启动需要多个与核心能力无关的服务 | user-confirmed-direct: “尽量低依赖” | active |
| PD9 | 支持 macOS、Linux、Windows 部署 | 三个平台保持一致协议和核心行为 | 不得把任一指定平台降为未支持状态 | 用户明确要求多端部署 | 发布版本缺少任一平台可用产物 | user-confirmed-direct: “支持多端部署， macos、linux、windows” | active |

## 13. 待确认决策与风险

### 实施前需用户确认

1. **信任边界**：是否接受“共享一个密钥的节点完全互信”？建议接受，这是不省略安全底线情况下最小的模型。
2. **忙碌处理**：目标 session 忙碌时返回 `SESSION_BUSY`，还是允许通过 `turn/steer` 干预当前回合？建议第一版返回忙碌。
3. **暴露范围**：是否向信任组暴露所有未归档 session 的 ID、标题和状态？建议第一版如此；若标题也敏感，则必须增加 session 显式加入机制。
4. **平台基线**：macOS、Linux、Windows 的最低版本以及必须发布的 CPU 架构尚未确认。
5. **轻量预算**：产物大小、空闲内存、空闲 CPU 的硬上限尚未确认；实施计划应先测量最小原型，再请求确认发布门槛。

### 技术风险

- Codex App Server 官方协议支持 `thread/list`、`thread/resume` 和 `turn/start`，足以完成接收端投递。
- 当前 Codex 源码的 App Server MCP 调用路径会把调用方 `threadId` 写入 MCP 请求 `_meta`；实现前必须在目标 Codex App 版本做一次探针验证。若普通 MCP 调用未携带该字段，`send_message` 必须临时增加显式 `from` 参数。
- Codex App 与外部客户端共享或连接同一个 App Server 时的并发行为，需要用两个真实 session 做最小集成验证，不能仅靠协议单元测试推断。

## 14. 实施约束

- 以 Codex App Server 作为唯一 session 集成边界，不读取或修改 Codex 内部会话文件。
- `turn/start` 用于真正触发接收 Agent；不使用只写模型历史但不启动回合的 `thread/inject_items`。
- 先实现双机、单条文本、空闲 session 的端到端闭环，再考虑任何扩展。
- 第一版最多一个可执行程序、一个配置文件、两个 MCP 工具、两个局域网接口。
- 网络发现和连接采用有上限的退避与抖动自动恢复，不采用高频忙轮询。
- 依赖选择必须说明必要性；标准库或已有系统能力能够充分解决时，不增加第三方依赖。
- 三个平台共享同一核心实现和协议测试，不维护三套行为不同的产品实现。

## 15. 参考依据

- OpenAI Codex App Server 文档：<https://developers.openai.com/codex/app-server>
- OpenAI Codex 源码中的 MCP `threadId` 元数据注入路径：<https://github.com/openai/codex/blob/main/codex-rs/app-server/src/request_processors/mcp_processor.rs>
