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


## Evidence E-011

- 类型：Owner 直接确认，2026-10-08；覆盖 E-009/E-010 先前未决产品项。
- Owner 明确「谁持有则登记给谁，都未持有就保持睡眠状态」，并补充「持有者切换的时候也要能及时切换过去」。固定 CLI/App 优先级问题已被此确认取代，不再待确认。
- 产品权威 docs/v0.0.19/codex-ownership/prd.md 的 PD36/PD37；技术方案 plan.md 不得把平台取证缺口变成固定优先级或唤醒授权。

## Hypothesis H-007

- 状态：confirmed；对应 E-012。
- 主张：官方 writer 锁而非登记/历史/loaded 可证明实际写入归属，锁文件残留不能证明持有。
- 预测：当前原 App 目标 writer 锁对应 App backend；睡眠对照无 holder；独立锁释放后即使文件保留也无 holder。

## Evidence E-012

- 类型：固定官方 0.160/0.161 writer_lock.rs、正式原目标只读与独立原语实验。
- 官方 CODEX_HOME/thread-writer-locks/<ID>.lock 是空文件，以排他锁保持 writer 生命周期；协调锁只管理创建移除，异常退出可留残文件，两 tag writer_lock.rs 字节相同。来源 https://github.com/openai/codex/blob/rust-v0.161.0/codex-rs/rollout/src/writer_lock.rs。
- 原目标 Linux device/inode=103:0a:9306450，/proc/locks 精确 FLOCK ADVISORY WRITE PID888157；exe=/usr/lib/chatgpt/resources/codex，是 App 内置 app-server，进程身份与 App cgroup 匹配。App owner discovery client 与 E-009 一致。没有获取或修改正式锁。
- 真实历史对照 01a11132-9d61-7d62-a008-32ea588dfa9d 无 writer 锁，Desktop discovery 精确 no-client-found；仅历史存在不能发布。
- 独立临时 Rust File.try_lock 实验：PID1706610 持锁时 /proc/locks 指向它，额外只读打开不改变 holder，释放后保留文件但无锁记录；实验退出清理完成，.cache/owner-lifecycle/primitive-result.json 保存非敏感证据。
- 跨平台局限：Darwin F_GETLK 的 flock PID=-1，FHASLOCK/FWASLOCKED sticky 不能当当前持锁 PID；Windows LockFileEx 尚无已证只读实际 holder API。不能用 openers/RestartManager 代替持锁者。

## Hypothesis H-008

- 状态：confirmed；对应 E-013。
- 主张：投递前重复锁检查并非原子条件；现 CLI resume 和 App 恢复分支可在持有者变化后创建 writer，违反睡眠规则。
- 预测：CLI 原生已有线程投递无需 resume；App follower UI 路径可能在角色失效后恢复，而现 IPC 无禁止恢复条件。

## Evidence E-013

- 类型：独立固定官方源代码与已安装 App 调用链只读核验。
- 官方 0.160/0.161 turn_processor load_thread 只调用 thread_manager.get_thread 内存 map；缺失 ThreadNotFound，不 coldresume/读历史/获取 writer 锁。turn/start/steer 同步响应不依赖订阅。来源 https://github.com/openai/codex/blob/rust-v0.161.0/codex-rs/app-server/src/request_processors/turn_processor.rs 和 core/src/thread_manager.rs。
- 本项目 delivery.go 仍在读和写之前 threadResume，随后 unsubscribe；可独立删除此主动恢复环节，保留已有线程读取/directInput/active-turn gate 和一次写入。
- 已安装 bootstrap-CXJAEjVI.js：lx assertThreadFollowerOwner→startTurn→nue/rue；rue/cue 经 Zb 在角色迁移且 no-client-found 后调用 resumeConversationForUnavailableOwner。targetClientId 只固定 IPC client，不冻结 backend writer。没有发现 JSON 可传 loadedOnly/allowResume=false/expectedWriterPID/epoch；内部 assertRequestCurrent/beforeSendRequest 钩子不可远程注入。
- 因此 IPC 查询基础能力并不等于安全末端；App 动态集成继续等待安全已有 backend 接口证据。无业务写入、正式配置/服务修改或锁获取。


## Evidence E-014

- 类型：Mac/Windows一手接口源码的独立只读调查。
- Darwin XNU f6217f891ac0bb64f3d375211650a4c1ff8ca1ea kern_descrip.c sys_flock以fileglob作为owner，kern_lockf.c F_FLOCK设置lf_owner=NULL，lf_getlock返回PID=-1；lsof 1ebf257c64db1b2ece5e4d5e922ed711c692f161 Darwin dfile.c没有实际lockowner读取。单一opener和sticky FWASLOCKED不证明当前owner。来源 https://github.com/apple-oss-distributions/xnu/blob/f6217f891ac0bb64f3d375211650a4c1ff8ca1ea/bsd/kern/kern_lockf.c。
- Windows LockFileEx只返回成功失败；FileProcessIdsUsingFileInformation/RmGetList只有文件资源使用者集合，不能证明byte-lock holder；FILE_LOCK_INFO有ProcessId但属于内核system-use。来源 https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-lockfileex 和 https://learn.microsoft.com/en-us/windows-hardware/drivers/ddi/wdm/ne-wdm-_file_information_class。
- App原目标backend stdin/stdout是匿名socket，并非可另接WebSocket入口；复制FD注入会竞争App协议流，不是安全替代。完整动态仲裁和正式部署仍不进入；不部署unknown占位reader破坏当前CLI/App使用。

