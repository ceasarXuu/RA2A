# Problem P-001: Linux矩阵缺真实App安全接入
- Status: open
- Objective: 补齐真实私有Linux App接入，按实际writer选择stdio桥，无人持有保持睡眠；不修改正式App或使用自动resume的IPC投递。
- Known facts: matrix-006 App项BLOCKED_ADAPTER；原stdio桥仅允许既有thread方法，未经真实App接入；用户要求「修完」。Owner随后明确批准本阶段累计900行及6.1-sol light模型（本机准确档位low）。
- Fix criteria: 真实App原始初始化、独立GUI、实际writer到bridge匹配、预算先预留、原生收件/完成及关闭恢复；不能以mock或窗口存活判完整矩阵通过。
- Resolution basis: 尚未满足。

## Hypothesis H-001: 新spawn入口能够安全复用App自有stdio
- Status: confirmed
- Claim: CODEX_CLI_PATH可限定在新的私有App，让原App初始化四条新管道上的stdio桥，不需要接管正式FD或使用有resume回退的IPC。
- Diagnostic evidence plan: 先零认证/零模型/隔离网络真实App；记录原App initialize成功、bridge loaded查询、窗口/IPC及进程退出；如果无ready或接入正式transport则反驳。

## Evidence E-001: 真实私有App零模型桥验证
- Related hypotheses: H-001
- Direction: supports
- Source: .cache/sandbox-discovery/app-bridge-probe-001/summary.json及原始私有诊断。
- Matched signal: 原Appinitialize outcome=success/transportKind=stdio；bridge thread/loaded/list成功loadedCount0；真实窗口/backendIPC出现。零凭据、零模型输入、无宿主网络；自有App/显示/wrapper均回收。
- Interpretation: 可以在仅新建沙盒App接入；真实App12秒内spawn3个wrapper，不能假定单个后台。第一轮无ready证据，第二轮预建private CODEX_HOME并加stderr后成功，不把目录缺失宣称唯一根因。

## Hypothesis H-002: 私有socket加实际peer身份能限制桥调用者
- Status: confirmed
- Claim: 每wrapper单独Unix RPC、peerPID+创建ticks验证，可拒绝Agent工具进程绕过预算；actual writer PID+exe匹配该wrapper自有native，睡眠拒绝。
- Diagnostic evidence plan: 自有RPC连接对照允许worker的PID/ticks与拒绝其他进程；真实native fixture验证实际writer对应桥、关闭释放、睡眠输入不产生writer。测试需在新PID namespace共享视角，不能用hostPID代替peerPID。
- Evidence gate: 已通过实际内核writer、不同进程peer拒绝及原生隔离对照；完整GUI矩阵仍未完成。

## Evidence E-002: 生产桥初始化与原生收件对照
- Related hypotheses: H-001, H-002
- Direction: supports
- Source: .cache/sandbox-discovery/app-bridge-probe-001/final-summary.json；internal/sandbox/app_linux_test.go、app_native_linux_test.go。
- Matched signal: 真实私有App通过新生产wrapper完成两次stdio初始化，保护controller访问两个socket均HTTP200，零模型输入，全部自有资源回收。race测试通过；显式官方App内置0.160.0后端验证实际writer、忙时steer相同turn、原owner独立继续、释放后睡眠拒绝且无新增mock调用。不同进程peer被403拒绝。
- Interpretation: 安全接入模块已验证；原生fixture使用mock provider，不证明真实GUI消息渲染或收费模型完成。默认运行器仍BLOCKED_ADAPTER，不能升级完整验收状态。

## Hypothesis H-003: GUI控制pipe在独立user namespace中可阻止Agent绕预算
- Status: refuted
- Claim: 实际App使用无TCP的CDP pipe，额外隔离App user namespace；无CAP_SYS_PTRACE的同节点Agent不能重开App/proc/fd，可信controller仍能驱动界面。
- Diagnostic evidence plan: 通过现有Runtime._command的cap-drop ALL路径启动同节点进程，对实际App的fd3/fd4仅尝试重开，不读写pipe内容；同时核对namespace与cap字段。若可重开则反驳并停止收费GUI输入，不能由controller自身可读外推Agent可读或不可读。

## Evidence E-003: 实际Agent重开CDP pipe
- Related hypotheses: H-003
- Direction: refutes
- Source: .cache/sandbox-discovery/app-runtime-probe10/result.json。
- Matched signal: 现有Runtime._command启动的同节点Agent，CapEff=0、不同user namespace，仍可重开真实App fd3/fd4，拒绝结果为false/false。
- Interpretation: 仅额外user namespace不足，停止收费GUI输入；不能据controller访问权限证明Agent隔离。

## Hypothesis H-004: 专属Electron进程dumpable保护能封闭调试pipe
- Status: refuted
- Claim: 仅新沙盒App通过可信readonly guard加载，真实ChatGPT ELF启动时设置PR_SET_DUMPABLE=0；native backend不设置，保留actual writer exe身份读取。调用者仍不能设置loader override。
- Diagnostic evidence plan: 实际同kind无cap Agent仅重开App fd3/4需拒绝，可信controller仍能完成CDP，worker仍可匹配真实native writer；如Chromium恢复dumpable或持有者门禁失败则反驳，禁止进入收费验收。

## Evidence E-004: constructor提前保护阻止Electron sandbox初始化
- Related hypotheses: H-004
- Direction: refutes
- Source: .cache/sandbox-discovery/app-runtime-probe11。
- Matched signal: 加载立即PR_SET_DUMPABLE0的guard后真实App FATAL No usable sandbox，CDP closed；同uid1000无guard路径此前可初始化。
- Interpretation: 当前保护时机不可用；未证具体uid_map拒绝机制，不关Electron sandbox、不发送模型输入。

