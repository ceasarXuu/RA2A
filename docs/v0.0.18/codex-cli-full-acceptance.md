# Codex CLI↔CLI 全量验证（2026-10-04）

- 状态：执行中，尚未完整通过。
- 来源：Owner要求“开始执行codex-cli to codex-cli的全部测试验证”。
- 产品权威：[PD33](../v0.0.15/prd.md#confirmed-product-decisions)：收件ACK与回复/执行完成分开；未知写入不重放。
- 矩阵依据：[Phase 5](../v0.0.15/engineering-plan.md#phase-5交叉矩阵与退化验证)。本轮覆盖CLI↔CLI，不将结果扩展为App/CLI全交叉发布准入。
- 当前部署：Ubuntu/Mac为e5d97f2收件修复；ROG已从5410aea隔离源码部署相同生产修复，三端均已发布验收CLI。Mac本地分叉源码保留，使用隔离archive构建。

## 验证矩阵

| ID | 项目 | 必需证据 | 状态 |
| --- | --- | --- | --- |
| L01 | Linux相关包全部race回归 | 原始包结果、source提交 | PASS：8包race及CLI 54个测试事件 |
| L02 | 原生0.159.3/0.160.0隔离daemon | ACK先于阻塞模型完成、22轮、owner继续、重启恢复、home/auth保护 | PASS：0.159.3/0.160.0分别原生复验 |
| M01 | macOS原生相关包 | race结果及所有skip原因 | PASS：5410aea七包race，182个pass事件 |
| W01 | Windows原生相关包 | 原生执行结果；不以交叉编译代替 | PASS：5410aea八包Windows原生race |
| W02 | Windows夹具与ACL边界 | 原生helper替换批处理；已有ACL测试5项 | PASS：原生exe fixture及五个ACL测试；16个runtime skip另记 |
| C01 | 双向基本投递 | 双方新版、完整from/to、接收标记和ACK/turn匹配 | PASS：Ubuntu↔Mac、Mac↔ROG新版收件匹配 |
| C02 | 双向各22轮 | 每轮收件ACK、无重复/缺失、完成仅为独立观察 | PASS：Mac↔ROG新版双向各22独立turn，无缺失重复 |
| C03 | 双向active follow-up | 单start+同turn steer、原生ACK、marker匹配 | Ubuntu→ROG BASE/FOLLOWUP同turn；Mac/ROG双向现场待补 |
| C04 | 长工作收件解耦 | 工作未完成前ACK；随后执行独立核验 | PASS：Linux阻塞native模型；Mac/ROG→Ubuntu持续工作收件22条 |
| C05 | 交错回复无互等 | 单次有界回复、最多固定hop、无重复写入 | 待执行 |
| C06 | 同机多端点 | 明确登记、只写目标、无历史/所有权串线 | PASS：真实native双线程逐项输入计数 |
| C07 | 三设备与不同版本 | Ubuntu/Mac/ROG一条有界跨机链及原生/daemon各自版本 | 待ROG |
| U01 | TUI实时显示及人工继续 | 用户在Mac/ROG物理TUI输入、画面与回复确认 | 已请用户配合；时机另通知 |
| R01 | 官方daemon未运行/线程未loaded/旧版本 | 明确错误、零静默拉起/错误writer、零重放 | 相关适用平台回归PASS；排除/skip单独记 |
| R02 | RPC/daemon中途退出与重启 | 仅隔离实验资源；unknown不重放、重新加载后可用 | Linux PASS；Linux及Darwin两版本PASS；Windows eed6dc7原生执行中 |
| R03 | LAN中断与恢复 | 隔离节点/测试路径中断，预写失败与不确定写入分类、恢复后单次成功 | PASS：Linux真实native+CoAP预写失败、后写缺确认、恢复及不重放；跨设备发现恢复仍待验 |
| N01 | 异常/缺失ACK、能力、UUID、归属、home | 无伪成功/回退/实际非目标写入，原始边界结果 | 相关适用平台回归PASS；排除/skip单独记 |
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
- Mac前置任务及新版fixture复验完成，详见以下证据。
- ROG验收CLI已发布且新版前置完成，详见以下证据。

### 2026-10-04 前置结果

- Linux：8个相关包 race PASS；CLI包54个pass事件，默认跳过1项opt-in native；显式指定0.159.3/0.160.0后native各PASS。5410aea测试修补另以-count=2复验，108个pass事件，2次默认native skip已由显式运行补证。原始日志在 `.cache/full-cli-acceptance/linux-*.jsonl`、`linux-core-race.log`。
- Mac e5：6包PASS，CLI receipt start/steer因冷探测超过300ms失败；原始失败保留。临时诊断预连接406–449ms、其后Deliver四次均<2ms且PASS。原始证据 `/tmp/ra2a-full-mac-precheck.0K0RZ0/` 与 `/tmp/ra2a-mac-timing.7NwiXF/`。正式5410aea七包race全部PASS，182个pass事件，FAIL0/runtime SKIP0；receipt两测试-count=2共20个pass事件。证据 `/tmp/ra2a-mac-fixture.lvU2uI/`，source archive SHA256=df0bbc2e637f91713aacf917c008230c21400b756fd6bbb42b9babdb59dc5cbc，race日志SHA256=dceb23754ae67080a8fabf78a527e1e311bd914436ab2aa4977acfa56825bac8。Linux-only native排除不计Mac恢复通过，隔离daemon恢复安全未证实，保持待验。
- Linux保护：官方daemon PID/PGID15822未变；config/auth/App desktop/proxy配置和官方socket inode、mtime、长度与部署后基线相同。本轮测试未改生产代码、正式配置或重启正式daemon。
- ROG验收codex-cli已上线，前置任务发送曾返回DELIVERY_UNKNOWN且未重发；接收侧随后报告完成部署与原生测试，旧22轮不充当新版结果。

### 新版现场与网络恢复阶段

- Mac→Ubuntu ACTIVE_01–22：22/22工具accepted，error/unknown0、无重试，耗时528–1036ms。Ubuntu原生rollout独立核验22条、缺失0/重复0、同一持续工作turn；不是22个独立完成回合。Mac证据 `/tmp/ra2a-mac-ubuntu-active22.YnMuEq/evidence.json`，Ubuntu `.cache/full-cli-acceptance/mac-ubuntu-active22-receipt.json`。
- Ubuntu→Mac IDLE_01–22：逐条发送前确认codex-cli/ready，每轮至少间隔8秒并重新检查ready；22/22工具accepted，原始入参/结果/耗时 `.cache/full-cli-acceptance/ubuntu-mac-idle22.json`。Mac原始rollout独立核验消息/精确ACK/task_started/task_complete均22，独立turn22，缺失0/重复0，行序及同turn匹配22/22、无工具调用；证据 `/tmp/ra2a-full-mac-idle22.nqMtQy/evidence.json`，保护前后快照一致。
- 新增Linux测试仅172行helper与1行调用，复用隔离native daemon/mock。0.159.3 race-count=1与count=2通过，0.160.0 count=2通过：两个登记线程仅目标收到一次输入；测试LAN receiver关闭后ErrPeerUnreachable、adapter调用不增加、线程无输入；恢复后新marker一次、离线marker零次。真实DTLS/CoAP，但使用显式loopback peer，不覆盖跨设备发现恢复或native ACK丢失；测试随机身份/PIN/端口，短暂mDNS广告和全接口监听，未操作正式节点/网络。
- 人工TUI操作按Owner要求最后执行，自动阶段完成后提供操作与观察清单；U01仍待验。

- ROG前置：收到d4147f4831f851e5c06636791cf1ce26证明先前UNKNOWN控制任务实际已到达，未重试。独立5410aea源码构建部署仅RA2A，已有任务daemon16908→28444；CLI/officialdaemon均0.160.0，正式CLI/App/daemon进程与配置/auth/launcher/代理及正式repo保留。8包Windows原生race全部PASS，ACL5项分别PASS，runtime skip16及平台排除另记、不计通过。证据 `C:\Users\77585\AppData\Local\Temp\ra2a-full-rog-precheck-cf8ae21c8a2748b38abe997d02f4e672`。未展开Windows隔离daemon恢复，group/job隔离证据尚缺。

## 人工操作与观察清单（自动阶段完成后再执行）

使用两台设备已经打开的“v0.0.18联调验收”CLI，先等待自动阶段结束。每项反馈“通过/失败”、设备、实际观察及时间；失败保留屏幕与完整错误，不重复投递unknown消息。

| 顺序 | 操作 | 应观察到的结果 |
| --- | --- | --- |
| 1 | Mac输入 `RA2A_MANUAL_MAC_01 请仅回复ACK_RA2A_MANUAL_MAC_01`；ROG输入对应 `RA2A_MANUAL_ROG_01 请仅回复ACK_RA2A_MANUAL_ROG_01` | 能输入、提交、显示原文与精确回复，结束后输入框可继续使用 |
| 2 | 在ROG CLI要求：刷新list_targets，向Mac验收codex-cli地址只投递一次 `RA2A_MANUAL_ROG_MAC_02 请仅最终回复ACK_RA2A_MANUAL_ROG_MAC_02，不回投`，使用ROG完整from地址；记录工具收件结果 | Mac物理TUI实时显示远端标记和回复；ROG收件结果与Mac后续回复分别观察 |
| 3 | 在Mac CLI要求反向投递一次 `RA2A_MANUAL_MAC_ROG_03 请仅最终回复ACK_RA2A_MANUAL_MAC_ROG_03，不回投`，使用Mac完整from地址 | ROG物理TUI实时显示标记和回复，无重复；不需要关闭或重新打开TUI |
| 4 | 各自在收到远端消息后，再手工输入 `RA2A_MANUAL_CONTINUE_MAC_04 请仅回复ACK` / `RA2A_MANUAL_CONTINUE_ROG_04 请仅回复ACK` | 原验收会话仍可人工继续，无输入被吞、无错误线程跳转、无界面卡死 |
| 5 | Mac先在验收CLI要求“等待20秒后仅回复RA2A_MANUAL_ACTIVE_MAC_DONE”；等待期间由ROG只发一次 `RA2A_MANUAL_ACTIVE_MAC_FOLLOWUP 请同时保留此标记，不回投`。之后交换设备进行一次 | 发送方及时获得收件ACK；接收TUI显示活跃追加标记，后续任务继续；记录是否同一会话，不能用屏幕代替后台turn匹配 |
| 6 | 在两台设备各自正常Codex App中发送一条普通短消息，再在现有CLI发一条普通短消息 | App与CLI均正常启动执行并回复，原登录状态保留；发生故障记录完整提示及时间 |

Mac完整地址：`ra2a://macmini-m4/01a0f8c1-b41a-79b1-85f2-5e0380b1d361`。ROG完整地址：`ra2a://rog306/01a0f8c0-3578-70f3-8ba4-7f35dcd2d6b8`。执行前仍应刷新发现，若原地址消失或agent不为codex-cli，应停止该项并反馈。任何发给历史App端点的结果不能充当CLI验收。

### 固定版本资源校验与Darwin原生准备

3811492仅修改测试：canonical短临时home；隔离HOME/CODEX_HOME/TMPDIR；start前拒绝已有socket和PID资源；start输出需backend=pid、socket准确匹配、解析后的package位于临时home、PID记录与返回值相同且有启动时间；stop前复核同一记录，只在首个归属gate通过后登记cleanup。Linux0.159.3/0.160.0原生race分别PASS，日志 `.cache/native-port-linux-0.159.3.log` / `.cache/native-port-linux-0.160.0.log`；Darwin arm64交叉编译PASS，不计Mac原生执行。

固定版本依据：官方 [rust-v0.160.0 backend/mod.rs](https://raw.githubusercontent.com/openai/codex/rust-v0.160.0/codex-rs/app-server-daemon/src/backend/mod.rs)仅PID backend；[lib.rs](https://raw.githubusercontent.com/openai/codex/rust-v0.160.0/codex-rs/app-server-daemon/src/lib.rs)从指定home派生资源；[pid.rs](https://raw.githubusercontent.com/openai/codex/rust-v0.160.0/codex-rs/app-server-daemon/src/backend/pid.rs)检查记录的进程身份后停止。这里推导独立home的PID实例可与正式实例分离，仍需每台原生运行与保护证据，不将源码推导升级为实测通过。

当前实际版本：Ubuntu CLI/daemon=0.160.0/0.160.0，Mac=0.159.3/0.160.0，ROG=0.160.0/0.160.0。

- Mac→ROG新版IDLE01–22发送22/22 accepted，error/unknown0、无重试/就绪超时，最低轮间隔9519ms；证据 `/tmp/ra2a-mac-rog-idle22.LBGWv5/evidence.json`。ROG接收核验及反向阶段已下达。Ubuntu发现曾短暂ROG unreachable，随后恢复ready；已下达一次新的只读状态诊断，未重复原测试消息，不能据发现恢复猜测反向阶段成功或故障根因。
- 06b7ec3增加隔离LAN后写缺确认验证：handler先取得有效native ACK，暂不返回CoAP响应，才取消sender caller；Node返回包装context.Canceled且不为ErrPeerUnreachable，control现有映射为unknown。目标原始userMessage恰1、另一线程0、handler调用1；释放handler后新marker1、旧marker仍1，无重放。0.159.3/0.160.0各race-count=2 PASS，原始日志 `.cache/native-r03-linux-0.159.3.log` / `.cache/native-r03-linux-0.160.0.log`，Darwin交叉编译PASS；尚未以Mac/Windows原生结果补证。

- ROG→Mac唯一新版反向发送accepted22/error0/unknown0/重试0，最短轮间隔9130ms；Mac→ROG接收端独立核验22条/精确ACK/started/complete/独立turn、缺失0/重复0。ROG证据目录内 `full-mac-rog-receipt-audit.json`、`full-rog-mac-reverse22.json`、保护前后快照。临时unreachable未导致该批次止步或重发；没有诊断其根因，也没有改正式网络/daemon。
- ROG→Ubuntu活跃22：22/22工具accepted，无error/unknown/重试；Ubuntu原生收件22、缺失重复0、同一持续turn（与Mac活跃22同属本次Ubuntu长任务），`.cache/full-cli-acceptance/rog-ubuntu-active22-receipt.json`。Ubuntu→ROG BASE/FOLLOWUP在发送前确认ROG busy，均accepted，ROG原始两消息归属同turn01a102b4-0f35-75d1-9830-a3b4aecdcd43、task_started1；当次汇报时回合尚未结束，complete待下一只读阶段补核。ROG证据 `active22-sends.json`、`active22-same-turn-audit.json`，正式保护不变。
- Windows前置16个runtime skip均为Unix服务/进程组/socket或portable process fixture不可用；原始名字/原因在ROG `runtime-skips.json`，不计PASS。两个Windows wrapper daemon/native fake fixture尚为Unix-only，不能以Linux用例代替。
- eed6dc7只修改测试：Windows短temp路径/107字节gate、profile与temp隔离、fresh current-user SID私有state DACL、file凭据store、启动或停止归属失败保留temp home。Linux两版本native各PASS；Windows/Darwin crosscompile PASS；已下达Windows原生运行，编译不算执行。原始日志 `.cache/native-windows-port-final-linux-{0.159.3,0.160.0}.log`。

- Mac06b7ec3隔离原生完成：0.159.3与0.160.0各race-count=1 PASS，均覆盖22轮/阻塞模型ACK/owner继续/daemon重启/双线程/LAN前写失败恢复/后写缺确认不重放。临时4个PID已退出、2个home及PID/socket/package清理，正式全部保护快照一致。archive SHA256=2c4ed026061baffa6f2aed1768cabd9f709e3535ddb849e832cea89dd1a23651，证据 `/tmp/ra2a-mac-native.4npKDK/`，native日志SHA256（0.160.0/0.159.3）68aa0729a0e0abae13ea093096f5edfa01cb895ea7aa981f23a10476cbefd17b / 4fb178c6d20392058ead8051ef86fb81ff4ae6c04c567339b1fddd3874078535。固定两个tag backend/lib/pid三文件hash相同。
- ROG→Mac新版22轮接收原始消息/精确ACK/started/complete/独立turn均22，匹配22，缺失重复0、无工具调用，原始JSON/turn/行号在上述Mac `receipt-evidence.json`。至此Mac↔ROG新版双向各22独立完成回合闭环，不依赖旧验收或把accepted当执行完成。
