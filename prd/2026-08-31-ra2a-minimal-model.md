# PRD：RA2A 局域网 Agent 通信（极简闭环）

- 状态：Draft
- 创建日期：2026-08-31
- 更新日期：2026-09-01
- 发起人：用户
- 原始诉求：让局域网内多台设备上的多个 Codex Agent 能发现彼此，并向指定 session 发送消息。
- 产品权威：`Confirmed Product Decisions` 章节

## 请求方评审摘要

- 已确认：第一版仅面向 Codex App；局域网运行、多设备安装 Codex App 与本 MCP、自动发现、定向向指定 session 注入消息；服务轻量、低依赖、自愈，并支持 macOS、Linux、Windows 与 Agent 友好安装。
- 已确认：向信任组公开全部未归档 session；忙碌时拒绝；消息不自动重试；成功只代表远端创建回合；用户级 daemon 由操作系统启动和保活。
- 已确认：第一版使用长期共享的 6 位 PIN，并将 PIN 原样作为 DTLS-PSK；安全强度暂不作为目标，不实现密钥派生、一次性配对、密钥轮换或设备吊销生命周期。
- 已确认：V1 由 RA2A 管理唯一 Codex App Server，Codex App 通过 Remote/SSH 使用同一宿主；普通 Desktop 本地 session 不属于正式可写目标。
- 官方文档与实机判定：受管单宿主的 session 枚举、恢复、启动回合和实时客户端同步已经通过；普通 Desktop 私有 stdio 宿主不可附着，第二宿主会触发 writer conflict。
- 当前状态原因：macOS 单宿主闭环已经通过；正常 MCP 来源路径、Windows/Linux 实机、安装服务和轻量资源新基线仍待完成，因此 PRD 保持 Draft。

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
Codex App / remote client A ── managed App Server A
                                      │
                               RA2A daemon A
                                      │ mDNS + CoAP/DTLS
                               RA2A daemon B
                                      │