## Evidence E-015

- 对应：H-008独立CLI修复，范围rebase记录于plan.md Phase B；类型：源码最小修复、相关race与真实原生隔离验证。
- delivery.go删除投递前threadResume及此次unsubscribe；保留实时thread/read/directInput/active-turn与原生一次start/steer。写入错误仍unknown、不跨宿主重试。新增2行生产注释、删除旧恢复代码，本阶段保守新增累计89行，修改源文件均≤500行。
- fake已纠正错误的“必须resume后start”假设，以官方已有内存线程模型拒绝unloaded；测试覆盖发现后unload不resume/不写、read后start/steer前unload不resume/不重放，原收件与capability回归保留。
- GOPROXY=off go test -race ./internal/codexcli -count=1 PASS。绝对真实0.161.0二进制RA2A_TEST_CODEX_BIN opt-in TestNativeCLIIsolatedDelivery -race/count1/timeout180s PASS，24.08s：22receipt+独立owner执行/继续+heldmodel活跃receipt+隔离daemon重启+DTLS/CoAP目标隔离/关闭节点恢复/丢ACK取消不重放。
- 临时home /tmp/ra2a-native-210171139 已移除；两个临时daemon PID1712109/1712475均不存在。临时native stop记录官方自身forced shutdown，未作用于正式宿主。
- 未部署正式RA2A、未调整正式配置/官方宿主。上述仅验证独立禁止唤醒子项，不代表三平台动态仲裁或App安全末端已完成。

## Evidence E-016

- 类型：Owner 提供新的跨设备协助会话，2026-10-08；继续 H-007/H-008 的平台取证，不属于修复验收通过。
- Mac 完整地址 ra2a://macmini-m4/01a117fc-febe-71c3-9460-6914ff413bc5；ROG 完整地址 ra2a://rog306/01a117fc-7d0b-7bb0-9070-e4920fa98eb9。实时 list_targets 两节点 ready/sessionsStale=false，两端点 agent=codex-app/status=busy。这里只证明发布字段，不据此证明官方实际锁归属。
- 显式 from=原 Ubuntu 地址，分别唯一发送 RA2A_OWNER_MAC_PRECHECK_20261008_001 与 RA2A_OWNER_ROG_PRECHECK_20261008_001。原始工具结果各 status=accepted/to=对应完整地址/isError=false，没有重发。accepted 仅证明收件，现场核验报告尚待接收。
- 指令只做有界只读版本/产物来源/锁文件与宿主身份/脱敏保护基线/既有CLI验收端点核验；无正式锁获取、会话唤醒、部署或多轮业务测试。平台 actual holder 不能证明须明确 unknown，不把使用文件者或历史加载当锁持有者。

## Evidence E-017

- 类型：ROG 新协助会话唯一前置报告，消息ID04f0ae4b21d2976fc106aa997d595189；对应E-016的实际平台证据目标。
- standalone真实CLI0.160.0，App内置二进制真实版本0.162.0-alpha.2；另有backend路径含0.161.0，但路径不是运行版本证明。当前实际writer holder PID及本session backend精确关联unknown，没有发布codex-cli验收端点。
- ROG报告此前用户授权main快进和install.ps1已部署磁盘vcs.revision=38589fbca9e36514ed8aca748cc0c6ce8aeab4ad、vcs.modified=true；repo当前HEAD38589fb、tracked diff0，只有既有.commandcode/。构建修改位未澄清，不以revision或当前clean等同完整产物来源。daemon此前11600→3184；本次前置核验没有部署重启。
- 正式锁file ID=0x000000000000000001c100000000b62e；hash读取共享冲突，不重试/取锁，文件存在或共享冲突不证明holder。RA2A own lease/socket只证明managed进程，不证明thread writer归属。
- 私有证据C:/Users/77585/AppData/Local/Temp/ra2a-owner-precheck-e2f8fd1f15954b53b5b0218b475fef58/precheck.json；ROG报告保护终点hash无变化，无PIN/token/auth正文。现场结果尚未独立读取原始附件，作为接收端报告证据保留。
- E-013的App调用链结论基于Ubuntu已安装版本，不自动外推至ROG0.162.0-alpha.2，需要固定其实际App来源核验。

## Hypothesis H-009

