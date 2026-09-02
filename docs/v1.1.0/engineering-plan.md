# RA2A v1.1.0 工程实施计划

- 状态：ready-for-execution
- 计划日期：2026-09-02
- Product Authority Source：[prd.md](./prd.md)
- Applicable Decisions：PD25、PD26、PD27、PD28、PD29、PD30、PD31、PD32

## 1. 目标

在保持现有 Codex App 能力的前提下，引入 Codex CLI，并建立“统一端点 + 统一消息 + Agent 适配器”的内部架构。路由复杂度应随 Agent 类型数量线性增长，而不是形成 N×N 的成对集成。

## 2. 当前实现基线

当前代码已经存在部分可复用边界，但数据模型和运行时仍是单宿主：

- `cmd/ra2a/main.go` 中存在 `sessionSource` 接口，但 daemon 只启动一个 Codex session source，并在其中混合 App Server 与 Desktop IPC 路由。
- `internal/operator/operator.go` 只有单一 `Codex` 可执行文件配置。
- `internal/lannode/node.go` 的 `Session` 没有 Agent 类型、能力和适配器身份。
- `internal/control/control.go` 默认目标地址为 `ra2a://node/session`，来源也只携带 session ID。
- `internal/mcpserver/server.go` 的工具描述和调用上下文均绑定 Codex session，并依赖 `_meta.threadId`。
- `internal/codexhost/host.go` 直接管理 Codex App Server。

结论：不能把 Codex CLI 作为现有 source 内的额外条件分支。应先把已有 Codex App 行为收口为适配器，再增加 CLI 适配器。

## 3. 目标架构

```mermaid
flowchart LR
    subgraph Callers[本机调用方]
        APP[Codex App MCP]
        CLI[Codex CLI MCP]
    end

    subgraph RA2A[RA2A daemon]
        MCP[MCP 接入层]
        CTX[来源上下文解析]
        ROUTER[统一消息路由器]
        REG[端点注册表]
        PROTO[LAN 协议 / 发现]
        AA[Codex App Adapter]
        CA[Codex CLI Adapter]
    end

    APP --> MCP
    CLI --> MCP
    MCP --> CTX --> ROUTER
    ROUTER <--> PROTO
    ROUTER --> REG
    REG --> AA
    REG --> CA
    AA --> APPHOST[Codex App / Desktop IPC]
    CA --> CLIHOST[Codex CLI / App Server]
```

核心规则：

1. MCP 与 LAN 层只处理统一端点和统一消息，不调用宿主原生 API。
2. 每种 Agent 实现一个适配器；适配器之间不互相引用。
3. 注册表汇总多个适配器的端点，并维护端点到适配器的确定性映射。
4. 路由器只负责本地/远端路由、超时和统一结果，不解释宿主错误。
5. 宿主所有权、writer、UI/TUI 同步和恢复逻辑封装在对应适配器内。

## 4. 建议内部契约

以下为工程候选契约，不属于已确认产品决策；在 D0 实验后收敛字段。

### 4.1 AgentAdapter

```go
type AgentAdapter interface {
    Descriptor() AdapterDescriptor
    ListEndpoints(context.Context) ([]Endpoint, error)
    Deliver(context.Context, EndpointRef, MessageEnvelope) DeliveryResult
    ResolveCaller(context.Context, CallerContext) (EndpointRef, error)
    Health(context.Context) AdapterHealth
    Close() error
}
```

接口只表达当前两类 Codex 宿主共同需要的能力。实验未证明需要的生命周期钩子不得提前加入。

### 4.2 统一端点

候选字段：

- `endpointID`：节点内唯一标识
- `agentType`：如 `codex-app`、`codex-cli`
- `nativeSessionID`：仅适配器解释
- `title`、`status`
- `capabilities`：例如 `receiveText`、`replyAddress`、`interactiveSafe`
- `address`：由 daemon 生成的完整、不透明目标地址

LAN 和 MCP 对外只暴露完成任务所需字段，不暴露 socket、pipe 或 App Server 地址。

### 4.3 统一消息信封

v1.1.0 保持文本消息，候选字段：

