# Engineering Plan：RA2A OpenCode V2 自动接入

- Product Authority: `docs/opencode-v2/prd.md#confirmed-product-decisions`
- Applicable Decisions: PD1–PD5、PD7–PD11；PD6 已 superseded
- Plan Authoring: `planned`
- Pre-investment Validation: `planned`
- Execution Tracking: `not-started`
- Plan Validity: `valid-with-qualifications`
- Product Decisions: `active`
- Discovery / Design Artifacts: `reviewed`
- Code Implementation: `planned`
- Review Report: `vs_review/2026-11-25-opencode-v2-plan-review.md`

## Execution Contract

- 上述 Product Authority Source 是本范围唯一产品决策权威；其 active 决策均来自用户直接确认。
- 修改 active 产品决策必须取得用户对该变更的明确批准；Agent 自我批准无效。
- 已验证的工程证据可以修订本计划，但不得静默改写产品权威。
- 新材料产品选择必须延后、作为局部可逆临时方案，或由用户明确确认。
- 每个实质阶段结束后只审计该阶段的产品决策增量。
- 每个增量必须分类为 `covered`、`engineering-only`、`provisional` 或 `conflict`。
- 只要存在未解决的实质 `provisional` 或 `conflict`，依赖它的后续工作不得继续。

## 1. Current Behavior And Target Behavior

### 当前行为

- `internal/ochost/compatibility.go:20-38` 检测到 OpenCode major ≥ 2 后拒绝旧 adapter；探测失败反而返回 nil。
- `cmd/oc-wrapper/main.go:98-104` 对普通 V2 启动透传原生，对显式 `--ra2a` 返回不支持。
- `cmd/ra2a/registry.go:53-67,143-151` 跳过 V2 adapter，因此不发布 OpenCode endpoint。
- `internal/ochost/host.go:171-172` 的 V1 路径会启动 `opencode serve --port <port> --hostname 127.0.0.1`。
- `internal/opencode/client.go:141-213,288-347` 使用 V1 的 `/session`、`/prompt_async`、`/event` 和 V1 事件结构。
- 当前 OpenCode 配置写入器固定 `~/.config/opencode/opencode.json`，只处理 strict JSON 和 V1 `mcp.ra2a`。
- 当前 `ra2a exit` 和四个 installer 不卸载 OpenCode plugin；release 版本常量仍为 v0.0.18。

### 目标行为

- 用户照常执行 `opencode`，无需添加 `--ra2a`；TUI、Desktop、Web 所有打开 tab 自动发布。
- RA2A 按官方算法选择本机 OpenCode V2 共享服务，使用官方注册 URL 和密码鉴权。
- RA2A 不启动、停止或配置官方服务，不读取或改写 remote route。
- daemon 在 service 缺失、后启动、换端口/密码、重启或升级后持续只读 rediscover。
- V2 adapter 只为有在线客户端的 session 发布 endpoint；多客户端按 session 去重，最后关闭撤销。
- 投递使用 `POST /api/session/{sessionID}/prompt`，显式 `delivery: "steer"`，使用符合 `^msg_` 的幂等 ID。
- V2 权限完全由 OpenCode 默认策略处理；RA2A 不实现 `--yolo` / `--auto`。
- V1 adapter 和 wrapper 路径保持现状；未知版本不进入 V1 私有 server 路径。

## 2. Goals And Non-goals

### Goals

1. 用最小新增抽象完成 V2 service 发现、认证、session/status/prompt、location-scoped presence 和 rediscovery。
2. 复用现有 `agentbridge.Adapter`、registry、LAN 和 MCP 链路，不改路由协议。
3. 将在线状态插件作为 RA2A 专属资产嵌入主二进制，避免用户额外安装 Node/Bun 包。
4. 保留用户现有 OpenCode 命令、配置、插件、MCP、认证、模型、代理和 remote 设置。
5. 为 v0.0.20 提供可审计的实现、验收和版本门禁证据。

### Non-goals

