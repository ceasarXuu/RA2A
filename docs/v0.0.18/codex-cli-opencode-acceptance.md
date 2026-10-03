# Codex CLI↔OpenCode 验收（2026-10-04）

- 状态：执行中。Owner在CLI↔CLI人工验收通过后要求继续本方向测试。
- Codex CLI：`ra2a://ubuntu407/01a0f6ff-b902-7970-acba-ef8d3451c6f7`。
- OpenCode：Owner指定 `ra2a://rog306/ses_efcdeaba1ffeWmQEng78CCAXRO`；首次发现node ready/stale=false、agent=opencode/session ready。
- 收件、业务回信、任务完成分别记录；accepted不等于任务完成。不确定写入不重发。
- OpenCode只声明receiveText/replyAddress，未声明steerActiveTurn/interactiveSafe；活跃投递按其prompt_async宿主受理及独立执行观察，不套用Codex同turn steer语义。

## 验证矩阵

| ID | 项目 | 状态/证据要求 |
| --- | --- | --- |
| O01 | 适配器、租约、共享server、wrapper相关race回归 | 待执行；skip不计PASS |
| O02 | 双向基本收件与业务回信 | PASS：原生输入一次、一次业务回信、精确final ACK完成 |
| O03 | CLI→OpenCode 22条串行空闲输入 | PASS：22accepted，原生用户/精确ACK/完成parent匹配22，缺失重复0/工具0 |
| O04 | OpenCode→CLI 22条串行活跃收件 | PASS：22accepted，Ubuntu原始输入22，缺失重复0，同一活跃任务 |
| O05 | 双向工作中收件 | OpenCode宿主受理/排队与CLI活跃追加分开；不等待模型完成才收件 |
| O06 | 单次有界交错业务回复 | 固定次数send，无无限回投或自动重试 |
| O07 | 多端点归属/故障恢复边界 | 现有隔离夹具，未附着不发布、post未知不重放；不关闭正式server |
| U01 | 人工显示与继续输入 | 自动最后给清单，不能由后台日志替代 |
| P01 | CLI/App/OpenCode/认证/代理保护 | 各阶段前后元数据与PID记录；不部署/重启/改正式配置 |

## 执行纪律

- 仅对已发现且agent匹配的完整地址投递，from显式填写。
- 多轮API仅等收件，空闲组另行观察ready并至少8秒间隔；活跃组不要求独立任务或8秒间隔。unknown/错误即止步，不重试。
- 保留所有既有修改、故障原始结果与证据。隔离HOME/USERPROFILE/LOCALAPPDATA/APPDATA/CODEX_HOME及XDG变量，GOPROXY=off；正式CLI/App与OpenCode配置不动。
- OpenCode会话存储条目不等于执行权，确认live附着租约与共享server。保护正常TUI，不自动重启。
- 本方向仅测试已声明能力；人工操作最后进行，未回报前不宣称完整验收。

## 原始证据

Ubuntu证据存忽略目录 `.cache/cli-opencode-acceptance/`；ROG目录由接收端报告。前置任务RA2A_CLI_OC_PRECHECK_001已accepted，未重发。

### Ubuntu前置结果

- 来源853c967（测试期间仅文档变化，四包测试源码未改），四个相关包race exit0，41个pass测试事件、fail0、1个opt-in native skip；随后显式native OpenCode1.18.34权限session隔离测试race PASS1/fail0/skip0。日志 `.cache/cli-opencode-acceptance/linux-race.jsonl`、`linux-native.jsonl`，隔离profile与包清单见local-test-manifest.json。不将native权限回复测试视为真实模型消息完成；现场投递另核。
- 前置控制send仅等收件，原始入参/结果/耗时在precheck-send.json；ROG的原生记录与独立回信待到达，不因短暂无回信重投。

### ROG OpenCode前置与偏差

- PRECHECK原生输入msg_103309b2a001a0BpSTJaGEPWBe（created=1791054879530）恰一text part，含RA2A message-id 2e3fec4e3ad29b3d94e01d17b6f03458；一次业务回信已到Ubuntu，message-id 8c37bb7cdc5610366aa9207325325520。两者不与390ms工具收件混同。
- 本session唯一live lease PID38256(wrapper)，attach PID35136明确--session；owner PID59884及child监听server PID64824/127.0.0.1:4099，MCP PID49100。OpenCode实际1.18.33，RA2A PID28444/bin SHA256=3A6BE01624D08CDE4CB48A7638337CEE37C830510484C42D581F30A4C0A4FFDA，与上一CLI验收artifact相同；archive无vcs字段，不能从当前repo HEAD181b77e断言运行源码。此前5410aea绑定manifest为独立来源，不由版本v0.0.17推断。
- 证据 `E:/RA2A/.cache/precheck/RA2A_CLI_OC_PRECHECK_001/`，包含原生session/messages、lease/owner/PID、配置与launcher/proxy保护基线及原始send。
- 操作偏差：对端违反只读要求误执行一次`ra2a opencode`，120秒后shell终止，创建空session ses_efcce2e9dffe02zo2WrBR6iRW7与失效lease PID57012（已死，Active排除）。未删除；原正式server/TUI/CLI/App/配置未重启或改写。该阶段不能描述为全部只读或全程进程集合未变化，后续保护以原正式进程及配置为对象。误操作不作为测试功能通过证据。
- 收件后业务回信延迟中包含前置取证与误启动等待，不能拿它估计普通消息时延；接下来只用无工具精确ACK消息测试。独立同机CLI只读状态任务已发一次，未向OpenCode重发PRECHECK。

