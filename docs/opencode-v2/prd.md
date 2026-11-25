# PRD：RA2A OpenCode V2 完整接入

- 状态：Ready for implementation
- 创建 / 更新：2026-11-25
- 版本：独立纳入 v0.0.20；不阻塞当前 v0.0.19 目标
- 发起人：Owner
- 原始诉求：修复 OpenCode 升级 V2 后 RA2A 无法兼容、并与官方远程服务冲突的问题
- 产品权威：本文件 `Confirmed Product Decisions`

## Requester Review Summary

- 已确认：完整适配 OpenCode V2，保留 V1 路径；RA2A 复用本机官方共享服务，不启动私有 server，不修改官方 remote/pairing 配置。
- 已确认：自动接管 TUI、Desktop、Web 中所有打开 tab 的 session，无需用户添加后缀；同一 session 只发布一个 endpoint，最后一个客户端关闭后撤销。
- 已确认：没有客户端打开 session 时不启动官方服务、不发布 endpoint；忙碌消息立即 steer。
- 已确认：安装并维护 V2 TUI+GUI 在线状态插件；插件被禁用时，对应客户端不发布 endpoint。
- 已确认：V2 不提供 RA2A `--yolo` / `--auto`，权限完全遵循 OpenCode 默认规则。
- 实施前必须确认：无阻塞产品决策；测试范围仍需按仓库规则另行申请。
- 状态原因：影响用户可见行为的接入范围、生命周期、投递和权限规则均已直接确认。

## 1. Background And Product Intent

RA2A 当前对 OpenCode V1 的接入采用“RA2A 启动并监督一个 `opencode serve`，TUI 再 attach 到该实例”的模型。OpenCode V2 将 Server API、Client、Plugin API 和后台服务生命周期改为破坏性变更：

- V1 的 `/session`、`/event` 等路径不再存在，V2 使用 `/api/session`、`/api/event`。
- V2 响应结构和认证契约变化，旧 HTTP 客户端不能直接复用。
- V2 默认由一个官方共享后台服务拥有 session、认证和 remote/pairing。
- V1 TUI focus 插件不能直接在 V2 运行。

当前仓库已实施 V2 门禁：普通 `opencode` 透传原生启动，daemon 跳过 V2 adapter。该修复解决了启动报错，但没有恢复 OpenCode V2 的 session 发现和跨设备投递。

本 PRD 要恢复的是：用户正常使用 OpenCode V2 时，其他 RA2A Agent 能发现其已打开 session 并定向发送消息，同时官方共享服务和远程接入保持由 OpenCode 自己管理。

## 2. Goals And Success Criteria

### 目标

1. OpenCode V2 的 TUI、Desktop 和 Web 已打开 session 能被本机 RA2A 发布。
2. 其他节点可通过 `list_targets()` 发现这些 endpoint，并通过 `send_message()` 投递文本。
3. 目标 session 能明确接收消息；工作中投递使用 steer，不终止当前工作。
4. 最后一个客户端关闭 session 后，endpoint 被撤销，不能继续误投。
5. 官方共享服务、remote/pairing、认证、模型和用户配置不被 RA2A 改写。
6. OpenCode V1 现有行为不回归。

### 成功标准

- 用户无需改变 OpenCode 命令或添加 `--ra2a`；正常 `opencode` 及 Desktop/Web 中打开的 session 自动发布。
- 在真实 OpenCode V2 环境中完成 TUI、Desktop、Web 的打开、发现、投递、关闭验证。
- 忙碌、多客户端、权限、服务中断、版本升级和 remote 开启状态均有明确行为与证据。
- `accepted` 只表示宿主已接收消息，不被宣称为模型执行或任务完成。
- 未取得明确收件结果时不自动重放，避免重复消息。
- 全平台与全 Agent 交叉验收完成前，OpenCode V2 继续保持 Preview，不提前声明正式支持。

## 3. Users And Usage Context

- **RA2A 用户**：在可信局域网内运行 RA2A，使用 OpenCode V2 终端、Desktop 或 Web 客户端。
- **发送方 Agent**：通过 RA2A MCP 的 `list_targets()` 和 `send_message()` 与其他设备协作。
- **目标 OpenCode session**：作为接收文本、执行任务和可选回复的宿主。
- **运维者**：安装、升级和排查 RA2A；需要明确区分 RA2A daemon、OpenCode 官方服务和客户端在线插件。

