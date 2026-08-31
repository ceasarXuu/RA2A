# PRD：RA2A 局域网 Agent 通信（极简闭环）

- 状态：Draft
- 创建日期：2026-08-31
- 更新日期：2026-08-31
- 发起人：用户
- 原始诉求：让局域网内多台设备上的多个 Codex Agent 能发现彼此，并向指定 session 发送消息。
- 产品权威：`Confirmed Product Decisions` 章节

## 请求方评审摘要

- 已确认：第一版仅面向 Codex App；局域网运行、多设备安装 Codex App 与本 MCP、自动发现、定向向指定 session 注入消息；服务轻量、低依赖、自愈，并支持 macOS、Linux、Windows 与 Agent 友好安装。
- 已确认：向信任组公开全部未归档 session；忙碌时拒绝；消息不自动重试；成功只代表远端创建回合；用户级 daemon 由操作系统启动和保活。
- 建议的最小闭环：使用 6 位一次性 PIN 完成设备配对，PIN 只建立临时安全通道，长期通信使用 daemon 生成的随机高强度组密钥。
- 官方文档判定：接收端会话操作具备协议基础，但连接正在运行的 Codex App 实例、识别 MCP 调用来源 session 尚无公开稳定契约，因此整体为“条件可行”。
- 实施前必须完成：Codex App 本机集成探针，并确认 PIN 配对与组密钥轮换模型、平台版本与架构基线、轻量资源预算。
- 当前状态原因：session、消息和 daemon 生命周期已经明确，但配对安全模型仍待确认，两项核心集成能力仍待实机验证。

## 1. 一句话模型

RA2A 是一个局域网内的“定向消息投递器”：Agent 发现可达 session，向其中发送一段文本，接收 session 将它作为新的 Codex 用户回合开始执行。

RA2A 不是任务平台、消息队列或多 Agent 编排框架。

本文中的 **Codex App** 专指 OpenAI 桌面 App 中的 Codex 使用界面。第一版不以 Codex CLI、IDE 扩展、云任务或其他 MCP Host 为兼容目标。

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
      │ localhost MCP
      ▼
RA2A daemon A ── LAN HTTP ──► RA2A daemon B
      ▲                            │
      │ mDNS discovery             │ local App Server
      └────────────────────────────▼
                               Codex Session B