## Hypothesis H-005: renderer初始化后激活进程保护可兼顾Electron及预算
- Status: confirmed
- Claim: 可信controller通过真实CDP确认document.body后写专属激活文件；guard线程随后PR_SET_DUMPABLE0并回写main PID；controller经真实cap-drop Agent路径验证两FD均PermissionError且CDP仍可用，才允许任何模型输入。不是固定延时；不提供解除保护入口。
- Diagnostic evidence plan: 真实App无模型诊断需实际初始化、激活ACK、Agent fd3/4拒绝、controller继续CDP和native writer门禁；失败止步。矩阵启动先完成App保护，再启动其他Agent模型bootstrap。

## Evidence E-005: 动态保护与私有认证界面门禁通过
- Related hypotheses: H-005
- Direction: supports
- Source: .cache/sandbox-discovery/app-runtime-probe12/、app-auth-probe-001/result.json与脱敏截图。
- Matched signal: 真实Electron sandbox正常、document.body初始化；激活后mainPID14 ACK，同节点真实cap0 Agent两FD均PermissionError，controller CDP继续可用，App stdio initialize成功。私有正式认证副本下editorAvailable/protected均true，界面显示GPT-6.1 Sol Light；bootstrapSubmitted=false、budgetReserved0，worker ready/stale=false且尚无会话。
- Interpretation: 支持此保护时机与最小认证投影；未提交模型、不将编辑栏或空端点当完整矩阵通过。正式auth/config终点SHA256相同，临时副本已清理。

## Evidence E-006: 额外显示凭证路径仍暴露
- Related hypotheses: H-005
- Direction: neutral
- Source: .cache/sandbox-discovery/app-runtime-probe13/result.json。
- Matched signal: 同节点cap0 Agent重开CDP fd被拒绝，但可经某App相关PID的proc/root读取display.auth及控制文件；自己的/tmp显示socket不能连接。
- Interpretation: CDP门禁通过不证明全部GUI输入边界封闭，暂停收费矩阵。需精确区分PID及实际X11认证连接，不能仅socket connect或cookie文件可读就宣布有真实模型输入。

## Hypothesis H-006: 已连接GUI无需长期保留可读取的显示cookie文件
- Status: confirmed
- Claim: 每次App只mount专属auth副本，真实X11连接初始化后controller清空该inode；原显示auth仅hostcontroller持有，后续App重开复制新副本。既有真实界面继续可用，Agent不能再经任何App进程root取得cookie发起新的认证X11连接。
- Diagnostic evidence plan: 对照清空前后实际X11 setup认证结果，精确PID/root路径；清空后CDP截图/界面仍可用及重开新App可认证，零模型。不依赖固定延时或关闭X11认证。

## Evidence E-007: 每App独立显示cookie副本清空后实际认证拒绝
- Related hypotheses: H-006
- Direction: supports
- Source: .cache/sandbox-discovery/app-runtime-probe16/result.json；probe15失败诊断保留。
- Matched signal: 本轮guard ACK PID13严格对应main，无--type；两CDP FD所有open模式拒绝。清空前Agent通过精确main proc/root读取一次性cookie并完成真实X11 setup认证；只清空App专属副本后cookieBytes0，无cookiesetup首字节0/长度8拒绝。既有App CDP/body/screenshot正常，停止后由原hostauth新复制的App也可打开。
- Interpretation: 支持候选最小修正；probe15诊断误清原Xephyr auth导致服务器不再要求cookie，不能当产品根因。必须保留controller独占的显示服务原auth，仅清空每App客户端副本，不能用文件可读、socket connect替代实际认证结论。零模型，无额外X11 ACL修改。

## Evidence E-008: 冻结真实矩阵007未通过
- Related hypotheses: H-001, H-002, H-005, H-006
- Direction: neutral
- Source: .cache/sandbox-discovery/matrix-007/summary.json、ledger.jsonl；source239852f，dirty=false/sourceChanged=false。
- Matched signal: App与CLI真实bootstrap均出现原生task_complete，但均BLOCKED_ADAPTER/RuntimeError且未发布端点；保护源8文件SHA256终点不变、自有资源REAPED、临时凭证副本清理。App1/CLI1/OC7/Pi6，各limit10，未自动重发。OC/Pi四个基本格、工具回信、恢复及ACK丢失原生完成通过；OCBASE活跃输入未找到独立parent final而FOLLOWUP已完成。
- Interpretation: 整体INCOMPLETE，不能因UI输入或其他Agent通过升级App接入成功。App第一拒绝门禁未保存，清理后无法补现场归属。CLI源码枚举共享HOME全部历史首meta存在错误选择；本轮actual registeredUUID未持久保存，不能伪造已证误登记。后续须只登记TUI启动后新文件，并以零收费插桩定位App首拒绝门禁，保持安全检查不放宽。

## Evidence E-009: OpenCode真实忙时parent切换补证

- Source: .cache/sandbox-repair/opencode-working-002/result.json；真实1.18.34/openai gpt-6.1-sol low，两条新输入继承matrix007原账本7→9/10。
- Matched signal: BASE/FOLLOWUP各唯一user，busy gate通过，四次list_targets唯一callID均完成；第1次parent为BASE，后3次及唯一final parent为FOLLOWUP。最终精确两个ACK换行、finish=stop/completed存在；没有两个独立final。
- Interpretation: 原匹配仅BASE parent会漏判这一原生合并完成；修正明确指令和组合关联，保留BASE.native=PENDING及ONE_COMBINED_NATIVE_FINAL说明，不伪造第二final。单ACK、无关parent、重复输入的拒绝回归通过。matrix007旧结果不回写升级。正式8项hash相同，原账本不改写，资源与私有凭据回收。

## Evidence E-010: CLI真实新会话选择补证

- Source: .cache/sandbox-discovery/cli-history-fix-001/result.json；绑定2b4e7fc、dirty=true（根工作树当时有已知OC修复），临时测试仅替换新的bootstrap预算ID，不将该诊断当冻结完整矩阵。
- Matched signal: 预置App历史hash不变，实际codex-tui创建新ID并发布codex-cli，READY_SANDBOX final/task_complete同turn匹配。gpt-6.1-sol low，继承原CLI1→2/10，只有1条新输入。
- Interpretation: 支持启动前历史排除的局部修复；source=vscode仍不能识别终端。正式8项hash相同，私有资源及凭据回收；未采正式PID集合，不外推其全过程无变化。