## 4. Scope

### In Scope

- OpenCode V2 本机官方共享服务发现与只读接入。
- V2 HTTP API、认证、SSE 事件和 session 状态适配。
- TUI、Desktop、Web 客户端在线状态插件及安装、升级、卸载。
- 所有打开 tab 的 session 发布、多客户端去重和最后关闭撤销。
- V2 session 消息投递、busy steer、收件确认和未知结果处理。
- V2 权限完全遵循 OpenCode 默认规则，不实现 RA2A 自动批准。
- V1/V2 版本识别与实际 server 版本校验。
- 安装器、文档、预检和 v0.0.20 发布准备。

### Out Of Scope

- RA2A 启动、停止、重启或配置 OpenCode 官方后台服务。
- 修改官方 remote/pairing、密码、CORS、hostname、port 或隧道配置。
- 跟随 `opencode --server <远程URL>` 连接远程服务；RA2A 节点仍只代表本机官方服务。
- 发布没有客户端打开的历史、后台或 headless session。
- V2 `--yolo` / `--auto` 自动批准、capability/ACK/授权 lease 或权限 fence。
- 删除 V1 接入路径。
- 文件传输、工作流编排、公网中继或新的任务模型。

## 5. Core User Journey

1. 用户照常执行 `opencode`，或正常打开 Desktop/Web，无需添加 `--ra2a`；打开一个或多个 session tab。
2. RA2A V2 客户端插件枚举本客户端打开的 session，向本机 RA2A 上报在线状态。
3. RA2A daemon 从官方共享服务读取 session 摘要，按 `ra2a://<node-id>/<session-id>` 发布 endpoint。
4. 其他节点通过 `list_targets()` 看到这些 OpenCode endpoint。
5. 发送方调用 `send_message()`；RA2A 再次确认 session 仍在前台打开，再以 steer 方式提交。
6. OpenCode 明确接收后返回 `accepted`；消息出现在目标 session 中，模型是否完成不作为收件条件。
7. 目标 Agent 可继续通过 RA2A MCP 向来源地址回复。
8. 用户关闭该 session 的最后一个客户端 tab 后，插件清理或心跳失效，endpoint 被撤销。

## 6. Interaction And Information Design

- `list_targets()` 对每个在线 OpenCode session 返回一个 endpoint，不按客户端数量重复。
- endpoint 的 agent 类型为 `opencode`；能力声明必须以真实验证为准，不使用未证明的 `interactiveSafe` 或忙碌安全承诺。
- session 标题、忙碌状态和在线状态来自官方服务与在线插件的合并结果。
- 官方服务不可达、认证失败或版本未知时，不发布虚假的可投递 endpoint；已有结果按现有 stale/degraded 规则明确标记。
- 在线插件只上报本机客户端已打开 session 与清理状态，不传输 prompt、消息正文、权限策略或模型输出。

## 7. Product Rules And State Logic

### 服务与版本

- RA2A 只连接本机官方共享服务注册的 URL。
- 没有官方服务或没有在线客户端时，不启动服务、不创建后台 session、不发布 endpoint。
- 版本判断以实际官方 server 信息为准；探测失败时不得把 V2 误当 V1 继续旧路径。
- OpenCode 原地升级或官方服务重启后，RA2A 应重新发现并恢复适配，不要求用户修改 RA2A 配置。

### Session 在线与归属

- 一个 session 只要在 TUI、Desktop 或 Web 中仍有打开的 tab，就保持发布。
- 后台 tab 也属于“已打开”；当前可见 tab 不是唯一发布条件。
- 多个客户端打开同一 session 时只发布一个 endpoint，消息只写入一次。
- 最后一个 tab 关闭、窗口退出、插件卸载或心跳超时后撤销 endpoint。
- 历史 session、归档 session、已删除 session 和纯后台执行 session 不发布。

### 消息投递