Codex App / remote client B ── managed App Server B
```

RA2A Node 只有三个职责：

1. 对本机 Codex 暴露 MCP 工具。
2. 发现局域网内其他 RA2A Node，并收发消息。
3. 管理并连接唯一 Codex App Server，列出受管 session、恢复 session、启动新回合。

不设置中心节点。所有节点对等。

每个操作系统用户只运行一个 RA2A daemon。首次执行 `ra2a` 引导时将其注册为用户级后台服务并立即启动；后续由 `launchd`、`systemd --user` 或 Windows 用户登录任务负责开机登录启动和崩溃重启。Codex App 只连接本机 MCP 端点，不负责 daemon 生命周期。

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
- 节点发现使用 mDNS/DNS-SD 服务 `_ra2a._udp.local`，与 CoAP/DTLS 的 UDP 传输一致；广播内容只包含节点 ID、名称、端口和协议版本。

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

所有节点配置同一个长期共享 PIN，形成一个完全互信的 RA2A 信任组：

- mDNS 发现可以公开，但 session 列表和消息接口必须鉴权。
- 持有相同 PIN 的节点可查看全部未归档 session，并可向其发送消息。
- PIN 不通过 mDNS、局域网请求或消息正文传播。
- 第一版不做账号、角色、逐设备授权和逐 session ACL。

PIN 不是可省略的装饰：向 Codex session 注入文本可能触发文件、网络或终端操作，因此“同一局域网”不能直接等同于“可信”。

### 长期共享 PIN

第一版采用静态配置，不提供配对协议：

1. 首台设备安装时由 daemon 随机生成 6 位 PIN；字符集排除 `0/O/1/I/L` 等易混淆字符。用户也可以通过非交互安装参数提供自己的 6 位 PIN。
2. PIN 保存在当前用户可读取的配置中；macOS/Linux 使用仅当前用户可读的文件权限，Windows 使用仅当前用户可读的 ACL。
3. 用户通过可信的线下方式把 PIN 复制到其他设备，例如在新设备执行安装命令时传入 `--pin <PIN>`。RA2A 不通过网络自动传播 PIN。
4. daemon 将 6 位 PIN 的原始字节直接交给 DTLS 实现作为预共享密钥（PSK），不增加 KDF、证书、PAKE 或应用层加密封装。DTLS 握手不会把 PSK 本身作为应用数据发送。
5. PIN 长期有效，重启、升级和网络重连不会改变它。第一版不实现过期、单次使用、自动轮换、逐设备吊销或 PAKE 配对。
6. 若用户主动更改 PIN，必须在所有设备上手动设置同一新值；配置不同期间，节点视为不同信任组并互相拒绝。

第一版的安全目标仅限于“相同 PIN 可以握手，不同 PIN 拒绝握手”。6 位 PIN 熵很低，配置文件也不使用系统密钥链，因此不得宣称能够抵抗猜测、凭证窃取或恶意局域网攻击；该取舍必须在 README 中明确。第一版不增加限速、锁定、审计或其他安全机制。

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
- 用户体系、配对协议、密钥自动轮换、设备吊销和细粒度权限；
- 自动等待远端任务完成。
- Codex CLI、IDE 扩展、云任务及其他 MCP Host 的兼容承诺。

## 11. 验收标准

1. Given 两台设备运行 RA2A 且 PIN 一致，when Agent A 调用 `list_targets`，then 能看到设备 B 及其可达 session。
2. Given session B 在线且空闲，when Agent A 向其地址调用 `send_message`，then session B 出现包含来源地址和正文的新用户回合并开始处理。
3. Given Agent B 收到消息，when 它向消息中的 `from` 地址发送回复，then 原 session A 收到新回合。
4. Given 两台设备 PIN 不同，when 任一方请求 session 列表或发送消息，then 请求被拒绝且不泄露 session 数据。
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
15. Given 首台设备尚未配置，when 首次执行 `ra2a` 完成引导，then 生成并明确显示一个长期 6 位 PIN；given 新设备设置相同 PIN，then 它加入同一信任组且重启后配置保持不变。

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
| PD15 | RA2A 以用户级 daemon 常驻 | 首次 `ra2a` 引导时注册并启动；登录时自动启动；崩溃时由操作系统重启 | Codex App 或单个 session 不得拥有 daemon 生命周期，也不得要求系统级/root 常驻服务 | 保证唯一实例、自愈和用户态 Codex 访问 | 每个 session 启动独立服务，或关闭 Codex App 后服务必然消失 | user-confirmed-direct: 对新交互方案及确认问题回复“都确认” | active |
| PD16 | 第一版使用长期共享的 6 位 PIN | PIN 保存在各节点用户配置中并长期有效；其他设备通过线下复制相同 PIN 加入信任组 | 不得实现一次性 PIN、过期、PAKE 配对、自动轮换或逐设备吊销生命周期 | 用户要求第一版进一步简化密钥模型 | PIN 自动失效、每台设备产生不同长期密钥，或加入设备必须完成额外配对流程 | user-confirmed-direct: “比如生成一个 6位字符之类的”；“第一版做简单点，不要搞这种生命周期，就做成长期的” | active |
| PD17 | 第一版暂不以安全强度为目标，6 位 PIN 原样作为 DTLS-PSK | 相同 PIN 必须握手成功，不同 PIN 必须握手失败；README 明示低强度边界 | 不得增加 KDF、证书、PAKE、应用层加密、轮换、限速或锁定机制 | 先以最小凭证跑通局域网通信闭环 | 安装或握手要求派生密钥、证书或额外配对步骤 | user-confirmed-direct: “进一步简化方案，暂不考虑安全问题，先跑通，有个简单的凭证能握手就行” | active |
| PD18 | V1 使用 RA2A 受管的单一 Codex App Server | RA2A 与 Codex App Remote/SSH 客户端必须连接同一宿主；只把该宿主管理的 session 视为正式可写目标 | 不得启动第二个 writer 抢占普通 Desktop 本地 session，也不得依赖未公开的 Desktop 内部投递接口 | 成熟开源实现与实机均证明单宿主可以实时同步，跨进程 writer 不可协调 | 普通 Desktop 本地 thread 被宣称可写，或正式实现调用私有 `send_message_to_thread` bridge | user-confirmed-direct: 对“RA2A 托管单宿主模式，普通本地 session 注入仅作实验兼容层”的建议回复“确认” | active |

## 13. 官方文档可行性判定

**结论：受管单宿主模式在 macOS 实机可行；普通 Desktop 本地 session 注入不可行。**

| 关键链路 | 官方文档依据 | 判定 |
|---|---|---|
| Codex App 安装并调用本地 MCP | Codex MCP 文档说明桌面 App 支持 MCP，可配置 STDIO 或 Streamable HTTP 服务 | 可行 |
| 枚举、恢复并读取 thread 状态 | App Server 提供 `thread/list`、`thread/read`、`thread/resume` 和 thread 状态 | 可行 |
| 向空闲目标启动新回合 | App Server 提供 `turn/start`；`thread/inject_items` 只写入历史而不启动回合 | 可行 |
| 忙碌 session 拒绝而非干预 | thread 状态包含 `active`，协议另有 `turn/steer`；第一版可在 `active` 时拒绝 | 可行 |
| RA2A 与 Codex 客户端连接同一受管 App Server，并实时显示外部启动的回合 | 官方提供 `--listen unix://` 和 `--remote unix://`；macOS 实机完成 LAN 注入并由官方客户端实时显示和回复 | macOS 已通过 |
| 向普通 Desktop 本地 session 注入 | Desktop 本地 App Server 为私有 stdio，外部第二宿主恢复同一 thread 返回 active-writer conflict | V1 明确不支持 |
| MCP 工具调用自动携带来源 session ID | 官方 MCP 文档未定义该元数据；实机使用 Codex App 自带 0.151.0-alpha.7.2 和全局 0.146.0 调用 `mcpServer/tool/call` 时，均观察到匹配目标的 `_meta.threadId` | 当前版本通过，正式实现仍需能力检测 |
| 枚举 Codex App 创建的全部目标 thread | 独立 App Server 实机列出共享存储中的 58 个 thread，当前工作区命中 1 个；普通 thread 可恢复，但当前 paginated history thread 因 lineage cycle 不可恢复 | 部分可行，必须暴露不可恢复状态 |
| macOS、Windows、Linux 部署 | 官方提供三端桌面 App；Linux 当前为 Preview，且仅列出特定发行版和 x64/ARM64 | 有条件可行 |