## Evidence E-011: App成功initialize后遗漏通知导致桥永久未就绪

- Source: .cache/sandbox-discovery/app-gate-real-003/gate-safe.json、app-gate-zero-004/gate-safe.json；独立诊断只固定布尔/enum，不记录协议正文或凭据。
- Matched signal: 真实App多backend原initialize成功且experimentalApi=true；protected worker peer/ticks通过。首个loaded查询明确errors.Is(ErrNotReady)=true、ErrClosed=false；只有首backend发initialized，其余未发，但原App请求继续工作。收费唯一第2条继承原预算；后续精确对照零输入。
- Interpretation: 原桥把initialized通知设为必需门禁，是可证根因。以唯一原App成功initialize响应设ready，不二次initialize、不改原参数，仍拒绝初始化失败、resume/thread-start及无writer投递。单元race通过失败初始化拒绝+成功响应无通知可查询；真实App内置官方0.160.0 fixture移除通知后收件、owner continuation、睡眠拒绝均PASS（自有child取消后signal killed/reaped）。该fixture仍不是完整真实GUI矩阵。

## Evidence E-012: 修复后真实GUI收件与关闭撤回

- Source: .cache/sandbox-discovery/app-reverify-005/summary.json、result.json、app-ui.png；绑定7817058 archive，生产没有诊断插桩，测试仅新的bootstrap预算ID/文本。
- Matched signal: gpt-6.1-sol low，原App2→4/10；bootstrap native task_complete；真实codex-app ready端点发布。唯一RA2A自投accepted，原始user/final各1、精确ACK和task_complete同turn，实际截图中独立assistant ACK回复行可见。窗口关闭后端点撤回；writer/canAccept检查原样保留。
- Interpretation: 已证App初始化修复的真实GUI专项通过，正式Auth/config终点hash相同，自有资源和私有副本清理。不是完整16格矩阵、App忙时/恢复/CLI持有者交接或人工验收通过；旧matrix007仍INCOMPLETE。累计各类CLI2/App4/OC9/Pi6；已请求新的独立全量轮额度批准，未自动清零执行。

## Evidence E-013: 新轮matrix008与ACK故障sender节点误选

- Source: .cache/sandbox-discovery/matrix-008/summary.json、report.md、ledger.jsonl；3ed3e89源码冻结dirty=false/changed=false，整体INCOMPLETE。14个basic PASS，Pi→App包含工具控制输入无final，App self依赖未跑；两项工具回信FAIL。CLI/OC/Pi busy+resume PASS，OC组合完成模式明确；CLI ACK-loss收到accepted，OC/Pi UNKNOWN+原生唯一完成PASS。
- Matched signal: 四类预算CLI10/App4/OC9/Pi8，没有重试。runtime REAPED/私有凭据清理/正式8项hash一致。App挂起的exec包含实际mcp__ra2a__send_message且显式正确from/to，MCP及code-mode-host存在、命令可执行，未关联到工具错误或modelerror，根因待透明stdio诊断；guest_load_failed不当作RA2A故障因果。
- Interpretation: ACK-loss从endpoints插入顺序选了App→CLI，但两者共享Codex节点；control.AdapterCoordinator同node分支直接registry.Deliver，不走lannode handler的withhold。这与accepted吻合，是已证测试路径缺陷。修正按实际address节点选择不同且未失败的sender；同节点/失败节点排除测试PASS。完整CLI真实ACK故障旧项未计通过，额度已满不重试。当前阶段生产新增900/900；后续仅取证，不擅自扩张修复代码。

## Evidence E-014: 透明MCP诊断的启动器环境失败

- Source: .cache/sandbox-discovery/app-mcp-proxy-006/result.json、summary.json；仅测试副本，生产源码未改。
- Matched signal: App已提交一个bootstrap；Pi环境误继承App专属socket目录，worker拒绝启动，无Pi端点，控制消息及嵌套MCP调用均未执行。正式认证配置终点hash相同，自有资源和凭据副本回收。
- Interpretation: 属于诊断启动器问题，不能归为产品投递失败。新增App输入仍计入matrix008累计4→5/10，Pi8/10、CLI10/10、OC9/10不变；不重放旧marker、不自动开新轮。后续先零输入核验独立环境、worker和透明代理握手，通过后才使用剩余额度。

- Follow-up source: .cache/sandbox-discovery/app-mcp-proxy-007/result.json。独立Pi环境、两worker API及透明MCP initialize/tools-list预检通过；测试代理日志目录须在预检前创建。随后唯一新bootstrap已提交，App累计6/10，其他类别不变；未发送control/reply。
- Follow-up limitation: 诊断端点门禁仅12秒，短于生产默认35秒及矩阵90秒；只证明该窗口内未ready。失败前没有保存native完成/代理快照，清理后不能补证。不得据此宣布产品回归或MCP导致失败；后续先零输入核验代理启动，使用生产窗口且异常清理前保存安全投影。

## Evidence E-015: 透明代理零输入通过但有界App启动未发布

