# 下一版本 Pi 支持工程方案

- 状态：P1 已实现并完成本地隔离验证；P2 跨平台/全交叉尚未完成。Owner 在插件与安装职责说明后要求「开始实施ba」。
- 发布版本：下一版本；建议 v0.0.19，尚未确认，暂沿用主题目录。确认后迁至 `docs/releases/<version>/pi-support/plan.md`，不保留两份有效方案。
- Product Authority：[prd.md](./prd.md#confirmed-product-decisions)。该 PRD 引用的既有统一互通 PD26–PD33 继续有效，不复制或改写其权威记录。
- Applicable Decisions：PI1–PI3、继承 PD26–PD28 / PD30–PD33；PD29 原文仅约束 Codex CLI，不自行扩展为 Pi 产品决策。
- Plan validity：valid-with-qualifications。

## Execution Contract

上述有效产品决策属于用户权威，变更必须由用户明确批准，禁止 Agent 自行批准。工程证据可以修订本方案，不能改写产品权威。新产品选择保持待确认；临时行为限于隔离实验，不成为正式承诺。

每个实质阶段开始前核对已完成实现、证据与全部剩余方案，记录 Pre-Phase Plan Rebase Gate。Gate 为 pending 或 blocked-on-plan-approval 时不开始该阶段；实质方案变化记录 Plan Delta，并取得针对该变化的明确批准。每阶段结束只审计本阶段 Product Decision Delta；未解决的实质 provisional / conflict 阻断依赖它的后续工作。

## 当前事实

1. 本机真实 Pi 已升级为 `@earendil-works/pi-coding-agent` 1.0.0；未改登录、模型或代理配置。
2. RA2A 已有统一 `agentbridge.Adapter`、Registry、LAN 消息和 loopback control；新增 Pi 只增加适配器，不建立 Pi ↔ 各 Agent 的专用桥。
3. Pi 原生扩展具备 session 生命周期、工具注册、MCP 注册和输入能力；动态 MCP 注册不写用户 `mcp.json`。
4. Pi 1.0 的 `sendUserMessage()` 仍返回 void。真实 SDK 无凭据隔离实验中，调用返回和 input 事件均出现，随后认证失败，user message 数为 0。二者都不能证明原生收件。
5. 内部 `prompt()` 有 `queued/started/handled` preflight，RPC 暴露对应结果；普通 ExtensionAPI 不暴露此回调。user `message_start` 到消费时才出现，忙时不能作为即时入队确认。
6. 因此“保持原生 TUI + 直接沿用原生入队确认”尚无可用接口证据。不能以超时放宽、等待模型完成或未确认的内部 API 绕过。

证据：本机包 `dist/core/agent-session.js:1481–1595、2676–2684`、`dist/core/extensions/types.d.ts:1222`；`.cache/pi-discovery/` 的 1.0.0 实验与保护快照。外部文档固定 [v1.0.0 extensions](https://github.com/earendil-works/pi/blob/v1.0.0/packages/coding-agent/docs/extensions.md)、[RPC](https://github.com/earendil-works/pi/blob/v1.0.0/packages/coding-agent/docs/rpc-commands.md)。这是调研证据，不是原生 TUI 接入通过。

## Design：建议采用原生扩展接入

```text
其他 Agent → 既有 LAN/control → Pi Adapter → 当前 Pi 进程内 RA2A 扩展
Pi 原生工具 → 既有 control → 统一 Registry/LAN → 其他 Agent
```

- 扩展在 `session_start` 建立当前会话绑定，使用原生 session ID；resume 不生成替代会话。每次 session 替换刷新上下文，shutdown/reload 释放本实例资源。
- Adapter 只发布由活跃扩展明确登记且未过期的端点。历史 JSONL 不赋予执行权；会话标题、更新时间、全局最近会话不参与寻址。
- 本机绑定使用用户私有记录和仅 loopback 的实例通道；按请求中的目标身份再次校验当前 session，防止发现与发送之间发生切换。同一会话被多个活跃实例同时持有时拒绝模糊投递，不选“最近一个”。
- Pi 公开端点使用与其他 Agent 不冲突的内部命名；外部仍是完整不透明地址。不承诺内部编码为公开接口。
- 扩展注册发现和发送工具，调用既有 `/v1/targets`、`/v1/send`；发送时从工具的当前 session 上下文生成准确 `from`，不要求模型猜测身份。是否改用动态 MCP，由来源身份实验决定；不维护重复的两套工具入口。
- 初始接入通过 `ra2a pi` 向真实 Pi 传入专属 extension 参数，保留原生 TUI 和用户配置；不新增 PATH 包装器、不覆盖 `pi`。用户已要求实施安装流程接入，专属插件同时纳入安装/升级；ra2a pi 保留为显式启动入口。
- 不启用 RPC 替代 TUI，不 patch 官方 Pi，不引入共享模型服务或新的通用任务调度器。

### 收件边界：必须先确认

**建议路线 A：** Pi 进程内扩展建立明确的入站接收边界。验证目标会话、保存宿主内接收记录并提供可见的收到标记后，返回扩展收件确认；再交给 Pi 输入接口执行。确认仅说明“Pi 内 RA2A 扩展收到”，不宣称 Pi 原生队列已接受或模型任务成功。后续鉴权失败、扩展拦截等仍单独报告，不推翻收件结果。使用 Pi 已有会话记录能力，不建设跨重启自动重放邮箱；记录可见性、写入错误与关闭时语义必须通过实验。

此具体收件边界已按 PI2 获用户实施授权；ACK 文案明确是 received_by_bridge，不宣称 native queued。宿主接收记录存在于原生 session entry tree；Pi 对尚无对话的新会话可能延迟落盘，因此不承诺断电耐久或跨重启自动重放。

**路线 B：** 若必须由 Pi 原生执行队列确认接受，则先验证或争取公开扩展 preflight 接口；当前普通扩展证据不足。接口不可用时停在发现阶段，不以 RPC 切换、私有字段调用或自有队列冒充原生接收。

两条路线都不等待业务回复或模型完成，也不自动重发未知结果。

## Pending Product Decisions

| ID | 决策面 | 建议 | 影响与确认边界 |
|---|---|---|---|
| Q1 | 收件成功由谁确认 | 已确认 PI2：路线 A | 不把插件收件冒充原生队列接受 |
| Q2 | 工作中消息策略 | 已确认 PI2：followUp | 不声明 steerActiveTurn；不自动 abort |
| Q3 | 安装与启动入口 | 已确认 PI3：安装/升级管理专属扩展，原生 pi 自动加载 | 仅写 ra2a.mjs；不改 settings/auth，不覆盖非本产品文件 |
| Q4 | 发布版本 / 兼容范围 | 建议 v0.0.19；先以 Pi 1.0.0 为验证基线 | 不自行宣称兼容旧 Pi，不因为本机升级就保证三平台支持 |

跨 Agent 验收范围按既有 PD26 / PD31 覆盖 Pi ↔ Codex App、Codex CLI、OpenCode 及 Pi ↔ Pi，不缩减为仅 Codex CLI。跨设备会话地址在自动本地验证完成后收集，人工操作留在最后。

## 投资前验证

| ID | 关键假设 / 解锁工作 | 最小验证与足够证据 | 隔离预算 / 止步条件 | 当前状态 |
|---|---|---|---|---|
| V1 | 原生扩展存在可靠接收边界 | 真实 1.0 扩展、假 provider；空闲成功、无凭据、输入 handled/transform、工作中接收、异常退出。逐条请求 ID 与接收记录关联；模型挂起时仍能确认收到 | 单一隔离 profile；只允许 fixture 通道，禁真实凭据/模型/正式 control；边界不能证明则停止适配实现 | 本地 SDK 明确插件收件通过；直接 sendUserMessage 返回方案 direction-rejected；handled/transform 与多平台待验 |
| V2 | 原生 session 生命周期可绑定当前 TUI | 真实 TUI A→B→resume A、reload、双实例、异常退出；同时记录事件、原生 ID、接收位置和租约撤回 | 临时会话；不以 SDK 身份测试代替 TUI；切换错投即止步 | 本地单实例 switch/resume、TTL 通过；reload/双实例及多平台待验 |
| V3 | 发送来源身份可自动准确获取 | 从原生工具 execute 上下文发往临时 control，检查切换前后完整 from；如用 MCP，独立验证来源随 resume 更新 | 两个隔离会话；来源模糊不降级为猜测 | 本地真实 SDK 当前来源通过；跨设备与切换后工具来源待验 |

## Work Units

| ID | 目标 / 轴 | 位置与具体动作 | Resulting Behavior / Benefit | Side Effects：复杂度及影响 | 精确验证 / 安全止步 |
|---|---|---|---|---|---|
| W1 | 收件可行性 / Discovery | 隔离 fixture 验证 V1，记录原生事件与扩展接收结果 | 决定哪条路线可执行，防止重现收件耦合 | 仅测试脚本、假模型、临时记录；不改生产依赖 | V1 全部边界及挂起测试；失败保留证据，清理明确归属资源 |
| W2 | 会话执行权 / Discovery | 原生 TUI fixture 验证 V2/V3 | 确认 resume、当前焦点和来源身份，避免 OpenCode 曾发生的误投 | 两个测试进程与会话；不运行裸 Pi 查询、不触碰正式 profile | 切换、双实例、TTL、来源匹配；不明确归属则不停止该资源 |
| W3 | 原生接入 / 扩展 | 拟新增 `internal/pi/extension.mjs`；绑定 lifecycle、收到记录、模型工具及实例通道 | Pi 作为统一消息收发端，用户保留原生界面 | 增加一个宿主扩展及私有实例状态；不增加 npm 依赖/通用队列 | 原生收件/执行分离、错误不假成功、未知不重放；Q1/Q2 确认且 V1/V2 支持后才做 |
| W4 | 路由接入 / Adapter | 拟新增 `internal/pi/adapter.go`；在 `agentbridge` 增加 AgentPi，在 `cmd/ra2a/registry.go` 注册 | 复用统一发现和投递，不出现成对桥接代码 | 新适配器及 AgentKind；影响注册、来源解析、缺失目标语义 | 本地双向、过期/错会话/重复 owner/ACK 丢失；既有 Adapter 回归 |
| W5 | 启动入口 / CLI | 拟新增 `cmd/ra2a/pi.go` 并接入既有命令；安装专属原生扩展与真实 Pi 参数转发 | 可体验接入，避免改用户配置和官方二进制 | CLI 增加命令与专属扩展文件；保留原生参数及退出码 | 参数/退出码、reload、异常退出、正式文件保护；失败只撤本任务资源 |
| W6 | 交付 / 安装与验证 | Q3/Q4 确认后更新 harness 检测、安装路径、CI 和 README；补全交叉及人工清单 | 支持声明可追溯，安装行为可回退 | 安装器和 CI 范围增加；不把入口实验当自动安装完成 | 真实三平台、全交叉及人工继续；不通过就保持预览/未支持，不发布正式支持 |

## Phases 与代码预算

### P0：接口与原生会话实验

- Work Units：W1、W2；允许完成制定方案所需的隔离可行性验证，未获授权的产品选择不固化。
- 生产代码预算：0 行；fixture 不是生产接入。
- 下一阶段条件：V1–V3 的方向有证据；Q1–Q3 的依赖决策已确认；核对实际预计行数。

#### Pre-Phase Plan Rebase Gate

- Rebase scope：1.0 安装包、0.87 历史实验、1.0 重验、当前 RA2A 实现及剩余 W3–W6。
- Material plan delta：none；旧接口调用返回路线已在首次方案中排除，未执行生产路线。
- Plan delta record：not-required。
- User approval：not-required；Owner 要求继续制定方案。
- Gate status：ready。

### P1：最小本地双向接入（本地验证通过）

- Work Units：W3–W5；**合计新增手写生产代码不超过 500 行**，不是每个单元 500 行。任何单个手写源文件也不超过 500 行。
- 入口：决策与实验门槛完成，重新读当前代码和保护基线。若最小闭环估算超过 500 行，先提出更简方案或申请具体预算，禁止靠拆文件、拆提交或追加阶段绕过限制。
- 退出：隔离 Pi ↔ 本地 control 双向、工作中收件、resume/切换、异常 ACK 不重放通过；原子 commit/push。

#### Pre-Phase Plan Rebase Gate

- Rebase scope：P0 实际证据、Q1–Q3 确认、W3–W6 的边界和预算。
- Material plan delta：material；将安装专属插件前移，保持现有原生配置。
- Plan delta record：D1。
- User approval：user-approved-plan-direct：插件及安装流程说明后，Owner 要求「开始实施ba」。
- Gate status：ready。

### P2：平台、安装与全交叉验收

- Work Units：W6。生产改动预算不预先扩张；根据 P1 实际结果列明剩余改动和预算后再确认阶段范围。
- 自动顺序：相关 Go 单元与 race → 原生隔离 fixture → 三平台与现有宿主保护回归 → 跨设备 Pi 全交叉 → 人工 TUI 可见、输入继续、重启 resume、切换和双实例。
- 单独核验原始收件记录、发送工具返回和业务完成；沿用已验证的 22 条串行样本方法，但各测试只运行有意义的最小批次。出现 unknown/error 停止并保存证据，不自动重试。
- 安装行为未确认或某组合未验收时，不升级正式支持声明。不会因计划完成而自动发布版本。

#### Pre-Phase Plan Rebase Gate

- Rebase scope：P1 实现、全部剩余安装/平台/验收工作与保护证据。
- Material plan delta：pending。
- Plan delta record：pending。
- User approval：pending-if-material。
- Gate status：pending。

## Product Decision Delta 与 Plan Delta

| Phase | Decision Surface | Implemented / Observed Semantics | Authority Coverage | Classification | Required Action |
|---|---|---|---|---|---|
| 前置调研 | 原生接口与配置保护 | 1.0 SDK 调用返回不证明收件，正式文件前后不变 | PI1 / PD32；没有实现新收件语义 | engineering-only | 将事实用于 P0，不能批准 Q1 |
| P1 | 收件、忙时行为、安装 | 明确插件收件、followUp、准确来源、安装/卸载专属文件；本地隔离验证通过 | PI2 / PI3 | covered | 不升级为全平台正式支持 |

D1：原首期仅 ra2a pi；用户明确讨论安装/升级负责插件后授权实施，将专属插件安装前移至 P1，保留正常 Pi 入口和全部既有配置。不增加包装二进制。用户确认见 PI3。

P0 基线：真实 Pi 1.0 PTY A→new→resume A，两个接收记录均属于 A 且原生 entry renderer 在终端实际输出，实例 PID 570248 已退出；证据 .cache/pi-implementation/p0.json 和 p0-terminal.log。后续异常/多实例/来源与 busy 用正式 fixture 继续验证，不以此小实验宣称全量通过。

## 完成和回退

完成必须同时具备：明确的收件边界、正确来源及会话绑定、原生人工交互持续可用、已声明平台的真实原生证据、全交叉准入及配置保护证据。实验和功能通过分别记载。

实验仅停止本次有原生 PID/创建时间/实际 executable 与隔离目录归属证据的进程。只清理本任务拥有的临时文件。上线只更换授权 RA2A 产物及其专属扩展，保留备份和旧入口，不操作官方 Codex/App、Pi 认证或用户会话历史；正式部署另行按已完成成果执行。


## P1 实际交付与 P2 前置核对

- 已实现 `internal/pi` 原生扩展、统一 Adapter、归属保护的安装/卸载；接入 `cmd/ra2a`、harness 检测及四个安装脚本。无新增二进制包装器或 npm 依赖。
- 保守新增生产行数为 371（包含空白、注释和安装脚本变动；预算文件 `.cache/pi-implementation/production-budget.json`），阶段总预算未超过 500，单个新源文件未超过 500。
- 全仓 Go 测试通过；相关四包 race 通过。显式 opt-in 的 Pi 1.0 真实 SDK + Go Adapter + Unix PTY 原生 fixture 通过：22 条收件/独立输入，模型挂起时插件 ACK 约 2ms，原生 TUI 渲染、switch/resume、准确来源与租约到期通过。
- 保护证据 `.cache/pi-implementation/{native,gates,full}-protection.json`；Pi/Codex/OpenCode/RA2A 已检查文件未变化，没有运行正式安装器或启停正式服务。
- `.cache/pi-implementation/` 保留首次 fixture 失败日志：最初假凭据没有注册到所选模型 provider，后更正为专用内存 provider；另核对 1.0 公共 `streamFunction/getAllRegisteredTools/emit` 签名后修正测试夹具。没有通过修改生产收件语义绕过失败。
- P2 仍 pending：真实 Mac/Windows、跨设备目标和全交叉、双真实实例、人工继续、自更新新插件生效链路。安装脚本升级路径与 `ra2a update` 旧进程路径不能混为一谈；见 [runbook](../../runbooks/pi-native-extension.md)。
- 建议发布版本仍未确认，不更新 Version、不创建 tag、不改正式支持声明。