- 空闲与忙碌 session 都使用 V2 prompt admission，忙碌时显式选择 steer。
- 每条消息保留 RA2A 来源和唯一 message ID；重试同一 ID 不得产生重复消息。
- `accepted` 仅代表官方服务已接收该消息，不代表模型开始、完成或回复。
- 请求结果未知时返回 `DELIVERY_UNKNOWN`，不自动重发。
- 目标离线、session 已关闭或服务不可达时，返回明确的不可投递错误。

### 权限

- V2 完全遵循 OpenCode 的权限规则和用户交互，RA2A 不监听、不转发、不自动批准权限请求。
- V2 不提供 RA2A `--yolo` / `--auto`；wrapper 遇到这些旧 RA2A 参数时明确提示暂不支持，不改变权限行为。
- 为兼容旧调用，V2 `--ra2a` 可作为无操作标记剥离；是否添加它都不改变自动接管结果。
- RA2A 不保存、复制或打印官方服务密码、OAuth 凭据和 token。

## 8. Edge Cases, Errors, And Recovery

- **官方服务未运行**：OpenCode endpoint 不发布；原生 OpenCode 仍可自行启动服务。
- **官方服务 remote 已开启**：RA2A 只使用本机注册 URL，不读取或改写 remote route，不干扰官方远程接入。
- **用户使用 `--server <远程URL>`**：RA2A 不跟随该远程服务；本机官方服务没有在线 tab 时不发布本节点 OpenCode endpoint。
- **插件被禁用或加载失败**：对应客户端不发布 endpoint，其他 Agent 类型不受影响。
- **客户端崩溃**：心跳超时后撤销 endpoint；不长期保留虚假在线状态。
- **同一 session 多客户端**：单 endpoint；关闭其中一个客户端不撤销，关闭最后一个才撤销。
- **busy steer**：消息被接收且不终止当前工作；不得以排队替代已确认的 steer 行为。
- **权限请求**：完全由 OpenCode 默认策略处理；RA2A 不参与。
- **HTTP 响应丢失**：保持 unknown，不自动重放。
- **服务重启或升级**：先撤销不可信 endpoint，重新完成版本与认证检查后再发布。
- **V1 环境**：继续使用现有 V1 adapter，不改变其 server、attach 和投递行为。

## 9. Content And Terminology

- **官方共享服务**：OpenCode V2 自己管理的后台服务，拥有 session、认证、插件和 remote/pairing。
- **在线状态插件**：RA2A 安装的 TUI+GUI 插件，负责上报本客户端打开的 session 与清理状态。
- **endpoint**：RA2A 对外发布的一个可投递 session 地址。
- **收件**：官方服务明确接收消息；不等同于模型执行、任务完成或业务回复。
- **steer**：V2 在 agent 下一次模型调用前插入消息，不终止当前工作。

## 10. Acceptance Criteria

1. Given 用户照常执行 `opencode` 或打开 Desktop/Web，且分别打开多个 session tab，When 调用 `list_targets()`，Then 每个已打开 session 只出现一个 OpenCode endpoint，后台 tab 也包含在内，无需 `--ra2a`。
2. Given 同一 session 被多个客户端打开，When 关闭其中一个客户端，Then endpoint 仍存在；关闭最后一个客户端后撤销。
3. Given 没有客户端打开 session，When RA2A 启动或刷新，Then 不启动官方服务、不创建后台 session、不发布 endpoint。
4. Given 官方服务 remote 已开启，When 安装、启动和投递，Then 官方 service/remote 配置内容与状态不被 RA2A 修改。
5. Given 用户显式连接远程 `--server`，When RA2A 刷新本机 adapter，Then 不连接该远程 URL，也不修改其配置。
6. Given 目标 session 忙碌，When 发送一条 RA2A 消息，Then 官方服务以 steer 接收一次，目标界面可见且当前工作不被终止。
7. Given 默认权限策略，When RA2A 消息触发工具权限，Then RA2A 不自动批准，遵循 OpenCode 的用户交互。
8. Given V2 wrapper 收到旧 RA2A `--yolo` / `--auto`，When 启动 OpenCode，Then 明确提示暂不支持且不改变 OpenCode 权限行为；普通 `opencode` 与 `opencode --ra2a` 的自动接管结果相同。
9. Given HTTP 响应超时，When 无法确认是否写入，Then 返回 `DELIVERY_UNKNOWN` 且不自动重发。
10. Given 官方服务重启或 OpenCode 原地升级，When 服务恢复，Then RA2A 重新校验实际 server 版本并恢复正确适配。
11. Given 版本探测失败，When daemon 选择 adapter，Then 不把未知/V2 服务误交给 V1 路径。
12. Given OpenCode V1 环境，When 执行原有 OpenCode 流程，Then 现有 attach、resume、权限和投递行为不回归。
13. Given 安装、升级或卸载 RA2A，Then 只增删 RA2A 专属插件与配置，不覆盖用户其他插件、认证、模型和 remote 设置。
14. Given 任一平台或客户端类型未完成真实验证，When 发布支持状态，Then 明确标为 Preview 或未验证，不以 mock 结果替代。

