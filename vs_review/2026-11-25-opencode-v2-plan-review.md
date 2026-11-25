# Subagent VS Review: RA2A OpenCode V2 Engineering Plan

- Created: 2026-11-25
- Updated: 2026-11-25
- Report schema: adversarial-v2
- Task: 在计划阶段审查 RA2A OpenCode V2 完整接入方案，先于生产实现
- Report path: `vs_review/2026-11-25-opencode-v2-plan-review.md`
- Review mode: fresh internal subagents
- Source session policy: no inherited main-agent context; reviewer receives only the review packet
- Status: blocked
- Control outcome: user-decision-required
- Automatic round budget: 2
- User-approved extra rounds: 3
- Completed rounds: 5
- Last known-good checkpoint: `df220f3`

> Scope note（2026-11-25）：用户随后将目标改为“自动接管所有前台会话，V2 不实现 `--auto/--yolo`”。PD6 已 superseded，PD10/PD11 active；R5-B09 随 auto 功能移除而不再适用于当前计划。下方 blocked 记录保留给旧 auto-enabled 方案，不阻塞简化后的自动接管方案。

## Review Control Contract

### Frozen Objective

审查 `docs/releases/v0.0.20/opencode-v2/plan.md` 是否能可靠实现 `docs/opencode-v2/prd.md` 中 PD1–PD9：完整接入 OpenCode V2，同时保留 V1、复用本机官方共享服务、不启动私有 server、不修改官方 remote 配置。

### Acceptance Criteria

- 计划的 Product Authority 唯一且引用正确，不包含未经确认的产品决策。
- 技术方向与官方 V2 API、service、TUI/GUI 插件契约一致。
- 工作单元覆盖生产路径、集成入口、安装/卸载、错误恢复和回滚，不停留在协议或 mock。
- TUI、Desktop、Web 的所有打开 tab 能被可靠发布，多客户端去重，最后关闭撤销。
- 认证、秘密、权限、配置保护和 remote 非干扰边界明确。
- 测试策略能区分 spike、mock、真实集成和人工验收，不把前者冒充后者。
- 运维者和用户能理解插件安装、失败原因和恢复动作。

### Explicit Non-goals

- 不实现生产代码，不执行测试，不启动或重启正式服务。
- 不修改 PD1–PD9 或把新的材料产品选择写入计划。
- 不跟随显式远程 `--server`，不管理官方服务生命周期，不改 remote/pairing。
- 不引入新依赖、公共 API、持久化数据格式或跨模块重构。
- 不审查与本计划无关的既有 Codex/Pi/网络缺陷。

### Frozen Target Locations

- `docs/opencode-v2/prd.md`
- `docs/releases/v0.0.20/opencode-v2/plan.md`
- `internal/ochost/compatibility.go`
- `internal/ochost/host.go`
- `internal/opencode/client.go`
- `internal/opencode/adapter.go`
- `internal/agentbridge/agentbridge.go`
- `internal/agentbridge/registry.go`
- `internal/operator/harness.go`
- `internal/operator/opencode_mcp.go`
- `cmd/ra2a/registry.go`
- `cmd/oc-wrapper/main.go`
- `.github/workflows/release.yml`
- OpenCode V2 官方迁移、API、TUI/GUI/Server Plugin、RPC 文档

### Allowed Change Categories

- 修订 `plan.md` 的工作单元、顺序、验证和回滚边界。
- 修订本审查报告。
- 补充指向官方来源或现有代码的证据。

### Approval-required Changes

- 修改 PRD active 决策。
- 新增顶级模块、外部依赖、公共 API 或持久化数据格式。
- 扩大产品范围或改变 PD1–PD9。
- 执行测试、spike、生产代码或正式服务操作。

### Authoritative Sources

| Authority | Source | What It Controls |
|---|---|---|
| E0 | 2026-11-24 用户对完整 V2、session 发布、busy、权限、remote、插件和 v0.0.20 的直接选择 | 目标、范围和产品取舍 |
| E1 | `docs/opencode-v2/prd.md`、`docs/releases/v0.0.20/opencode-v2/plan.md`、仓库 `AGENTS.md` | 已确认意图、工程约束、测试授权边界 |
| E2 | 本机 OpenCode v2.4.1、官方 service 127.0.0.1:49374、RA2A 60 个 codex-app endpoint/0 个 opencode endpoint、4099 无监听 | 当前实际行为 |
| E3 | OpenCode V2 官方 migration/API/client/plugins/RPC 文档 | 外部协议和平台事实 |
| E4 | 计划中的实现假设与评审推理 | 待验证假设 |

### Baseline And Rollback

- Baseline revision: `df220f3`，加上未提交的 `plan.md`
- Rollback checkpoint: 保留 `df220f3`；删除或还原未提交的 plan/review 文档
- Expected benefit: 形成可执行、可回滚、与 PD1–PD9 一致的 V2 接入计划
- Acceptable side effects: 仅计划与审查文档变化；无运行时、配置、数据或服务变化
- Automatic round budget: 2

## Round 1: Architecture Plan Adversarial Review

### Round Control

- Round type: initial
- Round number: 1
- Completed automatic rounds before launch: 0
- User approval for this round: n/a
- Closure finding IDs: n/a
- Permitted closure relation: n/a
- Target scope delta allowed: none

### Review Input

#### Objective

尝试推翻计划的核心假设：它能否在遵守 PD1–PD9 的前提下，可靠覆盖 OpenCode V2 官方服务、TUI/Desktop/Web 在线状态、安装升级和投递，而不是制造新的服务、权限或配置冲突。

#### Acceptance Criteria

- 见 Review Control Contract。
- 每个 blocking finding 必须给出反例、触发条件、影响、所需证据和证据等级。

#### Explicit Non-goals

- 见 Review Control Contract。
- 不把通用“最佳实践”扩展为新的产品范围。

#### Review Target

架构工程计划与验证策略，不是生产实现。

#### Target Locations

- `docs/opencode-v2/prd.md`
- `docs/releases/v0.0.20/opencode-v2/plan.md`
- 上述 Frozen Target Locations 中的现有代码与官方文档

#### Baseline And Rollback Checkpoint

- Baseline: `df220f3` + 未提交 plan
- Rollback: 删除或还原 plan/review 文档，保留 PRD commit

#### Change Introduction

计划提议新增独立 `internal/opencodev2` package，只读接入本机官方服务；通过 RA2A server/TUI/GUI plugin RPC 维护打开 tab 的短期 presence；将插件资产嵌入主二进制；在 registry/wrapper 中按实际 server 版本选择 V1/V2。

#### Risk Focus

- 三平台官方 service 注册路径、loopback 约束和密码读取是否可靠。
- TUI/GUI 是否能枚举所有打开 tab，并在最后关闭、崩溃、插件重载时正确撤销。
- Server plugin 的内存 lease、TTL、重连和 server restart 是否产生虚假在线或永久离线。
- 显式远程 `--server` 与官方 remote 隧道是否被混淆。
- V2 prompt 的幂等 ID、steer、未知结果对账和错误分类是否足够。
- Plugin package 的 server/TUI/GUI 加载点、构建产物和 `go:embed` 是否可发布。
- 安装器能否保留 JSON/JSONC、其他插件、MCP、认证、模型和 remote 配置。
- 版本探测 fail closed 是否会误伤 V1 或阻塞其他 adapter。
- 工作单元是否真正覆盖生产集成，还是只完成接口、mock 或 spike。

#### User-Perspective Review Focus

- 用户能否理解为什么需要常驻插件、插件禁用时 endpoint 消失、如何恢复。
- 安装失败、配置冲突、服务未启动、认证失败时是否有清晰动作。
- 普通 `opencode`、`--ra2a`、Desktop/Web 的行为是否一致且可预测。

#### Implementation Completeness Focus

- W0–W10 是否覆盖生产代码、集成入口、构建、安装、卸载、registry、wrapper、文档和发布。
- 是否缺少 server plugin 与 daemon 的认证/调用契约、presence 生命周期或 installer 冲突处理。
- 是否把 W1 spike 或 mock 结果当作后续生产完成的充分证据。