- 不实现 PRD `Out Of Scope` 中的远程 `--server` 跟随、headless session 或官方服务生命周期管理。
- 不实现 V2 `--yolo` / `--auto`、自动权限批准、capability/ACK/lease/fence。
- 不重写 V1 adapter，不把 V2 协议硬塞进 `internal/opencode` 的 V1 client。
- 不引入数据库、常驻第三方服务或新的中心节点。
- 不在生产代码之前先写实现；所有生产代码遵循测试先行。

## 3. Least-Construction Design

### 3.1 新增边界明确的 V2 package

新增 `internal/opencodev2`，与 V1 `internal/opencode` 并存：

- `service.go`：官方 registration 发现、选择、loopback 校验、密码读取、Basic Auth、`/api/info` 版本校验。
- `client.go`：session 读取、active/busy、幂等 prompt、inbox/history 对账。
- `events.go`：`/api/event` SSE、状态同步和重连快照。
- `presence.go`：枚举 loaded locations，逐 location 调用 presence RPC 并聚合。
- `adapter.go`：实现现有 `agentbridge.Adapter` 与 `CallerResolver`，不改变 registry 接口。

独立 package 避免两套响应、认证和事件契约互相污染；它们只在 registry 选择阶段相遇。

### 3.2 官方 service 发现与常驻 rediscovery

- 路径解析遵循官方 XDG 规则：state 使用 `XDG_STATE_HOME` 或 `~/.local/state/opencode`；config 使用 `OPENCODE_CONFIG_DIR`、`XDG_CONFIG_HOME` 或 `~/.config/opencode`。
- 发现顺序严格对齐 v2.4.1：
  1. 依次探测 `OPENCODE_CHANNEL` 对应文件与默认 `service.json`，首个 usable registration 直接返回。
  2. 只有 preferred files 均不可用时，才扫描其余 `service*.json` 并并行探测 `/api/info`。
  3. 扫描结果少于两个 ready 时直接返回唯一 ready；零 ready 时保留首个 pending，否则返回最终 failed。
  4. 至少两个 ready 时才依次按当前原生二进制 owner、version fallback、registration 新旧顺序选择。
- 只接受 loopback URL；拒绝 `0.0.0.0`、远程 URL、非 HTTP(S) 或缺少密码的 registration。
- 密码只保存在 client 内存中，不写入 RA2A 配置、日志、备份或环境快照。
- OpenCode supervisor 维护 `absent → discovered → ready → revalidating → ready/unavailable`：
  - 只要配置了 OpenCode harness 就注册 supervisor 壳，启动时无 service 也保持注册。
  - 初始每 2 秒重读 registration；SSE 断线、401、版本变化或 `/api/info` PID/URL 变化时立即 revalidate。
  - 切换 endpoint 前撤销旧 endpoints；重建 client 后重新校验 version/auth/presence。
  - 绝不调用官方 `Service.ensure/start/stop`，不写 registration，不修改 service config。
- 检测到 V2 后，legacy owner 只有在 executable、真实进程 start identity 与记录的 RA2A V1 server 身份全部匹配时才允许清理；PID 复用、身份不明或匹配当前官方 V2 registration 时只标记 stale/隔离，绝不发信号。

### 3.3 版本证据与路由矩阵

| Evidence | Routing | Wrapper / Daemon Behavior |
|---|---|---|
| 实际 `/api/info` major ≥ 2 | V2 | 使用 V2 adapter；普通启动原生透传 |
| CLI 明确 major ≥ 2，但 service 尚不存在 | V2 inactive | supervisor 持续 rediscover；不启动私有 server |
| CLI 明确 major 1，且 V1 service 未运行 | V1 cold-start allowed | `ra2a opencode` / V1 `--ra2a` 保持现有 `ochost.Start` |
| CLI 明确 major 1，且配置 URL 的 V1 `/session` 返回合法 JSON 数组 | V1 | 保持现有 serve/attach 路径 |
| CLI 无法识别，但实际端点由 `/api/info` 证明为 V2 | V2 | 按实际 server 路由，不信 CLI 输出 |
| CLI 无法识别，但无 V2 registration，且实际 `/session` 证明为 V1 | V1 | 允许采用已验证的 V1 端点 |
| CLI 无法识别且无实际 API 证据 | supervisor dormant | wrapper 原生透传；不发布 endpoint，也不启动 V1 server |

