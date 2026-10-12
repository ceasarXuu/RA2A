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