### 实施前 Go/No-Go 探针

必须先在真实 Codex App 上完成以下最小探针，不得用协议单元测试替代：

1. 安装一个只记录非敏感结构的测试 MCP，确认普通工具调用是否携带可验证的来源 thread ID。
2. 通过官方支持的本地传输连接 App Server，确认能列出与 Codex App 界面一致的 thread。
3. 对空闲目标执行 `thread/resume` 与 `turn/start`，确认 Codex App 界面同步出现新用户回合且 Agent 开始处理。
4. 对活动中的目标验证状态判断与拒绝行为不会修改当前回合。
5. 在 macOS、Windows 和官方支持的 Linux App 环境重复核心闭环；记录 App/Codex 版本及差异。

只有第 1～4 项通过，首版核心闭环才可判为 **Go**。第 1 项失败意味着自动回复地址无法按当前双工具模型实现；第 2 或第 3 项失败意味着公开接口不足以向正在使用的 Codex App session 投递消息，必须回到产品决策层重新选择交互契约，不得直接依赖未公开内部实现。

### 2026-08-31 第一阶段实验进展

- 已通过：独立 App Server 枚举共享会话存储；普通 thread 恢复；`mcpServer/tool/call` 向下游 MCP 注入匹配目标的 `_meta.threadId`；ephemeral thread 的 `turn/start` 返回 `inProgress`。
- 已发现限制：当前长任务的 paginated history 存在 lineage cycle，独立 App Server 无法恢复；并非所有未归档 thread 都可投递。
- 尚未通过：桌面 App 正常模型调用 MCP 的路径；既有持久化 thread 注入后的 App UI 同步；Windows/Linux 实机。
- 证据与复现方式见 [`experiments/README.md`](../experiments/README.md)。