#### Target Benefit Focus

- 声称恢复 OpenCode V2 跨设备互通；当前基线是 0 个 OpenCode endpoint。
- 目标：真实 TUI/Desktop/Web 的发现、steer 投递、关闭撤销和 V1 不回归。
- 测量方法：定向测试、隔离真实服务、真实客户端证据和配置保护快照。
- 需要挑战：计划是否包含足够测量，是否可能用 mock 掩盖未接通。

#### Evidence Sources And Gaps

- E0：用户直接确认 PD1–PD9。
- E1：PRD 与计划。
- E2：当前本机服务与 endpoint 状态。
- E3：OpenCode V2 官方文档。
- E4：跨平台路径、真实插件加载、TTL、prompt 收件边界仍需验证。
- Known evidence gap：无生产实现、无测试、无隔离 spike、无三平台真实客户端证据。

#### Assumptions To Attack

- 单一 plugin package 能同时覆盖 TUI、Desktop、Web。
- 官方 service 注册文件在所有支持平台可稳定发现且密码可读。
- 客户端 cleanup 加心跳足以覆盖崩溃、重载、断网和 server restart。
- V2 `{data:...}`、SSE、prompt metadata 和错误形态可按计划映射。
- `go:embed` 与 release 构建能可靠产出 GUI CJS bundle。
- 版本未知时 fail closed 不会破坏现有 V1 用户体验。

#### Adversarial Lenses

architecture、state、failure、security、usability、ease-of-use、comprehension、implementation-completeness、testing、observability、maintenance

#### Verification Status

- 仅文档与只读代码/运行现状核对。
- 未运行测试、spike、构建或服务操作。
- PRD 已提交为 `df220f3`；plan 未提交。

#### Reviewer Instructions

- 使用 fresh internal subagent session，不继承主 Agent 历史、推理或结论。
- 直接读取目标文件，不修改任何文件。
- 尝试反驳计划，而不是确认它。
- 引用文件路径和行号；blocking/scope-expanding claim 标注 E0–E4。
- 每个 blocking finding 给出：broken assumption、failure scenario、trigger、impact、proof needed、evidence authority/source、closure relation、scope effect。
- 输出 summary、blocking findings、non-blocking risks、user-perspective checks、implementation completeness checks、target benefit checks、required fixes、missing tests、missing logs/observability。
- 不执行测试或启动服务。

### Reviewer Timeout Policy

| Complexity | Initial Wait | Extension | Max Attempts Per Role | Blocking Closure Behavior |
|---|---|---:|---:|---|
| complex | 20 minutes observation window | one bounded extension if visibly alive | 2 | cannot pass if review unavailable |

### Reviewer Selection

| Reviewer | Reason Selected | Risk Area |
|---|---|---|
| architecture-adversary | 最高风险是官方服务、插件、daemon 和 installer 的边界与长期可维护性 | 模块边界、依赖方向、生命周期、回滚、跨平台可发布性 |

### Reviewer Launch Records

| Reviewer | Internal Mechanism | Session / Job ID | Trace Source | Context Forked | Input Packet | Context Explicitly Excluded | Read-only |
|---|---|---|---|---|---|---|---|
| architecture-adversary | `subagent` tool | `ses_defff81c8ffe6cJetkFdAAvPCx` | subagent spawn tool call and background-result notification | fork_context=false | Round 1 Review Input | main-agent history, reasoning, failed attempts, drafts, conclusions, full diff | yes |

### Reviewer Timeout Records

| Reviewer Output Key | Reviewer Role | Attempt | Session / Job ID | Waited | Status | Reason | Action |
|---|---|---:|---|---:|---|---|---|
| R1-arch | architecture-adversary | 1 | `ses_defff81c8ffe6cJetkFdAAvPCx` | within complex window | completed | fresh reviewer returned full output | completed |

### Reviewer Outputs

#### R1-arch

##### Summary

- 计划方向与 PRD 一致，V1–V5 预投资门禁方向正确，但当前版本不能直接作为“可可靠实现 PD1–PD9”的执行计划。
- 10 个 blocker 分别是：多 channel service 选择、service 恢复、跨 location presence、TUI tabs off、GUI 混合 server、有效配置/JSONC、真实卸载入口、版本路由、单会话 auto 授权、v0.0.20 版本门禁。
- 另有 10 个 non-blocking risk，覆盖 prompt ID、SSE 重同步、TTL、bundle 构建、legacy cleanup、用户诊断、caller、capability、packaged Desktop 隔离和重复插件加载。
- Reviewer 只读代码、Git 历史、OpenCode v2.4.1 官方文档/源码/OpenAPI；未修改文件、未运行测试、未启动服务。

##### Blocking Findings

