# Codex CLI↔CLI 全量验证（2026-10-04）

- 状态：自动功能与隔离资源清理阶段通过；人工TUI/App操作仍待验，尚未完整验收通过。
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
| C03 | 双向active follow-up | 单start+同turn steer、原生ACK、marker匹配 | PASS：Ubuntu→Mac/ROG及Mac↔ROG活跃输入原始同turn核验 |
| C04 | 长工作收件解耦 | 工作未完成前ACK；随后执行独立核验 | PASS：Linux阻塞native模型；Mac/ROG→Ubuntu持续工作收件22条 |
| C05 | 交错回复无互等 | 单次有界回复、最多固定hop、无重复写入 | PASS：固定四hop，原始同活跃turn回信与ROG一次发送独立核验 |
| C06 | 同机多端点 | 明确登记、只写目标、无历史/所有权串线 | PASS：真实native双线程逐项输入计数 |
| C07 | 三设备与不同版本 | Ubuntu/Mac/ROG一条有界跨机链及原生/daemon各自版本 | PASS：Ubuntu0.160/Mac0.159.3/ROG0.160；固定四hop闭环 |
| U01 | TUI实时显示及人工继续 | 用户在Mac/ROG物理TUI输入、画面与回复确认 | 待用户按下方清单实操（现在可开始） |
| R01 | 官方daemon未运行/线程未loaded/旧版本 | 明确错误、零静默拉起/错误writer、零重放 | 相关适用平台回归PASS；排除/skip单独记 |
| R02 | RPC/daemon中途退出与重启 | 仅隔离实验资源；unknown不重放、重新加载后可用 | PASS：Linux/Darwin两版本及Windows 0.160.0原生；Windows目录残留另记 |
| R03 | LAN中断与恢复 | 隔离节点/测试路径中断，预写失败与不确定写入分类、恢复后单次成功 | PASS：三平台真实native+CoAP预写失败、后写缺确认、恢复及不重放；物理跨设备发现恢复未覆盖 |
| N01 | 异常/缺失ACK、能力、UUID、归属、home | 无伪成功/回退/实际非目标写入，原始边界结果 | 相关适用平台回归PASS；排除/skip单独记 |
| P01 | 正式CLI/App/配置保护 | PID/所属进程组、配置auth/代理/launcher元数据前后一致 | PASS：三端前后保护快照一致，未作持续监控 |

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

### Windows native 原始失败与资源保护

- eed6dc7单次Windows native-race FAIL（pass0/skip0），4.89秒在资源归属gate失败，未进入投递/LAN/recovery，不重试。官方返回temp current路径，Go EvalSymlinks报找不到路径；临时PID23476/home r2-1741810079保留，正式所有保护不变。原始证据 `C:\Users\77585\AppData\Local\Temp\ra2a-native-rog-0cbf4f7795d041a187bbedb0564f5763`，archive SHA256=8020D2F7E82F07F5327F202F8B21E09F2C2119CF02C0AF2228A49A4EB15823EE。
- 只读对照：current真实Junction指向temp release，原串及规范串的PowerShell/Win32 file handle解析成功，三个exe hash一致；Go1.27 Eval两串均失败。PID record与native FILETIME创建时间精确匹配，home version probe running。排除alias不存在与仅分隔符问题，Go内部失败原因未断定，不修改生产或升级工具。
- Windows-only测试修正范围：使用Win32 canonical路径，匹配实际PID创建时间/进程exe，socket只initialize核home后才登记cleanup；Unix保留原canonical gate。保留原始失败，仅清理全部归属通过的指定temp实例，不按group/job停止。修正原生复验尚未执行，不计Windows native通过。

- Windows失败实例已完成归属核验：native FILETIME/record/Win32 exe/temp socket initialize home全部相等，正式资源不同。第一次PowerShell因stderr warning终止未取得native exit，保留原始失败；观察40秒仍运行后，重新gate通过用.NET直接单次stop，exit0/459ms、PID23476/record/socket退出，home和原始证据保留，正式保护不变。不是重发RA2A消息。证据同失败目录 `process-cleanup-*.json`。
- 546e6c0 Windows-only canonical改Win32 handle，start/stop核actualPID creationtime/exe，公共只initialize home gate；Linux两版本native-race及最终CLI包54pass/1opt-in skip通过，Win/Darwin编译通过。ROG原生复验及Mac公共gate回归已下达，未计通过。
- Mac长工作现场：BASE/FOLLOWUP原始各1、同turn01a102c6-a316-7981-812f-a829afd338bb，FOLLOWUP到来时原任务仍未最终回复；原等待被输入唤醒后续等，首开始至结束20.932秒（非连续20秒阻塞）。证据 `/tmp/ra2a-mac-active.AHopL1/receipt-evidence.json`；工具两次accepted，原生完成在下一阶段补核，正式保护不变。