- `CheckCompatibility` 返回“明确 V1 / 明确 V2 / 未知”三态，不再把失败当 V1。
- 只要配置了 OpenCode harness，registry 就注册 `cmd/ra2a/opencode_supervisor.go`；它按证据委托 V1/V2，避免 service 后启动时无人重新注册。
- V2 wrapper 不调用 `ochost.Start`；普通 `opencode` 与 `opencode --ra2a` 行为相同，`--ra2a` 仅剥离为兼容 no-op。
- V2 `--yolo` / `--auto` 明确返回暂不支持，不修改任何权限配置。

### 3.4 Location-scoped presence

- Plugin RPC ID 使用 `ra2a.opencode.presence`，方法为 `report`、`list`、`withdraw`。
- lease key 包含 `serverID`、`location.directory`、`clientID`、`sessionID`、`clientType`、`lastHeartbeat`。
- daemon 通过只读 `GET /api/debug/location` 枚举 loaded locations，再对每个 location 调用 `location[directory]=...` 的 RPC。
- 单 location 的缺失或 404 只跳过该 location，不得让其他 location 的有效 endpoint 全部消失。
- presence 变化通过 `rpc.ra2a.opencode.presence.changed` 事件触发刷新；事件丢失时周期快照重同步。
- 初始工程参数：heartbeat 5s、lease TTL 15s；正常 cleanup 立即 withdraw，崩溃目标在 TTL 内撤销。
- Server plugin 只保存内存 lease，不持久化 prompt、消息正文或凭据。

### 3.5 TUI / GUI 自动在线插件

- **TUI tabs enabled**：从 `context.ui.tabs.list()` 枚举所有打开 tab，变化时 report，周期心跳，cleanup withdraw。
- **TUI tabs off**：从 `context.ui.router.current()` 获取当前 session；只发布该当前 session，路由切换/退出时更新或撤销。
- **GUI Desktop**：只上报 `session.server.id === "sidecar"` 的打开 shell tab。
- **GUI Web**：W0/W1 必须确定 plugin host 的 server identity；只上报属于该 host 的 tab，不报告其他显式远程 server。
- 多客户端同一 session 由 daemon 去重；关闭一个 client 不撤销，最后一个 client withdraw 或 TTL 到期才撤销。
- 插件不监听、转发或回复 permission；V2 权限完全由 OpenCode 默认策略处理。

### 3.6 V2 adapter 与投递

- `ListEndpoints`：聚合 presence session ID，逐个读取官方 session 详情，按 ID 去重，映射 title/status/capabilities。
- `Health`：只读检查官方 `/api/info`、registration 与 presence RPC；服务或插件不可用时明确 unhealthy。
- `Deliver`：再次检查 presence，使用统一 message envelope 渲染文本，提交 V2 prompt。
- 原生 message ID 使用 `msg_` + SHA-256(`nodeID + sessionID + envelopeID`)，并在 metadata 保存 RA2A message/source ID。
- 同 ID 重试返回原 admission 视为成功；跨 session 409 进入明确冲突/未知分类，不自动重放。
- 超时后同时查询 `/api/session/{id}/inbox` 与分页 `/api/session/{id}/message`，按 metadata 中的 RA2A ID 对账。
- `Watch`：消费 `session.status`、`session.idle`、presence change 与 `location.shutdown`；重连后先用 `/api/session/active` 和 presence 快照重同步，同步完成前不发布新 endpoint。
- 实现 `CallerResolver`：只有一个在线 session 时自动解析；多个时要求显式 `from`，与 V1 行为一致。
- `steerActiveTurn` capability 只有在真实 busy 测试证明后声明；`accepted` 文档明确只代表 admission。

### 3.7 有效配置、安装与卸载