- Source: .cache/sandbox-discovery/app-mcp-zero-008/result.json、app-mcp-proxy-009/result.json。008抑制自动bootstrap：真实GUI body/editor、guard和进程可用，App自身initialize/tools-list返回非error，输入0；不以UI代替bridge ready/writer归属证明。
- Matched signal: 009沿生产90秒窗口，只提交一个全新bootstrap。native ID 01a12d28-19f1-7662-a756-e2c1edadb04c，originator Codex Desktop，同turn task_started/task_complete存在；该窗口未发现ready App端点。透明代理initialize/tools-list均返回非error，tools/call为0。控制输入与Pi回信未执行；清理前保存安全native计数和代理元数据。
- Interpretation: 尚未进入原pending嵌套send路径，不能宣布其根因、修复或通过。累计App7/10、Pi8/10、CLI10/10、OC9/10，未提交的App第8条/Pi第9条不算消耗。正式Auth/config终点hash相同，自有资源与私有副本回收。停止付费重复，只读对照诊断与原矩阵启动差异；原matrix008整体INCOMPLETE保持。
- Static comparison: 009/summary.json记录生产bridge/adapter/main及app_client/runtime/clients六文件hash与当前源一致。诊断改变MCP command为透明代理，并在App启动前额外预检；初次endpoint要求ready=True，而原matrix初次默认False。未保存实际targets投影、thread/read capability或kernel writer PID/exe/ticks，因此不能据筛选差异认定其为未发布的根因；下一取证须先覆盖这些门禁信号，不能再仅保存异常字符串。

## Hypothesis H-007: App发现拒绝发生在实时owner/capability门禁

- Status: open
- Claim: 原生bootstrap完成但端点缺失，是loaded、初始化、真实writer或canAccept中的某个实时门禁拒绝，而非模型未完成；具体门禁未知。
- Prediction: 完成后首次拒绝的安全日志会明确loaded查询错误类别、writer PID/exe/ticks匹配或canAccept=false；若全部通过且targets包含App，则该假设降级，转查helper筛选。
- Diagnostic evidence plan: repair010冻结85a2a01，仅测试副本插桩，原判定保留。新bootstrap后逐socket采集ready/closed安全布尔、loaded ID、kernel writer/native身份匹配、thread/read canAccept和status、实际targets安全投影；默认ready=False发现。异常前保存，禁止正文、凭据、thinking和正式锁获取。通过才执行最多一条新send控制，透明MCP仅方法/ID hash/响应元数据分离通信层与投递层。

## Evidence E-016: Owner批准独立新轮修复额度

- Source: Owner直接消息「再给一轮修复额度」。
- Matched signal: 授权每类10条的新独立修复账本repair010，沿既定模型与认证保护；旧matrix008账本及失败结果不改写。先有界门禁诊断，不自动整轮矩阵。
- Interpretation: 这是模型输入次数授权，不擅自解释为新增生产代码上限授权；当前900/900保留。第一次诊断最多App bootstrap1/control1、Pi必要bootstrap1/reply1；未到相应阶段不计提交，失败不退款、不重放。

## Evidence E-017: 新轮真实工具调用未进入RA2A MCP

- Source: .cache/sandbox-discovery/repair-010安全诊断（目录以实际交付为准），独立85a2a01测试archive，生产未修改。
- Matched signal: 原生App ready端点发布，ID01a12fde-8667-7441-ad3b-2be8ab6832e6；唯一新control delivered、原生user1。第一次exec定义查询有880bytes输出，第二次嵌套send exec无output。透明代理initialize/tools-list返回非error，tools/call=0；原生完成PENDING。
- Interpretation: 可排除本次卡住已经进入RA2A MCP/CoAP的路径，具体App委托阻塞机制未知。ready/closed及peer安全信号已保存，但loaded/writer/threadRead投影被合并日志解析漏掉，不能宣称全gate诊断完成；以实际端点与收件证据证明这次到达调用阶段。新轮App2/10、Pi0/10，正式认证终点hash相同，自有资源回收，不重试旧marker。

## Hypothesis H-008: 嵌套工具调用阻塞在App customtool委托响应

- Status: refuted for repair011
- Claim: 原生backend已向App发送customtool请求，而App未返回该请求或其Code Mode委托未进入MCP；哪一个子边界未知。
- Prediction: 卡住时能关联native→App请求ID hash、App→native响应存在性与MCP tools/call；无App响应则定位到委托处理，无native请求则降级并查backend请求之前阶段，已有MCP响应则反驳本次预MCP阻塞。
- Diagnostic evidence plan: repair011仅测试Bridge记录固定direction/method enum/ID hash/响应存在及error布尔，独立安全单行日志、零输入验证解析。全新bootstrap+唯一list_targets控制最多两App输入，新轮累计上限4/10；不send、不回投，排除收件方依赖。清理前保存native关联与实际code-mode/MCP进程元数据；禁止payload、thinking、凭据或更改正式客户端。

## Evidence E-018: 实际MCP批准请求无人响应

- Related hypotheses: H-008
- Source: .cache/sandbox-discovery/repair-011/summary.json、stdio-live.json、app-ui.png；root独立查看截图。官方固定rust-v0.160.0路径/哈希在app-code-mode-static/manifest.json，仅标签来源不声称安装binary精确绑定。
- Matched signal: bootstrap原生已完成；唯一只读control进入后，native→App发mcpServer/elicitation/request，ID hash5feceb66ffc86f38，无App→native response；item/tool/call为0，MCP tools/call为0。真实界面显示Awaiting approval、Waiting for your answer和Allow ra2a MCP list_targets卡片。
- Interpretation: 定位本次pending到无人处理的MCP工具批准，而非动态handler丢回复或RA2A投递网络。未代点击批准；新轮累计App4/10、其他0，正式认证hash相同，资源回收。原send挂起同属候选，但不能用这次list证据直接宣称原send已修复。

## Hypothesis H-009: 私有沙盒缺少已授权RA2A工具的明确批准配置

- Status: confirmed for repair011
- Claim: 自动测试的私有Codex配置没有两项RA2A工具的approve策略，App产生需要人工回答的MCP批准请求，自动测试不回答，因而等待且未执行工具。
- Evidence gate: E-018实际协议与界面；固定官方config/src/mcp_types.rs允许per-tool approval_mode=approve，core/mcp_tool_call.rs按原生工具名匹配且Approve不请求用户批准。当前官方配置参考https://learn.chatgpt.com/docs/config-file/config-reference也列出该per-tool字段；仍需实际本机原生验证候选生效。
- Diagnostic evidence plan: repair012仅测试私有配置追加list_targets/send_message两项approve，不设server/global默认、不改其他工具或正式配置。原生config/read零输入校验两项有效字段，再唯一新控制先list一次/send一次；实际MCP两call返回、elicitation0、App/Pi唯一final、GUI独立回复行，验证原send症状消失。新轮累计最多App6/10、Pi1/10；900生产上限不自动扩张。

