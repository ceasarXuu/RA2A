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