- W0 固化 `OPENCODE_CONFIG`、`OPENCODE_CONFIG_DIR`、XDG、`.json`/`.jsonc` 与系统/托管配置的有效优先级。
- W8 同时识别 V1 `mcp.ra2a` 与 V2 `mcp.servers.ra2a`，不产生重复或互相覆盖的 entry。
- JSON 使用现有原子写入；JSONC 使用窄范围语法保留编辑，只改 RA2A owned key。无法安全保留时失败并保持原文件不变。
- Plugin package 安装在 `~/.config/ra2a/opencode-plugin/`，通过有效 `opencode.json(c)` 的显式 `plugins` entry 加载；不得同时放入自动发现目录造成重复加载。TUI 是否需要 `cli.json` 入口由 W0 验证，同一 surface 只配置一次。
- 新增 `opencode-plugin-unregister`，接入 `ra2a exit`、`install.sh`、`install.ps1`、`install-remote.sh`、`install-remote.ps1`；重复卸载安全，只删 RA2A owned entry/目录。
- V2 路径不创建 legacy owner、不调用 `ochost.Start`；现有 `opencode-server-cleanup` 对无 owner 的 V2 环境必须 no-op。

### 3.8 Plugin 构建与嵌入

- Plugin 源码与构建产物放在 `internal/opencodev2/plugin/`，通过 `go:embed` 进入 RA2A 主二进制。
- W7 锁定 Node/Bun 与依赖版本、lockfile、GUI 单文件 CommonJS bundle、package exports 和 bundle hash。
- Release workflow 在 tag gate 的首次 `go run` 之前构建并校验 plugin；用户运行时不需要 Node/Bun。
- 验证 `plugin.list` 只有一个 RA2A plugin 实例，server/TUI/GUI entry 均加载成功。

### 3.9 可观测性与用户恢复

- service discovery 记录 channel/file、PID、version、loopback URL 和失败原因；禁止密码/token。
- rediscovery 记录旧/新 endpoint identity、重连次数、auth/version 重校验结果。
- plugin lifecycle 记录 install/upgrade/uninstall/load failure/disable/reload、bundle hash 和 plugin ID。
- presence 记录按 location/client type 的 lease 数、心跳超时和 withdraw 原因；不记录正文。
- endpoint 记录 publish/dedup/withdraw session ID 与最后 client 关闭原因。
- delivery 记录 RA2A ID、原生 admission ID、steer、HTTP class、reconcile 来源和最终结果。
- W10 用现有命令、日志和 runbook 说明四层状态（daemon、官方 service、server plugin、TUI/GUI client）；不新增 doctor CLI。

## 4. Minimum Necessary Pre-investment Validation

| ID | Critical Assumption | Decision Unlocked | Cheapest Credible Method | Enough Evidence / Not Proven | Budget / Isolation | Stop / Cleanup | Status |
|---|---|---|---|---|---|---|---|
| V1 | 官方多 registration/channel 选择、V1 冷启动与 rediscovery 可只读复刻 | W2/W9 | preferred 优先于 owner、单 ready/pending、owner/version/newest；V1 无 service 冷启动；端口/密码变更 | 证明选择、冷启动与恢复 | 隔离 state/config，无模型 | 停止自有探针，删除隔离目录 | planned |
| V2 | 单一 plugin package 能覆盖 TUI、Desktop、Web，且按 location 聚合 | W5–W7 | 隔离 v2.4.1 加载 server+TUI+GUI，两个 location report/list/withdraw | 证明加载、location、cleanup | 无凭据、无业务 prompt | 关闭测试客户端，撤销 lease | planned |
| V3 | tabs off、后台 tab、混合 sidecar/remote、多客户端生命周期正确 | W6/W9 | TUI tabs off/on、Desktop/Web sidecar+remote、双 client 同 session | 证明枚举/过滤/撤销 | 无模型调用 | 关闭客户端，检查 lease 清零 | planned |
| V4 | `msg_` ID、steer、inbox/history 对账是可靠收件边界 | W3 | OpenAPI + `httptest` + 真实服务无模型 admission 探针 | 证明请求/错误形状 | 无正式 session、无模型 | 删除测试 session 或隔离 HOME | planned |
| V5 | 有效配置解析与真实卸载入口只改 RA2A owned 资产 | W8/W10 | JSON/JSONC、V1/V2 MCP、XDG/config dir、双文件、四个 installer + `ra2a exit` | 证明配置保护 | 仅临时配置与隔离 HOME | 恢复临时目录 | planned |

