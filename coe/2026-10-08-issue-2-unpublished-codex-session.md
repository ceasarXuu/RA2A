# Problem P-001

- 状态：原目标归属冲突已确认，本机原地址发布及实际收件已恢复；M2 原链复验待在线。错误分类代码已提交，正式二进制尚未升级。
- 来源：https://github.com/ceasarXuu/RA2A/issues/2。
- 症状：目标会话一直打开，但 319 条发布列表不含目标，投递报 DELIVERY_UNKNOWN: endpoint not found。
- 已知事实：节点 ready、sessionsStale=false，远端业务错误证明 DTLS/CoAP 可往返。原目标 ID 已匿名化，Ubuntu 实际版本、宿主、用户及有效 CODEX_HOME 未采集。
- 基准：v0.0.18 与 e891277；相关枚举及分类路径相同。
- 修复标准：原目标发布、授权投递收到并回复；无替代会话/竞争 writer；缺失目标与投递未知准确分类。

## Hypothesis H-001

- 状态：rejected（本次原目标）；只读 thread/read 存在，发布缺失原因已由 H-002 证明。
- 主张：原始 thread/list 因宿主/数据根目录、state DB 或过滤条件缺少原目标。
- 预测：原始全分页列表不含目标；只读单变量对照能定位条件。
- 诊断证据计划：确认真实 ID、宿主、用户、有效 CODEX_HOME，对比 thread/list 与只读 thread/read，逐项对照 archived/sourceKinds/useStateDbOnly。列表含目标将反驳该分支；不执行 resume/start。
- 缺失：匿名 issue 不足以定位原目标，待 Ubuntu 现场上下文。

## Hypothesis H-002

- 状态：confirmed；E-004 与 E-006 确认持久 CLI 登记对已迁至 App 的同一 ID 形成双重排除。
- 主张：原始列表含目标，但 CLI 所有权排除、发布状态、endpoint 校验或枚举错误使 registry 缺失。
- 预测：原始列表含目标，registry 不含目标，存在具体排除/校验证据。
- 诊断证据计划：同时间对照原始列表、CLI 注册/发布和 endpoint problems。原始列表不含目标将降级此分支。
- 缺失：真实目标和 Ubuntu registry 证据。

## Hypothesis H-003

- 状态：confirmed；仅解释错误分类，不解释主故障会话缺失。
- 主张：registry 的 not_found 被 deliverOverLAN 包装为 ErrDeliveryUnknown，即使没有任何宿主调用。
- 预测：空 registry 直接 Deliver 返回 not_found，经 LAN 桥接返回 DELIVERY_UNKNOWN。
- 诊断证据计划：本地空 registry 测试，无 adapter、真实宿主或网络；若 LAN 保留 not_found 则反驳。
- 证据：E-002、E-003。

## Evidence E-001

- 对应：H-001/H-002 路径定位；类型：代码事实，不能确认现场根因。
- 结果：appserverprobe 使用 archived=false、useStateDbOnly=true、sourceKinds 和分页；codexapp 排除明确接受 CLI 注册的 ID；managedCommand 未覆盖 Env，继承 daemon 环境；registry.Lookup 忽略 Endpoints problems。
- 含义：仍有多个需要 Ubuntu 单变量证据区分的分支，不推断用户关闭会话或提供错误 ID。

## Evidence E-002

- 对应：H-003 源码预测；类型：代码事实。
- 结果：registry.explainMiss 返回 ResultNotFound；cmd/ra2a/registry.go 的 deliverOverLAN 仅区分 delivered/start_required，剩余结果统一 ErrDeliveryUnknown。v0.0.18 和 e891277 均存在。

## Evidence E-003

- 对应：H-003 受控测试预测；类型：本地诊断实验，2026-10-08。
- 命令：go test ./cmd/ra2a -run TestIssue2DiagnosticMissingEndpointClassification -v。
- 结果：PASS；registry=not_found; LAN=DELIVERY_UNKNOWN: endpoint missing-session not found; no adapters registered and no host delivery possible。
- 含义：支持错误分类机制，不证明 H-001/H-002。临时诊断测试不保留为以错误行为为标准的永久回归测试。后续现场步骤见 docs/v0.0.19/issue-triage.md。

## Evidence E-004