### CLI→OpenCode 22条发送侧

- IDLE_01–22全部accepted，error0/unknown0/stopped=null，无重发；每次重新发现ROG node ready/stale=false、指定OpenCode session ready；21个间隔均>=8秒。原始selected node/session字段、完整from/to/text、UTC/耗时与原始工具结果保存 `.cache/cli-opencode-acceptance/cli-oc-idle22.json`。接收原生消息/精确ACK/完成由ROG Codex CLI独立GET核验，不直接由accepted推导。
- OpenCode→Ubuntu活跃22条唯一反向批次已发控制（accepted），Ubuntu持续当前测试工作，不将反向收件视为22个独立完成任务。ROG CLI只读取证和隔离Windows四包race并行执行，不对OpenCode追加投递或改权限。
- 同机只读诊断已完成：PRECHECK原始输入一次，send回信一次原始accepted，final ACK完成于19:24:18.890Z；session message/status/permission GET均200、unfinished0/pendingPermissions0，原owner/lease/PID配置保护一致。证据 `C:/Users/77585/AppData/Local/Temp/ra2a-oc-readonly-92211df06c344e4fa0957af557f4847f`。当前快照不能证明此前全部延迟原因，误启动等待仅已知贡献，未作额外修复。

### 双向22接收与Windows失败

- CLI→OpenCode收件317–484ms，平均391.9ms，最小轮间9053ms；ROG原生GET独立核验raw users22、exact final ACK22、missing0/duplicates0/tools0，每条messageID/parentID/完成时间匹配。证据 `C:/Users/77585/AppData/Local/Temp/ra2a-oc-audit-34f0b4ee890e44e192cf6c3bee9b2bbc/receipt-matching.json`；19:32:51.607Z原始GET SHA256=17b509d7781a7174b1d18b11916681bf23c49a6721251750e967c240a8fc50ca。
- OpenCode→Ubuntu ACTIVE_01–22发送accepted22/error0/unknown0、报告无重发；Ubuntu原生rollout匹配原始输入22、每个round恰1，同一活跃turn 01a1032f-400a-7772-917d-81d7db86a55c，证据 `.cache/cli-opencode-acceptance/oc-cli-active22-receipt.json`。不是22个独立完成任务。发送原始工具记录由ROG保存在OpenCode native store及 `E:/RA2A/.cache/active22/`；工具未返回message-id，不推造。
- Windows source=a27bac95955cf591e585d63eec964624a569c299/archive SHA256=B917041D7411E4D2F443C85331F447C98E9053C41854085FE8E879B698693A30，四包原生race exit1：opencode18pass，ocsession1pass，ochost7pass/2skip，wrapper11pass/1fail/2skip。唯一FAIL为TestNativeExecutableSkipsTheWrapperItselfInPath，夹具创建无后缀opencode而Windows生产候选只查.exe/.cmd，得到空路径；仅fixture按Windows生成.exe，不改生产解析。Linux针对原断言race PASS1.014秒，保留Windows原FAIL，不计闭环直到原生复验。
- 四个Windows skip：ochost监督重启/host outlives caller因无Unix shell；wrapper自动attach Unix fake fixture；native权限测试刻意未opt-in（真实现场OpenCode投递不替代权限native用例）。平台排除为ocsession/process_unix.go及ochost/process_unix.go，另记。上述证据目录test原始日志/summary/go-list以及正式前后保护均保留，保护一致，旧误启动空session/stale lease未删除。

### Windows夹具复验与活跃窗口取证

- e57616504ae7ee3e70235dd3c77233e7f5e0b720独立archive SHA256=6C21588423EF0343082F9AF9039F2E5C410EED49C4CB98BF1B92E24F3FEB49C8，仅cmd/oc-wrapper Windows原生race一次exit0/pass12/fail0/skip2；原失败PATH选择断言PASS，已通过其他3包不复跑。证据 `C:/Users/77585/AppData/Local/Temp/ra2a-oc-fixture-ee87e72dec9a4044bb9b807c0037a377`，全部正式保护一致。
- 同目录ACTIVE22原生tool匹配证明每标记恰一次、accepted22，工具start>=上一end且标记顺序01–22，严格串行。控制任务final完成19:34:47.459Z，首行ACK正确但有额外摘要，整条精确ACK匹配0；仅此控制输出格式不合规，不据此否定22条已收件，也不声称精确格式通过。
- 第一WORKING_STAGE收到BASE_01后，ROOT查询发现OC已ready，忙态gate拒绝FOLLOWUP_01，零写入；该轮不计活跃追加通过。原30秒窗口比主Agent处理时延短，不通过重放修复测试。
- 新独立STAGE_02采用工具内直接观察busy发送：base收件390ms（19:42:08.185Z），followup收件444ms（19:42:14.494Z，明确busy）；完整入参、原始结果与snapshot `.cache/cli-opencode-acceptance/working-02-send.json`。是不同nonce/样例，不重投旧任务或unknown；原生两次等待、输入/任务段与完成由ROG CLI独立只读核验。OpenCode不声明同turn steer，不按Codex原语作通过推断。