**进入条件**：用户批准本阶段定向测试范围。  
**通过条件**：V1–V5 均为 `direction-supported`，且没有发现需要修改 active PD 的证据。  
**停止条件**：任一关键假设被否定且无法用不改变产品规则的方式绕过；回到用户决策。

## 5. Executable Work Units

| ID | Objective | Change Axis | Change Location | Target Object | Concrete Action | Resulting Behavior | Benefit | Side Effects | Verification | Safe Stop / Rollback | Plan Status |
|---|---|---|---|---|---|---|---|---|---|---|---|
| W0 | 固化 V2 真实契约 | Discovery | `plan.md`、`runbooks/opencode-cli-integration.md` | service/config/RPC/plugin/prompt 契约 | 核对官方 v2.4.1，记录多 channel 分段、XDG、location、tabs off、sidecar、`msg_`、加载点 | 后续实现不再猜测路径或 API | 避免跨平台返工 | Complexity: 无；Reach/cost: 仅文档 | 官方引用、隔离命令输出、契约矩阵 | 不写生产代码 | not-started |
| W1 | 验证最小技术方向 | Spike | `experiments/opencode-v2-spike/`（临时） | 最小 plugin + V2 API/rediscovery | 隔离验证 preferred/owner、两 location、tabs off、sidecar/remote、prompt、配置矩阵 | direction-supported/rejected | 排除路线失败 | Complexity: 临时脚本；Reach/cost: 隔离、无模型 | V1–V5 证据；packaged Desktop 单列 | 删除隔离目录 | not-started |
| W2 | 实现官方 service 发现与认证 | Service client | `internal/opencodev2/service.go`、测试 | Registration selector / Basic Auth | XDG、preferred-first、单 ready/pending、多 ready owner/version/newest、loopback、`/api/info` | 可信 V2 endpoint 或不可用 | 建立官方服务边界 | Complexity: 新 package；Reach/cost: 读敏感文件，不写/不启动 | 多 registration/channel/pending/401/loopback/秘密测试 | 删除 package；supervisor unavailable | not-started |
| W3 | 实现 V2 session 与幂等投递 | API client | `internal/opencodev2/client.go`、测试 | Session/prompt/inbox/history | `{data:...}`、Basic Auth、`msg_`、steer、metadata、inbox+分页对账 | 明确收件且 unknown 不重放 | 恢复核心投递 | Complexity: HTTP 方法；Reach/cost: 只写目标 session | 精确 body/header/path、重复 ID、409、超时、对账 | 删除 client；不注册 V2 | not-started |
| W4 | 实现 V2 事件与重同步 | Event stream | `internal/opencodev2/events.go`、测试 | SSE status/presence/location | 解析事件、重连 active+presence 快照，同步前 fail closed | 状态不依赖旧缓存 | 避免虚假 ready | Complexity: SSE+重连；Reach/cost: 内存 | 假 SSE、畸形、断线、active 重同步、取消 | 关闭 Watch | not-started |
| W5 | 实现 location presence RPC | Plugin server | `internal/opencodev2/plugin/index.ts`、`rpc.ts`、测试 | `ra2a.opencode.presence` | report/list/withdraw、location lease、TTL、change event | 分 location 可信 presence | 跨 project 全 tab | Complexity: RPC+内存 lease；Reach/cost: 不持久化 | Node 测试 + W1 两 location 加载 | 注销 RPC；V2 不发布 | not-started |
| W6 | 实现 TUI/GUI 自动在线插件 | Plugin clients | `internal/opencodev2/plugin/tui.ts`、`gui.tsx`、测试 | TUI tabs/router、GUI sessions/server | tabs on/off、sidecar/host 过滤、心跳/cleanup | 正常 opencode 自动发布所有打开 tab | 满足 PD3/PD8/PD10 | Complexity: 两入口；Reach/cost: 周期 RPC，无权限逻辑 | TUI/Web/Desktop、tabs off、混合 server、双 client、崩溃 TTL | 禁用 plugin entry | not-started |
| W7 | 构建并嵌入 plugin 资产 | Build/package | plugin package/lockfile/CJS/embed/release workflow | RA2A plugin bundle | 锁工具/依赖、单文件 CJS、exports/hash、`go:embed`；早于 tag gate | 单二进制自带插件 | 可重复发布 | Complexity: 构建链；Reach/cost: 体积/CI | clean CI、bundle load、单实例、跨平台 build | 恢复旧 workflow | not-started |
| W8 | 安全安装/升级/卸载 | Installer/config | `opencode_plugin.go`、`opencode_mcp.go`、`harness.go`、`cmd/ra2a`、`operator.Exit`、四个 installer | 有效配置与 owned assets | 配置优先级、JSON/JSONC、V1/V2 MCP、显式 entry、冲突拒绝、卸载接线 | 自动维护且保留用户配置 | 用户无需手工配置 | Complexity: 配置模块；Reach/cost: 写 config/plugin | 哈希、JSONC、V1/V2 MCP、双文件、五入口卸载 | 删 owned entry/目录或恢复备份 | not-started |
| W9 | 集成 supervisor/wrapper/rediscovery | Runtime | `opencode_supervisor.go`、V2 adapter、registry、wrapper、compatibility、owner 校验 | 版本路由与生命周期 | supervisor 委托、V1 冷启动/V2 dormant、rediscovery、CallerResolver；V2 普通启动透传、`--ra2a` no-op、auto 参数拒绝 | 自动接管且 V1 不回归 | 完成产品闭环 | Complexity: supervisor+状态机+owner 校验；Reach/cost: daemon/wrapper | 路由、先启动恢复、换端口/密码、owner 复用、caller、V1 回归、E2E | `RA2A_DISABLE_OPENCODE`；原生透传 | not-started |
| W10 | 验收、文档与 v0.0.20 发布 | Acceptance/release | acceptance、README、runbook、release notes、`operator.Version` | 支持矩阵与版本门禁 | 授权验证、Preview/恢复文档、版本 bump、tag dry-run | 可发布 V2 Preview | 防止 mock/规避冒充支持 | Complexity: 无生产逻辑；Reach/cost: 测试/发布 | 定向测试、真实三客户端、配置保护、全交叉、version gate | 不打 tag | not-started |