## 11. Review Checklist And Sign-off Questions

- [x] 接入范围与版本目标已确认。
- [x] Session 发布、多客户端和关闭规则已确认。
- [x] 服务生命周期与 remote 边界已确认。
- [x] 自动接管无需后缀、busy 和未知投递规则已确认。
- [x] V2 不实现 RA2A `--yolo` / `--auto`，权限遵循 OpenCode 默认。
- [x] 在线状态插件的安装与维护责任已确认。
- [ ] 定向测试与真实客户端验证范围按仓库规则另行申请。
- [ ] 全平台与全 Agent 交叉验收完成后，再讨论正式支持状态。

## Confirmed Product Decisions

> PROTECTED USER-AUTHORITY SECTION
> Rows in this section MUST NOT be created, modified, deleted, reinterpreted,
> or superseded without explicit user approval for that specific decision change.
> Agent self-approval is forbidden.

| ID | Confirmed Decision | Must Do | Must Not Do | Rationale | Violation Signal | Confirmation | Status |
|---|---|---|---|---|---|---|---|
| PD1 | 完整适配 OpenCode V2，并保留 V1 | 建立独立 V2 adapter，保持 V1 现有路径 | 用 V2 门禁或原生透传冒充完整支持 | 恢复 OpenCode V2 跨设备互通且避免回归 | V2 仍无 endpoint，或 V1 行为改变 | user-confirmed-direct：2026-11-24 选择“完整适配 V2（推荐）” | active |
| PD2 | 复用本机官方共享服务，不启动私有 server，不修改 remote/pairing | 只连接官方本机注册 URL，保持官方服务由 OpenCode 管理 | 再启动一套 RA2A server，或改写 remote、密码、CORS、端口 | 消除双服务模型冲突 | 出现第二个 OpenCode server，或官方配置被修改 | user-confirmed-direct：2026-11-24 选择“完整适配 V2（推荐）” | active |
| PD3 | 发布 TUI、Desktop、Web 所有打开 tab 的 session，单 endpoint，最后关闭撤销 | 包含后台 tab，按 session 去重，最后一个客户端关闭后撤销 | 只发布当前 focus、历史 session，或按客户端重复发布 | 匹配用户对“前台打开”的实际使用方式 | 后台 tab 缺失、同 session 多地址、关闭后仍可投递 | user-confirmed-direct：2026-11-24 确认“终端与官方 App/Web”“所有打开的 tab”“单 endpoint，最后关闭撤销” | active |
| PD4 | 没有在线客户端时不启动服务、不发布 | 保持 OpenCode endpoint 为空，让原生 OpenCode 自行管理服务生命周期 | 自动启动官方服务或创建后台 session | 避免 RA2A 接管官方服务 | 无 TUI 时服务被启动或出现 headless endpoint | user-confirmed-direct：2026-11-24 选择“不启动、不发布（推荐）” | active |
| PD5 | 忙碌消息立即 steer | 使用 V2 steer admission，保留明确收件 | 排队、拒绝或终止当前工作 | 延续现有忙碌追加体验 | busy 消息未进入、被延迟或当前工作中断 | user-confirmed-direct：2026-11-24 选择“立即 steer（推荐）” | active |
| PD6 | 权限默认遵循 OpenCode，不自动批准 | 保持用户权限交互；`--yolo`/`--auto` 仅限显式 attach 当前 session | 默认为 RA2A 消息放行工具权限 | 保持权限边界与用户控制 | RA2A 静默批准或作用范围扩大 | user-confirmed-direct：2026-11-24 选择“遵循 OpenCode 默认（推荐）” | superseded |
| PD7 | 只连接本机官方服务，不跟随显式远程 `--server` | 将 RA2A 节点绑定本机官方服务 | 连接用户显式远程 URL 或改变官方 remote 配置 | RA2A 当前是局域网本机节点，远程 OpenCode 接入由官方管理 | RA2A 流量跟随远程 URL 或 remote 配置变化 | user-confirmed-direct：2026-11-24 选择“只连本机官方服务（推荐）” | active |
| PD8 | 安装并维护 V2 TUI+GUI 在线状态插件 | 由 RA2A 安装、升级、卸载专属插件；插件禁用时停止发布对应客户端 session | 依赖无法证明关闭时机的服务端近似事件 | 可靠覆盖 TUI、Desktop、Web 的打开 tab 生命周期 | App/Web 无法枚举或最后一个客户端关闭后 endpoint 残留 | user-confirmed-direct：2026-11-24 选择“接受常驻插件（推荐）” | active |
| PD9 | 完整 V2 适配独立纳入 v0.0.20 | 独立跟踪、验收和发布 | 将其并入 v0.0.19 并阻塞当前版本 | v0.0.19 已有独立目标，V2 适配需要完整验收 | V2 适配被混入 v0.0.19 发布门禁 | user-confirmed-direct：2026-11-24 选择“独立 v0.0.20（推荐）” | active |
| PD10 | V2 自动接管所有前台会话，无需后缀 | 正常 `opencode`、Desktop、Web 打开 tab 即发布；`--ra2a` 仅作兼容 no-op | 要求用户添加后缀才接入，或改变普通 OpenCode 用法 | 用户要求原用法不变且前台活跃即可投递 | 普通 opencode 不发布，或文档要求添加 `--ra2a` | user-confirmed-direct：2026-11-25 选择“自动接管所有前台会话（推荐）” | active |
| PD11 | V2 不实现 RA2A `--yolo` / `--auto` | 权限完全由 OpenCode 默认策略处理；旧参数明确提示不支持 | 引入自动批准、capability/ACK/lease/fence 或改变权限 | 避免可选便利功能破坏权限边界和核心接入 | V2 仍出现 RA2A 自动批准逻辑 | user-confirmed-direct：2026-11-25 用户明确“不要折腾别的”，并确认 V2 不提供 auto | active |