## Evidence E-019: 候选配置原生读取通过，行为验证被诊断账本拦截

- Related hypotheses: H-009
- Source: .cache/sandbox-discovery/repair-012/result.json及summary.json（actual/dispatched按最终安全摘要）。
- Matched signal: 自有native config/read实际返回list_targets/send_message两项approval_mode=approve且工具key精确；bootstrap task_complete。测试复制账本后错误机械替换旧control ID，与新ID碰撞，duplicate_unresolved拒绝，control原生user0，未进入工具；App新增bootstrap1，累计5/10，Pi0。
- Interpretation: 配置parser/effective读取候选通过，不把未dispatch的elicitation0当行为修复PASS。实际控制尝试与投递分开计，旧记录不得更名或退款。修正仅测试继承方式：旧ledger字节不变、新UUID与旧ID不交集，先mock验证5→7预算及duplicate拒绝，再唯一有界候选。正式认证不变，自有资源回收。

## Evidence E-020: 两项私有批准配置消除实际App工具等待

- Related hypotheses: H-009
- Type: candidate-fix-validation
- Source: .cache/sandbox-discovery/repair-013/{summary.json,result.json,app-ui.png,ledger-original.jsonl,ledger-fixture.json}；root独立查看GUI截图与候选配置范围测试。
- Matched signal: 原生config/read两项approve及精确toolKeys通过；list_targets/send_message实际MCP各唯一call及reply，21ms/67ms，error=false；elicitation0。App原生user1/final精确ACK1及同turn task_complete，Pi原生user1/final精确ACK1；GUI独立assistant ACK可见、无批准卡。只这两个工具改变，其他工具策略与全局approval_policy保留。
- Interpretation: 支持H-009候选根因及最小私有配置修正，原工具send症状在此有界复现消失；不是完整矩阵通过。新轮累计App7/10、Pi1/10、CLI/OC0；正式认证hash相同，资源与副本回收。旧012 ledger原字节缺失，013明确以可核5条消耗重建延续，未退款或冒充旧hash；本次实际ledger已原样保存。生产仍900/900未改，4行候选diff已备好，等待Owner明确904上限授权。

## Evidence E-021: 最小修正落入沙盒配置生成器

- Related hypotheses: H-009
- Type: repair-validation
- Source: experiments/sandbox/clients.py、test_clients.py；python3 -m unittest test_clients test_clients_resume -v。
- Matched signal: Owner确认继续，904行扩展；生成器增加4行，仅codex/app私有配置的list_targets/send_message为approve。7项测试PASS，包括保留其他MCP prompt策略、全局approval_policy及无server默认批准。
- Interpretation: 已落地专项验证过的最小修正，阶段904/904；无新模型输入，预算仍App7/Pi1/CLI0/OC0。P-001完整矩阵验收条件仍未满足，继续按剩余额度检验关键路径。

## Evidence E-022: 落地版本App忙时收件与关闭撤回通过，恢复UI阻塞

- Related hypotheses: H-007、H-009
- Type: bounded-live-validation
- Source: .cache/sandbox-discovery/repair-014/{summary.json,manifest.json,result.json,app-ui.png,ledger-original.jsonl}；root独立验图。
- Matched signal: 绑定10cfdf6，继承013原ledger前缀逐字相同。GUI BASE与RA2A FOLLOWUP两输入同turn 01a13007-eba3-7091-9d3a-1b1fccdc6eb4；实际busy且首MCP结果返回后才派发FOLLOWUP；四次list_targets call/reply无error，精确combined final及task_complete，GUI可见。GUI还保留较早单独BASE ACK，不宣称全过程只有一个assistant文本。关闭后端点撤回、只读内核查询error=false/ownerPresent=false；未发睡眠业务输入。
- Interpretation: 忙时专项PASS、关闭归属撤回PASS；真实UI同ID恢复报PRIVATE_APP_SESSION_LINK_NOT_RENDERED，恢复业务未派发，不改判通过、不重试。新增App2条，累计App9/Pi1/CLI0/OC0；正式auth/config终点hash相同，owned资源和临时凭据目录回收；完整矩阵仍INCOMPLETE。实际ledger SHA256=4d3ca0359184d64cdab82d85cd756d0623781fac51232b33f863df31dd410f38已在清理前原样持久化。

## Hypothesis H-010: 重开App的可见会话选择链路尚未满足恢复门禁

- Status: open
- Claim: 真实select_session未找到带nativeID的渲染链接；原因尚未归属，不能据此推断RA2A恢复失败或改用后台thread/resume绕过UI。
- Diagnostic evidence plan: 后续先补零模型恢复界面的脱敏DOM链接类型、目标ID匹配、实际history路径及固定错误分类；不得读取thinking/凭据，不为取证重发旧标记，达到证据门禁后才拟定最小修复。

## Evidence E-023: 恢复失败的现有证据边界

- Related hypotheses: H-010
- Source: repair014 summary.resumeStaticReview、app_client.py真实select_session；安全stdio投影。
- Matched signal: 重开使用同Runtime/home/CODEX_HOME/App userdata/cwd，未证路径被重新配置；实际history数据库路径、失败界面DOM/截图均未保存。安全帧仅method/哈希/hasError且未标backend，不能把166个error响应精确归为某thread/read或恢复操作。
- Interpretation: 当前仅确认UI选择门禁失败，根因未证；不猜a/button差异、数据库缺失或协议异常。恢复探针未消耗，本轮App剩1条；生产904行上限已满，不擅自扩张修复。

## Evidence E-024: 恢复失败界面直接证明选择器漏匹配