## 6. Phases And Gates

### Phase 0：产品权威与简化计划

- Entry：PD1–PD5、PD7–PD11 active；PD6 superseded。
- Work：PRD 范围收紧、删除 auto 权限复杂度、计划简化。
- Product Decision Delta：`covered` by PD10/PD11。
- Exit：简化计划评审通过；用户批准 Phase 1 测试范围。

### Phase 1：发现与最小方向验证

- Entry：测试授权。
- Work：W0–W1。
- Product Decision Delta：`engineering-only`。
- Exit：V1–V5 均 direction-supported。

### Phase 2：V2 service/API 核心

- Entry：Phase 1 通过。
- Work：W2–W4。
- Product Decision Delta：`covered` by PD1、PD2、PD4、PD5、PD7。
- Exit：service、认证、prompt、事件与重同步测试通过。

### Phase 3：在线插件与安装器

- Entry：Phase 2 通过。
- Work：W5–W8。
- Product Decision Delta：`covered` by PD3、PD4、PD8、PD10、PD11。
- Exit：所有打开 tab 可枚举，最后关闭撤销，安装/卸载不覆盖用户配置。

### Phase 4：Runtime 集成

- Entry：Phase 2–3 通过。
- Work：W9。
- Product Decision Delta：`covered` by PD1–PD5、PD7–PD11。
- Exit：普通 opencode 自动发现与 steer 投递通过，V1 不回归。

### Phase 5：验收与发布

- Entry：Phase 4 通过；真实客户端测试获授权。
- Work：W10。
- Product Decision Delta：`covered` by PD1–PD5、PD7–PD11。
- Exit：支持矩阵、诊断、版本门禁与证据一致。

## 7. Product Decision Delta Audit

