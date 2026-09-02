# PRD：RA2A v1.1.0 Codex CLI 与全交叉 Agent 互通

- 状态：Ready for implementation
- 创建日期：2026-09-02
- 版本范围：v1.1.0
- 请求方：项目 Owner
- 工程计划：[engineering-plan.md](./engineering-plan.md)

## 评审摘要

已确认：

- v1.1.0 加入 Codex CLI 支持。
- RA2A 的长期支持模型是全交叉互通，而不是同类 Agent 互通或按 Agent 对逐一集成。
- 异构 Agent 的会话、消息、所有权和投递语义差异由 RA2A 的兼容适配层吸收。
- 架构必须允许通过新增适配器扩展 Agent 类型，避免修改已有适配器或增加 N×N 路由分支。
- 开发、实验和验证必须与本机已安装的 RA2A 正式版隔离，不得影响正式版的配置、进程、网络身份、宿主会话或正常通信。

新增确认：未运行的 Codex CLI 接入服务必须返回明确状态，由调用 Agent 提示用户是否拉起；目标地址保持不透明；全交叉双向能力是 Agent 支持列表的强制准入门槛。产品行为已足以进入可行性实验和实现。

## 背景

当前 RA2A 已支持局域网内多个 Codex App session 的发现和定向消息投递。现有实现围绕 Codex App 的 App Server、Desktop IPC 和 session writer 约束建立，直接在现有路径中加入 Codex CLI 条件分支，会把宿主差异扩散到发现、寻址、路由和错误处理层。

v1.1.0 首次引入第二类 Agent 宿主。它必须同时完成两件事：

1. 让 Codex App 与 Codex CLI 在四个方向上互通。
2. 建立可复用的兼容边界，使后续接入 Claude Code、OpenCode、Pi 等 Agent 时只需实现各自适配器。

## 产品目标

### G1：Codex CLI 全方向互通

同一局域网内，Codex App 和 Codex CLI 的可用会话可以互相发现并定向发送文本消息，包括：

- Codex App → Codex App
- Codex App → Codex CLI
- Codex CLI → Codex App
- Codex CLI → Codex CLI

### G2：全交叉支持模型

当 RA2A 支持的 Agent 类型集合为 `A` 时，任意发送端 `a ∈ A` 都应能与任意接收端 `b ∈ A` 通信。系统通过统一消息模型和端点模型实现互操作，而不是为每个 `(a, b)` 组合开发专用桥接器。

### G3：兼容适配责任归属于 RA2A

不同 Agent 在以下方面存在差异，RA2A 必须在适配层内处理：

- 会话或线程的发现与标识
- 发起新 turn 的入口
- 活跃 writer、busy 和会话所有权
- UI/TUI 状态同步
- 调用方 session 身份获取
- 投递结果、失败和不确定状态
- 生命周期、重连和健康检查

这些差异不得泄漏为 Agent 对之间的特殊路由逻辑。

### G4：保持最小用户模型

用户和调用 Agent 仍以“列出目标、向目标地址发送消息”为主流程。宿主类型可以作为目标元数据展示，但不要求调用方理解宿主的内部协议。

## 非目标

v1.1.0 不包含：

- 实现 Codex App 和 Codex CLI 之外的 Agent 适配器
- 文件、图片或流式内容传输
- 跨公网、Tailscale 或 Relay 中继
- 多 Agent 工作流编排、任务队列或自动回复协议
- 重新设计 PIN 或局域网安全模型
- 为每一对 Agent 建立专用转换协议

## 用户流程

1. 用户在设备上启动 RA2A 服务。
2. RA2A 加载本机可用的 Agent 适配器，并汇总可投递端点；若 Codex CLI 接入服务未运行，则保留明确的 `start_required` 状态。
3. 远端 Agent 调用 `list_targets`，获得完整、可直接使用的目标地址和必要能力信息。
4. 如果目标需要拉起服务，RA2A 向调用 Agent 返回明确、可操作的信息；Agent 必须先提示用户确认，不得静默启动。
5. 远端 Agent 调用 `send_message`，RA2A 将统一消息路由到目标端点所属适配器。
6. 目标适配器将消息转换为宿主能接受的操作，并返回统一投递结果。
7. 失败时，调用方获得稳定且与宿主无关的错误语义；诊断信息可以保留宿主细节。

## v1.1.0 功能范围

### 端点发现

- 同一节点可以同时暴露来自多个 Agent 适配器的端点。
- 每个端点必须有节点内唯一、重启后尽可能稳定的身份。
- 列表结果应能区分 Codex App 与 Codex CLI。
- 不可投递端点不得被伪装为 ready；如果保留展示，必须携带明确能力或状态。

