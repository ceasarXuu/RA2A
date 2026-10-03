# Codex CLI↔CLI 全量验证（2026-10-04）

- 状态：执行中，尚未完整通过。
- 来源：Owner要求“开始执行codex-cli to codex-cli的全部测试验证”。
- 产品权威：[PD33](../v0.0.15/prd.md#confirmed-product-decisions)：收件ACK与回复/执行完成分开；未知写入不重放。
- 矩阵依据：[Phase 5](../v0.0.15/engineering-plan.md#phase-5交叉矩阵与退化验证)。本轮覆盖CLI↔CLI，不将结果扩展为App/CLI全交叉发布准入。
- 当前部署：Ubuntu/Mac为e5d97f2收件修复；ROG节点ready但未发布验收CLI，需要登记与新版部署。Mac本地分叉源码保留，使用隔离archive构建。

## 验证矩阵

| ID | 项目 | 必需证据 | 状态 |
| --- | --- | --- | --- |
| L01 | Linux相关包全部race回归 | 原始包结果、source提交 | PASS：8包race及CLI 54个测试事件 |
| L02 | 原生0.159.3/0.160.0隔离daemon | ACK先于阻塞模型完成、22轮、owner继续、重启恢复、home/auth保护 | PASS：0.159.3/0.160.0分别原生复验 |
| M01 | macOS原生相关包 | race结果及所有skip原因 | 旧e5：6包PASS、CLI短限时失败；5410aea复验中 |
| W01 | Windows原生相关包 | 原生执行结果；不以交叉编译代替 | 待ROG上线 |
| W02 | Windows夹具与ACL边界 | 原生helper替换批处理；已有ACL测试5项 | 5410aea测试修补已push；Linux通过、Windows原生待验 |
| C01 | 双向基本投递 | 双方新版、完整from/to、接收标记和ACK/turn匹配 | 待ROG |
| C02 | 双向各22轮 | 每轮收件ACK、无重复/缺失、完成仅为独立观察 | 待双方新版重跑 |
| C03 | 双向active follow-up | 单start+同turn steer、原生ACK、marker匹配 | 待执行 |
| C04 | 长工作收件解耦 | 工作未完成前ACK；随后执行独立核验 | 既有现场/隔离证据；本轮补测 |
| C05 | 交错回复无互等 | 单次有界回复、最多固定hop、无重复写入 | 待执行 |
| C06 | 同机多端点 | 明确登记、只写目标、无历史/所有权串线 | 相关回归及隔离实例待汇总 |
| C07 | 三设备与不同版本 | Ubuntu/Mac/ROG一条有界跨机链及原生/daemon各自版本 | 待ROG |
| U01 | TUI实时显示及人工继续 | 用户在Mac/ROG物理TUI输入、画面与回复确认 | 已请用户配合；时机另通知 |
| R01 | 官方daemon未运行/线程未loaded/旧版本 | 明确错误、零静默拉起/错误writer、零重放 | 相关回归待汇总 |
| R02 | RPC/daemon中途退出与重启 | 仅隔离实验资源；unknown不重放、重新加载后可用 | Linux真实实验/回归；Mac/Win隔离待证明 |
| R03 | LAN中断与恢复 | 隔离节点/测试路径中断，预写失败与不确定写入分类、恢复后单次成功 | 待安全隔离实例，不关闭全局网络 |
| N01 | 异常/缺失ACK、能力、UUID、归属、home | 无伪成功/回退/实际非目标写入，原始边界结果 | 相关回归待汇总 |
| P01 | 正式CLI/App/配置保护 | PID/所属进程组、配置auth/代理/launcher元数据前后一致 | 各阶段记录 |

## 执行纪律

- 验收多轮每条send只等收件ACK；为了构造独立空闲回合，测试脚本可另行观察目标ready，不把观察完成混入投递API。
- 不确定结果立即停止对应轮，不自动重发；已收件后的执行失败另记。
- 串行执行各跨机阶段，先取证再进下一阶段，避免两端同时改验收流程。
- 网络/daemon恢复仅操作新建隔离实例，先证明socket、控制端口、节点身份、HOME/USERPROFILE/LOCALAPPDATA、CODEX_HOME、服务label与正式资源隔离。
- 不运行会重写正式配置的setup/install/restart流程；确需部署仅原子替换RA2A并重启既有服务。
- TUI人工继续不能由第二RPC客户端、rollout或单测替代；未取得用户实操证据则该项保持待验。
- Windows交叉编译、macOS/Linux专用测试skip不记为Windows/macOS原生通过。
- 不复用旧完成确认语义下的22轮结论充当新版完整验收。

## 本轮证据

- 本机原始日志：忽略目录 `.cache/full-cli-acceptance/`。
- Mac前置任务：`RA2A_FULL_CLI_PRECHECK_20261003`，等待单次结果报告。
- ROG当前缺少已发布CLI端点，已通过用户转达上线及e5d97f2隔离部署要求。

### 2026-10-04 前置结果

- Linux：8个相关包 race PASS；CLI包54个pass事件，默认跳过1项opt-in native；显式指定0.159.3/0.160.0后native各PASS。5410aea测试修补另以-count=2复验，108个pass事件，2次默认native skip已由显式运行补证。原始日志在 `.cache/full-cli-acceptance/linux-*.jsonl`、`linux-core-race.log`。
- Mac e5：6包PASS，CLI receipt start/steer因冷探测超过300ms失败；原始失败保留。临时诊断预连接406–449ms、其后Deliver四次均<2ms且PASS。原始证据 `/tmp/ra2a-full-mac-precheck.0K0RZ0/` 与 `/tmp/ra2a-mac-timing.7NwiXF/`。正式5410aea原生复验尚待结果；Linux-only native排除不计Mac通过，隔离daemon恢复安全未证实，保持待验。
- Linux保护：官方daemon PID/PGID15822未变；config/auth/App desktop/proxy配置和官方socket inode、mtime、长度与部署后基线相同。本轮测试未改生产代码、正式配置或重启正式daemon。
- ROG发现ready但仍无codex-cli端点；跨设备完整矩阵仍待其上线，旧22轮不充当新版结果。
