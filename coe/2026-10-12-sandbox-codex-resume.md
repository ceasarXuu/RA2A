# Problem P-001: Linux矩阵Codex关闭恢复未通过
- Status: resolved
- Objective: 按真实持有者语义恢复CLI，不通过硬撤回登记或coldresume隐藏所有权问题。
- Symptoms: matrix-006 resume-codex为RuntimeError，只有originalSession，没有resumedSession或完成证据。
- Known facts: 原CLI基础收件和busy/followup原生完成通过；恢复路径先old.stop，再等待旧发布撤回，后启动官方resume。
- Fix criteria: 独立实证旧TUI/daemon/writer/发布状态，证实失败所在门禁；修复后同原生ID恢复并收件完成，未持有保持睡眠，正式CLI/App不变。
- Current conclusion: 零真实额度实验已确证官方默认60秒保持writer，而测试12秒撤回期限过短；已改为按私有配置动态等待，真实TUI已通过同原生ID恢复及收件/模型完成验证。
- Resolution basis: E-002确证机制，E-004修复后真实TUI恢复通过；不外推App所有权交接。

## Hypothesis H-001: 测试停止未终止旧TUI
- Status: unverified
- Claim: Process.stop只等待group leader，真实TUI后代仍存活且续约/持有，导致撤回超时。
- Falsifiable predictions: stop后真实native PID仍在，writer/attachment保持；若为假，native PID退出但端点仍发布。
- Diagnostic evidence plan: 零模型真实CLI/fixture的PID创建身份、namespace PID映射、group及stop前后状态；不按名称kill，不接正式锁。必要时唯一预算bootstrap建私有原生会话。
- Evidence gate: pending
- Next step: 代码路径与隔离生命周期复现两路分开。

## Hypothesis H-002: RA2A连接或官方daemon保持thread持有
- Status: unverified
- Claim: 旧TUI已退出，但sidecar连接/daemon loaded thread仍保持writer或旧注册，从而旧ID未撤回。
- Falsifiable predictions: TUI真实退出，官方daemon/RA2A连接存活，loaded/writer或登记仍在；若为假，端点撤回，失败发生新resume阶段。
- Diagnostic evidence plan: 同一个私有fixture对照TUI退出、侧连接关闭、官方loaded列表/锁实际持有，先只读身份及接口，禁止RPC coldresume或获取正式锁。区分真实writer保持与登记缓存残留。
- Evidence gate: pending
- Next step: 最小隔离诊断，不以unregister硬规则冒充修复。

## Evidence E-001: 原始恢复失败摘要
- Related hypotheses: H-001,H-002
- Direction: neutral
- Type: observation
- Source: .cache/sandbox-discovery/matrix-006/summary.json; experiments/sandbox/clients.py
- Prediction or plan link: 尚不能区分old.stop、等待撤回、启动resume或新发现错误。
- Matched signal: 原始ID存在；异常为RuntimeError，未记录resumedSession。
- Interpretation: 已执行路径没有成功恢复证明；需补具体生命周期信号。

## Evidence E-002: 官方延迟卸载及真实writer对照
- Related hypotheses: H-001,H-002
- Direction: contradicts H-001 / supports H-002 官方延迟保持分支
- Type: controlled experiment
- Source: .cache/sandbox-discovery/cli-resume-diagnostic/{native-fixed.log,native-delivery.log,kernel-writer-observation.json}；官方rust-v0.162.1 codex-rs/core/src/config/mod.rs thread_unload_delay_secs默认60。
- Prediction or plan link: H-001/H-002先观察创建者断连、loaded列表和实际锁持有，禁止硬注销或获取正式锁。
- Matched signal: 自有创建者连接断开，12秒后仍loaded；/proc/locks精确writer inode的FLOCK WRITE属于自有native PID；默认约60.36秒卸载，配置1秒约1.007秒卸载。实际Adapter.Deliver start/steer通过本地mock后继续侧连接观察，1秒配置约1.013秒卸载，race PASS。
- Interpretation: RA2A投递没有永久订阅；创建者断连不等于官方backend立即放锁。12秒门禁错误，不应伪造睡眠或强制注销登记。
- Outcome: 从沙盒私有config读取延迟（当前固定官方版默认60），加12秒观察余量并设置180秒总上限；持续poll真实发现结果。仍有持有者时止步，报告NATIVE_OWNER_STILL_HELD，不重新打开。零真实模型额度、自有子进程已Wait回收。

## Evidence E-003: 独立复验启动器缺参止步
- Related hypotheses: H-001,H-002
- Direction: neutral
- Type: diagnostic execution failure
- Source: .cache/sandbox-repair/cli-resume-001/{result.json,ledger.jsonl}。
- Prediction or plan link: 修复后仅验证真实CLI同ID恢复，保留此前失败。
- Matched signal: bootstrap reserved1；还未取得originalSession，端点准备RuntimeError。独立脚本遗漏RA2A_SANDBOX_CODEX_BIN，而正式runner已传入；原生模型驱动未额外重放。
- Interpretation: 是独立执行脚本启动缺参，不是套件恢复门禁复发。首次记录保留，不冒充测试通过。

## Evidence E-004: 修正门禁后真实TUI同ID恢复通过
- Related hypotheses: H-001,H-002
- Direction: supports root cause and fix
- Type: real isolated acceptance
- Source: .cache/sandbox-repair/cli-resume-002/{result.json,ledger.jsonl}。
- Prediction or plan link: 旧TUI关闭→真实发布撤回→官方resume原ID→唯一RA2A输入及原生final/task_complete，不强制注销或停daemon。
- Matched signal: originalSession=resumedSession=01a12cc3-699d-7521-a8a8-5cb3a5862baf；withdrawalAndReopen62114ms。随后自投92ms confirmed/accepted，原生user1、精确ACK final1，task_complete同turn01a12cc4-6dc1-7dd2-af45-189e5a669f31，status PASS。
- Outcome: 本复验reserved2/limit2（bootstrap+恢复后输入），首次启动器失败另reserved1，总CLI3；OC诊断另1，均低于用户每类型10条限制，无unknown重放。自有资源REAPED、私有凭据REMOVED，8项正式auth/config终点hash不变。单项诊断通过不等于完整矩阵/App交接已通过。