### 消息投递

- 文本消息使用宿主无关的统一信封。
- 路由器只根据目标端点选择适配器，不处理宿主内部细节。
- 适配器负责将统一消息转换为宿主调用，并将宿主结果转换为统一投递状态。
- 现有 `SESSION_BUSY`、`TARGET_NOT_FOUND`、`TARGET_UNREACHABLE`、`DELIVERY_UNKNOWN` 等语义应保持稳定或显式迁移。

### 来源身份

- 消息来源必须能表达节点、Agent 类型和会话/线程身份。
- Codex App 当前依赖 MCP 调用上下文中的 thread ID；Codex CLI 是否提供等价上下文必须先实验确认。
- 无法可靠识别来源 session 时，不得静默伪造身份；该适配器不能作为可回复的正式发送端，也不能通过 PD31 的支持准入门槛。

### 安装与运行

- 保持单个 RA2A daemon，不为每种 Agent 启动独立局域网服务。
- 安装程序可以为多个宿主注册同一个 RA2A MCP 服务。
- Codex CLI 接入服务未运行时，返回明确的 `start_required` 结果及面向 Agent 的下一步说明。
- 调用 Agent 必须提示用户是否拉起服务；未获得用户确认前不得自动启动。
- 操作系统目标保持 macOS、Linux、Windows。

### 开发与正式版隔离

- 开发实例必须使用独立的配置、运行目录、日志、控制地址、App Server socket、节点身份和测试会话，不得复用正式版资源。
- 开发命令不得安装、升级、重启、停止或重新配置本机正式版，也不得覆盖正式版注册的 MCP、系统服务或计划任务。
- 实机实验只能向明确标记的测试端点和测试会话投递；不得把正式版会话作为隐式测试目标。
- 每次启动开发 daemon 或宿主实验前必须检查资源冲突；无法确认隔离时停止实验并报告，不得以暂时停用正式版作为默认解决方式。
- 实验结束时只清理本次开发实例创建且可明确归属的资源，正式版应继续保持原有配置和正常工作。

## 成功标准

### 用户级验收

- 四个 Codex App/CLI 方向均完成真实双设备投递测试。
- 连续多轮投递后，Codex App UI 与 Codex CLI TUI 均可继续正常交互。
- daemon 或网络短暂中断后，无需重新配置即可恢复发现和投递。
- 目标列表能明确展示端点属于 Codex App 还是 Codex CLI。
- Codex CLI 接入服务未运行时，Agent 能明确告知用户并询问是否拉起，而不是返回普通不可达或静默启动。
- 开发或实验期间，本机正式版无需停止、重启或重新配置，并能继续发现端点和完成正常投递。

### 架构级验收

- Codex App 和 Codex CLI 分别通过独立适配器接入统一端点与消息模型。
- 路由层不存在 `App→CLI`、`CLI→App` 等成对分支。
- 新增一个测试适配器时，不需要修改现有适配器。
- 宿主原生错误只在适配器内解释，对外映射为统一结果。
- 协议具备版本和能力表达，旧节点遇到未知端点类型时能够安全失败。

## 官方能力边界

Codex CLI 当前提供以下官方入口：

- `codex exec resume [SESSION_ID] [PROMPT]` 可恢复已有 session 并附带输入。
- `codex resume` 可恢复交互式 session。
- `codex --remote ws://...|unix://...` 可让 TUI 连接远端 App Server。
- App Server 提供 `thread/list`、`thread/resume` 和 `turn/start` 等接口。

但官方将 `codex app-server` 命令标为 Experimental，可能无预告变化。因此 v1.1.0 不能只依据命令存在就承诺兼容性，必须验证活跃 TUI writer、UI 刷新、并发 turn 和跨平台行为。

资料：