| ID | Broken Assumption / Counterexample | Trigger And Impact | Proof Needed | Evidence Authority / Source | Scope Effect |
|---|---|---|---|---|---|
| B01 | 单一 `service.json` 等于用户当前官方 service；实际上 v2.4.1 先看 `OPENCODE_CHANNEL`/默认 channel，再扫描 `service*.json`，并按二进制 owner 或新旧顺序选择 | 多 registration/channel 时连接错误实例，endpoint 为空或发布错误 session；影响 PD1/PD2/PD4/PD7 | 两个真实 loopback registration + channel/owner 选择测试，证明不 start/stop/write service | E3：[discover.rs](https://github.com/anomalyco/opencode/blob/v2.4.1/packages/cli-rs/crates/service/src/discover.rs#L1-L60)；E1：plan:67-81,124,140 | 扩充 W0/W2/W9，不改 PRD |
| B02 | 启动时选一次 adapter + SSE 重连即可恢复；实际上 RA2A 先启动时根本不注册 adapter，service 换 URL/PID/password 后旧 client 也失效 | 开机顺序、端口/密码变更、升级、crash 后必须重启 RA2A；影响 PD1/PD4、AC10 | RA2A 无 service 启动后再出现 service；变更端口/密码后无需重启 RA2A 即恢复 | E2：`cmd/ra2a/registry.go:53-67,143-151`；E3：[Desktop origin switch](https://github.com/anomalyco/opencode/v2.4.1/packages/desktop/src/main/service/background-service.ts#L31-L40)、[client reconnect](https://github.com/anomalyco/opencode/blob/v2.4.1/packages/cli-rs/crates/service/src/connect.rs#L224-L237) | W2/W9 增加常驻 rediscovery |
| B03 | 一次无 location 的 presence RPC 是 server-global；实际上 plugin instance 与 RPC 都是 location-scoped | 不同 project/tab 上报到不同 location，默认 location 只能看到部分 session；影响 PD3、AC1 | 两个 location 同时 report/list/withdraw，验证 union 与路由 | E3：[plugin context](https://opencode.ai/docs/build/plugins/server/#context)、[RPC location](https://opencode.ai/docs/api/#tag-rpc)；E1：plan:87-93,97,143 | lease key、RPC 和聚合纳入 W0/W5/W6/W9 |
| B04 | `context.ui.tabs.list()` 永远可枚举 TUI session；实际上 tabs 可 `mode:"off"`，此时只有当前 session | tabs off 时上报空集合并撤销正常 endpoint；影响 PD3/PD8 | 真实 TUI tabs off 打开/切换/退出，验证单 endpoint 生命周期 | E3：[TUI tabs API](https://opencode.ai/docs/build/plugins/cli/#routes-and-tabs)、[tabs config](https://opencode.ai/docs/cli/config/#tabs) | W6 增加当前 session 回退 |
| B05 | GUI `ctx.sessions.list()` 都属于本机 sidecar；实际上 `SessionRef.server` 可是显式远程 server | Desktop 混合 sidecar/remote tab 时污染 presence 或 404 连带隐藏本机 endpoint；影响 PD3/PD7 | sidecar+remote 混合 tab，验证只上报 sidecar 且不访问 remote URL | E3：[GUI sessions](https://opencode.ai/docs/build/plugins/gui/#sessions-and-the-session-screen)、[server identity](https://opencode.ai/docs/build/plugins/gui/#servers-and-server-events) | W6/W9/W10 增加 server identity 过滤 |
| B06 | 现有 JSON-only、固定 `~/.config` 的 MCP writer 可作为 V2 安全安装器；实际上忽略 XDG/`OPENCODE_CONFIG_DIR`，不处理 JSONC、V2 `mcp.servers` 和双文件优先级 | 安装失败、MCP 未生效、注释/格式丢失或错误覆盖；影响 PD8、AC13 | JSON/JSONC、V1/V2 MCP、XDG/config dir、双文件有效配置矩阵与哈希保护 | E2：`internal/operator/opencode_mcp.go:14-22,48-86,132-159`；E3：[V2 config](https://opencode.ai/docs/migrate-v1/#configuration)、[MCP shape](https://opencode.ai/docs/migrate-v1/#mcp-servers)、[paths.rs](https://github.com/anomalyco/opencode/blob/v2.4.1/packages/cli-rs/crates/local/src/paths.rs#L32-L45) | W8 必须覆盖 `opencode_mcp.go` 与有效配置解析 |
| B07 | 新增 unregister 函数等于卸载入口已接线；实际四个 installer 与 `ra2a exit` 只移除 MCP/Pi 和 legacy server | 卸载后 plugin entry/目录残留，presence RPC 仍可能加载；影响 PD8、AC13 | 三个真实卸载入口隔离验证，只删 RA2A owned 资产 | E2：`install-remote.sh:5-12`、`install-remote.ps1:12-24`、`internal/operator/operator.go:278-302`、`cmd/ra2a/main.go:131-134` | W8/W10 纳入 cmd/operator/四个 installer |
| B08 | 所有版本探测失败直接 error 就不会误伤 V1；实际上 launchd PATH、shim、hang、dev 输出都会失败，直接阻断可用 V1 | V1 用户升级后无法启动；若回退旧路径又可能让未知 V2 进入私有 server；影响 PD1/PD2/PD4、AC11/AC12 | 五输入路由矩阵：V2 registration、实际 V2 info、CLI V1、CLI 未知、无 service | E2：`internal/ochost/compatibility.go:20-38`、`runbooks/opencode-cli-integration.md:12`；E1：plan:42,111-116,147 | W9 先定义证据优先级和路由矩阵 |
| B09 | TUI plugin 自行知道 wrapper 的 `--yolo/--auto` 且只作用当前 session；实际缺少 per-process 授权通道 | 全局 autoaccept 会扩大权限，普通 env 可能被其他 TUI 继承，不传递则静默失效；影响 PD6、AC7/AC8 | 双 session/双 client/tab switch/reload/显式 deny 矩阵 | E2：`cmd/oc-wrapper/main.go:135-145,191-210`；E3：[global session.permissions](https://opencode.ai/docs/cli/config/#sessions)、[permission reply](https://opencode.ai/docs/api/#tag-permission) | W6/W9 定义一次性 per-wrapper 授权 |
| B10 | W10 只更新 Markdown/README 即可发 v0.0.20；实际 `operator.Version` 仍 v0.0.18，release gate 会比较 tag 与二进制版本 | 推送 v0.0.20 tag 直接失败或版本标识错误；影响 PD9 | clean checkout/tag dry-run 中 `ra2a version == v0.0.20` | E2：`internal/operator/operator.go:23`、`.github/workflows/release.yml:19-20`；E1：plan:148,192-198 | W10 增加版本源与 tag gate |

##### Non-blocking Risks

| ID | Risk And Counterexample | Evidence | Impact / Proof |
|---|---|---|---|
| R01 | V2 prompt `id` 必须匹配 `^msg_`，RA2A envelope ID 未必匹配；超时消息可能只在 inbox，不在 history | E3：OpenAPI `Session.Message.ID`、`Session.Inbox.User`；E1：plan:127,141 | 固定 `msg_` 映射、metadata、inbox+分页对账与 unknown 不重放测试 |
| R02 | SSE volatile，断线事件不补发；重连前 busy 状态可能过期 | E3：`/api/event`；E2：`internal/agentbridge/agentbridge.go:91-93` | 重连后用 `/api/session/active` 重同步，未知期间不发布虚假 endpoint |
| R03 | heartbeat/TTL 无数值与撤销 SLO | E1：plan:87,93,203；E3：GUI cleanup | 定义初始 heartbeat/TTL、崩溃撤销时限与抖动测试 |
| R04 | GUI bundle 构建链未锁 Node/Bun、依赖、lockfile、单文件 CJS 与 exports | E2：release workflow；E3：[GUI bundle](https://opencode.ai/docs/build/plugins/gui/#build-the-bundle) | clean CI 可重复构建、bundle hash/exports 校验 |
| R05 | `stop/exit` 和 installer 仍调用 legacy cleanup；owner 仅校验 PID | E2：`internal/ochost/host.go:298-315`、`internal/operator/operator.go:270-292` | V2 路径不得创建 owner 或调用 cleanup；legacy PID reuse 另行跟踪 |
| R06 | 用户只看到日志；Desktop 插件开关只关 GUI，degraded 列表可能返回缓存 endpoint | E3：Desktop plugin control；E2：`internal/control/adapter_coordinator.go:94-120` | 明确 disable/reload/stale 文案与恢复路径；新 doctor CLI 不在本范围 |
| R07 | V2 adapter 未设计 `ResolveCaller`，多 tab 时来源可能匿名 | E2：`internal/agentbridge/caller.go:20-69` | 实现 CallerResolver；唯一在线 session 可解析，否则要求显式 `from` |
| R08 | 未固定 `steerActiveTurn` capability 与 accepted 文案 | E1：PRD:95,121、plan:97,148 | 真实验证后固定 capability 与用户文档 |
| R09 | 隔离 HOME 的 spike 不代表 packaged Desktop userData/channel 加载路径 | E3：[Desktop prepare](https://github.com/anomalyco/opencode/blob/v2.4.1/packages/desktop/src/main/service/background-service.ts#L100-L124) | packaged/isolated 路径分开验证，mock/real 分账 |
| R10 | global plugins 自动发现再加显式 entry 可能重复加载同一 ID | E3：[plugin discovery](https://opencode.ai/docs/plugins/) | 只选一种加载方式，验证 `plugin.list` 单实例 |

##### User-Perspective Checks

- usability/ease of use：多 tab、多 project、多 server 的反例为 B03–B05。
- ease of understanding：`accepted` 与 Desktop 插件开关范围需在 W10 明示（R06/R08）。
- recovery/uninstall/upgrade/permissions：分别受 B02/B07/B08/B09 影响。

##### Implementation Completeness Checks

- W0–W10 均为 `not-started`；没有 landed 生产路径、测试或 runtime 证据。
- mock/spike 只能验证契约，不能证明 service 选择、location、真实客户端加载、配置优先级或权限边界。
- 对应缺口：W0/B01/R04、W1/R09、W2/B01/B02、W3/R01/R08、W4/R02、W5/B03、W6/B03–B05/B09、W7/R04、W8/B06/B07/R10、W9/B02/B05/B08/B09、W10/B10/R06/R08/R09。

##### Target Benefit Checks

- 基线：0 个 OpenCode endpoint；目标：TUI/Desktop/Web 发现、steer admission、最后关闭撤销、V1 不回归。
- 测量：真实客户端 list/send/receipt、进程树与配置哈希、busy/idle 事件、clean CI/tag gate。
- 结果：全部未测量；benefit warning 不单独构成 closure blocker。

##### Required Fixes

- 修正 service 分段选择、legacy owner 安全迁移、V1 冷启动路由和 per-process 授权通道。
- 完成 location/tabs/server/config/uninstall/version/build 门禁，并补齐 caller、capability、用户文案和诊断。

##### Missing Tests

- service 选择/rediscovery、V1→V2 owner 身份、V1 冷启动 vs V2/未知不启动。
- location/tabs/sidecar/remote/多客户端、prompt ID/inbox/409/超时、busy steer。
- nonce allowlist/嵌套 TUI/reload/tab switch/dual client/deny；JSON/JSONC/V2 MCP/五个卸载入口；clean bundle/tag gate。

##### Missing Logs / Observability

- 记录 candidate 选择、rediscovery、plugin lifecycle、presence/endpoint、delivery/permission 和四层用户诊断。
- 禁止记录密码、token、nonce、prompt、消息正文或权限资源正文。

##### Reviewer Conclusion

**blocking**。当前计划可继续调研，但不能作为可可靠实现 PD1–PD9 的执行计划。

## Main-Agent Triage

| Finding | Decision | Evidence Assessment | Response | Scope / Approval |
|---|---|---|---|---|
| B01 | accept | E3 官方 discover.rs 证实多 channel/owner 选择 | W0/W2/W9 实现官方选择算法与测试 | 计划内，无需改 PRD |
| B02 | accept | E2 当前 registry 启动即跳过；E3 官方 service 会换 origin | W2/W9 增加常驻 rediscovery 状态机 | 计划内 |
| B03 | accept | E3 RPC location 参数与 plugin context 明确 | W5/W6/W9 使用 location key、逐 location RPC 与 union | 计划内 |
| B04 | accept | E3 tabs mode off 有明确配置 | W6 增加 router 当前 session 回退 | 计划内 |
| B05 | accept | E3 `SessionRef.server` 与 sidecar id 明确 | W6/W9 只上报 plugin host/sidecar，混合 server 验收 | 计划内 |
| B06 | accept | E2 当前 writer 确实 JSON-only/固定路径；E3 XDG/V2 MCP 契约明确 | W8 覆盖有效配置解析、JSONC 保留、V1/V2 MCP；无安全解析时 fail visibly | 计划内；新增依赖需另行批准 |
| B07 | accept | E2 四个 installer 与 exit 均未调用 plugin unregister | W8/W10 接线真实卸载入口 | 计划内 |
| B08 | accept | E2 fail-open 现状与 E1 AC11/AC12 冲突 | W9 增加版本证据优先级与五输入路由矩阵 | 计划内 |
| B09 | accept | E2 wrapper 现有进程内权限逻辑；E3 全局 autoaccept 范围过大 | W6/W9 使用 per-process 一次性 env/nonce，plugin 仅当前 session | 计划内 |
| B10 | accept | E2 版本常量与 release gate 明确 | W10 增加 v0.0.20 bump 与 tag dry-run | 计划内 |
| R01 | accept | E3 OpenAPI `^msg_` 与 inbox 结构明确 | W3 固定 ID 映射、metadata、inbox/history 对账 | 计划内 |
| R02 | accept | E3 SSE live-only；E2 unknown endpoint 不可发布 | W4 重连 active 快照重同步，未知期间 fail closed | 计划内 |
| R03 | accept | E1 缺数值；E3 cleanup 语义明确 | W5/W6 初始 heartbeat 5s/TTL 15s，测崩溃撤销 SLO | 工程-only |
| R04 | accept | E2 workflow 只有 Go；E3 GUI 必须单文件 CJS | W7 锁构建工具、lockfile、bundle hash/exports | 计划内；新依赖需批准 |
| R05 | accept | E2 legacy cleanup 存在；V2 不创建 owner 时可保证 no-op | W9 保证 V2 不调用/不写 owner；legacy PID reuse 转为独立 ochost 风险 | V2 部分计划内；legacy 硬化 defer |
| R06 | defer | 用户诊断需求有效，但新 doctor CLI 属公共 API 扩展 | W10 用现有输出/runbook 说明 disable/reload/stale；新 CLI 另立 issue | 不扩大本范围 |
| R07 | accept | E2 CallerResolver 是现有扩展点 | W9 实现 V2 ResolveCaller，唯一在线 session 自动解析，否则要求 from | 计划内 |
| R08 | accept | E1 capability/文案尚未固定 | W3/W10 真实验证后声明 steerActiveTurn 与 accepted 语义 | 计划内 |
| R09 | accept | E3 packaged Desktop 有独立 userData/channel | W1/W10 分账 isolated HOME 与 packaged Desktop，未验证则不得 direction-supported | 计划内 |
| R10 | accept | E3 自动发现与显式 entry 可重复 | W7/W8 只选一种加载方式并验证单实例 | 计划内 |

## Plan Revision Response

- 修订文件：`docs/releases/v0.0.20/opencode-v2/plan.md`
- B01：W0/W2/W9 对齐官方 `OPENCODE_CHANNEL`、默认 channel、`service*.json` 扫描与 owner/version 选择。
- B02：新增常驻 OpenCode supervisor；无 service 也注册，2 秒 rediscover，401/断线/PID/URL/password 变化时 revalidate。
- B03：presence 使用 location key；daemon 通过 `/api/debug/location` 枚举 location 并逐 location RPC 聚合。
- B04：TUI tabs off 使用当前 router session 回退。
- B05：Desktop 只上报 sidecar；Web host identity 由 W0/W1 证明，混合远程 tab 不进入本机 presence。
- B06：W8 覆盖 XDG/`OPENCODE_CONFIG_DIR`、JSON/JSONC、V1/V2 MCP、有效配置优先级与安全失败。
- B07：新增 `opencode-plugin-unregister` 并接入 `ra2a exit` 与四个 installer。
- B08：新增六行版本证据路由矩阵；未知证据由 supervisor dormant，不进入 V1 server。
- B09：`--yolo/--auto` 改为 wrapper 子进程一次性 nonce，plugin 仅当前 session 回复 `once`。
- B10：W10 显式 bump `operator.Version` 并验证 v0.0.20 tag gate。
- R01–R10：已纳入 W3–W10；R06 新 doctor CLI defer，R05 legacy PID reuse 另行跟踪。
- 验证：`git diff --check` 通过；计划 305 行；未运行测试、构建、spike 或服务操作。
- Scope：仅计划文档变更；未修改 PRD、生产代码、依赖、公共 API 或持久化格式。

## Review Governor

- Decision: `start-closure-round`
- Reason：Round 1 的 B01–B10 已被计划修订逐项覆盖，证据为 E1/E2/E3；修复限于冻结的计划与验证边界。按自动预算启动唯一一次 focused closure review。
- Accepted blockers: B01–B10
- Unresolved blockers before closure: 0 known in revised plan，待 fresh reviewer 验证
- Scope growth: 仅计划与验证矩阵；无生产代码或产品决策变化
- Closure scope: B01–B10 是否真正关闭，以及修订是否直接引入同一目标回归

## Round 2: Focused Blocking-Closure Review

### Round Control

- Round type: closure
- Round number: 2
- Completed automatic rounds before launch: 1
- User approval for this round: n/a
- Closure finding IDs: B01–B10
- Permitted closure relation: original-blocker-open | fix-regression | direct-adjacent-objective-failure
- Target scope delta allowed: none

### Review Input

- Objective：验证修订后的 `plan.md` 是否关闭 B01–B10，且没有直接破坏 PD1–PD9。
- Target：`docs/releases/v0.0.20/opencode-v2/plan.md`、`docs/opencode-v2/prd.md`、Round 1 报告与相关代码/官方来源。
- Baseline：`df220f3` + 修订后的未提交 plan/review 文档。
- 重点：supervisor 常驻注册与 V1/V2 委托、location presence、tabs off、sidecar/Web host、JSONC/V2 MCP、真实卸载、版本矩阵、nonce 权限、版本门禁。
- 禁止：扩展产品范围、要求第三轮、修改 PRD、执行测试或服务操作。
- Reviewer：fresh architecture-adversary，只读，不继承主会话历史或推理。

### Reviewer Launch Records

| Reviewer | Internal Mechanism | Session / Job ID | Trace Source | Context Forked | Input Packet | Context Explicitly Excluded | Read-only |
|---|---|---|---|---|---|---|---|
| architecture-adversary | `subagent` tool | `ses_defee530dffeW6HBiMA2UsFiuW` | subagent spawn tool call and background-result notification | fork_context=false | Round 2 Review Input | main-agent history, reasoning, drafts, conclusions, full diff | yes |

### Reviewer Timeout Records

| Reviewer Output Key | Reviewer Role | Attempt | Session / Job ID | Waited | Status | Reason | Action |
|---|---|---:|---|---:|---|---|---|
| R2-arch | architecture-adversary | 1 | `ses_defee530dffeW6HBiMA2UsFiuW` | within complex window | completed | fresh closure reviewer returned full output | completed |

### Reviewer Outputs

#### R2-arch

##### Summary

- 结论：blocking。B03–B07、B10 在计划层面关闭；B01、B02、B08、B09 仍未关闭。
- “closed”只表示计划包含可执行设计和验收门禁，不代表运行时已实现或验证。
- 目标收益未测量，当前基线仍为 0 个 OpenCode endpoint；W1/W10 已区分 spike、mock 与真实客户端证据。

##### Blocking Findings

| ID | Closure Relation | Counterexample | Impact | Proof Needed | Evidence |
|---|---|---|---|---|---|
| R2-B01 | original-blocker-open | 计划写成全局 owner 优先；官方是 preferred channel 先返回，只有扫描阶段多 ready 才按 owner/version/newest 选择 | 选错官方实例，endpoint 为空或错误 | W0 官方分段伪代码；W1/W2 preferred-owner、单 ready、pending、version/newest 测试 | E3：`discover.rs:52-73,92-114,187-216,239-253`；E1：plan:82-90,183,185 |
| R2-B02 | direct-adjacent-objective-failure | V1 遗留 owner 的 PID 被 V2 service 或其他进程复用；`CleanupShared` 只验 PID 存活就发信号 | `ra2a exit`/四个 installer 可能终止官方 remote service 或无关进程树 | V1→V2 迁移测试；清理前验证 executable/start time/registration 身份，不匹配则失效 owner 不发信号 | E2：`internal/ochost/host.go:43-48,298-315`、`process_unix.go:20-39`、`process_windows.go:25-42`、`operator.go:278-302`、四个 installer |
| R2-B08 | original-blocker-open | 矩阵要求 V1 `/session` 已运行才进入 V1；但现有 V1 attach 本应在 server 不存在时 `ochost.Start` 冷启动 | 明确 V1 新安装/停止后恢复回归 | 补“明确 V1 + 无 service → 现有 V1 Start”行；V2/未知仍不启动；冷启动对照测试 | E2：`cmd/ra2a/registry.go:70-98`、`cmd/oc-wrapper/main.go:98-124`、`internal/ochost/host.go:103-150`；E1：plan:94-105 |
| R2-B09 | original-blocker-open | TUI JS host `env_clear()` 只继承 allowlist；`RA2A_*` nonce 会被过滤，`OPENCODE_*` bearer 又可能被嵌套 TUI/reload 复用 | `--yolo/--auto` 静默失效或扩大权限 | W0 固定 env 契约；设计可达 plugin 的 per-process 通道，绑定 wrapper/process/session、单次消费、无效显式失败 | E3：`packages/cli-rs/crates/plugin/src/pipe/mod.rs:271-355`；E1：plan:105,124,189,286 |

##### Non-blocking Risks

| ID | Risk | Evidence | Required Response |
|---|---|---|---|
| R2-N1 | 卸载接线已覆盖四个 installer + `ra2a exit`，但验证文本仍写“三个卸载入口” | E1：plan:144,173,191,272 | 统一为四个 installer + `ra2a exit` |
| R2-N2 | release workflow 首个 Go 调用是 tag gate；bundle 必须早于该 `go run` 生成 | E2：`.github/workflows/release.yml:19-20`；E1：plan:151,193 | W7 明确 bundle build 早于 tag gate |

##### Closure Status Table

| ID | Status | Evidence |
|---|---|---|
| B01 | open | plan:82-90；官方 discover 分段算法 |
| B02 | open | plan:86-90,145,289；legacy owner 仅验 PID |
| B03 | closed | plan:109-115,128,134；`/api/debug/location` + location RPC |
| B04 | closed | plan:119-120；tabs off router 回退 |
| B05 | closed（计划门禁） | plan:121-123；sidecar/Web host 过滤 |
| B06 | closed | plan:140-143,183,191；JSONC/XDG/V2 MCP |
| B07 | closed（接线）；计数待修 | plan:144,191,272 |
| B08 | open | plan:94-105；缺 V1 冷启动行 |
| B09 | open | plan:105,124；TUI env allowlist/per-process 绑定缺失 |
| B10 | closed | plan:151,193；版本常量与 tag gate |

##### Required Fixes

- B01：写入 preferred-channel 优先、单 ready、owner/version/newest 分段规则与测试。
- B02：定义 V1→V2 legacy owner 安全迁移/失效，退出和四个 installer 不得终止复用 PID。
- B08：补“明确 V1 + 无 service → 现有 V1 Start”路由行与对照测试。
- B09：重定义可达 TUI plugin 的 per-process 授权通道，绑定 wrapper/process/session、单次消费、显式失败。
- R2-N1/N2：统一五个卸载入口验收；bundle build 早于 tag gate。

##### Missing Tests / Logs

- preferred channel vs owner、单 ready/pending/version fallback；V1→V2 owner 身份不匹配；V1 冷启动 vs V2/未知不启动。
- nonce allowlist、嵌套 TUI、第二 client、reload、tab switch、dual client、显式 deny；四个 installer + `ra2a exit` owned-asset 清理。
- 记录 candidate 选择依据、legacy owner 跳过原因、CLI/API 路由证据、nonce 验证/拒绝原因；不记录密码、nonce 或权限正文。

##### Reviewer Conclusion

**blocking**。Round 2 后仍有 4 个 closure blocker，自动预算耗尽；不得自动开始第三轮。

## Round 2 Main-Agent Triage

| Finding | Decision | Evidence Assessment | Response |
|---|---|---|---|
| R2-B01 | accept | E3 官方源码明确分段算法；计划字面全局 owner 优先不充分 | 需用户批准后修订 W0/W2 与测试矩阵 |
| R2-B02 | accept | E2 cleanup 仅验 PID，升级复用反例可破坏 PD2 | 需用户批准后增加 owner 身份校验/安全失效 |
| R2-B08 | accept | E2 现有 V1 冷启动路径明确；矩阵缺行 | 需用户批准后补 V1 冷启动行 |
| R2-B09 | accept | E3 TUI JS host env_clear/allowlist 明确；nonce 通道未证明 | 需用户批准后重做授权通道与验证 |
| R2-N1 | accept | E1 文本计数不一致 | 统一为四个 installer + `ra2a exit` |
| R2-N2 | accept | E2 workflow 顺序明确 | bundle build 早于 tag gate |

## Final Review Governor

- Decision: `user-decision-required`
- Reason：两轮自动预算已用完；仍有 4 个 admissible closure blocker。按 governor 步骤 1 停止自动修改和第三轮审查。
- Status: `blocked`
- Control outcome: `user-decision-required`
- Blocker count: Round 1 = 10；Round 2 = 4
- Closed in Round 2: B03–B07、B10
- Repeated / new failure class: service/授权契约精度仍未闭合；无证据支持继续自动修补
- Scope growth: 仅文档；无生产代码、依赖、公共 API、数据或配置变化
- Benefit achieved: 计划风险从 10 降到 4；运行时收益未测量
- Last known-good checkpoint: `df220f3`

## Convergence Reflection And User Choices

- 原目标、验收和 non-goals 不变。
- 已触及文件：`docs/opencode-v2/prd.md`、`docs/releases/v0.0.20/opencode-v2/plan.md`、本报告；均为文档。
- 回滚成本：删除或还原未提交 plan/review，保留 PRD commit `df220f3`。
- 可选动作：
  1. 批准按 R2-B01/B02/B08/B09 做四项定点计划修订，并额外批准一次 Round 3 closure review（推荐）。
- User decision：2026-11-25 直接选择上述第 1 项；授权四项定点修订与一次 Round 3 closure review，不授权生产代码、测试或 PRD 变更。
- R2 targeted revision：B01 写入 preferred-first/单 ready/多 ready owner-version-newest 分段算法；B02 增加 legacy owner 身份校验与 stale 隔离；B08 补 V1 冷启动行；B09 改为 0600 capability file + `OPENCODE_RA2A_AUTO_APPROVE_FILE` + native PID 绑定。
- R2-N1/N2：验收统一为四个 installer + `ra2a exit`；bundle build 明确早于 tag gate 的首次 `go run`。
- 验证：`git diff --check` 通过；未运行测试、构建、spike 或服务操作。
  2. 接受当前 4 个风险，直接进入 W0/W1，不再追加审查（不推荐，尤其权限通道未证明）。
  3. 缩小 v0.0.20 范围或调整已确认产品决策，需重新确认 PRD。
  4. 回滚未提交计划与报告，暂停 OpenCode V2 完整适配。

## Round 3: User-Approved Extra Closure Review

### Round Control

- Round type: user-approved-extra
- Round number: 3
- User approval: 2026-11-25 选择“定点修订并加审一轮（推荐）”
- Closure finding IDs: R2-B01、R2-B02、R2-B08、R2-B09；R2-N1/N2 仅检查直接回归
- Permitted closure relation: original-blocker-open | fix-regression | direct-adjacent-objective-failure
- Target scope delta allowed: none

### Review Input

- Objective：验证四项定点修订是否关闭 R2-B01/B02/B08/B09，且不直接破坏 PD1–PD9。
- Target：修订后的 `docs/releases/v0.0.20/opencode-v2/plan.md`、PRD、本报告与相关官方来源。
- Baseline：`df220f3` + 当前未提交 plan/review。
- 禁止：扩展产品范围、修改 PRD、执行测试/构建/spike/服务操作、自动开始 Round 4。

### Reviewer Launch Records

| Reviewer | Internal Mechanism | Session / Job ID | Trace Source | Context Forked | Input Packet | Context Explicitly Excluded | Read-only |
|---|---|---|---|---|---|---|---|
| architecture-adversary | `subagent` tool | `ses_defdc720effegWDlnMgHdxtTjV` | subagent spawn tool call and background-result notification | fork_context=false | Round 3 Review Input | main-agent history, reasoning, drafts, conclusions, full diff | yes |

### Reviewer Timeout Records

| Reviewer Output Key | Reviewer Role | Attempt | Session / Job ID | Waited | Status | Reason | Action |
|---|---|---:|---|---:|---|---|---|
| R3-arch | architecture-adversary | 1 | `ses_defdc720effegWDlnMgHdxtTjV` | within complex window | completed | extra closure reviewer returned full output | completed |

### Reviewer Outputs

#### R3-arch

##### Summary

- R2-B01、R2-B02、R2-B08、R2-N1、R2-N2 在计划层面关闭。
- R2-B09 仍未关闭：capability file 传输路径正确，但注册 ACK、当前 session 撤销和插件缺席显式失败未形成闭环。
- 结论 blocking；不得自动修订或开始 Round 4。

##### Blocking Findings

| ID | Closure Relation | Counterexample | Impact | Proof Needed | Evidence |
|---|---|---|---|---|---|
| R3-B09 | original-blocker-open | plugin 禁用/`OPENCODE_PLUGIN_JS=0`/加载失败时无人注册 capability，wrapper 无 ACK；多 tab 从 S1 切到 S2 时 per-client lease 可能继续批准 S1 pending permission | `--auto` 静默失效或扩大 PD6 权限边界 | wrapper→plugin 注册 ACK/超时、native PID 两阶段发布；lease 绑定 `clientID+nativePID+nonce+expiry+currentSessionID`；切换先撤销旧 session；缺席/失败/过期/身份不匹配可见错误；dual-session/disable/reload/second client/nested/deny 测试 | E1：PRD:128,162,194、plan:111,130-134,181,199,202,296；E2：`cmd/oc-wrapper/main.go:105-150`；E3：[JS host disable](https://github.com/anomalyco/opencode/blob/v2.4.1/packages/cli-rs/crates/plugin/src/pipe/mod.rs#L113-L137)、[env allowlist](https://github.com/anomalyco/opencode/blob/v2.4.1/packages/cli-rs/crates/plugin/src/pipe/mod.rs#L280-L355)、[permission reply](https://opencode.ai/docs/build/plugins/server/#permissions) |

##### Non-blocking Risks

| ID | Risk | Required Response |
|---|---|---|
| R3-N1 | B01 未明确零 ready 时返回首个 pending 的官方 `Missing` 语义 | W0/W2 记录并测试 |
| R3-N2 | 现有 `StartedAt` 仅秒级，不是 OS 进程启动身份 | W9 使用真实 process start identity；无法证明则隔离 |
| R3-N3 | 建议增加 `CLI=V1 + 实际 V2` 冲突用例 | W9 测试实际 V2 证据优先 |

##### Closure Status Table

| ID | Status | Evidence |
|---|---|---|
| R2-B01 | closed | plan:82-87,179,195 |
| R2-B02 | closed | plan:95,155,202,299 |
| R2-B08 | closed | plan:99-107,202 |
| R2-B09 | open | plan:111,130-134,181,199,202,296 |
| R2-N1 | closed | plan:154,183,201,282 |
| R2-N2 | closed | plan:161,200；release workflow:19-20 |

##### Required Fixes / Missing Tests / Logs

- W6/W9 增加 capability 注册 ACK、超时、native PID 两阶段发布、currentSessionID 绑定与路由切换撤销。
- plugin 缺席、JS host 关闭、加载失败、注册超时、过期、身份不匹配时 wrapper 可见失败并清理 capability。
- 测试 `OPENCODE_PLUGIN_JS=0`、plugin disabled/load failure、ACK timeout、PID 延迟、expiry、dual-session switch、second client、nested TUI、reload、deny。
- 记录 wrapper auto 请求、ACK/超时、lease session 切换/撤销/拒绝、legacy owner 跳过和 CLI/API 路由证据；不记录 nonce/密码/token/正文。

##### Reviewer Conclusion

**blocking — R2-B09 仍未关闭。** 不能自动开始修订或 Round 4，需用户明确决策。

## Round 3 Main-Agent Triage

| Finding | Decision | Evidence Assessment | Response |
|---|---|---|---|
| R3-B09 | accept | E1/E2/E3 支持注册 ACK、session 绑定和显式失败缺口 | 需用户批准后补 W6/W9 状态机与测试 |
| R3-N1 | accept | 官方 pending 语义需显式记录 | 纳入 W0/W2 |
| R3-N2 | accept | 现有 StartedAt 不足以证明 OS 进程身份 | 纳入 W9 |
| R3-N3 | accept | 实际 V2 证据优先需冲突测试 | 纳入 W9 |

## Final Review Governor

- Decision: `user-decision-required`
- Reason：用户额外批准的 Round 3 已完成，仍有 1 个 admissible closure blocker；不得自动修订或开始 Round 4。
- Status: `blocked`
- Control outcome: `user-decision-required`
- Blocker count: Round 1 = 10；Round 2 = 4；Round 3 = 1
- Closed in Round 3: R2-B01、R2-B02、R2-B08、R2-N1、R2-N2
- Scope growth: 仅文档；无生产代码、依赖、公共 API、数据或配置变化
- Benefit achieved: 计划 blocker 从 10 → 4 → 1；运行时收益未测量
- Last known-good checkpoint: `df220f3`

## Convergence Reflection And User Choices

- 原目标、验收和 non-goals 不变。
- 已触及文件：PRD、plan、review；均为文档。
- 回滚成本：删除或还原未提交 plan/review，保留 PRD commit `df220f3`。
- 可选动作：
  1. 批准 W6/W9 定点补齐 capability 注册/撤销/显式失败契约与测试，并额外批准一次 focused review（推荐）。
- User decision：2026-11-25 直接选择上述第 1 项；授权 W6/W9 定点修订与一次 Round 4 focused review，不授权生产代码、测试或 PRD 变更。
- R3 targeted revision：wrapper 两阶段写 native PID；TUI plugin 写 registered/rejected ACK；wrapper 10 秒超时后 aborted 并可见失败；server lease 绑定 currentSessionID，路由切换先撤销旧 session；permission 只经 server autoReply。
- 验证：`git diff --check` 通过；未运行测试、构建、spike 或服务操作。
  2. 明确接受 `--auto` 在插件不可用或路由切换边界可能静默失效/扩大权限的风险。
  3. 暂停或回滚当前计划。

## Round 4: User-Approved Focused Review

### Round Control

- Round type: user-approved-extra
- Round number: 4
- User approval: 2026-11-25 选择“补齐并再审一轮（推荐）”
- Closure finding IDs: R3-B09（原 R2-B09）
- Permitted closure relation: original-blocker-open | fix-regression | direct-adjacent-objective-failure
- Target scope delta allowed: none

### Review Input

- Objective：验证 capability 请求/ACK、currentSessionID 撤销与插件缺席显式失败是否关闭 R3-B09。
- Target：修订后的 `plan.md`、PRD、本报告与相关官方来源。
- Baseline：`df220f3` + 当前未提交 plan/review。
- 禁止：扩展产品范围、修改 PRD、执行测试/构建/spike/服务操作、自动开始 Round 5。

### Reviewer Launch Records

| Reviewer | Internal Mechanism | Session / Job ID | Trace Source | Context Forked | Input Packet | Context Explicitly Excluded | Read-only |
|---|---|---|---|---|---|---|---|
| architecture-adversary | `subagent` tool | `ses_defbe17e7ffeGlKF0d3ghzhL61` | subagent spawn tool call and background-result notification | fork_context=false | Round 4 Review Input | main-agent history, reasoning, drafts, conclusions, full diff | yes |

### Reviewer Timeout Records

| Reviewer Output Key | Reviewer Role | Attempt | Session / Job ID | Waited | Status | Reason | Action |
|---|---|---:|---|---:|---|---|---|
| R4-arch | architecture-adversary | 1 | `ses_defbe17e7ffeGlKF0d3ghzhL61` | within complex window | completed | capability closure reviewer returned full output | completed |

### Reviewer Outputs

#### R4-arch

##### Summary

- ACK、currentSessionID、autoReply、reload/双 client/嵌套 TUI 等计划层设计已覆盖。
- 但 server lease 注册成功、ACK 延迟/丢失时，wrapper timeout 只标记本地请求，没有原子撤销 lease；`autoReply` 可能在 expiry 前继续批准。
- 结论 blocking，R3-B09 仍未关闭。

##### Blocking Findings

| ID | Closure Relation | Counterexample | Impact | Proof Needed | Evidence |
|---|---|---|---|---|---|
| R4-B09 | original-blocker-open | register 成功→ACK 丢失→wrapper timeout/aborted→lease 仍有效→pending permission 经 autoReply 批准 | 用户看到 auto 关闭但权限仍自动批准，违反 PD6/AC8 | pending→registered 与 rejected/aborted/expired 状态机；register 成功后才写 registered ACK；terminal 必须 withdraw；server register/autoReply 拒绝 terminal/revoked；register+ACK 丢失故障注入 | E1：PRD:128,162,194、plan:130-137,201-202,205,299；E3：[permissions](https://opencode.ai/docs/build/plugins/server/#permissions)、[RPC](https://opencode.ai/docs/build/plugins/rpc/#call)；E4：竞态推断 |

##### Non-blocking Risks

| ID | Risk | Required Response |
|---|---|---|
| R4-N1 | `nativePID=""` 阶段立即拒绝会误失败 | 明确 pending/retry 与最终 timeout |
| R4-N2 | ACK sidecar 非原子写入可能被误读超时 | ACK 原子写入；fail closed |

##### Closure Status Table

| Check | Status | Evidence |
|---|---|---|
| 两阶段 PID、ACK、10 秒 timeout、plugin 缺席失败 | 计划层覆盖 | plan:131-133,205 |
| lease 绑定、session 切换撤销、autoReply 复核 | 计划层覆盖 | plan:134-135,201-202 |
| reload/第二 client/嵌套/身份/expiry/deny | 计划层覆盖 | plan:136-137,202 |
| abort/rejected 后撤销已注册 lease，迟到 register 不复活 | open | plan:133-136,201-202 |
| R3-B09 总体 | open | plan:130-137,201-202,205,299 |

##### Required Fixes / Missing Tests / Logs

- 明确 capability `pending/registered/rejected/aborted/expired/revoked` 状态机；registered ACK 只能在 server register 成功后写。
- timeout/rejected/expiry 必须 withdraw；server register/autoReply 拒绝 terminal/revoked；迟到 plugin 不得复活。
- withdraw 失败保持 fail closed；nativePID 空值 pending/retry；ACK 原子写入。
- 故障注入：register 成功+ACK 丢失、rejected、PID 延迟、S1→S2 withdraw 失败、plugin disabled/JS host off/load failure、双 client/嵌套/reload/deny。
- 记录 terminal reason、withdraw 请求/确认/失败、late register 拒绝、autoReply revoked/session mismatch；不记录 nonce/密码/token/正文。

##### Reviewer Conclusion

**blocking — R3-B09 仍未关闭。** 无法自动继续生产实现或 Round 5，需用户明确决策。

## Round 4 Main-Agent Triage

| Finding | Decision | Evidence Assessment | Response |
|---|---|---|---|
| R4-B09 | accept | E1/E3 支持状态机缺口；E4 竞态需 W1/W9 故障注入 | 需用户批准后补 W5/W6/W9 terminal/withdraw 语义 |
| R4-N1 | accept | nativePID 两阶段可用性风险 | 纳入 W0/W1/W6 |
| R4-N2 | accept | ACK 原子写入可用性风险 | 纳入 W6/W9 |

## Final Review Governor

- Decision: `user-decision-required`
- Reason：用户额外批准的 Round 4 已完成，仍有 1 个 admissible closure blocker；不得自动修订或开始 Round 5。
- Status: `blocked`
- Control outcome: `user-decision-required`
- Blocker count: Round 1 = 10；Round 2 = 4；Round 3 = 1；Round 4 = 1
- Scope growth: 仅文档；无生产代码、依赖、公共 API、数据或配置变化
- Benefit achieved: 计划 blocker 10 → 4 → 1 → 1；竞态范围缩小但未关闭
- Last known-good checkpoint: `df220f3`

## Convergence Reflection And User Choices

- 原目标、验收和 non-goals 不变。
- 当前剩余风险集中在 `--auto` 的 ACK/lease 竞态；默认权限与核心 V2 投递不受此 blocker 影响。
- 可选动作：
  1. 采用 fail-fast：ACK 超时时 wrapper 终止本次自己启动的 TUI，并补 terminal/withdraw 状态机；额外批准一次 focused review（推荐）。
- User decision：2026-11-25 直接选择上述第 1 项；授权 fail-fast + terminal/withdraw 定点修订与一次 Round 5 focused review，不授权生产代码、测试或 PRD 变更。
- R4 targeted revision：新增 pending/ready/registered/rejected/aborted/expired/revoked 状态机；register 成功后才写 ACK；timeout/rejected 撤销 lease 并终止自有 TUI；server 拒绝 terminal/无心跳/session 不匹配；ACK 原子写入。
- 验证：`git diff --check` 通过；未运行测试、构建、spike 或服务操作。
  2. v0.0.20 暂不支持 V2 `--yolo/--auto`，保留默认 OpenCode 权限；这需要修改 PRD PD6 并重新确认。
  3. 明确接受“显示 auto 关闭但仍可能自动批准”的竞态风险。
  4. 暂停或回滚当前计划。

## Round 5: User-Approved Fail-Fast Closure Review

### Round Control

- Round type: user-approved-extra
- Round number: 5
- User approval: 2026-11-25 选择“Fail-fast 并再审（推荐）”
- Closure finding IDs: R4-B09（原 R3-B09 / R2-B09）
- Permitted closure relation: original-blocker-open | fix-regression | direct-adjacent-objective-failure
- Target scope delta allowed: none

### Review Input

- Objective：验证 fail-fast + capability terminal/withdraw 状态机是否关闭 ACK/lease 竞态。
- Target：修订后的 `plan.md`、PRD、本报告与相关官方来源。
- Baseline：`df220f3` + 当前未提交 plan/review。
- 禁止：扩展产品范围、修改 PRD、执行测试/构建/spike/服务操作、自动开始 Round 6。

### Reviewer Launch Records

| Reviewer | Internal Mechanism | Session / Job ID | Trace Source | Context Forked | Input Packet | Context Explicitly Excluded | Read-only |
|---|---|---|---|---|---|---|---|
| architecture-adversary | `subagent` tool | `ses_def8b520affevGiLzKer7GvCSm` | subagent spawn tool call and background-result notification | fork_context=false | Round 5 Review Input | main-agent history, reasoning, drafts, conclusions, full diff | yes |

### Reviewer Timeout Records

| Reviewer Output Key | Reviewer Role | Attempt | Session / Job ID | Waited | Status | Reason | Action |
|---|---|---:|---|---:|---|---|---|
| R5-arch | architecture-adversary | 1 | `ses_def8b520affevGiLzKer7GvCSm` | within complex window | completed | fail-fast closure reviewer returned full output | completed |

### Reviewer Outputs

#### R5-arch

##### Summary

- 状态机、ACK 顺序、timeout 终止 TUI、withdraw 调用已写入计划。
- 但 withdraw 失败时本地 terminal 不等于 server lease terminal；已进入 server 的 autoReply 仍可能完成 permission.reply。
- late/in-flight register 也可能在 withdraw no-op 后提交 lease。
- 结论 blocking，R4-B09 未关闭。

##### Blocking Findings

| ID | Closure Relation | Counterexample | Impact | Proof Needed | Evidence |
|---|---|---|---|---|---|
| R5-B09 | original-blocker-open | register 成功→ACK 丢失→autoReply 已通过校验→wrapper timeout/aborted→withdraw 失败→in-flight permission.reply 继续批准；或 withdraw no-op 后 late register 提交 | 用户看到 auto 失败退出但权限仍被批准，违反 PD6/AC8 | server 可见 terminal/tombstone；abort/withdraw/setSession 与 autoReply 线性化；terminal 后不得再批准；caller liveness 证明或移除该假设；故障注入 | E1：PRD:128,162,194、plan:131-138,176,202-206,275；E3：[RPC context](https://github.com/anomalyco/opencode/blob/v2.4.1/packages/core/src/rpc.ts#L42-L44)、[Promise RPC](https://github.com/anomalyco/opencode/blob/v2.4.1/packages/plugin/src/promise/rpc.ts#L7-L16)、[permission.reply](https://github.com/anomalyco/opencode/blob/v2.4.1/packages/core/src/permission.ts#L334-L343)；E4：交错竞态 |

##### Non-blocking Risks

| ID | Risk | Required Response |
|---|---|---|
| R5-N1 | 缺 autoReply 拒绝原因与 TUI 进程组终止结果日志 | 补结构化 reason 与终止结果 |
| R5-N2 | 15s TTL 不能替代立即 revocation | 不得以 TTL 作为 fail-closed 证明 |

##### Closure Status Table

| Check | Status | Evidence |
|---|---|---|
| capability 状态、ACK 顺序、原子写入 | 计划层覆盖 | plan:131-133,203 |
| timeout terminal/withdraw/终止 TUI/可见失败 | partial | plan:134,206 |
| withdraw 失败仍立即 terminal | open | plan:134 |
| autoReply 与 terminal/withdraw 线性化 | open | plan:134,136 |
| late/in-flight register 不复活 | partial | plan:137 |
| S1→S2 withdraw 失败与旧 in-flight reply | partial | plan:135-136 |
| R4-B09 总体 | open | plan:130-138,202-206 |

##### Required Fixes / Missing Tests / Logs

- 建立独立于 withdraw RPC 成功的 server 可见 terminal/tombstone，并定义 autoReply 与 terminal 的线性化顺序。
- 证明 caller liveness 或移除该假设；terminal 后不得再 permission.reply；late/in-flight register 拒绝。
- 故障注入：register+ACK 丢失+withdraw 失败、autoReply 校验后暂停、withdraw no-op 后 late register、S1→S2 withdraw 失败、TUI 进程组终止。
- 记录 terminal/withdraw/late register/autoReply reason 与 TUI 终止结果；不记录 nonce/密码/token/正文。

##### Reviewer Conclusion

**blocking — R4-B09 未关闭。** 无法自动继续生产实现或 Round 6；需用户选择继续实现 server fence，或回退 V2 auto 不可用并重新确认产品决策。

## Round 5 Main-Agent Triage

| Finding | Decision | Evidence Assessment | Response |
|---|---|---|---|
| R5-B09 | accept | E1/E3 支持 server fence 与线性化缺口；E4 竞态需故障注入 | 需用户决定继续复杂化或回退 V2 auto |
| R5-N1 | accept | 日志缺口 | 纳入后续方案 |
| R5-N2 | accept | TTL 不能证明立即撤销 | 纳入后续方案 |

## Final Review Governor

- Decision: `user-decision-required`
- Reason：用户额外批准的 Round 5 已完成，仍有 1 个 admissible closure blocker；不得自动修订或开始 Round 6。
- Status: `blocked`
- Control outcome: `user-decision-required`
- Blocker count: Round 1 = 10；Round 2 = 4；Round 3 = 1；Round 4 = 1；Round 5 = 1
- Scope growth: 仅文档；无生产代码、依赖、公共 API、数据或配置变化
- Benefit achieved: blocker 10 → 4 → 1 → 1 → 1；权限竞态未收敛
- Last known-good checkpoint: `df220f3`

## Convergence Reflection And User Choices

- 原目标、验收和 non-goals 不变。
- 剩余风险只在 V2 `--auto`；核心 service 发现、presence、投递、安装和 V1 路径已通过计划层审查。
- 继续实现 server fence 会显著增加并发状态、故障注入和运维复杂度，且不能保证自然收敛。
- 可选动作：
  1. v0.0.20 的 V2 暂不支持 `--yolo/--auto`，只遵循 OpenCode 默认权限；修改 PRD PD6、简化计划，再做一次简化方案的 focused review（推荐）。
  2. 继续实现 server 可见 terminal fence 与 autoReply 线性化，并额外批准一次 focused review。
  3. 明确接受“auto 失败后仍可能批准”的竞态风险。
  4. 暂停或回滚当前计划。

## Convergence And Closure

- Completed rounds: 2 automatic + 3 user-approved extra = 5
- Closure status: blocked
- Unresolved blocking findings: R5-B09（原 R4-B09 / R3-B09 / R2-B09）
- Tests or validations run: 无；仅只读代码、官方来源和文档检查
- Rollback checkpoint: `df220f3`

## Scope Redefinition Closure

- E0 user decision：2026-11-25 用户要求普通 OpenCode 用法不变，并选择“自动接管所有前台会话”；明确不再折腾 RA2A auto。
- Product authority change：PRD PD6 superseded；PD10（自动接管、无需后缀）与 PD11（V2 不实现 auto）active。
- R5-B09 disposition：随 V2 auto 整体移除而 `superseded-by-scope-change`，不是接受风险，也不是实现修复。
- Current plan：删除 capability/ACK/authorization lease/autoReply/fence，仅保留 service discovery、presence、session 投递、安装与 V1 兼容。
- Current unresolved blocker：无；旧报告的 blocked 状态仅描述旧 auto-enabled target。
- Additional review：用户未要求继续审查；后续如进行实现，仍按仓库规则申请定向测试。