- Related hypotheses: H-010
- Source: repair015/dom-safe.json、resume-ui.png、result.json；root独立查看截图。
- Matched signal: 原生bootstrap精确final/task_complete；重开界面Recents显示原会话。目标ID仅匹配DIV role=button的data-app-action-sidebar-thread-id属性，a[href]无ID；原选择器仍报link未渲染。正式auth/config终点hash相同，原账本继承9→10，不退款，恢复业务0输入。
- Interpretation: 反驳“本复现历史缺失”方向，确认当前真实界面控件与a[href]假设不匹配；不能外推所有App版本。原地替换三行选择逻辑，不增加生产文件行数或阶段累计904行。

## Hypothesis H-011: 精确匹配渲染的sidebar thread属性即可恢复原ID

- Status: refuted
- Claim: 选择器应匹配已实证data-app-action-sidebar-thread-id等于nativeID的可见控件，通过真实click恢复，不能调用后台resume或模糊标题匹配。
- Diagnostic evidence plan: DOM直接属性/角色证据与固定App代码独立对照；原地替换后独立测试准确ID、可见性及不点击其他会话。真实新轮bootstrap完成后关/开App，界面点击同ID，writer发布同ID后唯一恢复probe原生完成+GUI可见。

## Evidence E-025: 第1轮完整矩阵暴露两个新阻塞

- Source: .cache/sandbox-discovery/cycle-001/{summary.json,report.md,ledger.jsonl}，exec69923已真实exit1，非观察超时。
- Matched signal: 冻结39dfeb0/dirty=false/sourceChanged=false；11个基础格PASS，App自投递native PASS但5秒内GUI不可见，OpenCode接收出现UnknownError，3个后续格因依赖未执行。App两个工具环边PASS；CLI/Pi busy/followup/resume及真实跨节点丢ACK不重放PASS。实际App后端已为0.162.0-alpha.17.2，不能外推旧0.160证据为该版本全量通过。
- Interpretation: 本轮INCOMPLETE，App恢复/交接因UI失败未运行，不升级39dfeb0选择器真实恢复结果。预算CLI9/App6/OC2/Pi9；正式认证终点hash相同，资源REAPED/copiesREMOVED。保留同轮余额继续专项，不启动第2轮掩盖第1轮失败。

## Hypothesis H-012: App自投递未显示来自实际焦点或渲染路径偏差

- Status: open
- Claim: native final已完成而GUI5秒内未见，需区分目标会话未显示、渲染延迟与原生通知/界面缓存；当前没有失败截图，不能把猜测直接修成自动切焦点。
- Diagnostic evidence plan: 同轮余额App4，仅唯一BASE/FOLLOWUP、同IDUI重开与恢复probe、自投递各阶段保留实际截图、原生final/complete及可见conversationID属性匹配。真实修复选择器先验证同ID恢复；若自投递仍失败，再以截图和固定属性分离候选根因。所有输入新ID，不重放旧消息；其余Agent0新增。

## Evidence E-026: 专项启动器引用错误在模型输入前修正

- Related hypotheses: H-012
- Source: repair016/probe.py编译门禁及exec47677。
- Matched signal: 第一次诊断脚本querySelector Python引号产生SyntaxError，未创建子进程/提交输入；修正仅诊断脚本，compile PASS后启动唯一有界运行。继承cycle001原ledger，App6→最多10，未退款。
- Interpretation: 启动器问题不归于产品；生产未改，正式配置保持。专项结果尚待采集。


## Evidence E-027: 真实恢复界面与固定包源码共同证明local key编码

- Related hypotheses: H-010, H-011
- Source: repair016/result.json；repair017/dom-safe.json、resume-ui.png；固定app.asar SHA256 ce97d6ccd57e314134506b84234f65737a5ec9b55664f037a77aa95fac971d68。
- Matched signal: 016忙时两输入原生完成且GUI可见，关闭撤回通过，但bare nativeID精确属性匹配失败。017一次bootstrap完成后真实关/开，原会话DIV role=button可见，属性值local:01a13584-3d08-7ad2-b550-40c88332f99a，equalsID=false。固定包M4将nativeID加local:前缀，sidebar使用e.key，与现场一致。
- Interpretation: 015只证明属性包含nativeID，未证明等于；撤回H-011的bareID假设。原地把匹配键改为local:<nativeID>，保留精确/可见门禁，不用contains或后台resume。最小DOM测试覆盖相邻ID、隐藏控件、bareID拒绝；实际恢复probe仍待新轮。016/017同轮App6→8→9，其他CLI9/OC2/Pi9不变，无退款/旧标记重放；正式auth/config相同、owned资源回收。


## Evidence E-028: OpenCode原生失败定位到私有OAuth刷新错误族

- Source: cycle-001-oc-diag/run/{result.json,projection-safe.json,auth-expiry-safe.json}；固定1.18.34 OpenAI插件刷新源码。
- Matched signal: 收件confirmed/accepted，唯一user但final0；安全分类AUTH_EXPIRED，25bytes错误正文不保存。源OAuth过期，私有type/access/refresh/expires/accountId与源逐字段相等（仅保存布尔）；refresh存在、provider正确，auth域名已允许。固定插件先等待refresh再构造模型fetch，当前未证模型请求被拒绝；聚合tunnel1/validation1不证明刷新HTTP状态或拒绝方。
- Interpretation: 不改RA2A投递或模型，不反复付费重试；已请求Owner重新登录本机OpenCode后再继承认证。唯一诊断OC2→3，同轮CLI9/App9/Pi9不变，前缀原字节true，正式auth/config相同，owned资源REAPED且私有副本REMOVED。完整矩阵保持INCOMPLETE。


## Evidence E-029: 编码修复后真实界面恢复同ID及历史显示通过