```

RA2A Node 只有三个职责：

1. 对本机 Codex 暴露 MCP 工具。
2. 发现局域网内其他 RA2A Node，并收发消息。
3. 通过本机 Codex App Server 列出 session、恢复 session、启动新回合。

不设置中心节点。所有节点对等。

每个操作系统用户只运行一个 RA2A daemon。安装脚本将其注册为用户级后台服务并立即启动；后续由 `launchd`、`systemd --user` 或 Windows 用户登录任务负责开机登录启动和崩溃重启。Codex App 只连接本机 MCP 端点，不负责 daemon 生命周期。

核心服务必须保持轻量：不引入数据库，不依赖常驻的第三方基础服务，优先交付单一可执行程序。除 Codex 本身外，应尽量避免要求用户预装语言运行时或包管理器。

## 5. 极简交互面

### MCP 工具

仅提供两个工具：

```text
list_targets() -> Node[] + Session[]
send_message(to, text) -> accepted | error
```

- `list_targets` 返回当前已验证且在线的节点，以及这些节点上全部未归档 session 的 ID、标题和状态。
- `send_message` 的产品目标是从 MCP 调用上下文取得当前调用者 session，自动形成 `from` 地址；调用者只需要提供 `to` 和 `text`。
- OpenAI 官方 MCP 文档目前没有承诺工具调用会携带调用者 thread/session ID。实现前必须用探针确认该能力；若不存在，不得伪造或猜测 `from`，而应暂停实现并重新确认最小交互契约。

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
4. 目标 session 忙碌：返回 `SESSION_BUSY`，不排队、不插入或干预正在执行的回合。
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

### 信任边界

建议所有节点配置同一个随机共享密钥，形成一个完全互信的 RA2A 信任组：

- mDNS 发现可以公开，但 session 列表和消息接口必须鉴权。
- 持有共享密钥的节点可查看全部未归档 session，并可向其发送消息。
- 密钥不通过 mDNS 或消息正文传播。
- 第一版不做账号、角色、逐设备授权和逐 session ACL。

共享密钥不是可省略的装饰：向 Codex session 注入文本可能触发文件、网络或终端操作，因此“同一局域网”不能直接等同于“可信”。

### 建议的 PIN 配对模型（待确认）

六位 PIN 只作为一次性配对凭据，不能直接作为长期通信密钥：

1. 首台设备的 daemon 使用操作系统安全随机源生成一个组 ID 和 256 位随机组密钥。
2. 组密钥只保存在当前用户可读取的本地凭据文件中；macOS/Linux 使用仅当前用户可读的文件权限，Windows 使用仅当前用户可读的 ACL。密钥不得显示在终端、MCP 返回、消息或日志里。
3. 已入组设备执行配对命令时，daemon 生成一个避开易混淆字符的 6 位 PIN。PIN 默认 5 分钟过期、成功一次即失效，连续失败 5 次立即关闭本次配对。
4. 新设备输入 PIN 后，双方使用经审计的密码认证密钥交换协议建立临时加密通道，再传输真正的组密钥。候选为 SPAKE2+；不得自行设计“PIN 哈希后直接加密”的握手。
5. 新设备成功保存组密钥后加入信任组；PIN 不保存、不传播，也不再参与日常通信。session 列表和消息正文使用组密钥进行认证加密，只有 mDNS 中的节点发现摘要保持明文。
6. 第一版只支持一个信任组。新增设备可由任一已入组设备签发一次性 PIN。

建议的最小变更策略是“重建并重新配对”：在一台保留设备上生成新的组密钥，然后让其余保留设备逐台使用新 PIN 加入；每台设备成功加入新组后立即删除旧密钥。第一版不实现后台自动轮换、离线密钥追赶或逐设备吊销；移除设备时必须轮换组密钥。

该模型保留了 6 位码的输入体验，同时让短 PIN 只面对有次数和时效限制的在线猜测。密码认证密钥交换的目标是让双方从短密码导出强共享密钥而不泄露密码；实现必须使用成熟库和标准流程。

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
- Codex CLI、IDE 扩展、云任务及其他 MCP Host 的兼容承诺。

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
12. Given 第一版兼容矩阵，then 仅将 Codex App 标记为受支持的 MCP Host，不包含 CLI、IDE 扩展、云任务或其他客户端。
13. Given 信任组内存在多个未归档 session，when 对端调用 `list_targets`，then 返回这些 session 的 ID、标题和状态，不返回已归档 session。
14. Given 安装成功或用户重新登录，then 用户级 RA2A daemon 自动运行；given daemon 异常退出，then 操作系统服务管理器自动重启它。

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
| PD10 | 第一版目标仅限于 Codex App | 只围绕 Codex App 设计、验证和验收首版闭环 | 第一版不得扩展为 CLI、IDE、云任务或通用 MCP Host 兼容项目 | 收紧首版边界，优先验证核心可行性 | 验收范围包含非 Codex App 客户端，或为其增加兼容层 | user-confirmed-direct: “第一版目标仅限于 codex app” | active |
| PD11 | 默认向信任组公开全部未归档 session 的 ID、标题和状态 | `list_targets` 返回全部未归档 session 的最小摘要 | 第一版不得增加 session 手动发布机制或隐藏部分未归档 session | 保持发现模型和工具数量最小 | 同组节点无法发现某个未归档 session，或需要先手动发布 | user-confirmed-direct: 对“默认公开所有未归档 session 的 ID、标题和状态”回复“同意” | active |
| PD12 | 忙碌 session 拒绝新消息 | 返回 `SESSION_BUSY`，不修改当前回合 | 不得排队或通过 `turn/steer` 干预当前任务 | 避免并发消息改变正在执行的工作 | 忙碌 session 接收、排队或合并了远端消息 | user-confirmed-direct: 对“忙碌时直接返回 `SESSION_BUSY`”回复“同意” | active |
| PD13 | 结果未知的消息不自动重试 | 超时返回 `DELIVERY_UNKNOWN`；发现和连接继续自愈 | 不得因超时自动重发消息 | 避免重复创建 Codex 回合 | 超时后 daemon 自动再次投递同一消息 | user-confirmed-direct: 对“网络超时不自动重试”回复“同意” | active |
| PD14 | 发送成功只表示远端创建回合 | `accepted` 在远端创建回合后返回；回复使用独立消息 | 不得把 accepted 表述为 Agent 已完成，也不得同步等待结果 | 保持异步消息模型 | HTTP 请求等待 Agent 完成，或 accepted 被解释为任务完成 | user-confirmed-direct: 对“成功只表示远端已经创建 Codex 回合”回复“同意” | active |
| PD15 | RA2A 以用户级 daemon 常驻 | 安装时注册并启动；登录时自动启动；崩溃时由操作系统重启 | Codex App 或单个 session 不得拥有 daemon 生命周期，也不得要求系统级/root 常驻服务 | 保证唯一实例、自愈和用户态 Codex 访问 | 每个 session 启动独立服务，或关闭 Codex App 后服务必然消失 | user-confirmed-direct: 对“安装脚本注册用户级 daemon，由操作系统启动并保活”回复“同意” | active |

## 13. 官方文档可行性判定

**结论：条件可行，尚不能判定为可直接实施。**

| 关键链路 | 官方文档依据 | 判定 |
|---|---|---|
| Codex App 安装并调用本地 MCP | Codex MCP 文档说明桌面 App 支持 MCP，可配置 STDIO 或 Streamable HTTP 服务 | 可行 |
| 枚举、恢复并读取 thread 状态 | App Server 提供 `thread/list`、`thread/read`、`thread/resume` 和 thread 状态 | 可行 |
| 向空闲目标启动新回合 | App Server 提供 `turn/start`；`thread/inject_items` 只写入历史而不启动回合 | 可行 |
| 忙碌 session 拒绝而非干预 | thread 状态包含 `active`，协议另有 `turn/steer`；第一版可在 `active` 时拒绝 | 可行 |
| RA2A 连接正在运行的 Codex App 所使用的同一 App Server，并让 App UI 同步显示外部启动的回合 | App Server 支持本地 stdio、Unix socket 等传输，但 `codex app-server` 被官方标为实验性开发/调试接口；文档未承诺外部进程附着桌面 App 实例后的共享状态与 UI 行为 | 必须实机验证 |
| MCP 工具调用自动携带来源 session ID | 官方 MCP 文档未定义调用方 thread/session ID 元数据 | 必须实机验证 |
| 枚举 Codex App 创建的全部目标 thread | `thread/list` 支持来源过滤，但文档没有明确桌面 App thread 的来源类型与默认枚举行为 | 必须实机验证 |
| macOS、Windows、Linux 部署 | 官方提供三端桌面 App；Linux 当前为 Preview，且仅列出特定发行版和 x64/ARM64 | 有条件可行 |

### 实施前 Go/No-Go 探针

必须先在真实 Codex App 上完成以下最小探针，不得用协议单元测试替代：

1. 安装一个只记录非敏感结构的测试 MCP，确认普通工具调用是否携带可验证的来源 thread ID。
2. 通过官方支持的本地传输连接 App Server，确认能列出与 Codex App 界面一致的 thread。
3. 对空闲目标执行 `thread/resume` 与 `turn/start`，确认 Codex App 界面同步出现新用户回合且 Agent 开始处理。
4. 对活动中的目标验证状态判断与拒绝行为不会修改当前回合。
5. 在 macOS、Windows 和官方支持的 Linux App 环境重复核心闭环；记录 App/Codex 版本及差异。

只有第 1～4 项通过，首版核心闭环才可判为 **Go**。第 1 项失败意味着自动回复地址无法按当前双工具模型实现；第 2 或第 3 项失败意味着公开接口不足以向正在使用的 Codex App session 投递消息，必须回到产品决策层重新选择交互契约，不得直接依赖未公开内部实现。

## 14. 待确认决策与风险

### 实施前需用户确认

1. **配对与轮换**：是否确认第 9 节的模型——6 位 PIN 仅用于一次性 PAKE 配对，daemon 持有随机组密钥，移除设备时通过重建组并逐台重新配对完成轮换？
2. **平台基线**：macOS、Linux、Windows 的最低版本以及必须发布的 CPU 架构尚未确认。
3. **轻量预算**：产物大小、空闲内存、空闲 CPU 的硬上限尚未确认；实施计划应先测量最小原型，再请求确认发布门槛。

### 技术风险

- `codex app-server` 当前被官方标为实验性接口，可能随 Codex 版本变化。首版必须固定已验证版本范围、启动时做能力检测，并在不兼容时明确失败。
- Codex App 与外部客户端共享或连接同一个 App Server 时的并发和 UI 同步行为没有公开保证，必须用两个真实 session 验证。
- 官方文档未规定 MCP 调用携带来源 thread ID。该能力是实现可回复地址的硬门槛，不能把源码中的当前行为当成产品契约。
- Linux Codex App 当前为 Preview，平台支持结论必须绑定官方列出的发行版与架构，不能泛化为所有 Linux 环境。

## 15. 实施约束

- 第一版只支持 Codex App，不为 CLI、IDE 扩展、云任务或其他 MCP Host 增加兼容代码。
- 将 Codex App Server 作为候选且唯一允许的 session 集成边界；完成 Go/No-Go 探针前不进入完整实现，也不读取或修改 Codex 内部会话文件。
- App Server 连接仅限本机 stdio 或本机 socket，不在局域网直接暴露；局域网只暴露带鉴权的 RA2A 接口。
- `turn/start` 用于真正触发接收 Agent；不使用只写模型历史但不启动回合的 `thread/inject_items`。
- 先实现双机、单条文本、空闲 session 的端到端闭环，再考虑任何扩展。
- 第一版最多一个可执行程序、一个配置文件、两个 MCP 工具、两个局域网接口。
- 网络发现和连接采用有上限的退避与抖动自动恢复，不采用高频忙轮询。
- 依赖选择必须说明必要性；标准库或已有系统能力能够充分解决时，不增加第三方依赖。
- 三个平台共享同一核心实现和协议测试，不维护三套行为不同的产品实现。
- daemon 以当前用户身份运行，每个用户只允许一个实例；安装、升级和卸载必须幂等管理对应的用户级后台服务。

## 16. 参考依据

- OpenAI Codex App Server：<https://learn.chatgpt.com/docs/app-server>
- OpenAI Codex MCP：<https://learn.chatgpt.com/docs/extend/mcp>
- OpenAI Codex Developer commands：<https://learn.chatgpt.com/docs/developer-commands>
- OpenAI Codex desktop app：<https://learn.chatgpt.com/docs/app>
- OpenAI Codex on Windows：<https://learn.chatgpt.com/docs/windows/windows-app>
- OpenAI Codex on Linux：<https://learn.chatgpt.com/docs/linux/linux-app>
- RFC 9383 SPAKE2+：<https://www.rfc-editor.org/rfc/rfc9383.html>
