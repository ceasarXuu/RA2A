# RA2A v0.0.18 修复目标

## FIX-001：Windows Codex 控制目录权限兼容性

- 登记日期：2026-10-01。
- 状态：代码已实现，Linux 回归及 Windows 交叉编译通过；Windows 实机验收待完成。此前 Windows 本机临时权限修复不能代替本次验收。
- 影响：普通 Codex CLI 无法启动共享后台服务；用户输入停留在未发送草稿。

### 问题与已确认事实

在 Windows 上执行 `codex --yolo`，出现以下错误：

```text
Error: app server did not become ready on C:\Users\77585\.codex\app-server-control\app-server-control.sock
Error: socket directory is not private to the current user
```

本次环境为 Codex CLI `0.159.0`、托管 app-server `0.159.3`。检查发现 `app-server-control` 目录未禁用权限继承，包含其他账户与 `CodexSandboxUsers` 的访问权限；原访问规则均为继承规则。备份权限后，仅将该目录 ACL 收紧为当前用户 FullControl 并禁用继承，执行 `codex app-server daemon start` 成功，`daemon version` 返回 `status: running`。这确认了本次启动失败的直接原因是控制目录不符合 Codex 的私有权限要求。

RA2A 的[默认 socket 路径](../../internal/codexhost/owner.go)与官方 daemon 共用 `CODEX_HOME/app-server-control` 父目录；修复前，[受管宿主启动逻辑](../../internal/codexhost/host.go)及 owner lease 写入仅使用 `os.MkdirAll(..., 0o700)` 创建目录。本机 Go 的 Windows 实现调用 `CreateDirectory(..., nil)`，不能将 `0700` 转换为仅当前用户可访问的 ACL，因此 RA2A 原实现存在目录权限兼容性缺口。

归因边界：尚无证据证明 RA2A 主动放宽过 ACL，或本次目录由 RA2A 首次创建。不能把“RA2A 直接改坏权限”作为已确认结论。

[2026-09-28 Windows 验证记录](../../runbooks/windows-codex-cli-validation-evidence-2026-09-28.md)的“发现与修复”第 4 项已记载同类错误，当时只收紧了隔离实验目录及其 `app-server-daemon` 子目录权限，正式目录未处理。

### 修复目标与范围

- 消除 RA2A 与官方 Codex 共用控制目录时的 Windows 权限兼容性缺口，保证 RA2A 启动、重启及升级后，普通 Codex CLI 仍可正常启动或接入官方 daemon。
- 覆盖首次创建目录和已有目录继承了额外权限两种场景；实施前核实目录创建与权限变化路径，再确定采用目录隔离还是最小范围的权限处理。
- 保留 Codex 配置、登录状态、会话历史及已有 socket 所有权边界；不递归修改整个 `CODEX_HOME` 的 ACL，不停止或清理非 RA2A 所有的进程与文件。
- `--no-daemon` 可作临时绕行，不作为修复完成标准。

### 实现与验证进展（2026-10-01）