- [Codex CLI developer commands](https://learn.chatgpt.com/docs/developer-commands?surface=cli)
- [Codex App Server](https://learn.chatgpt.com/docs/app-server)
- [Codex MCP support](https://learn.chatgpt.com/docs/extend/mcp?surface=cli)

## 已确认的产品边界

- Codex CLI 未运行场景按 PD29 处理：明确返回并由 Agent 征求用户是否拉起，不允许静默启动。
- 地址按 PD30 处理：`list_targets` 返回完整、不透明地址，调用方不得依赖内部编码。
- Agent 准入按 PD31 处理：不能完成全交叉双向能力和人工继续交互验证，就暂不列为支持。

Codex CLI 具体采用 direct resume、共享 App Server 或其他宿主所有权实现，属于可行性实验需要回答的工程问题；只要最终行为满足 PD29 和 PD31，就不改变产品承诺。

## 风险

| 风险 | 影响 | 当前处理 |
| --- | --- | --- |
| 活跃 CLI TUI 与外部投递争夺 writer | 会话 busy、TUI 卡住或状态不刷新 | 进入实现前完成所有权实验 |
| App Server 命令仍为 Experimental | 上游升级导致接口变化 | 适配器隔离、版本探测、契约测试 |
| CLI MCP 上下文缺少来源 session ID | 无法形成可回复的来源地址 | 探测真实调用元数据，必要时设计明确降级 |
| 地址升级导致旧节点不兼容 | 混合版本投递失败 | 协议版本和能力协商，保留安全失败路径 |
| 适配器接口过度抽象 | 增加复杂度而未解决真实差异 | 只从 Codex App/CLI 两种已验证差异提取接口 |

## Confirmed Product Decisions

> 此区块为 v1.1.0 的受保护产品决策。后续实现计划只能引用，不能静默改写。既有版本已确认决策继续有效；其中“第一版仅限 Codex App”是 v1.0 范围约束，与本版本不冲突。

| ID | Status | Decision | Rationale | Confirmation Evidence | Supersedes | Revisit Trigger |
| --- | --- | --- | --- | --- | --- | --- |
| PD25 | Active | v1.1.0 加入 Codex CLI 支持。 | 本版本明确目标。 | Owner：`在 docs/v1.1.0 中开始做版本规划，目标是加入 codex cli 支持`（2026-09-02） | — | Owner 调整 v1.1.0 范围 |
| PD26 | Active | RA2A 的 Agent 支持采用全交叉模型：任意已支持 Agent 都必须能与其他任意已支持 Agent 进行 RA2A 通信。 | 避免同类孤岛和只覆盖部分组合。 | Owner：`agent 服务支持目标是全交叉支持，即任何 agent 都要支持和其他所有不同类型的 agent 进行 ra2a`（2026-09-02） | — | Owner 修改长期互操作目标 |
| PD27 | Active | 不同 Agent 架构之间的兼容适配是 RA2A 的核心职责。 | 调用 Agent 不应承担异构宿主转换。 | Owner：`ra2a 的一个重要工作就是做不同架构之间的兼容适配`（2026-09-02） | — | 兼容责任边界被重新定义 |
| PD28 | Active | RA2A 必须采用强扩展、灵活的架构支撑持续新增 Agent 类型。 | 异构兼容会持续增加，不应形成成对耦合。 | Owner：`这对 ra2a 本身的架构也有挑战，需要设计成强扩展性的灵活架构`（2026-09-02） | — | Owner 接受限定宿主或一次性集成架构 |
| PD29 | Active | Codex CLI 接入服务未运行时，RA2A 必须向调用 Agent 返回明确信息，由 Agent 提示用户确认是否拉起；不得静默自动启动。 | 保持用户知情和启动控制权，同时给 Agent 可执行的恢复路径。 | Owner：`未运行的要返回明确信息给 agent，让 agent 提示用户是否拉起服务`（2026-09-02） | — | Owner 修改服务启动授权方式 |
| PD30 | Active | 目标地址保持为由 `list_targets` 返回的完整、不透明地址；调用方不依赖或拼接 Agent 类型等内部结构。 | 保持调用模型简单，并允许内部寻址模型继续演进。 | Owner 对地址不透明推荐方案回复：`确认`（2026-09-02） | — | Owner 要求公开或稳定地址内部结构 |
| PD31 | Active | 一种 Agent 只有在可发现、可作为发送方和接收方、可返回统一投递结果、投递后可继续人工交互，并通过与所有已支持 Agent 的全交叉测试时，才能列为正式支持；否则暂不支持。 | 防止支持列表出现单向、局部或不可持续的兼容。 | Owner：`是，这是准入门槛，如果做不到就暂时不支持`（2026-09-02） | — | Owner 修改 Agent 正式支持门槛 |
| PD32 | Active | 开发、实验和验证必须与本机已安装的 RA2A 正式版隔离，不得干扰正式版正常工作。 | 保护日常使用中的配置、进程、网络身份和宿主会话，避免开发验证产生服务中断或状态污染。 | Owner：`开发过程中要和本机安装的正式版隔离，不要干扰它的正常工作`（2026-09-02） | — | Owner 明确允许某项实验共享或变更正式版资源 |