## 12. Open Questions And Risks

- OpenCode GUI 插件 API 当前为 experimental；需要锁定最低 V2 版本，并对升级做兼容验证。
- Desktop、Web、TUI 的插件加载点和清理时机必须在真实客户端验证，不能只依赖文档推断。
- 心跳间隔、超时和本机上报传输属于工程细节，不改变已确认产品规则。
- Windows/macOS/Linux 的官方服务注册路径、文件权限和认证读取方式需要逐平台验证。
- OpenCode V2 与 Pi、Codex App、Codex CLI 的全交叉验收尚未完成；完成前保持 Preview。
- v0.0.19 仍未发布；v0.0.20 的排期取决于 v0.0.19 收口，但本文范围不随之缩减。

## 13. Implementation Notes

- 官方契约来源：[V1→V2 迁移](https://opencode.ai/docs/migrate-v1/)、[V2 API](https://opencode.ai/docs/api/)、[TUI 插件](https://opencode.ai/docs/build/plugins/cli/)、[GUI 插件](https://opencode.ai/docs/build/plugins/gui/)。
- 现有诊断与启动恢复见 [OpenCode runbook](../../runbooks/opencode-cli-integration.md)；Issue #8 的关闭范围不包括完整 V2 接入。
- RA2A 不得在日志、报告或配置备份中输出官方服务密码、token、prompt 或消息正文。
- 所有安装和升级操作必须保留用户已有 OpenCode 插件、MCP、认证、模型、代理和 remote 配置。
- 实现与验收优先使用隔离配置和假凭据；真实客户端验证按仓库测试授权规则执行。