- 受管宿主启动、自动重启和 owner lease 写入共用平台目录准备逻辑。Unix 保持既有 `0700` 创建行为；Windows 首次创建直接传入与官方 Codex `0.159.3` 等价的当前用户 owner 与 protected、单条 OICI FullControl DACL。
- 已有目录必须是当前用户拥有的真实目录；拒绝普通文件与 reparse point，保留其他用户的 owner。已满足官方私有权限契约时不写 ACL，因此可与官方 daemon 持有的目录保护句柄共存。
- 已有坏 ACL 仅调整该目录的 DACL。修复句柄使用 `MAXIMUM_ALLOWED`，依据 [Microsoft SetSecurityInfo 文档](https://learn.microsoft.com/en-us/windows/win32/api/aclapi/nf-aclapi-setsecurityinfo)避免将权限变化传播到现有子对象；不设置 owner、SACL，不移动或删除既有 socket。
- 已新增 5 项 Windows 专用测试：首次创建与官方式目录保护句柄共存；坏继承 ACL 修复、重复执行及父目录/子文件/兄弟文件权限与内容保留；非目录拒绝；reparse point 拒绝；owner 不匹配拒绝。Linux `go test ./internal/codexhost` 通过，Windows amd64 测试二进制交叉编译通过。
- Wine 11.0 的独立临时 prefix 运行尝试在初始化阶段超时，未产生测试结果；不计入 Windows 验收，也未因此引入生产特殊处理。验证入口及证据格式见 [Windows 验证记录补充](../../runbooks/windows-codex-cli-validation-evidence-2026-09-28.md#fix-001-实现验证补充2026-10-01)。

官方权限依据：[0.159.3 目录创建源码](https://github.com/openai/codex/blob/rust-v0.159.3/codex-rs/uds/src/windows_security.rs)、[既有目录校验源码](https://github.com/openai/codex/blob/rust-v0.159.3/codex-rs/uds/src/windows_socket_validation.rs)。Windows 实机仍须执行下列五项验收，完成前不标记 FIX-001 fixed。

### 验收条件

1. **RA2A 先启动**：在具有额外继承权限的隔离 `CODEX_HOME` 中，先启动 RA2A，再执行普通 `codex` / `codex --yolo`；官方 daemon 正常启动，TUI 可输入，无上述权限错误。
2. **已有目录与升级**：复现控制目录已经存在且继承其他账户权限的环境，验证修复后的 RA2A 启动、重启及升级路径；官方 `daemon version` 返回 `running`，无需手工修 ACL。
3. **官方 daemon 先启动**：先启动官方 daemon，再启动和重启 RA2A；官方 daemon 与 RA2A 的受管宿主均保持各自所有权，既有 Codex 会话仍可继续使用。
4. **边界回归**：已满足私有权限要求的 Windows 环境继续正常工作；macOS/Linux 的相关宿主生命周期检查通过，配置与会话数据保留。
5. **证据交付**：记录验证使用的 CLI/app-server 版本、启动顺序、目录 ACL 与 daemon 状态，并更新现有 Windows runbook；当前本机临时修复不能替代上述复现与验收。


## Codex CLI 实现缺陷修复（2026-10-01）

以下缺陷先通过隔离诊断复现，再实现局部修复；源码回归已通过，尚未发布 v0.0.18；本机正式服务部署状态见下方协调会话修复记录。

| 缺陷 | 修复结果 | 验证范围 |
| --- | --- | --- |
| 已登记 CLI 被共享 App 历史覆盖，走错写入通道 | App 枚举排除 CLI 成功登记的 IDs；CLI 未加载也不回退 App | 真实 Registry 的 Lookup/Deliver、buildRegistry 接线、未登记 Desktop 与无效登记边界。提交 `e7aa4ae`。 |
| CLI 输入只含正文，丢失来源和消息 ID | start/steer 统一使用 `RenderIncomingText` | 捕获实际 WebSocket 输入并核对完整包络。提交 `dd02ee1`。 |
| 终态先于提交响应到达，确认被丢弃 | 短期保留提前到达的终态 | fake 宿主先通知、后 response；移除原固定 30ms 延迟。提交 `dd02ee1`。 |
| 同一 turn 多等待者互相覆盖 | 共享终态并广播确认 | completed/failed、单等待取消、Close 释放、重复通知及过期保留；验证限定于等待器层。提交 `dd02ee1`。 |
| 断线后一直复用失效 RPC | 后续操作重建连接；不重放不确定写入 | 断线后恢复端点、已提交写入仅发生一次且返回 unknown；同时修正通知 handler 同步。提交 `dd02ee1`。 |

本次验证：

- `go test -race -count=1 ./internal/codexcli ./internal/codexapp ./internal/agentbridge ./cmd/ra2a ./cmd/codex-wrapper` 通过。
- `go test -count=1 ./internal/codexhost ./internal/appserverprobe ./internal/lannode ./internal/control ./internal/mcpserver ./internal/operator` 通过。
- Windows arm64、Darwin arm64 的 `ra2a` / `codex-wrapper` 交叉构建通过；Windows amd64 ACL 测试编译通过。均不替代目标平台原生运行。

上述回归不覆盖真实宿主的并发订阅生命周期、跨设备 App/CLI 四方向互通、20+ 多轮、人工继续和恢复矩阵。[PD31 与原互通验收](../v0.0.15/engineering-plan.md)仍需现场完成，不能将局部缺陷修复等同于 CLI 正式支持准入。

### CLI 写入能力门禁补齐（2026-10-02）

- 修复前隔离 WebSocket 复现确认：`canAcceptDirectInput` 缺失或 `null` 时，空闲线程实际提交 `turn/start`、活跃线程实际提交 `turn/steer` 并返回 delivered；`false`、缺失、`null` 的线程均错误发布为带 `receiveText` 的 ready/busy 端点。
- 枚举与直接投递共用既有 `acceptsDirectInput()`，要求宿主明确返回 `true`。其余形态不发布可收件端点；直接投递返回 `unsupported / capability_rejected`、释放订阅且零 turn 写入。此前发布的地址不能绕过实时门禁。
- 新增 true/false/missing/null × idle/active 八项协议回归，以及三项能力变化后的旧地址回归；实际捕获 start/steer 调用。上述五个包的 `go test -race -count=1` 通过。
- 本阶段仅修改源码并运行临时 fake 宿主，没有连接正式 daemon、发真实模型任务或部署本机服务；本机 CLI/App、认证和代理配置未修改。正式支持准入仍遵循上述 PD31 验收边界。

### 显式 CLI 实例目录隔离修复（2026-10-02）

- 双 fake 宿主复现确认：`Config.CodexHome` 原先未传给 daemon 探测；显式 home 可用或未运行时，均可能错误连接环境默认 home 并实际写入，健康检查也会误报 ready。
- 适配器连接、重连及健康检查将已保存的 home 传给私有探测函数，仅覆盖子进程 `CODEX_HOME`，不修改全局环境；缺省 socket 同样使用该 home。公开 `DetectDaemon` 保留继承环境的行为，daemon 返回的迁移 socket 仍优先。
- 回归覆盖指定 home 运行/未运行、默认实例零 RPC、零回退、原进程环境不变、公开默认探测和无 socket 字段的目录回退。`go test -race -count=1 ./internal/codexcli ./cmd/codex-wrapper ./cmd/ra2a` 通过。

### Linux 真实 app-server 隔离验证（2026-10-02）

新增默认跳过、需显式指定绝对原生二进制路径的 `TestNativeCLIIsolatedDelivery`，使用独立临时 `CODEX_HOME`、关闭自动更新和远程控制的实验 daemon、动态 loopback 端口与本地 Responses SSE 模型。启动环境不继承代理注入、Agent 路由或账户密钥，不复制正式配置和认证；实验结束正常停止自己启动的 daemon。

已在 Codex CLI / app-server `0.159.3` 执行带 `-race` 的真实协议验证：

| 项目 | 结果与边界 |
| --- | --- |
| 多轮收件 | 22 轮端点发现及投递成功，每轮以 `turn/completed` 确认；本地模型请求含来源与对应消息标记。 |
| 原客户端继续 | 独立拥有线程的 RPC 客户端在多轮收件后仍能发起并完成回合；不代替 TUI 人工操作验收。 |
| 活跃回合追加 | 模拟模型保持回合活跃，适配器 steer 在相同 turn ID 上确认完成，follow-up 到达模型。 |
| daemon 重启恢复 | 只停止并重启实验 daemon，重新加载线程后原适配器恢复投递，模型收到对应消息。 |
| 配置与认证 | 模型始终为 `mock-model`；模型请求仅使用实验假凭据；独立 home 未生成 `auth.json`。 |
| 本机保护 | 正式 CLI daemon、App 主进程/后端、RA2A 的关键 PID 保留；正式配置、socket、App 桌面入口及代理配置的 inode、mtime、size 与实验前一致。 |

新能力/隔离回归的 Windows amd64 测试二进制交叉编译通过。该验证仍不覆盖 Windows/macOS 原生运行、真实模型/工具/限流、TUI 人工继续、跨设备四方向及其 20+ 多轮矩阵；本机已部署 `6a9e810`，但跨机投递仍待现场验证。复用入口见 [隔离实验 runbook](../../runbooks/codex-cli-isolated-daemon-experiment.md#自动验证真实-adapter-协议2026-10-02)。

### 跨机 CLI 协调会话归属修复（2026-10-02）

- ROG306 投递 ubuntu407 返回 CoAP `InternalServerError / DESKTOP_OWNER_UNAVAILABLE / no-client-found`，请求已到达服务。当前协调会话实际为 Warp 中的 Codex CLI，官方 `0.159.3` daemon 的 loaded list 包含该线程，`canAcceptDirectInput=true`；RA2A 配置却未登记 CLI，共享历史因此走 Desktop。
- 已备份原二进制与 RA2A 配置，原子部署干净提交 `6a9e810`，显式登记 `01a0f6ff-b902-7970-acba-ef8d3451c6f7`，仅重启 RA2A 服务。当前端点已发布为 `codex-cli`，具备 `steerActiveTurn`。
- 正式 CLI daemon、App 主进程及后端 PID 保留；Codex 配置、认证文件、官方 socket、App 桌面入口和代理配置的 inode/mtime/size 未变；RA2A 其他配置字段未变。
- ROG 重试通知 `f57a2ead5aab661fd844e4436cdcc584` 已实际到达本机 CLI，本机收件阻断恢复。两端指定验收会话仍发布为 `codex-app`，向 ROG 原地址回投也返回 Desktop `no-client-found`；需各自完成显式登记及服务重载。CLI 双向投递和多轮矩阵均尚未确认通过，不能将本次通知恢复记为正式 CLI↔CLI 验收。

### Ubuntu→ROG 真实 CLI 投递阶段（2026-10-02）

ROG 完成显式登记后，刷新发现确认双方端点均为 `codex-cli`、节点 ready、sessionsStale=false。使用现有 CLI 验收会话，不新建会话、不重启 CLI/App：

| 检查 | 结果 | 证据边界 |
| --- | --- | --- |
| Ubuntu→ROG 基础投递 | 成功 | `RA2A_V18_CLI_U407_ROG_BASIC_001` 返回 accepted。 |
| Ubuntu→ROG 连续投递 | 22/22 成功 | `RA2A_V18_U407_ROG_MULTI_01` 至 `_22` 逐条发送、全部 accepted，无自动重试。 |
| 活跃回合追加 | 未完整通过 | 观察到 ROG busy；`ACTIVE_FOLLOWUP` accepted，但 `ACTIVE_BASE` 返回 DELIVERY_UNKNOWN。禁止重发，等待 ROG 核对本地 ACK、turn 与完成证据；不能单凭追加成功判整项通过。 |
| ROG→Ubuntu 正式反向 | 待执行 | 已向 ROG 下达独立回合指令，接收本控制消息的回合仅 ACK，避免同步回投形成互相等待。此前准备通知不代替此项。 |
| MacMini↔ROG CLI | 待执行 | MacMini 指定会话仍发布为 codex-app；未向该地址发送正式 CLI 验收消息。 |

工具成功结果依据 adapter 的完成确认；ROG 本地输出、同一 turn 证明、TUI 人工继续与恢复矩阵仍需补齐。原始本机工具结果保留于忽略目录 `.cache/cross-cli-acceptance/ubuntu-rog-results.json`。本阶段不修改生产代码或正式配置，不重放不确定消息；不是完整跨设备准入结论。

阶段结束保护检查发现官方 CLI daemon 已从 `0.159.3` 更换为 `0.160.0`（当前 PID `3173242`、同一 Warp cgroup，官方 updater 仍运行），官方 socket 元数据改变；本阶段未调用 daemon stop/start，日志未给出可归因的更新记录，因此只记环境变化，不宣称受控重启验收通过。CLI 客户端仍报 `0.159.3`，RA2A 刷新后当前线程仍为可收件的 `codex-cli`。App 主进程/后端保留，Codex 配置、认证、App 桌面入口和代理配置元数据未变。

### MacMini→ROG 真实 CLI 多轮阶段（2026-10-02）

双方已登记为 `codex-cli`、节点 ready、sessionsStale=false。Mac 完成报告 `9c21d92f75591f39c788bd293088806a` 确认 `RA2A_V18_MAC_ROG_MULTI_01` 至 `_22` 串行工具成功 22/22、失败0、unknown0、无重试。原始完整入参/结果、发现快照和保护校验在 Mac `/tmp/ra2a-v18-mac-rog.4oFEtC/evidence.json`；本机尚未读取该文件，ROG 的 ACK/回合输出待接收端独立核对。

Mac 报告原生 CLI `0.159.3`、RA2A 协商 app-server `0.160.0`、已部署 `6a9e810`（version仍为v0.0.17）；RA2A config、Codex config/auth、codex launcher/codex.bin 前后 SHA256 均一致，未重启/升级/恢复或修改生产代码。ROG 实际版本待其补证。

已向 ROG 发出独立反向22轮任务，要求先等待 Mac 验收会话 ready、逐条确认、错误或unknown停止且不重发，并核对上述收件及此前活跃测试证据。长任务启动请求的确认窗口不作为22轮结果；反向完成报告及接收核验现已收到，见下节。该阶段不代表完整互通、TUI人工继续或恢复矩阵通过。

### ROG→Mac 反向结果与接收核验（2026-10-02）

ROG 完成报告 `627d579b30d15cb659b346df17a26a81` 确认 `RA2A_V18_ROG_MAC_MULTI_01` 至 `_22` 串行工具成功22/22、错误/unknown0、无重试；发送前确认 Mac 目标为 codex-cli/ready。本机未直接读取远端原始文件，以下为接收端核验报告：

| 项目 | 接收端证据 | 当前结论 |
| --- | --- | --- |
| Mac→ROG 22轮 | ROG本地22个对应ACK及独立task_complete turn ID | 双方报告互相印证，22轮完成。 |
| ROG→Mac 22轮 | ROG发送侧22/22 accepted；Mac核验22条原始输入、22条精确ACK及22个独立task_complete turn逐条匹配，无缺失/重复 | 双方报告互相印证，22轮完成。 |
| Ubuntu→ROG活跃追加 | BASE/FOLLOWUP输入元数据与双ACK属于同一turn `01a0fd08-e5ff-7ca3-bc0b-aabc3840700f`，task_complete=`2026-10-02T14:33:54.928Z` | 接收端同回合完成已确认；发送侧BASE DELIVERY_UNKNOWN仍保留，不判端到端整项通过、不重发。 |

ROG 原始入参/结果及本地核对证据位于 `C:\Users\77585\AppData\Local\Temp\ra2a-v18-rog-mac-20261002\evidence.json`。原生 launcher CLI 为 `0.159.0`，运行 app-server 的 releases/0.159.3 原生二进制为 `0.159.3`；前者不支持 daemon version，不能将 launcher 和宿主版本混写。ROG报告 Codex config/auth、RA2A config 的 SHA256、长度、mtime前后一致，本阶段无配置改动/重启/升级/生产代码改动。

长任务启动消息的15秒控制客户端超时另记为 DELIVERY_UNKNOWN，22轮结果依据实际逐条证据而非启动请求返回值。Mac 接收核验现已完成；本阶段 MacMini↔ROG CLI 双向各22轮文本投递通过，TUI人工继续、工具/真实模型行为与受控恢复矩阵仍不在通过结论内。

### 本轮双向多轮验收结论（2026-10-02）

Mac 只读核验完成报告 `8f58ad4037420091feee9e69f4437a9a`：ROG→Mac `_01` 至 `_22` 原始消息22条、精确ACK22条、task_complete22个及独立turn ID22个，均缺失0、重复0；每轮消息/ACK/task_complete同turn匹配22/22，均有task_started、无工具调用。证据为 Mac `/tmp/ra2a-v18-mac-receipt.ykj8Wt/evidence.json`，含逐轮message-id、turn_id、原始JSON事件及源行号；核验脚本 `verify.cjs` 同目录。接收源为 `/Users/zhangxu/.codex/sessions/2026/10/02/rollout-2026-10-02T02-37-19-01a0f8c1-b41a-79b1-85f2-5e0380b1d361.jsonl`。本机依据两端独立发送结果与接收记录核验报告，不声称已直接读取远端原始文件。

结论：本轮 MacMini↔ROG 的 CLI 双向各22轮文本投递通过，无错误/unknown/自动重试、无缺失/重复；本机→ROG额外22轮发送成功。接收核验未新增投递、修改生产代码/配置或重启。活跃回合追加的接收端同turn完成得到证明，但发送端BASE仍为 DELIVERY_UNKNOWN，保留未完整通过状态。单次成功和多轮完成不升级为 v0.0.18 正式发布或 CLI 全量准入；TUI人工继续、受控网络/daemon恢复及其余已记录缺陷仍待后续阶段。

### 协调汇报确认的补充边界（2026-10-03）

用户反馈 M4 仍看到 DELIVERY_UNKNOWN，具体发送标记尚待对照。Ubuntu 原生记录确认最后核验汇报 `8f58ad4037420091feee9e69f4437a9a` 在 `2026-10-02T15:53:50.904Z` 已收到，但本接收回合直到 `15:55:10.002Z` 才 task_complete，间隔79.098秒。HTTP控制客户端仅等待15秒，CLI adapter默认确认窗口30秒；这种长回合的实际收件与发送方unknown可以同时成立。

因此本轮通过结论仅覆盖双方报告及独立ACK/turn记录匹配的 `_MULTI_01` 至 `_22`，不覆盖协调汇报的端到端发送确认。已询问具体unknown消息，未重发、未将RPC接受改成成功、未静默延长窗口。客户端短于adapter窗口是待修正的预算问题，但仅延长至30秒也不能解决79秒回合；需要保持收到、执行完成及未知状态的区别。

### Windows 测试配置污染修复（2026-10-02）

ROG 现场确认：运行 `cmd/ra2a` 的 OpenCode-only 测试后，节点身份变为 `open-node`，测试 PIN 覆盖正式 PIN，引发 DTLS 握手超时；已从本地备份恢复身份与 PIN，通知恢复。源码独立核对确认两个测试仅设置 `HOME`，而 Windows 的 `os.UserHomeDir` 使用 `USERPROFILE`。

六个相关测试现共用临时 `HOME`、`USERPROFILE`、`LOCALAPPDATA`，写入前断言 `operator.ConfigPath()` 精确落在临时目录；同时避免四个 selftest/send/serve 测试读取正式 CLI 登记、mailbox 或迁移旧 Windows 配置。仅修改测试，不改生产路径。六项 Linux `-race` 回归通过，Windows amd64 测试二进制交叉编译通过；Windows 原生复验待 ROG 完成。正式 CLI/App 的关键 PID 和配置、认证、代理文件元数据保持不变。

ROG 另报 `codexcli` 的 `.cmd` 临时路径执行失败；当前 fake fixture 使用批处理，而宿主探测直接执行二进制。该项单独记录，完整错误与 Windows 原生复现尚待补齐，不通过增加生产 shell fallback 绕过测试。

## 收件与回复解耦修复（2026-10-03，PD33）

Owner 明确：收到了就确认收到，回复与任务完成属于下一阶段。产品权威记录于 [PD33](../v0.0.15/prd.md#confirmed-product-decisions)，取代旧实验推导的“等turn/completed才确认投递”。

- CLI adapter 仅等待原生 `turn/start` / `turn/steer` 有效输入 ACK；校验 start 非空turn ID且无内嵌error、steer为expected turn ID。随后返回收件成功，不等待模型、工具、最终回复或task_complete。
- 移除与收件无关的完成等待/cache、ConfirmWindow及过时白盒测试；保留明确登记、direct-input门禁、来源信封、只退订RA2A自身订阅和断线重连。App既有受理确认路径不改动。
- 缺失/异常ACK、拒绝、写入后断线或超时保留unknown，不自动重放、不改走其他writer。以后执行失败不回滚收件成功；接收方原生客户端仍负责执行/回复观察。
- 隔离复现先证明旧实现start/steer明确ACK后仍unknown；新回归覆盖无completed立即成功、后续执行失败、异常/缺失ACK、错turn及丢ACK只写一次。相关五包 `-race` 回归通过。
- 真实 `0.159.3` 独立home/mock模型实验通过：22轮收件与独立完成观察、原客户端继续、模型仍阻塞时active steer已经ACK、解除后完成及实验daemon重启恢复。正式home/认证未复制。`0.160.0` 同一隔离实验亦通过（3.46秒）；Windows amd64交叉编译通过。本机部署验证另补结果；跨机双方仍需部署新语义后复验，不将旧22轮结果当新契约验收。

### 部署与现场收件探针（2026-10-03）

修复提交 `e5d97f2` 已在合并保留远端 Windows 安装器提交 `181b77e` 后推送；本机从干净源码构建、备份原RA2A二进制/配置后原子部署，仅重启RA2A服务。正式官方daemon PID15822保留；Codex config/auth、官方socket、App桌面入口/代理配置的inode/mtime/size未变，RA2A配置字节一致。

M4 收件探针 `a358861a0b60a55fa9da99a0b4c04f54` 已到达，Ubuntu服务在 `2026-10-03T23:15:57.593+08:00` 输出 `cli_message_received`，mode=steer、turn_id=`01a10249-5318-70e2-8387-0a20b885fd1d`；本原生回合仍在处理，日志确认不依赖回合完成。M4报告 `7333db19c1530843e8f12aee33d874fe` 提供探针原始结果：status=accepted、isError=false，确认接收方尚未结束当前工作时发送侧已成功。新版M4部署完成报告待补。

已授权M4保留已有修改、备份后获取此提交，仅构建更新RA2A并重启其服务，不改CLI/App/launcher/登录设置。ROG节点仍ready，但当前不发布验收CLI端点，未向不存在的端点投递新版验收任务。本地修复与跨设备双方新版部署完成应分别记录。

## 本机 Codex App 地区登录错误调查（2026-10-01）

- 环境：Ubuntu，App `26.924.22138`，bundled Codex `0.158.0-alpha.2.1`；正常 CLI / 官方 daemon 为 `0.159.3`。
- 直接失败证据：浏览器 OAuth callback 成功，App 自带原生后端随后三次在 token 兑换收到 HTTP403、`unsupported_country_region_territory`。因此拒绝发生在后端兑换阶段。
- 网络对照：App Chromium 使用 GNOME 的现有 localhost 代理，原生后端没有继承 HTTP(S) proxy 环境；CLI 后端使用该现有代理。公开无凭据认证元数据 GET 经现有代理为 HTTP200、直连为 HTTP403，但 GET 拒绝页本身不作为 OAuth 地区 code 的替代证据。
- RA2A 归因边界：App 直接启动自己的 bundled 后端，进程树和启动日志不经过 RA2A wrapper/managed host；未发现 RA2A 产品代码写认证文件或调用登录/退出接口的因果证据。当前证据支持 App 原生网络环境差异，而非 RA2A 启动接管。
- 网络状态：已新建用户级桌面入口，原生后端使用现有代理，本地账号读取从约15秒恢复到5–30毫秒。Node 环境代理开关无效并已移除；App 入口加载用户目录的标准 TCP 代理库后，真实 durable WebSocket 初始化成功、状态 connected。默认线程 DNS 模式未通过 zygote 启动对照，已回退为验证成功的 TCP-only 配置，保留完整沙箱。
- **本机 App 问题已恢复（2026-10-02 用户确认）**：升级前用户发送消息仍卡启动，未见对应 `turn/start`。旧版真实 App 的 Git 检查子进程成功 exit 0，但 App 内部仍将其判为不可用；错误早于代理修复，不能据标签认定 Git 缺失或 RA2A 侵入。临时 debug/进程跟踪已移除。
- 升级结果：用户明确要求后，已完成系统 APT 原位升级至 `26.928.31416`，bundled 后端为 `0.159.2`；包与实际二进制校验通过，运行日志确认新版本。本地/云端初始化成功，启动日志未再出现旧版 Git 不可用错误；用户在实际验证后反馈“修复了”，本机故障关闭。保留登录、配置、会话和桌面代理入口；未直接改写凭据、删除缓存、改 App 包源码或重启正式 RA2A/CLI 服务。

复用诊断步骤见 [Harness runbook](../../runbooks/auto-harness-install.md#linux-codex-app-登录地区错误的诊断边界)。