- 对应：H-002；类型：Ubuntu 本地进程、配置、日志和官方 socket 只读 RPC，2026-10-08。
- 当前开发会话 ID 为 01a0f6ff-b902-7970-acba-ef8d3451c6f7；RA2A config 的 cliSessions 仍含该 ID，日志持续报告 cli_ownership_unknown/reason=not_loaded。当前开发宿主为 Codex App（调用环境）；它与匿名 issue 原目标是否为同一会话仍待 Owner 确认。
- 官方 daemon socket initialize 报 0.161.0、CodexHome=/home/zhangxu/.codex；thread/loaded/list 返回 data=[]、nextCursor=null。只读 thread/read 找到该 ID，status=notLoaded、canAcceptDirectInput 缺失、source=vscode、originator=codex-tui。RA2A 独立 App Server socket 的对应结果也为 notLoaded。
- 含义：当前目标不是历史列表不存在，而是持久 CLI 登记与当前 App 宿主不一致：CLI loaded gate 排除，App 明确所有权过滤也排除。不能依据 source/originator 历史字段自动接管，也不能删 loaded gate。若 Owner 确认目标，应显式 release-cli 后仅重启 RA2A，核验同 ID App 端点；本轮尚未修改正式配置/服务。

## Evidence E-005

- 对应：H-003；类型：修复及永久回归测试。
- 修复：LAN bridge 保留 ResultNotFound，接收端编码 CoAP NotFound，发送端用 ErrEndpointNotFound 并在 control 转为 TARGET_NOT_FOUND。其他错误和已写入后未知仍保留 unknown，无自动重发。
- 附加修复：registry 查找无匹配且枚举出错时返回 enumeration_failed/unknown，避免将枚举失败宣称为确定缺失；其他 adapter 的健康目标仍可投递。
- 验证：go test -race ./internal/agentbridge ./internal/control ./cmd/ra2a ./internal/lannode -count=1 全部 PASS。TestMissingEndpointClassificationSurvivesDTLSCoAP 实际经过隔离 DTLS/CoAP；TestCoordinatorPreservesRemoteMissingEndpointAndUnknown 检查每例仅一次发送；TestLookupFailureDoesNotClaimEndpointAbsent 区分失败/真实缺失且不调用宿主。
- 局限：错误分类修复不等同原 issue 目标发布修复；原目标确认及授权同地址投递仍待完成。

## Evidence E-006

- 对应：H-002；类型：Owner 原目标确认与单变量操作，2026-10-08。
- Owner 确认故障链是 MacMini M2 → Ubuntu407，并提供原目标 ra2a://ubuntu407/01a0f6ff-b902-7970-acba-ef8d3451c6f7；与 E-004 的当前 App 会话一致。
- 先私有备份 RA2A 配置，然后使用既有 release-cli 解除该 ID 登记。对比解析后的完整配置，除 cliSessions 外所有字段严格相等（PIN 未输出）。仅 systemctl --user restart ra2a.service；daemon 222587 → 1689730。原生 CLI/App/daemon 等服务 cgroup 外 111 个受保护进程 PID/创建身份/路径均未变，Codex config/auth、官方 launcher、RA2A binary hash/mtime/mode/realpath 均未变。
- 新 list_targets：ubuntu407 ready/stale=false；原完整地址出现，agent=codex-app/status=busy/capabilities=receiveText,replyAddress,interactiveSafe。ID、地址和原会话保留，没有创建替代会话，没有移除 CLI loaded gate 或启用竞争 writer。
- 向原地址仅发送一次 RA2A_ISSUE2_ORIGINAL_APP_RECEIPT_20261008_001，工具 accepted；随后当前原 App 会话真实收到 from=同一完整地址、message-id=491e890e4fac1ab40e839e964a14e0dd 的原始输入。accepted 与实际收件分别证明，无重发/回投。
- 私有本地证据 .cache/issue2-handoff-20261008-031103/，before.json、verification.json 和权限 0600 的配置备份；配置备份含本地秘密，仅本机保留，不提交。
- 局限：M2 当前 discovery 为 unreachable，不能宣称 MacMini M2 → 原 Ubuntu 会话的跨设备复验完成。当前正式 RA2A binary 未升级到分类/双网卡修复提交；本次恢复由已有显式归属命令完成。

## Hypothesis H-004

- 状态：confirmed；对应 E-006、E-007。
- 主张：持久 cliSessions 和启动时静态 App 排除集合脱离宿主生命周期，迁移、关闭或恢复后仍保持旧排他归属，因此手工 release-cli 只能恢复单次现场，不能防止再次发生。
- 预测：wrapper 不维护动态登记，配置不随退出迁移失效；未 loaded 的登记目标仍被 App 排除；只有改登记并重启才改变发布结果。
- 诊断证据计划：独立源码生命周期跟踪与 E-006 单变量现场恢复对照；若登记有可靠退出/迁移失效机制、或 App 仅排除当前所有权，则反驳此机制。

## Evidence E-007