- 消息 ID
- 协议版本
- 来源端点地址
- 目标端点地址
- 文本正文
- 创建时间与可选诊断上下文

信封不得包含 Codex App/CLI 专用命令。

### 4.4 统一投递结果

至少区分：

- `delivered`：宿主明确接受消息，并新建 turn 或追加到现有 turn
- `busy`：目标存在但当前不能写入
- `not_found`：端点不存在
- `unreachable`：节点或适配器不可达
- `unsupported`：目标存在但不具备所需能力
- `start_required`：所需本地 Agent 接入服务未运行，调用 Agent 应提示用户确认是否拉起
- `unknown`：请求可能到达，无法确定是否创建 turn

适配器保留原始诊断信息，但不得把宿主异常直接变成跨 Agent 协议。

## 5. 预投资验证

产品决策已经确认。以下实验未通过前，不进入大规模重构；实验负责选择满足 PD29、PD31 的最小技术路线，不得降低产品准入标准。

| ID | 要验证的问题 | 方法 | 通过条件 | 失败后的处理 |
| --- | --- | --- | --- | --- |
| V1 | Codex CLI 调用 MCP 时是否提供稳定 session/thread 身份 | 使用现有 MCP context probe 记录真实调用元数据 | 能稳定映射到当前 CLI session | 若不能稳定识别，则按 PD31 暂不支持 CLI 作为发送端，不伪造来源 |
| V2 | `codex exec resume <id> <prompt>` 能否向空闲 CLI session 精确创建一次 turn | macOS/Linux/Windows 分别执行并核对历史 | 无重复、结果可判定、session 可继续恢复 | 排除 direct-resume 路线 |
| V3 | 外部 resume 对活跃 TUI 的 writer 和刷新有何影响 | TUI 活跃时注入，多轮观察状态和继续输入 | 不抢占 writer、不永久 thinking、TUI 可继续交互 | direct-resume 不作为正式支持路径 |
| V4 | RA2A App Server + `codex --remote` 是否能共享所有权 | 由 RA2A 启动 App Server，TUI remote 接入，多轮交叉投递 | 单一所有者、消息实时显示、人工输入正常 | 重新评估 CLI 支持边界 |
| V5 | App Server 版本变化能否被探测和隔离 | 对当前与最低支持 Codex CLI 做契约测试 | 不兼容时明确报错，不污染路由层 | 增加适配器版本门槛 |
| V6 | 同一节点 App 与 CLI 端点能否无冲突汇总 | 同机同时运行两类宿主并发现 | 地址唯一、类型正确、投递到唯一目标 | 调整端点身份模型 |
| V7 | 单 App Server + remote TUI 能否安全接收 active-turn follow-up | 首条消息触发长时间 turn，在执行期间注入第二条消息并继续人工输入 | follow-up 在同一 thread 中精确执行一次、无重复、TUI 实时更新、人工输入正常 | 不进入 CLI 适配器实现，重新评估活跃 turn 投递入口 |

实验输出写入 `docs/v1.1.0/experiments/`，记录命令、版本、平台、观察结果和结论。只有结论进入架构，原始日志不提交敏感信息。

所有实验先通过 PD32 隔离门禁：使用独立的开发配置、运行目录、日志、控制地址、App Server socket、节点身份和测试会话；不得执行会安装、升级、停止、重启或重新配置本机正式版的命令。启动前检查与正式版的资源冲突，无法确认隔离时立即停止。

当前进度：V1-V4 与 V7 已完成 macOS 首轮验证；V7 证明 CLI active turn 接收 follow-up 时采用同 thread 排队并在当前 turn 后执行的语义。V5、V6 和三平台复验仍未完成。

## 6. 实施阶段

### Phase 0：产品决策与可行性冻结

依赖：通过 PD32 隔离门禁并完成 V1-V7。

工作：

- 以 PD29-PD31 作为启动行为、地址兼容和 Agent 支持门槛。
- 选定满足这些决策的 Codex CLI 所有权路线。
- 将实验结论映射到适配器最小接口。

完成标准：技术路线满足受保护产品决策，并证明不会破坏活跃 TUI、人工继续交互和全交叉支持门槛。