- 状态：blocked；对应E-017/E-019。已检查的新版follower路径仍带resume，不能通过门禁；完整接口集合缺schema，不把局部证据升级为全接口不存在。
- 主张：ROG当前App版本可能提供与Ubuntu不同的已有writer-only投递接口；现有证据不能确认或排除。
- 预测：若有可用接口，已安装协议schema与handler应出现可由外部JSON调用的禁止恢复/预期writer条件，且执行路径在角色失效或not-found后不resume。只有内部函数钩子、UI owner或普通turn/start名称不足以通过。
- 诊断证据计划：ROG只读固定实际App包hash/版本，定位owner discovery、follower start/steer及恢复分支调用链、支持的请求字段与backend传输形态；不调用业务接口、不获取writer锁、不修改宿主。明确参数和无恢复路径支持假说，仍有恢复或无外部入口则反驳当前可用接口主张。

## Evidence E-018

- 类型：Mac 新协助会话唯一前置报告，消息ID1431be448ff2fb03732529c77482a6a8；对应E-016实际平台证据目标。
- App26.1002.52244内置官方backend0.162.0-alpha.2，官方CLI0.160.1；另有独立daemon路径标注0.161.0，不等同真实版本。锁device16777234/inode37785033/size0；lsof只证明App backend PID8513打开文件，实际holder仍unknown。独立CLI验收会话unknown，无额外官方backend连接或owner RPC。
- Mac报告此前用户授权部署来源临时clone /tmp/ra2a-dev-h4OG7M2k，RA2A显示v0.0.18、revision38589fbca9e36514ed8aca748cc0c6ce8aeab4ad、modified=false、binary SHA256=f0091e06e9a02bc58471e1894d22edc88b224a6dac2d8337f54daa5d2fef7af8、运行PID71269映像inode一致。原分叉repo HEADffb52fd、ahead5/behind156和.commandcode/保留。本轮未部署/服务操作。
- 私有证据/tmp/ra2a-owner-precheck-20261008-001/evidence.json；保护hash/mtime/进程身份基线仅本轮快照，不推断未来持续不变，未输出凭据。与ROG共同触发H-009当前App新版本接口只读核验。
- 向ROG与Mac各唯一发送APP_INTERFACE_20261008_001控制请求，均原始accepted/isError=false，尚待接口源码报告；仅诊断，不做动态切换或业务验证。

## Evidence E-019

- 类型：ROG新版实际App包只读调用链报告，消息ID56635feaf5a6e0d8baeb9d74d5960558；对应H-009禁止恢复/预期writer条件证据计划。尚未独立读取本机私有附件，保留为接收端详细源码报告。
- 实际Appx26.1002.7124.0，asar package版本26.1002.52244，backend0.162.0-alpha.2；app.asar SHA256=76fe7078248c00e4e03dd2177a4275ec9ce158a9dd43452a4f0427d39a4ed012；bootstrap-Dz9A8y86.js=a70497f5ffa764fe74f4de7ac05a5f73aff8d1e2f44de4476d1d9a684af43ddd；src-BPM2XJL0.js=0f43ea10acf2852395ba10a8a24838bc5646b8543197e8eeadd1e4fadea4c7b1。
- bootstrap _se入口assertThreadFollowerOwner，start只调用startTurn(conversationId,turnStart)，steer转发既定输入/附件/context/toolOutput字段；Fue/Bue await之后经Yv，Yv捕获follower no-client-found后markConversationNeedsResumeForUnavailableOwner→resumeConversationForUnavailableOwner，随后本地执行。resumeThread明确sendRequest thread/resume。因此targetClientId不保证writer epoch，也不禁止睡眠窗口恢复。
- start上下文内部beforeSendRequest/assertRequestCurrent是可调用函数钩子，外部JSON不能提供函数；steer入口不转发这些钩子。协议版本表不是完整schema，未拿到完整schema，不能宣称全部接口没有loadedOnly/expectedWriter。已核实入口没有已证安全条件，动态集成门禁继续未通过。
- application-network-startup-Bt0a8E1L.js=aa5478e31c8a623632dfedffcecfbe40d5033d38a8504649716657681a0840b4：spawn stdio三pipe，send JSON+newline写proc.stdin，native参数app-server --analytics-default-enabled无listen。实际backend28172/parent12628也hasListen=false；支持当前App自身stdio，不证明可另连接的安全socket，未复制或注入正式流。
- 对ROG产物modified=true补充：当时部署聊天记录只见.commandcode/未跟踪、tracked diff0，与Git未跟踪dirty标记相容；没有精确构建输入manifest，不能宣称可复现来源已证。现场保护hash及正式进程创建身份报告未变，未业务请求、部署或重启。
- 私有证据在ROG PRECHECK目录app-interface-excerpts.json、interface-final-baseline.json，包含文件/offset/源码摘录。Mac同版本独立报告仍待返回；本项目只补充SelectThreadOwner注释，明确pin客户端不锁writer且不关闭resume回退，避免未来误用，不改变功能。