### 2026-08-31 第二阶段网络方案实验进展

- 已对比完整 `go-libp2p` 与轻量成熟组合，均通过同 PSK 通信、异 PSK 拒绝、本机发现和断线重建测试。
- 已验证长期 6 位 PIN 可原样作为 Pion DTLS 的 PSK：相同 PIN 握手并完成消息往返，不同 PIN 握手失败；未增加自定义密码协议。
- 原始 DTLS 不承担应用消息可靠性，因此拒绝在其上自行设计确认、重传和分片；补充采用成熟 CoAP 协议后，20 KB Block-Wise 消息实机通过。
- CoAP 的传输重发必须保持同一 Message ID 并由接收库去重，不得在结果未知后创建新的应用层消息；go-coap 上游已有 handler 去重测试，RA2A 双机故障注入仍待验证。
- 当前工程推荐为 `libp2p/zeroconf v2.2.0 + go-coap v3.5.4 + Pion DTLS v3.1.8`；macOS arm64 PoC 为 6,327,058 B，实测最大 RSS 14,696,448 B。
- Linux amd64 与 Windows amd64 已交叉编译通过，但真实双机发现、网卡切换恢复和目标系统防火墙行为尚未验证。
- 证据、版本与完整量化对比见 [`experiments/network/README.md`](../experiments/network/README.md)。

### 2026-08-31 第三阶段最小 LAN Node 进展

- 已将成熟网络组合移入正式根模块，提供 `ra2a serve` 和 `ra2a selftest` 两个入口，共用同一套 LAN Node 实现。
- 已在本机通过真实 mDNS/DNS-SD 发现自身，并使用长期 6 位 PIN 经 CoAP/DTLS 调用自身的 `GET /v1/sessions`；当前按设计返回空数组。
- 自检只证明发现、凭证握手和请求响应链路；尚未连接 Codex App Server，因此不代表已能列出或注入真实 Codex session。
- DNS-SD 服务类型已从早期候选图中的 `_ra2a._tcp` 收敛为与实际传输一致的 `_ra2a._udp`。
- 裁剪后的正式命令开发构建为 macOS arm64 6,614,482 B、Linux amd64 6,975,650 B、Windows amd64 7,137,792 B；macOS 自检最大 RSS 为 15,138,816 B。

### 2026-08-31 第四阶段本机 Codex session 枚举进展

- 正式 `ra2a selftest` 和 `ra2a serve` 现在会启动本机 `codex app-server`，按官方协议完成 `initialize` 与 `initialized` 握手，并分页调用只读 `thread/list`。
- `/v1/sessions` 已从固定空数组切换为 App Server 数据源，只返回 `id`、标题和运行状态，不向局域网暴露完整 thread 内容。
- 全局 Codex 0.146.0 与 Codex App 内置 0.151.0-alpha.7.2 均通过真实 mDNS 自发现、DTLS 握手和 CoAP 调用返回 60 个未归档 thread。
- 接入 App Server 后的裁剪构建为 macOS arm64 6,717,106 B、Linux amd64 7,106,722 B、Windows amd64 7,287,296 B。
- macOS 前台常驻单次测量中，RA2A 自身空闲 RSS 为 12,704 KiB，独立 Codex App Server 子进程为 55,904 KiB；读取 60 个 thread 的自检进程树观测峰值为 94,470,144 B。独立 App Server 是当前主要资源成本。
- 本阶段未调用 `thread/resume` 或 `turn/start`，没有修改任何既有 session。独立 App Server 与桌面 App UI 的实时写入同步仍未证明，整体可行性结论保持“条件可行”。

### 2026-09-01 第五阶段单宿主注入进展