### Phase 1：提取宿主无关核心

主要位置：

- 新增 `internal/agentbridge/`：统一类型、适配器接口、端点注册表。
- 调整 `cmd/ra2a/main.go`：加载多个适配器，不再持有单一 session source。
- 调整 `internal/control/`：通过注册表和路由器投递。
- 将当前 App Server/Desktop IPC 路径收口到 `internal/codexapp/` 适配器；迁移时尽量复用现有代码。

验证：

- 现有 Codex App 单元测试和双设备投递全部通过。
- 保持 v0.0.10 active-turn 语义：空闲消息只 start 一次，执行中的 follow-up 只 steer 一次且进入同一 turn；UI 实时更新、人工可继续，不确定结果不回退或重试。
- 使用内存假适配器证明注册、冲突检测、选择和错误映射。
- 路由层代码中不存在具体宿主类型判断。

回滚边界：此阶段不改变 LAN 协议和公开地址；可以按一个原子提交回退。

### Phase 2：端点协议与混合版本兼容

主要位置：

- `internal/lannode/`：session 表达升级为带 Agent 类型和能力的 endpoint。
- `internal/control/`：解析完整目标地址并选择适配器。
- `internal/mcpserver/`：`list_targets` 返回 Agent 类型、能力和完整地址，工具文案改为 Agent 中立。

要求：

- 明确协议版本和能力字段。
- v1.0 节点与 v1.1.0 节点混用时，不得误投或崩溃。
- 若地址格式变化，提供已确认的兼容读取期；写出统一新格式。

验证：协议编解码、旧新节点矩阵、未知 Agent 类型和未知能力测试。

### Phase 3：Codex CLI 适配器

主要位置：新增 `internal/codexcli/`，具体实现由 Phase 0 选定路线决定。

职责：

- 探测 Codex CLI 版本和支持能力。
- 发现或管理 CLI session。
- 将统一消息精确投递为一个 turn。
- 处理活跃 writer、busy、超时和不确定结果。
- 确保投递后 TUI 可继续人工使用。
- App Server 实验接口变化只影响该适配器和契约测试。

验证：单适配器测试、真实 CLI 冒烟测试、长时间多轮退化测试。

### Phase 4：MCP 来源识别与安装生命周期

主要位置：

- `internal/mcpserver/`：通过适配器解析调用方身份，不再只读取 Codex App thread ID。
- `internal/operator/` 与安装脚本：检测并注册 Codex App、Codex CLI；保持幂等安装、重启和更新。
- 配置迁移：将单一 Codex 路径迁移为可扩展的适配器配置，同时兼容已有安装。

验证：全新安装、v0.0.x 升级、重复安装、卸载/重启，以及三平台路径差异。

### Phase 5：交叉矩阵与退化验证

至少覆盖：

| 发送端 | 接收端 | 基本投递 | active follow-up | 20+ 多轮 | 人工继续 | 断网恢复 | daemon 重启 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| App | App | 必测 | 必测 | 必测 | 必测 | 必测 | 必测 |
| App | CLI | 必测 | 必测 | 必测 | 必测 | 必测 | 必测 |
| CLI | App | 必测 | 必测 | 必测 | 必测 | 必测 | 必测 |
| CLI | CLI | 必测 | 必测 | 必测 | 必测 | 必测 | 必测 |

此外覆盖同机多端点、双设备、三设备和不同 Codex CLI 版本。任何方向出现永久 thinking、writer 抢占、重复 turn 或假成功，均阻止发布。

### Phase 6：文档与发布

- README 支持列表将 Codex CLI 从计划支持改为当前支持。
- 补充 CLI 启动、发现、发送和故障排查说明。
- 记录实验接口兼容范围和最低 Codex CLI 版本。
- 按现有 GitHub Release 流程发布 v1.1.0，附迁移说明和已知限制。

## 7. 工作单元与提交边界