### 546e6c0 最终原生结果

- Mac两版本原生race均PASS，无fail/skip：0.159.3测试4.82秒/package6.604秒，0.160.0测试4.77秒/package6.209秒；公共temporary socket initialize home gate及全部native/CoAP覆盖均通过。证据 `/tmp/ra2a-mac-final-native.lOKv6O/`，archive SHA256=61ca2975fd6c4cf2508b3debe92de458ecd957f5867db63a9b997a81a455aa57。两版日志SHA256分别f094785583d0947ecb7617d6679964bc4456b7e92926416be14910e1d72fc7d9、8a452669c0d63155ff64b407a39acc667fb7aeb8b53d3d859ddae736ea41a9c5。四个临时PID已退出、home已清理，正式保护一致。
- Mac此前BASE/FOLLOWUP同turn 01a102c6-a316-7981-812f-a829afd338bb 的task_complete已补核：2026-10-03T17:20:35.109Z，rollout行2042，final保留两个marker，原始事件见上述目录previous-active-evidence.json。
- Windows第一次546e6c0复验在启动前socket长度gate失败：operator将TEMP设在长证据目录，得到156字节>107；没有启动daemon，不计Win32修复通过。原始失败保留，未放宽gate或修改源码。
- Windows使用仅go子进程的短TEMP `C:/Users/77585/r3-61558e`（最坏socket81字节）重新执行独立阶段，native0.160.0 race exit0/PASS1/fail0/skip0，16.13秒。包含privateDACL、Win32 canonical junction、PID FILETIME/exe、initialize home、22收件/owner继续/活跃收件/daemon恢复、真实DTLS/CoAP两thread隔离、关闭节点恢复及后写ACK缺失cancel不重放。证据 `C:/Users/77585/AppData/Local/Temp/ra2a-win32-reverify-045bd78b991d4b4da8e7bd3d82d466fe/short-temp-stage`；archive SHA256=D9823266BA775D4AD8C7BD5AB924DDC898D72253A8EA1CCECDB46BA0B474D43D，官方binary SHA256=FDDA5FA3CF3FB3D000B876720742857676293E4315E4B045FAE6F8BD7E866D1D。正式配置、CLI/App/daemon和代理保护全部一致。
- Windows清理边界：临时PID63924/26788均退出，PID record/socket/package不存在；fresh home仍剩 `.tmp`、`app-server-daemon`，源码忽略os.RemoveAll错误。因此功能测试PASS不等于目录清理完整，保留原始残留清单及目录，不手工删除。旧失败home及外层短TEMP也有意保留。

### 有界交错回复与三设备链

- 唯一链Ubuntu→Mac→ROG→Mac→Ubuntu：Mac发送一次TRI_MAC_ROG_01，收件accepted耗时873ms；ROG业务回复在send返回50.726秒后进入Mac原活跃turn（90秒界限内）。发送方API没有等待该业务回复。
- Mac原控制输入行2155与ROG reply行2188/2189同turn 01a102dc-e6dc-77b0-b028-fd5cdf58c1d0，task_started行2153；收回复时未完成。实际ROG from完整，message-id=f6be073fd40fdec72a348ad874e9f31a。证据 `/tmp/ra2a-tri.ilX7NU/` 含发现、完整入参、原始结果、源事件及保护快照；ROG发送侧和双方最终完成待下一阶段只读补核。Root send原始结果 `.cache/full-cli-acceptance/tri-stage-send.json`。
- Windows目录清理结果被源码忽略已确定：测试现改为os.RemoveAll错误时报错并保留路径，未改变目录删除范围/资源归属gate。Linux0.160.0显式native race单次PASS（5.710秒），原始日志 `.cache/full-cli-acceptance/cleanup-gate-linux.log`；不将Linux结果代替Windows残留问题闭环。

### 跨设备活跃输入与转发闭环