| Phase | Decision Surface | Implemented / Observed Semantics | Authority Coverage | Classification | Required Action |
|---|---|---|---|---|---|
| Phase 0 | 自动接管与删除 V2 auto | 普通 opencode 自动发布；V2 不实现 auto | PD10、PD11 | covered | 简化计划评审 |
| Phase 1 | 临时验证 | 不进入生产 | 不适用 | engineering-only | 填写证据 |
| Phase 2 | 服务、认证、投递、状态 | 待实现 | PD1、PD2、PD4、PD5、PD7 | pending | 实现后审计 |
| Phase 3 | 在线发布、安装 | 待实现 | PD3、PD4、PD8、PD10、PD11 | pending | 实现后审计 |
| Phase 4 | Adapter、wrapper、恢复 | 待实现 | PD1–PD5、PD7–PD11 | pending | 实现后审计 |
| Phase 5 | 支持、诊断、发布 | 待实现 | PD1–PD5、PD7–PD11 | pending | 验收后审计 |

## 8. Verification Strategy

### 定向自动验证

- Go：`go test ./internal/opencodev2 ./internal/operator ./cmd/ra2a ./cmd/oc-wrapper`
- Plugin：Node 内置测试覆盖 presence reducer、TTL、location key、tabs 枚举和 server 过滤。
- 契约：`httptest` 覆盖多 registration、Basic Auth、`{data:...}`、SSE、RPC location、prompt `msg_`、inbox/history、错误与超时。
- 静态：相关包 `go vet`、`git diff --check`、bundle/exports/hash 校验。

### 隔离集成验证

- 临时 HOME/XDG/config/state、假凭据、无模型调用。
- 真实 OpenCode v2.4.1 官方服务与最小 RA2A plugin package。
- 多 channel registration、service 后启动、换端口/密码、升级/重启 rediscovery。
- 两个 project/location、TUI tabs on/off、Desktop/Web sidecar+remote、多客户端同 session。
- 官方 remote 开启/关闭时 service/remote/config 不被 RA2A 修改。
- JSON/JSONC、V1/V2 MCP、配置优先级、安装/升级/五个卸载入口的哈希保护。

### 真实端到端验证

- 仅在用户授权后执行，限制消息数量和模型额度。
- 正常 `opencode` 无后缀自动发布；覆盖 TUI、Desktop、Web、busy steer、多客户端、关闭撤销、服务重启和 V1 回归。
- V2 `--yolo` / `--auto` 只验证明确不支持且权限配置不变。
- 自动验证只使用脚本直接控制；视觉确认最后人工完成。
- mock、spike、协议探针和真实客户端结果分开记录。

## 9. Risks, Rollback, And Operational Boundaries

- **多 registration 选错**：严格对齐官方分段算法；W1 未证明则 W2 不实现。
- **service 恢复缺失**：常驻 rediscovery；切换期间撤销 endpoint。
- **location/tab/server 误报**：location key、tabs-off 回退、sidecar/host 过滤与真实矩阵均为门禁。
- **配置损坏**：JSONC 安全编辑或失败不变；冲突拒绝；五个真实卸载入口覆盖。
- **legacy cleanup 误伤**：V2 不创建 owner；升级后清理前验证真实进程身份，不匹配只标记 stale。
- **未知投递**：保持 `DELIVERY_UNKNOWN`，不自动重发。
- **回滚**：使用 `RA2A_DISABLE_OPENCODE` 或关闭 V2 adapter；wrapper 原生透传；删除 RA2A owned plugin。绝不修改官方 remote/service registration。

## 10. Pending Product Decisions

无。PD1–PD5、PD7–PD11 已覆盖阻塞产品选择；PD6 已 superseded。

## 11. Authorization Gate And Next Action

当前只完成 PRD、计划与只读审查，不执行测试、不写生产代码。

下一步需要用户批准：

1. W0 的只读官方契约与三平台路径/配置矩阵核对。
2. W1 的隔离、无模型、无正式配置的 V2 插件/API/rediscovery spike。
3. Phase 2 起的相关 Go/Node 定向单元与契约测试范围。

批准后从 W0 → W1 开始；只有 V1–V5 均为 `direction-supported` 才进入生产实现。