- 对应：H-004 生命周期预测；类型：独立代码路径研究。
- cmd/codex-wrapper/main.go 不写会话登记/lease；operator.AdoptCLISession 永久保存 cliSessions，ReleaseCLISession 只人工移除；cmd/ra2a/registry.go 在 buildRegistry 时将 cliAdapter.Registered() 复制给 App adapter 的排除集合。CLI ListEndpoints 另要求 loaded/directInput，不会清除静态登记或刷新 App 排除集合。
- 与 E-006 的“原会话仍在 App，旧登记解除后立即发布且收件”相互支持；排除归档、网络故障和历史 thread 缺失作为本次根因。

## Hypothesis H-005

- 状态：confirmed；对应 E-008。
- 主张：仅用 thread/loaded/list + canAcceptDirectInput 自动认定 CLI 活跃独占归属，会把 daemon 缓存也当作 TUI owner，不能根治竞争。
- 预测：官方实现没有 TUI 连接存活判定；unsubscribe 不卸载线程，idle loaded 可只是缓存。
- 诊断证据计划：核对固定 0.160.0/0.161.0 官方输入、loaded、unsubscribe 生命周期源码；若 capability/loaded 直接受 TUI 活跃租约约束，则反驳。

## Evidence E-008

- 对应：H-005 源码预测；类型：独立固定版本官方源代码。
- rust-v0.160.0 与 rust-v0.161.0 的 request_processors/thread_input.rs:12 仅拒绝特定 V2 ThreadSpawn 子 agent；普通 thread 的 canAcceptDirectInput 不证明 TUI 存活。
- thread_processor.rs:2777 loaded 来自当前 thread_manager.list_thread_ids()；:1020 unsubscribe 仅删除当前连接订阅；0.161.0 :4371 明确 idle loaded 只是缓存项。source/originator 同样不承担实时归属。
- 官方来源：https://github.com/openai/codex/blob/rust-v0.161.0/codex-rs/app-server/src/request_processors/thread_input.rs 和 thread_processor.rs。本机私有 .cache/owner-lifecycle/ 保存版本化源码。

## Hypothesis H-006

- 状态：confirmed；对应 E-009。
- 主张：当前 App 的只读 thread-owner-discovery 能提供本机现存 Desktop owner 信号，可在投递前验证并定向到同一 owner，而不用从历史推断。
- 预测：原 App 会话返回 handledByClientId 与 supportsUntrustedAppInput；不存在会话精确返回 no-client-found；协议 handler 依据当前 thread role。
- 诊断证据计划：已安装 App 源码协议表/handler + initialize/discovery 正反对照，禁止 start/steer、服务操作或配置更改。任意不支持/超时/缺失字段不能降级为“无 App owner”。

## Evidence E-009

- 对应：H-006 原生只读对照；类型：App 源码与实际 IPC。
- /usr/lib/chatgpt/resources/app.asar 中 thread-owner-discovery v1，params={hostId:local,conversationId}；handler 检查 getThreadRole===owner。bootstrap-CXJAEjVI.js SHA256=cc62d7cbd85833ea829a9b9dd3d0faba8a4c1652f49393de467456149ba5ec9a。
- 当前原目标查询 4ms 成功，handledByClientId=e8947ae8-741e-4848-a346-d54779f4a859，supportsUntrustedAppInput=true；不存在 UUID 对照 1ms 精确 error=no-client-found。仅 initialize/discovery 后关闭连接，没有业务写入。
- router 无 targetClientId 时选择首个可处理 client，不能宣称唯一；显式 targetClientId 可限定已发现 owner，后续 follower handler 仍复核归属。信号不得永久缓存。
- 剩余产品边界：同 ID 同时具备 daemon 输入能力与 Desktop owner 时，投递优先级/拒绝规则尚未由 Owner 确认。正在实现只读查询基础能力，尚未改变路由或宣称根治完成。

## Evidence E-010

- 对应：H-006 修复基础能力验证；类型：隔离 IPC fake 回归，尚未接入正式仲裁。
- desktopipc 新增 FindThreadOwner，只将完整精确 no-client-found 解释为空 owner；协议拒绝、不支持、缺字段、transport/cancel/deadline 均保留 error。保存顶层 handledByClientId，显式 SelectThreadOwner 给后续 start/steer 添加 targetClientId，发现不会隐式选择 writer。
- go test -race ./internal/desktopipc ./cmd/ra2a -count=1 两包 PASS；原默认请求与低层错误语义兼容，owner 查询与显式定向、取消/超时等新增测试通过。
- 新增生产代码保守统计 87 行（含字段格式调整），client.go 413 行，owner.go 56 行；本阶段 500 行预算继续生效。
- 没有部署、改变路由、写正式配置、重启服务或发送业务消息；不将基础能力通过等同根治完成。剩余为实时仲裁接入和双宿主冲突规则确认。