- Related hypotheses: H-010, H-011, H-012
- Source: repair018/result.json、selected-ui.png、ledger-original.jsonl，App选择代码绑定1a2bdaf；root独立查看截图。
- Matched signal: 唯一bootstrap原生exact final/task_complete PASS，真实关闭后重开；可见控件local键精确匹配点击成功，live endpoint sameNativeID=true，resumedHistoryACKVisible=true。恢复阶段0新业务输入，不能据此升级恢复投递通过。App9→10，CLI9/OC3/Pi9不变；正式Codex auth/config终点hash一致，自有Runtime退出与私有临时树清理完成。
- Interpretation: UI选择/历史恢复缺陷有真实现场回归证据；Issue3仍开放待恢复后的新投递完整验证。第1轮基础矩阵原失败不改判，新轮等待OpenCode源认证更新后执行；不重置当前账本。017复制的诊断源码已另存为独立018，014的app_client.py恢复至原10cfdf6原字节；016实际使用39dfeb0时的来源另列，禁止以现已恢复014源码重跑016或改写旧结果。


## Evidence E-030: 第2轮App恢复后的新投递与跨节点ACK损失通过

- Source: cycle-002-reverify/{summary.json,ledger.jsonl}；e182827/dirty=false/input SHA冻结，独立诊断控制器显式将OC设认证阻塞（0输入）。
- Matched signal: App self GUI、Pi→App工具回信、App/Pi busy+followup、两者resume及distinct-node ACK-loss/no replay均PASS。resume-app原ID=恢复ID01a13590-d66b-75c3-982f-e458f1c542d5，新user/final各1、精确GUI ACK可见。CLI BLOCKED_ADAPTER，故holder transfer未运行；完整矩阵INCOMPLETE。账本CLI1/App7/OC0/Pi7，正式8认证配置hash相同，owned REAPED/copies REMOVED。
- Interpretation: Issue3原恢复缺陷真实新投递回归已满足并关闭；App cycle1 self显示失败不抹除，cycle2序列少OC/CLI，不能据单次通过宣称偶发显示问题根治。第2轮余额继续CLI独立定位，不重置预算。

## Evidence E-031: 启动诊断保护挡住重复bootstrap，随后唯一新标记证实not_loaded

- Source: cycle-002-cli-diag、cycle-002-cli-diag2/3/4；原账本继承，固定字面异常投影及唯一ID零模型fixture。
- Matched signal: 初始自组控制器APP_START失败，仅固定RuntimeError、paid0，不归于产品；改回原runner setup后明确bootstrap budget unavailable/not replayed。原因是既有bootstrap ID重复，账本正常拦截；其余失败机制不猜。新随机ID fixture验证旧ID/原字节保留及duplicate拒绝，唯一实际诊断4 CLI start returned、register HTTP200、codex-tui新rollout1，但endpoint未发布，cli_ownership_unknown/not_loaded28；三种TUI embedded fallback固定日志均0，无TRACE证据不能排除。
- Interpretation: 不扩大生产适配器或猜半写/认证/模型错误，下一门禁比较实际daemon/TUI/backend thread-loaded归属。App7→8、CLI1→2、OC0/Pi7不变；所有诊断owned REAPED/凭据副本REMOVED/正式hash相同。重复ID零输入不是模型失败或退款。


## Evidence E-032: CLI启动选择误认延迟落盘的App会话

- Source: cycle-002-cli-diag5/6/summary.json；原账本前缀继承。diag5登记CLI元数据ID、端点与独立官方loaded/list均一致，PASS但不能改判此前失败。diag6复现register200/not_loaded28，登记01a135a9-c8ff-75e2-97d4-e5e12c662084的native metadata实际为Codex Desktop；真正codex-tui ID01a135a9-d3b7-7280-ac45-3082cb54609c存在于独立loaded/list中。官方socket前后dev/inode相同，peerPID1079、版本0.162.1；不凭相同路径推定全部历史generation未变。
- Interpretation: 本次具体失败来自沙盒clients.start选择第一个“snapshot后新文件”，App文件可能晚于snapshot落盘；不是已证后端分流或生产owner gate失效。固定originator/source不能证明持有者。启动器改为随机唯一bootstrap文本与原生user精确匹配后才登记，拒绝assistant/substring/App文本，不修改生产owner判定、不cold resume。生产文件总行数未增加，仍在904新增行批准范围；8项相关零模型测试通过。真实修复回归另列，完整矩阵仍INCOMPLETE。
- Budget/protection: diag5/6各新增CLI1/App1，累计CLI4/App10/OC0/Pi7；App达到10后本轮不得继续App输入。两次owned REAPED/私有凭据REMOVED/正式8认证配置hash一致；OpenCode源认证仍过期，不再付费重试。


## Evidence E-033: 唯一输入登记修复后的官方CLI真实回归

- Source: cycle-002-cli-fixed/{summary.json,ledger.jsonl}，绑定fe98cc8/源码输入冻结；第2轮账本原字节继承。诊断范围明确仅CLI，App10已满禁止启动/输入，OC源认证阻塞且不执行，Pi未增加输入。
- Matched signal: 官方CLI0.162.1真实TUI register200，端点01a135ae-5bf8-7e61-aea5-32a54ffd4cad发布；basic self、active（busyAtFollowup=true）、followup原生完成均PASS。关闭后恢复originalSession=resumedSession，恢复新投递原生完成PASS。不含App持有者切换、跨节点ACK损失、OC或人工界面观察。
- Interpretation: Issue4启动错误登记修复有针对性迟到App零模型fixture与真实CLI业务回归；关闭该具体缺陷，不改判全矩阵或Issue2生产症状。完整矩阵仍INCOMPLETE，等待本机OpenCode重新登录后完成余项；本轮CLI9/App10/OC0/Pi7，无退款/重放。正式8认证配置hash相同，owned REAPED/凭据副本REMOVED。


## Evidence E-034: 第3轮真实三端矩阵与编排/界面缺口

