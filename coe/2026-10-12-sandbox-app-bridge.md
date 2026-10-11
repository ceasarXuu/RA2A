# Problem P-001: Linux矩阵缺真实App安全接入
- Status: open
- Objective: 补齐真实私有Linux App接入，按实际writer选择stdio桥，无人持有保持睡眠；不修改正式App或使用自动resume的IPC投递。
- Known facts: matrix-006 App项BLOCKED_ADAPTER；原stdio桥仅允许既有thread方法，未经真实App接入；用户要求「修完」。本阶段默认500行，900行扩展待批准。
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