| 工作单元 | 可独立验收结果 | 建议原子提交 |
| --- | --- | --- |
| W0a | V1-V4 首轮所有权实验完成 | `docs(v1.1.0): validate Codex CLI ownership model` |
| W0b | V5-V7、三平台和正式版隔离验证完成，Phase 0 结论冻结 | `docs(v1.1.0): freeze Codex CLI feasibility` |
| W1 | Agent 核心契约与假适配器通过 | `refactor(core): introduce agent adapter boundary` |
| W2 | 现有 Codex App 行为迁入适配器且无回归 | `refactor(codex-app): isolate host integration` |
| W3 | 端点协议与混合版本测试通过 | `feat(protocol): add typed agent endpoints` |
| W4 | Codex CLI 适配器真实投递通过 | `feat(codex-cli): add session adapter` |
| W5 | 安装、MCP 来源识别和配置迁移通过 | `feat(setup): register Codex CLI integration` |
| W6 | 四方向长时间测试和发布文档完成 | `release: prepare v1.1.0` |

不得将架构重构、CLI 新能力和发布元数据压入同一提交。

## 8. 可观测性

只增加能定位兼容边界的结构化字段：

- `adapter_kind`
- `endpoint_id`（避免记录消息正文）
- `delivery_stage`
- `native_error_class`
- `protocol_version`
- `owner_mode`

关键路径应能区分“LAN 未到达、远端路由失败、适配器拒绝、宿主结果未知”，避免统一表现为超时。

## 9. Execution Contract

### 环境前提

- Go 与仓库现有版本要求一致。
- macOS、Linux、Windows 各至少一台真实设备用于宿主验证。
- 测试固定记录 Codex CLI 版本；App Server 为 Experimental，不能只使用 mock 验收。
- 本机正式版视为受保护的外部系统；开发实例使用独立配置、运行目录、日志、控制地址、socket、节点身份和测试会话。
- 验证脚本必须在启动前检查资源归属和冲突，且只能清理本次开发实例创建的资源。
- 不创建新分支，遵守仓库原子提交和推送约束。

### 执行顺序

`Phase 0 → Phase 1 → Phase 2 → Phase 3 → Phase 4 → Phase 5 → Phase 6`

Phase 0 完成并冻结 V1-V7 结论前不进入 Phase 1 主体架构重构。Phase 3 依赖 CLI 所有权路线验证通过。

### 停止条件

出现以下任一情况时停止扩张并回到 Owner：

- 官方可用入口无法在活跃 CLI 中避免 writer/UI 状态破坏。
- 无法在不静默启动的前提下为 `start_required` 提供明确恢复路径。
- 地址兼容方案要求调用方理解或拼接地址内部结构，违反 PD30。
- 为接入 CLI 需要在路由层加入 Agent 对特殊分支。
- 任一交叉方向无法达到 PD31 的准入门槛。
- 开发实例无法与本机正式版的配置、进程、端口、socket、网络身份或宿主会话可靠隔离。
- 单个手写生产代码阶段预计新增超过仓库约束，且没有更小方案。

### 完成定义

- PRD 状态变为 Ready，所有阻塞决策已确认。
- 四方向真实设备测试通过，包含多轮退化和恢复。
- 现有 Codex App 行为无回归。
- 适配器边界通过假第三适配器扩展测试。
- 安装、升级、重启和版本检查在三平台通过。
- 文档、版本号、Git 标签和 GitHub Release 一致。

## 10. Product Decision References

| 决策范围 | 权威决策 | 工程约束 |
| --- | --- | --- |
| Codex CLI 未运行 | PD29 | 返回 `start_required` 和可操作说明；不得静默自动拉起 |
| 目标寻址 | PD30 | 地址由 `list_targets` 完整返回并视为不透明值 |
| Agent 支持准入 | PD31 | 所有已支持 Agent 之间的双向矩阵必须全部通过，否则不发布该适配器 |
| 开发环境隔离 | PD32 | 开发与实验不得变更、重启、停止或占用本机正式版资源；冲突时停止实验 |

## 11. Product Decision Delta

当前无实现后的产品决策差异。

执行过程中若发现原确认决策不可实现，必须记录：受影响 PD、证据、用户影响、建议选项和 Owner 决定。不得把工程限制静默改写为产品行为。