- Source: cycle-003/{summary.json,ledger.jsonl}；绑定1213e55、输入SHA冻结。OC认证BLOCKED_AUTH/0输入。三端9格basic、Pi→App和App→CLI工具回信、三端BASE原生完成及CLI/Pi FOLLOWUP均PASS；App FOLLOWUP原生唯一combined final/task_complete PASS，但GUI false，实际截图停旧消息位置且有向下跳转按钮。仅假设虚拟化/滚动，未据截图证明点击后可见。App原IDresume新投递PASS；CLI→睡眠→App真实writer gate→CLI恢复PASS；Pi同IDresume及CLI/Pi distinctLAN丢ACK不重放PASS。
- Matched failure: resume循环按endpoints插入顺序先App再CLI。CLI持有者切换会stop App，之后ACK-loss仍向旧App endpoint派发；App预算维持8（该条未预留），native users/finals0而超时。真实循环抽取零模型回归原版FAIL“handoff stopped App without restoring it”；原地调整App恢复最后执行后PASS，16项相关测试PASS，生产文件总行数不增加。
- Interpretation: 此编排缺口不是App接收后丢ACK故障，不重放原unknown。真实修复回归待后续运行；App FOLLOWUP GUI失败另行零模型渲染/滚动取证，不改判完整矩阵。第3轮CLI8/App8/OC0/Pi8，资源REAPED/私有凭据REMOVED/正式8认证配置hash相同。


## Evidence E-035: 同轮App二输入诊断未复现旧滚动失败

- Source: cycle-003-app-scroll/{summary.json,ledger.jsonl}及cycle-003-scroll-gui/before.png、after.png，绑定edb936a；原第3轮账本原字节继承。只增加App BASE UI输入1/FOLLOWUP1，Pi只启动真实TUI作为独立LAN发送端、0模型输入；CLI不启动，OC仍阻塞。
- Matched signal: busyBeforeFollowup=true，原始send结果DELIVERY_UNKNOWN，FOLLOWUP user1/唯一combined native final/task_complete PASS。真实窗口已有精确完整final的DIV/P段落（y308.25,height45.5），inViewport=true；没有可滚动历史容器，scrollAction performed=false；前后均渲染，未点击/后台刷新/重放。
- Interpretation: 短会话未复现cycle003旧滚动位置问题，不能根治解释长序列GUI失败，也不能替代CLI交接后App重启编排修复的真实回归。第3轮CLI8/App10/OC0/Pi8，App额度冻结；owned REAPED/凭据副本REMOVED/正式8认证配置hash一致。Issue5编排/Issue6长序列UI缺口保留。

## Evidence E-036: 原body substring界面判据存在确定性假阳性

- Source: ExactFinalVisibilityTest零模型真实JS DOM fixture及AppProcess.visible/runner.send调用链；cycle003-app-scroll已实证完整combined final渲染为P段落。
- Matched signal: 只有用户请求“Reply only ACK_TEST”与display:none回复时，旧body.innerText.includes判据返回true，fixture FAIL；原地改ACK检查为完整精确段落+getClientRects，runner使用native finals实际ack（含combined完整内容），fixture转PASS。认证界面等非ACK判断保留。23项相关测试PASS，生产文件行数未增加。
- Interpretation: 这是测试判据假阳性修复，不解释旧GUI false，也不改变原失败结论；完整真实矩阵新判据回归待执行，人工实际可见/继续输入仍未通过。不能把DOM渲染自动升级为人工观察。


## Evidence E-037: 第4轮真实交接后App丢ACK回归通过

- Source: cycle-004/{summary.json,ledger.jsonl}，冻结d2154bc/sourceChanged=false。CLI/App/Pi三端9格basic和两工具环PASS；同ID恢复、CLI→睡眠→App实际writer→CLI恢复PASS；App恢复置最后后，三端distinct-LAN postwrite ACK损失/cancel unknown/不重放及native完成均PASS。Issue5具体编排缺陷已关闭，不外推Issue2生产竞争。
- UI: App FOLLOWUP原生完成PASS，原自动GUI false保留。裸DOM scroll后完整exact P出现但top979/bottom1025、inViewport=false；原会话composer ID始终相同。支持旧滚动/虚拟化方向，不证明视口可见或数据丢失。
- Budget/protection: CLI8/App9/OC0/Pi8；OC仍认证阻塞。资源REAPED/私有copies REMOVED/正式8认证配置hash相同，完整矩阵INCOMPLETE。

## Evidence E-038: 原生下跳按钮使长会话完整final进入视口

- Source: cycle-005/summary.json，cycle-005-gui/before.png、after-button.png；绑定d2154bc/frozen source。独立Pi真实发送端0模型输入，App bootstrap+三历史+BASE/FOLLOWUP共6输入；忙时FOLLOWUP原生精确combined final完成。
- Matched signal: 导航前exact P top1641.5/bottom1687、inViewport=false，同会话composer true；唯一可见原生Scroll to bottom button点击一次后top580.5/bottom626、inViewport=true，scrollTop -1061→0。root独立查看截图确认完整两ACK与输入栏同屏。不曾backend history/resume/reload或新增模型输入；无需Latest response备用focus。
- Static binding: thread-layout eccd85e6…b2 的按钮回调更新跟随/scroll controller或responseSpacer；与裸DOM scrollHeight不同。app-initial a9f4a1a8…720确认真实button及隐藏态无onClick。
- Interpretation: 检查器将视口外P也算可见是第二个确定假阳性。原地更新ACK判据：精确完整段落须在视口；若不在，点击唯一可见原生下跳按钮，返回false，下一采样独立检查；隐藏/多个按钮不操作。这是显式GUI导航后的可见性验收，不声称App自动跟随无需操作；旧失败不改判，人工仍留最后。24项相关零模型测试通过，生产文件行数不增加，阶段仍904。真实修复回归待执行。
- Budget/protection: 第5轮App6/其他0；源未变/正式8认证配置hash相同/owned回收/私有copies移除。随后Owner明确要求OpenCode使用Go订阅，下一轮以现有Go凭据私有投影和kimi-k3执行，不改正式配置。