- 已验证普通 Desktop 本地 thread 即使显示为 `notLoaded`，第二 App Server 执行 `thread/resume` 仍可能返回 `already has an active writer`；该方向正式关闭。
- RA2A 改为连接 canonical Unix control socket，不存在时启动并监管 `codex app-server --listen unix://`；不再接入 Desktop 私有 tools pipe。
- 官方 `codex --remote unix://` 在同一宿主创建持久 session 后，第二 RA2A 节点经真实 mDNS、DTLS 和 CoAP `/v1/messages` 投递成功。
- 官方客户端实时显示来源 `ra2a://managed-sender-2` 和正文，并完成回合回复 `RA2A_MANAGED_HOST_OK`；原 writer-conflict 症状在受支持的单宿主路径上消失。
- 当前 Windows/Linux 仍只有交叉构建或协议证据，不能标记为实机通过。

### 2026-09-01 第六阶段安装器进展

- 根目录提供 `install.sh` 与 `install.ps1`；均支持传入长期 PIN 或在首台设备生成 6 位 PIN，并输出明确安装结果。
- macOS 注册用户 LaunchAgent，Linux 注册 systemd user unit，Windows 注册当前用户计划任务；三者均配置登录启动与异常退出重启。
- macOS/Linux 安装流程已在隔离 HOME 和伪服务管理器下完成自动化契约测试；真实 macOS 安装仍需在合并后执行，Windows/Linux 真实系统验收仍待完成。
- 安装器当前从源码构建，运行期仍保持单一 RA2A 二进制，不引入数据库或额外常驻运行时。

### 2026-09-01 第七阶段正式 MCP 闭环

- 单一 `ra2a` 二进制新增 stdio MCP 模式，`tools/list` 严格只暴露 `list_targets` 与 `send_message`；安装、升级和卸载同步管理 Codex 全局 MCP 注册。
- MCP 子进程通过仅绑定 `127.0.0.1:47321` 的本地控制面复用常驻 daemon；不会重复启动 mDNS、DTLS listener 或 Codex App Server。
- `list_targets` 已经通过真实 mDNS/DTLS/CoAP 返回本机受管宿主的未归档 session；不可连接的陈旧 peer 会从工具结果过滤。
- `send_message` 从实机 `_meta.threadId` 形成 `ra2a://mcp-e2e-local/caller-mcp-e2e`，自动生成 message ID，并完成正式 MCP → daemon → LAN node → App Server → 官方客户端闭环；目标回复 `RA2A_MCP_COMPLETE_OK`。
- 错误语义已落地：active writer/turn 映射 `SESSION_BUSY`，结果未知超时映射 `DELIVERY_UNKNOWN` 且不重发，缺失来源 thread 映射 `CALLER_SESSION_UNKNOWN`。

## 14. 待确认决策与风险

### 实施前需用户确认

1. **平台基线**：macOS、Linux、Windows 的最低版本以及必须发布的 CPU 架构尚未确认。
2. **轻量预算**：产物大小、空闲内存、空闲 CPU 的硬上限尚未确认；实施计划应先测量最小原型，再请求确认发布门槛。

### 技术风险

- `codex app-server` 当前被官方标为实验性接口，可能随 Codex 版本变化。首版必须固定已验证版本范围、启动时做能力检测，并在不兼容时明确失败。
- 普通 Desktop 本地 App Server 不提供公开附着入口；V1 必须清晰区分受管 session 与只读可发现的本地 session。
- 官方文档未规定 MCP 调用携带来源 thread ID。该能力是实现可回复地址的硬门槛，不能把源码中的当前行为当成产品契约。
- Linux Codex App 当前为 Preview，平台支持结论必须绑定官方列出的发行版与架构，不能泛化为所有 Linux 环境。

## 15. 实施约束

- 第一版只支持 Codex App，不为 CLI、IDE 扩展、云任务或其他 MCP Host 增加兼容代码。
- 将 RA2A 受管的单一 Codex App Server 作为唯一正式可写的 session 集成边界；不读取或修改 Codex 内部会话文件。
- App Server 连接仅限本机 control socket，不在局域网直接暴露；局域网只暴露带凭证握手的 RA2A 接口。
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