- Mac→ROG BASE/FOLLOWUP只有各一次send，accepted2/error0/unknown0；BASE耗时1204ms，重新发现目标busy后立即FOLLOWUP耗时797ms，不等待业务完成。Mac证据 `/tmp/ra2a-mac-rog-active.U7EEoq/`，包含ready/busy发现、完整原始入参/结果和保护。
- ROG独立原始输入：同turn 01a102e0-8cd9-7ed3-99b1-e6feae922a11，started行1967，BASE行1969/message-id=a34fa724a813ccbb75d38fad315f56fd/count1，FOLLOWUP行1980/message-id=bc044fba02a11fb60b25906371923271/count1。等待期限17:47:22–17:48:22 UTC；报告时完成pending，不把输入确认计作完成。证据 `C:/Users/77585/AppData/Local/Temp/ra2a-active-final-a485736358f54508bbafeacb96a8384e`。
- 三设备链ROG侧已独立核验：TRI输入一次，reply send一次/原始accepted，完整from/to匹配；ROG turn 01a102dd-a101-73a0-83e5-806b64ecaaac，started行1935/input1939/final ACK1963/task_complete1966（17:44:37.714Z）。证据 `C:/Users/77585/AppData/Local/Temp/ra2a-tri-mac-rog-01-1791049470718`。Mac上一TRI turn完成也已补核，final保留两marker，见Mac active目录tri-completion.json。完整四hop且无自动重发，所有正式保护一致。
- ROG正在对b4f159e运行一次cleanup显式错误gate复验，新临时目录与旧残留均不手工删除；结果用于解释清理边界，不追加跨机发送轮次。物理网络断开/发现恢复与人工TUI仍未覆盖，不能宣称全量无缺口。

### Windows cleanup显式失败与测试隔离修补

- b4f159e Windows原生单次整体FAIL（exit1/pass0/fail1/skip0）；22投递、活跃收件、owner继续、daemon重启、CoAP及owned stop已完成，失败发生在cleanup：`plugins-clone-2mwstS/.git/index.lock`被其他进程占用。test PID55160/6520已退出，两个home均无PID record/socket；仅按exe路径查询的processCount0不能排除系统Git正在访问目录，不据此杀进程或删目录。新旧残留清单分别5515/5579项；旧证据完整保留。证据 `C:/Users/77585/AppData/Local/Temp/ra2a-cleanup-gate-602e37c93aec423ab28dda3aca4271cc`，archive SHA256=57D7AC69313BF009175462D6FDC096D339FB5DCAC89093903454B92F2CDD008F。
- ROG active已补核完成：turn01a102e0-8cd9-7ed3-99b1-e6feae922a11，final两ACK行2026，task_complete行2029（17:49:47.490Z），见上述目录active-complete.json。
- 官方固定0.160.0 [插件manager](https://raw.githubusercontent.com/openai/codex/rust-v0.160.0/codex-rs/core-plugins/src/manager.rs)按plugins_enabled启动后台同步；[startup_sync](https://raw.githubusercontent.com/openai/codex/rust-v0.160.0/codex-rs/core-plugins/src/startup_sync.rs)创建plugins-clone临时目录；[features](https://raw.githubusercontent.com/openai/codex/rust-v0.160.0/codex-rs/features/src/lib.rs)默认开启插件。本fixture只验证RA2A投递与隔离daemon，故仅在新home配置features.plugins=false，消除非目标后台插件下载，保留cleanup严格失败门禁；不改正式配置或放宽归属。Linux0.159.3/0.160.0 native-race分别PASS4.510/4.501秒，日志 `.cache/full-cli-acceptance/no-plugin-native-{version}.log`；Windows完整清理仍需原生复验。此测试不覆盖插件启用时的同步生命周期。

### 自动阶段收尾（d13d656）

- Windows新隔离实例native0.160.0 race单次PASS：exit0/pass1/fail0/skip0，13.09秒，cleanupError=null；严格归属、107字节/privateDACL/8env/file凭据、22收件与独立执行、owner继续、活跃收件、daemon重启、DTLS/CoAP故障恢复与不重放均通过。PID9328/35252退出，新home `C:/Users/77585/r5-fa54a7/r2-1617430854` 完全不存在，socket/PID/package消失；外层shortroot空且保留。
- 证据 `C:/Users/77585/AppData/Local/Temp/ra2a-no-plugin-160dfe4d0e0445219978a32b2a7e65b6`，source=d13d656120036d93ac97f1e7354547fd52fc9efa，archive SHA256=9F4EE276E116DC70AA05981FBF3E68D2CBF2F57EC1F01F1FBD11B644B5A83285，native SHA256仍FDDA5FA3CF3FB3D000B876720742857676293E4315E4B045FAE6F8BD7E866D1D。仅有清理后文件观察，不能推断生命周期内从未创建plugins-clone；测试配置plugins=false和完整清理PASS为已证实事实。
- 旧r3/r4失败目录及全部原始失败日志保留，不把新home清理成功说成旧残留已删除；本轮没有手工删除、ACL修补或按进程组杀任务。正式保护全部一致。Ubuntu收尾再次核config/auth/App启动项/代理/socket元数据及官方daemon15822存活无变化；工作区任务修改均原子提交推送。
- 自动阶段已结束，现在按上方六项清单执行人工TUI/App观察。平台skip仍是skip；物理跨设备网络中断/发现恢复、插件启用的同步生命周期不在本轮实测覆盖范围，不扩大已通过结论。U01未回报前不得标记全量验收或发布准入通过。
