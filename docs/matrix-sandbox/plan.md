# Linux 矩阵沙盒工程设计

- Product Authority：[PRD Confirmed Product Decisions](prd.md#confirmed-product-decisions)。
- Applicable Decisions：PD1、PD2、PD3、PD4、PD5；Codex 所有权语义引用既有 PD36/PD37。
- 状态：Linux隔离预检查与真实矩阵运行器已交付；真实验收结果INCOMPLETE，App适配、CLI恢复及人工观察仍有资格限制。
- 更新：2026-10-12。基础设施主题独立于版本目录，不自行指定发布版本。

## Execution Contract

产品权威仅限用户直接确认行，Agent 不自批准。当前工程证据可修订方案，不能改变产品权威；新增核心规则列入待决。每个实现阶段前按实际实现 rebase，pending/blocked-on-plan-approval 不执行；重大方向/范围变化记录 Plan Delta 并取得批准。阶段后仅审计本阶段 Product Decision Delta。Owner「继续，把沙盒做完」授权继续实施。Q1/Q2已由Owner三项清单后「允许，继续，这个算测试套件」确认；1800行实现范围获批。可执行私有凭据导入与有界付费输入，不启动正式服务。

## 1. 已核实的实现基础

| 位置 | 可复用内容 | 当前限制 |
|---|---|---|
| internal/lannode/node.go、listeners.go | mDNS、IPv4逐地址监听、DTLS/CoAP与真实message-id | 启动会发布 `_ra2a._udp` 且枚举所有本地IPv4；宿主网络多进程会污染正式发现 |
| cmd/ra2a/main.go | serve控制端与registry；RA2A_CONTROL_ADDRESS、独立HOME | 正式安装/setup/restart路径不能用；当前没有沙盒预算入口 |
| internal/control、agentbridge | 发现与投递、适配器结果 | 全节点还需接收侧预算，不能只限制controller主动发信 |
| internal/codexcli/native*test.go | 真实backend、原生记录、隔离凭据mock、故障验证 | 大部分是backend/SDK测试；native_lan_test会发布临时mDNS，不能宿主直跑当完全隔离沙盒 |
| internal/codexstdio、stdio_bridge_native_test.go | 三平台隔离桥、ACK与通知、既有owner继续/睡眠拒绝 | 未接真实App或actual writer仲裁；不能当 ownership 修复已完成 |
| cmd/oc-wrapper/focus_native_test.go、internal/ocsession | 真实attach及动态焦点、lease撤回 | 需真实TUI窗口与收费输入再核验 |
| internal/pi/native_fixture.mjs、native_test.go | 真SDK/自动extension发现、followUp与lease | 目前mock stream；真实TUI/账号/provider消费仍需沙盒实测 |

本机只读命令库存：docker、unshare、bwrap、Xephyr、dbus-run-session、ip及三个CLI路径存在；未确认daemon运行、userns权限或GUI可用性，不据command -v声称环境已具备。

外部依据：[Linux network_namespaces](https://www.man7.org/linux/man-pages/man7/network_namespaces.7.html)说明网络设备、协议栈、路由/端口与abstract Unix socket命名空间隔离；路径型Unix socket仍需mount/私有目录边界。[D-Bus dbus-run-session](https://dbus.freedesktop.org/doc/dbus-run-session.1.html)提供独立session bus用于回归测试，随被启动程序生命周期退出。这些原语不自动证明真实App单实例与全部宿主通道已隔离，D2必须实测。

## 2. 拓扑：一个私有网络，三个真实节点

```text
可信 controller（预算账本、调度、脱敏报告）
  └─ 私有 IPv4 bridge（mDNS/UDP只在其中，模型流量受控出口）
      ├─ node-codex：RA2A + 官方 CLI + 官方 App
      ├─ node-opencode：RA2A + OpenCode serve + 真实 attach TUI
      └─ node-pi：RA2A + 真实 Pi TUI/extension
```

- Codex CLI/App同一节点共享该节点私有CODEX_HOME，用于真实同ID交接；基础互通矩阵使用各自不同ID，避免同时争同一锁。
- 每类Agent可创建第二个来源会话验收同类互通；该类接收总预算不增加（PD4：同类跨会话共享10）。
- 候选以Linux namespace为隔离原语；先验证现有工具能否建立user/mount/pid/net/ipc隔离，再确定用rootless容器还是受控namespace启动器。不是同时建设两套运行时。
- 私有地址池根据现有路由检查后分配，不硬编码冲突网段；无host network、无宿主控制端/IPC/用户home可写挂载，无Docker socket给Agent。mDNS、DTLS、节点重启使用真实协议，不直接调用Deliver冒充全链路。
- controller仅从隔离节点control端发请求或启动绑定真实session的MCP；真实Agent主动工具回信另行验证。不能仅伪造from地址就宣称发送端工具链通过。
- 模型出口使用所选provider配置所需域名/代理。节点不能通过出口访问宿主正式RA2A/control/daemon；代理回环地址需隔离relay映射，不能无条件开放宿主端口。

## 3. 目录、GUI与生命周期

短私有运行根目录 `/tmp/r2s-<随机ID>`（0700）保存work/home/runtime；凭据优先在私有tmpfs，0600。报告输出到repo忽略的.cache/sandbox/<run_id>，不提交运行证据或秘密。每节点HOME、XDG、TMP/TEMP、CODEX_HOME、PI_CODING_AGENT_DIR、lease/owner/控制socket独立。socket启动前核长度。

真实Linux GUI采用独立显示服务器（候选Xephyr）和私有session D-Bus/XDG_RUNTIME_DIR，不共享正式App的用户数据、单实例锁或IPC。优先使用已安装官方App只读映像，不更新它；CODEX_ELECTRON_USER_DATA_PATH/CODEX_HOME等仅是候选隔离设置，必须核实际IPC路径和监听归属。TUI在沙盒PTY中启动，既能自动采集也能展示给人工。

显示服务器本身接宿主显示的必要桥不等于把宿主X socket/DBus/keyring完整暴露给Agent。Electron sandbox、GPU、桌面依赖不能靠--no-sandbox等临时削弱项悄悄绕过。若namespace方案不能保持原生UI/认证行为，记录方向不成立，提出独立Linux VM方案再评审，不假装全覆盖。

只给启动器最小系统权限；模型Agent运行非特权身份。仅清理具有run_id、PID创建身份、进程组/cgroup与路径证据的自有资源；不注册systemd/全局服务、不使用setup/restart/install流程、不按名称kill正式进程。退出时先停止付费入口→客户端→节点→凭据副本→网络/显示；失败残留报告明确资源与清理状态，不使用通配删除。

## 4. 真实凭据导入

先只读识别认证类型和当前选择的provider/model，不输出值；文件/API key选择最小可用集合复制/传递到私有环境，原文件只由宿主启动器读取。正式配置中的任意shell command、MCP、插件不得整套继承执行。

不继承host Secret Service/DBus解锁会话作为快捷方式。优先验证文件认证可复用；keyring-only或设备绑定情况做单独Discovery：若无法隔离复用，报告该Agent AUTH_BLOCKED并停止收费用例，不能擅自更改正式store。复制token可能触发账户刷新互斥/失效，需小规模验证并报告；PD5已确认：刷新只留私有副本，不写回正式认证。

真实凭据用于真实客户端小规模矩阵；mock fixture保留用于不限真实额度的计数并发/故障回归。日志只白名单采集协议状态、marker、原生事件ID和时间，不导出完整会话thinking、auth、headers或环境块；trace/screenshots可能有敏感内容，人工UI证据按需保存。

## 5. 预算必须在接收端可信入口落实

候选新增test专用运行器 `cmd/ra2a-sandbox`，复用registry/Coordinator/LAN构造（位置重用可行性先Discovery）；正式serve默认行为不加沙盒分支。

所有沙盒接收路径在进入Adapter.Deliver/原生输入前向可信controller预留额度。Agent无账本写权限、不能选择预算桶；桶由启动时登记的harness身份决定。MCP/control、LAN回信、直接sandbox CLI send均须同一接收门禁。原生客户端自身启动提示/人工输入还需按相同账本调度；无法阻止绕过的路径不得开放为自动验收入口。

预留记录至少run_id、bucket、case_id、message_id、状态、时间；不得保存秘密或全文。原子持久预留只提交≤10，计数服务停止/损坏时拒绝收费输入。restart/resume重新关联同桶；已有message-id返回记录状态，不重新写入。写前拒绝与写后unknown分别报告；PD4已确认：ACK超时不退款、不自动重放。

只在controller检查额度不充分：真实Agent工具会绕过调度器发信。实现前必须证明每条可能的原生输入路径有预算门禁；否则先禁止该未守门路径，而不是宣传10条硬上限。人工直接键盘无法天然被RA2A拦截，最后人工步骤需受控PTY/UI输入桥或明确用户操作协议并标注保障级别，具体方案先验证。

## 6. 10条预算下的矩阵排程（建议）

四类Agent：CLI、App、OpenCode、Pi。每个接收类型10个slot，共最多40个预留；新的source会话不带来新额度。

| 每类接收slot | 内容 | 证据 |
|---|---|---|
| 1–4 | 四来源各一条（含同类来源）；16条全有向组合 | 实际LAN参数/ACK、接收端原生输入/final；控制端发起不计发送端模型工具通过 |
| 5–6 | 固定环CLI→OC→Pi→App→CLI的真实工具请求与回信 | 每类型收1请求+1回复，真实工具args/结果、原生来源和完成链 |
| 7 | 已有任务工作时追加 | 收件及时、原工作继续、追加按原生语义处理；尽量复用slot6尚在执行的工作 |
| 8 | 恢复后新输入；Codex加同ID持有者迁移核验 | 真实客户端/锁/lease变化、地址保持与无重放；其他类型resume保持自己的原生语义 |
| 9 | 原生收件后ACK阻断 | 接收1次、发送unknown、不重发、不再次计费 |
| 10 | 最后人工继续输入 | 可见标记、输入可继续；人工输入同样计入额度 |

时间调度或客户端准备若需要额外模型提示，同样占slot并缩减后续项目；不额外偷偷seed。无法在预算内证明的项目NOT_RUN_BUDGET，用户可以下一次明确选择子集，不能自动续轮。

无付费输入的检查可另行执行：隔离、mDNS发现、认证文件存在/权限、无人持有时不可达、服务关闭/重启与地址发布、controller断链拒绝、预算第11条被拦截。真实认证成功只能通过已计数的首条模型执行证明，不另开不计数auth probe。

## 7. 报告与通过标准

输出summary.json与中文report.md：源码commit/dirty、产物hash、真实客户端版本、run_id、拓扑、auth类型/模型名、各桶预留/实际收件/拦截、每case证据索引、ACK耗时和独立完成时间、missing/duplicate/unknown、进程/目录清理与正式保护差异。

状态分PASS、FAIL、BLOCKED_AUTH、BLOCKED_ISOLATION、BLOCKED_ADAPTER、NOT_RUN_BUDGET、MANUAL_PENDING、CLEANUP_INCOMPLETE。这些是报告设计，不添加生产网络错误协议。只有要求项目全有对应证据才给矩阵总体PASS，skip和控制请求accepted不算通过。

## 8. 工作单元与阶段

| 单元 | 位置/对象 | 动作及收益 | 副作用/成本 | 验证与停止边界 |
|---|---|---|---|---|
| D1 | experiments/network既有经验、namespace启动工具 | 验证三个私有net节点真实发现/DTLS可用，确定唯一运行时 | 临时namespace/bridge，所需系统权限 | 无真实凭据/模型；宿主看不到测试mDNS，节点不能连正式control；失败不安装第二套系统 |
| D2 | 官方App启动与display/DBus/IPC | 验证真实GUI隔离和单实例互不影响 | 临时display/cgroup，GUI依赖成本 | 不复用正式stdio，不接正式home；IPC/资源未知停止，无UI通过结论 |
| D3 | Auth导入器候选 | 最小凭据副本与provider复用 | 秘密副本、真实调用/refresh副作用 | 先文件权限/路径，第一条模型输入计入预算；keyring未证则阻塞 |
| W1 | test runner接收入口、controller账本 | 原子预留与全入口预算门禁，保护真实额度 | 小型持久账本/门禁，节点失联停止 | mock并发11+条、重启/恢复/回信/直接send均≤10；unknown不重放 |
| W2 | 单一启动器、三节点profiles | 真实二进制/PTY/App启动与资源身份记录 | 配置投影、进程生命周期/权限复杂度 | 版本来源与前后正式保护；仅清理自有，失败保留 |
| W3 | 矩阵case表、原生日志读取 | 串行计划、真实工具环、关联证据和报告 | 少量各客户端差异，不建通用插件框架 | 16组合与10slot预算一致，精确ACK/原生完成分开；未集成ownership显式blocked |
| W4 | 人工观察入口 | 最后受控输入和可见窗口 | GUI/PTY操作桥或人工协议 | 剩余slot10；绕过预算能力未证不称硬限流 |

### Phase A：隔离与入口Discovery

- Pre-Phase Plan Rebase Gate：ready（仅D1/D2无凭据验证）；material delta=none；approval=user-approved-plan-direct：Owner「先验证」。依据当前库存与代码复用，仅做namespace/bwrap/display/DBus探针，D3及实现仍未授权。
- 执行verified（本次无Agent基础探针范围）；D1真实三节点LAN、D2显示/DBus/bwrap原语通过，D2真实 App 私有窗口已观察；无网络时停在组织设置加载门禁，backend/IPC 与单实例完整验收仍未通过，不能整体声明GUI已兼容。预算桶/写回规则确认后才D3。
- 建议阶段生产新增≤250行；主要一次性诊断脚本/报告，不把阶段验证扩成完整部署工具。依赖安装、特权常驻服务或VM变更另行评审。
- 2026-10-12本机实际结果：[复现与证据摘要](../../experiments/sandbox/README.md)。214行一次性探针，无新增依赖；unprivileged namespace可建私有bridge/3个net，真实mDNS+DTLS/CoAP环形投递通过；bwrap隔离HOME/运行目录与私有passwd支持D-Bus，Xephyr单socket/new-cookie下xdpyinfo通过。正式Agent、Auth与模型未运行，收费消息0；原语方向direction-supported，不等同完整App/预算实现。

### Phase B：预算与最小真实矩阵

- Pre-Phase Plan Rebase Gate：ready；基于d2591ef私有LAN/显示证据，单一namespace+bwrap；实际CLI/OC/Pi现有适配器复用，App actual-owner仍BLOCKED_ADAPTER。material delta=实现行数限额扩大，user-approved-plan-direct：Owner「允许，继续，这个算测试套件」。
- 执行in-progress；W1先mock验证，再W2/W3真实凭据；建议每个批准实现阶段新增生产≤500行、单文件≤500，超过就先简化并请求批准，不借测试分类隐藏生产运行器。
- 不以新沙盒任务顺便实现Codex actual-owner根治或App stdio安装；现有缺口标BLOCKED_ADAPTER，独立任务接入完成后才可验收。

### Phase C：真实UI/持有者生命周期

- Pre-Phase Plan Rebase Gate：pending；依赖A GUI隔离证据及actual-owner实现。
- W4人工放最后；UI完整验收不能用fakeApp或native-only替代。权限弹窗、焦点切换、正常/异常退出、同IDCLI/App转移分别核验；不足预算显示未执行，不增额。

## Pending Product Decisions

| ID | 待确认建议 | 影响 |
|---|---|---|
| Q1 | 本轮同类Agent所有会话共10条、预留后不退回，初始化/回信/人工输入都算 | 防重启/多会话绕过，失败可能使本轮不能完成全矩阵 |
| Q2 | Auth文件导入私有副本，刷新不自动写回正式文件 | 保留正式文件写入边界；token轮换使正式客户端受影响的风险仍需验证 |

## Product Decision Delta

| 阶段 | 决策面 | 观察/拟实施 | 分类 | 下一步 |
|---|---|---|---|---|
| 本轮设计 | 单机真实客户端、认证复用、10条限制 | PD1/2/3覆盖；只有文档，无运行 | covered | 评审设计 |
| 本轮设计 | 计数桶/保守预留/认证写回 | Q1/Q2建议，未实施 | provisional | 用户确认前不执行依赖项 |
| 无凭据基础预验证 | 三节点namespace/显示/DBus | 真实网络与显示原语通过，无Agent/Auth/额度实现 | engineering-only | 真实App与认证/预算门禁另行验证，不扩大为正式部署 |

## Plan Delta

首次设计，无旧沙盒实现计划需要替换。后续隔离运行时、真实认证能力或收件入口证据否定本方案时，在此记录变更及批准，不修改产品权威来迁就实现。

### 2026-10-12 继续实施的范围核验

- 已形成 `experiments/sandbox/preflight.py` 一条命令：现有工具检查、构建绑定探针、三节点真实私有 LAN、私有显示/DBus、可选真实 App 窗口观察及中文报告。无认证/收费输入；不称完整矩阵。
- D2 实际网络禁用时组织设置无法加载，已保存窗口截图与 namespace/进程观察；下一阶段需受控出口及凭据副本，不能通过扩大宿主挂载绕过。
- 完整 W1–W4 预计需 1,200–1,800 行手写生产代码，超过 AGENTS.md 当前阶段500行限制。已请求将本次实现上限扩大到1,800行，各文件仍≤500；未获批准前不扩张到依赖该上限的完整运行器。不把脚本或运行逻辑藏进测试文件规避统计。
- Q1/Q2 已同时提供可选精确定义供 Owner 确认；不以通用继续指令替代额度桶/退款/认证写回的明确确认。

### 本次实现授权与边界

- Owner已确认前次最终消息列出的三项具体规则，PD4/PD5记入唯一PRD权威；本次测试套件新增运行代码上限1800行，单文件≤500，测试不计。测试套件运行器本身仍计入行数，避免借测试名隐藏可执行逻辑。
- Runtime复用真实适配器、三节点LAN、私有PID/network/mount/user/IPC。OC租约要求node与TUI共享本轮PID命名空间；节点/控制器设置PR_SET_DUMPABLE=0并分别私有mount，预算UDS不挂给Agent，验证/proc越界拒绝。
- 当前App无安全actual-owner边界，套件保留真实App启动观察，正式消息矩阵App相关项为BLOCKED_ADAPTER，不能创建替代App或借SDK/fakeApp填PASS。这与原计划明确的适配器阻塞语义一致，不顺带实现ownership修复。

### 2026-10-12 套件实现与实际验收

- 主入口 `experiments/sandbox/run.py --output <新目录> [--manual]`。独立receiver worker复用真实adapter/control/mDNS/DTLS-CoAP；原生TUI获得并发布会话，禁止RPC cold resume冒充TUI。
- 已实现接收端持久预算、重复ID去重、私有凭据、限定HTTPS出口、基础16格、工具环、忙时输入、原生restart/resume、原生写入后故意延迟transport ACK、最后的受控人工观察。预算故障/模型错误时停止相关付费依赖项，不自动重放。
- 同kind self格使用同一原生会话；不声称证明同类两个不同会话隔离。App actual-owner及CLI↔App交接仍明确BLOCKED_ADAPTER；无安全生产适配器不冒充完整四类PASS。
- 真实首轮报告matrix-002为INCOMPLETE：OpenCode→Pi原生收件+精确final通过；OpenCode自身模型error、Pi self重复原生输入、Codex启动缺口分别保留。使用量CLI1/OC2/Pi3，未超10；自有资源reap、凭据副本删除、八项正式auth/config哈希终点相同。此轮结果不是最终新版回归。
- 无模型验证：Python41项全部通过（显式namespace opt-in）；Go两个相关包race通过。用例覆盖并发预算/损坏关闭、原生记录负例、HTTPS拒绝路径、同kind PID和跨kind/proc隔离、超时清理独立process-group后代。正式root ownership故障未据这些用例宣称根治。
- 公开报告不复制凭据、headers、thinking；报告原生ID/源行/白名单ACK/hash及预算。原生完整session位于临时私有HOME，正常结束删除，不能用公开摘要恢复全文。异常资源归属不明时保留0700恢复目录并报不完整。

- 最后修订：按RA2A envelope message-id精确匹配或业务正文首token关联，FOLLOWUP引用BASE不计重复；恢复前等待旧attachment撤回，拒绝sessionsStale节点，旧端点未撤回则停止，不coldresume。

### 最终套件交付记录

- 新增运行代码共1775行（含空行/注释保守计数），单文件最高341行，未超过批准的1800/500限制；测试不计入。
- matrix-006六个basic格native PASS：CLI自收、CLI→Pi、OC→CLI、OC→Pi、Pi→CLI、Pi自收。CLI/Pi busy gate及BASE/FOLLOWUP原生完成通过；Pi原生关闭恢复、跨节点写入后故意丢ACK返回UNKNOWN且接收唯一完成/不重放通过。
- OpenCode返回NATIVE_MODEL_ERROR；CLI resume未取得恢复完成证明，App和writer交接仍BLOCKED_ADAPTER，人工MANUAL_PENDING。整体INCOMPLETE；失败停止依赖项，没有追加重试或更换模型。预留CLI6/OC1/Pi7，私有资源reap、凭据副本清理，8项正式auth/config终点哈希不变。
- matrix-006原汇总active两个格留在RECEIPT_ONLY，但native子证据均PASS；是运行器成功状态漏更新，代码已修正，原报告不改写，qualification.md单列解释。
- 本轮启动绑定c851e87、sourceDirty=false，但sourceChangedDuringRun=true：执行期间补充错误报告/预算提示/异常清理。因此是真实功能观察，不是最终源码冻结的发布验收；最终无模型回归独立记录。matrix-005结束时Git字段也不等于其启动构建来源，原报告保留。
- Codex官方socket tmp别名、paste Enter窗口、跨节点控制层重生message-id三项运行期经验已纳入runbook。跨节点关联改用完整source+正文首个唯一marker，存储的message-id仍采用真实接收值；ACK故障仅消费一次首marker，不能用正文引用触发。
- 资格解释及原始公开白名单摘要留于本机.cache/sandbox-discovery/matrix-006；未提交Auth、token、环境块、headers、thinking或完整native会话。

### 2026-10-12 发现问题修复阶段

- 本阶段只修复已证测试门禁和诊断信息缺口，新增运行代码54行（保守按diff新增计），不延续此前1800行上限，也未扩张App适配范围。
- CLI旧12秒撤回门禁与官方0.162.1默认60秒真实writer保持冲突；按本轮私有config动态等待并设置180秒上限，始终观察实际端点，不强行注销/停后台。零额度默认60秒/配置1秒及实际Adapter start/steer对照证明机制；真实官方TUI关闭恢复同ID、收件和final/task_complete专项PASS。
- OpenCode唯一输入证实当前space-bunny-free服务端HTTP400/MODEL_NOT_AVAILABLE，收件confirmed/accepted。补充安全类型/状态/白名单类别诊断，不记录message/header/body/thinking；未切换provider/model。等待Owner指定可用模型再继续收费验收。
- Python套件44项全部PASS（开启Linux运行时隔离测试），前序失败保留。专项CLI2输入，首次独立启动器缺参止步1输入另记；OC1输入。资源已回收、私有凭据移除、正式8项auth/config终点hash不变。不外推完整矩阵通过；App仍BLOCKED_ADAPTER，人工阶段未重跑。

### 2026-10-12 剩余缺口的最小安全接入阶段

- Owner「修完」授权继续修复，正式App/CLI保持不变。当前默认500行限制；完整GUI矩阵预计600–900行，已提出900行扩展问题，未把未回复当批准。
- 零模型真实App已证新spawn stdio桥唯一原App初始化，可复用现有codexstdio；实际多wrapper要求每个私有socket绑定独立nativePID/创建tick，按kernel writer选择，禁止单一全局backend推断。
- 最小接入新增Linux-only沙盒wrapper与App适配器；Unix SO_PEERCRED核对保护worker的namespace PID与创建tick，wrapper/worker设dumpable0。发现/投递需真实writer的PID/创建tick/exe均匹配当前自有native；只有既有thread start/steer，不resume，不据历史或loaded单独声称所有权。预算仍通过worker的sandbox.Wrap在投递前预留。
- 本阶段仅交付可编译、可验证的安全接入模块及显式OpenCode参数；默认运行器的App GUI完整矩阵不得被这些mock/原生隔离检查替代。未完成真实GUI模型验收前继续标BLOCKED_ADAPTER。
- 本阶段新增运行代码407行（按新增行保守计数，含通用诊断文案替换），单文件最高200行。Python48项通过；Go sandbox/worker/bridge相关包race通过，另显式官方App内置0.160.0的原生fixture通过busy steer、owner continuation、睡眠拒绝与清理。真实私有App生产wrapper两次初始化成功、两个保护socket HTTP200，零凭据/零模型输入；这些结果不替代真实GUI thread与模型验收。

- Owner已明确批准本阶段累计900行上限及6.1-sol light模型。按本机模型声明，准确标识为gpt-6.1-sol、轻量推理low；OpenCode显式选择openai/gpt-6.1-sol/low，Codex/App私有配置保留对应low。正式配置不改，Pi保留现有选定provider/model。
- 四类矩阵工具环与对应basic边复用同一次真实输入，另独立核验工具回信，避免重复付费；各类最大10输入仍由接收端预算硬限制，耗尽后人工项明确拦截说明，不能自动开新run规避。

### 2026-10-12 冻结矩阵与有界补证

- source239852f冻结matrix-007：sourceDirty/sourceChanged均false；整体INCOMPLETE。OC/Pi基础互通、工具回信、恢复和ACK丢失不重放有原生通过证据；Pi忙时追加通过。App/CLI启动端点未发布，OC忙时BASE没有独立final，人工未执行，不计通过。
- 原账本CLI1/App1/OC7/Pi6；后续诊断继承各类原预算，不自动创建新轮清零。App仅本地mock模型诊断，真实Auth私有副本不向真实模型发送；CLI最多1、OC最多2新输入，分别核查新会话选择与原生parent切换。
- CLI共享目录的新启动选择只接受启动前不存在的rollout，防止误选App历史；57项Python测试通过。App第一拒绝门禁和OC合并完成语义仍待实际补证，不能据预测删掉writer/capability门禁或宣布完整矩阵完成。
- 当前本阶段手写生产新增按保守差异计877/900；测试与证据不计入。正常运行后的临时凭据副本已随自有资源回收；正式8项认证配置终点hash相同，不扩张为全过程无变化证明。

- 有界补证：CLI历史选择真TUI PASS（原账本1→2）；OC native parent切换与一个精确双ACK final确证（7→9），组合匹配修正后10项证据回归通过，完整Python58项PASS。App真实初始化遗漏initialized通知导致ErrNotReady已确证；只新增1行成功响应ready，失败初始化仍拒绝，官方0.160.0无通知fixture race通过。当前阶段生产894/900，完整GUI补证待执行。

- 修复后7817058真实App GUI专项PASS：unique bootstrap和自投两输入继承App2→4/10；真实writer/canAccept门禁、原生收件/final/task_complete、独立回复行可见及关闭撤回通过。累计CLI2/App4/OC9/Pi6，旧矩阵INCOMPLETE原样保留；新的全量轮额度已向Owner请求，未把等待视为批准。

### 2026-10-12 Owner批准新轮额度后的冻结验收

- Owner批准独立新轮，matrix008冻结3ed3e89/dirty=false/changed=false。14/16基础格PASS；CLI/OC/Pi busy及恢复PASS。App工具回信pending致两项ring失败并停止App依赖；App忙时/恢复/holder交接未执行。整体INCOMPLETE，人工未执行。
- CLI ACK-loss选中同node App，绕过真实LAN丢ACK钩子，故accepted是测试sender选择错误；按实际node且非failed筛选修正，相关7项运行器回归PASS。旧CLI ACK-loss不改判，CLI10/10不追加验证输入。
- 本轮预算CLI10/App4/OC9/Pi8，正式认证配置终点hash不变，owned资源reap且私有copies回收。App挂起仅只读保存安全投影，原因仍待透明MCP stdio唯一新标记补证；不自动开新轮、不重试旧标记。当前阶段保守新增生产900/900，后续若需扩张先据证据说明并取得授权。

- 透明MCP诊断累计追加App三条bootstrap，均保留计数：006误继承App环境导致Pi worker未启动；007零输入API/握手通过但12秒诊断就绪窗口不足；008纯零输入真实GUI/guard/MCP初始化通过；009采用生产90秒窗口，bootstrap原生task_complete已保存但没有ready端点，未发control，tools/call为0。不得将这些证据认定为原嵌套send根因或修复通过。当前累计CLI10/App7/OC9/Pi8；停止付费重复，原矩阵INCOMPLETE与失败项保留。

### 2026-10-12 独立修复额度与App批准等待定位

- Owner「再给一轮修复额度」授权各类独立10条。repair010真实App收到控制但MCP tools/call0；repair011协议及GUI确证无人处理mcpServer/elicitation/request，界面要求允许ra2a list_targets，不能归为网络或动态工具回复丢失。
- 固定官方0.160 schema及本机config/read核准per-tool approval_mode=approve。repair012被诊断账本旧ID碰撞拦住控制，已消耗bootstrap保留；013以明确reconstructed的5条旧消耗延续，不退款、不伪造旧ledger原字节/哈希，零输入fixture验证新UUID、5→7及duplicate拒绝。
- repair013仅测试私有配置两项approve，list_targets/send_message各调用返回一次、批准请求0、App/Pi精确原生final及App GUI回复可见，候选修正专项PASS。当前新轮App7/Pi1/CLI0/OC0，正式认证终点hash不变，资源回收，本次ledger原样持久化。
- 生产生成器尚未修改，4行最小候选已准备；当前阶段900行上限仍遵守，已请求仅904行扩展。完整矩阵和人工项没有据此改判通过，后续按剩余额度验证已授权范围，不自动再开轮。

- Owner「批注，继续验收」确认落地候选并继续，阶段上限扩展至904行。生产生成器已仅增加4行私有RA2A两工具批准配置；codex/app边界回归及恢复相关共7项PASS，其他MCP工具及全局策略不变。完整矩阵仍待后续验收，本轮剩App3/Pi9/CLI10/OC10，不自动重置。

- 落地10cfdf6后repair014唯一有界实测：App GUI BASE与RA2A FOLLOWUP同turn，四次list_targets真实返回、精确combined final/task_complete及GUI可见，忙时PASS；关闭后端点撤回且内核无人持锁，睡眠仅查询未派发。真实UI恢复原ID报会话链接未渲染，恢复probe未发，根因未证（失败DOM/截图/history路径缺证）。本轮App9/Pi1/CLI0/OC0，原账本继承及清理前持久化已证，正式auth/config终点hash不变；恢复/holder交接/完整矩阵/人工项仍未通过，不将专项PASS升级完整PASS。

### 2026-10-13 有界连续修复验收

- Owner授权持续验收、遇问题修复，最多10轮循环；每轮仍每类Agent10条，失败不退款、不重放。新循环最多10轮，明确账本/轮次，不以结束一次进程自动重置。当前准备第1轮；既有修复轮的repair015已使用最后App1条到10/10，单列冻结，不混入新轮。
- GitHub同步：[Issue #3](https://github.com/ceasarXuu/RA2A/issues/3)，主题Linux矩阵测试套件；版本待Owner指定，原PD未将测试设施纳入发版。
- 恢复根因直接证实：原会话在Recents可见，目标ID在DIV role=button/data-app-action-sidebar-thread-id，旧a[href]无匹配。原地替换可见精确ID控件选择器，不增加生产行数，阶段仍904；生产后续改动只有证据门禁后实施。

- 第1轮39dfeb0真实矩阵INCOMPLETE：11/16基础格PASS、2FAIL、3后续未执行；App两工具环边PASS，CLI/Pi busy/resume/ACK-loss PASS。OC UnknownError及App自投递GUI不可见待定位；本轮CLI9/App6/OC2/Pi9，正式保护hash相同且资源回收。使用该轮余量串行诊断App与OC，不新增第2轮或重放。

- 同轮repair016/017补证：App busy再次通过，但bare nativeID选择假设被反驳；实际侧栏属性为local:<nativeID>，静态固定App包编码与真实DOM一致。原地修正前缀、不增生产行数；恢复原生探针与完整矩阵仍待执行。App累计9/10、CLI9/OC2/Pi9，旧失败证据保留。

- 同轮OC唯一专项：收件正常但OAuth刷新错误族导致原生final0；私有凭据投影完整且源已过期，刷新HTTP状态/拒绝方尚未证。不改正式auth、不反复付费重试，已请求Owner本机OpenCode重新登录。累计CLI9/App9/OC3/Pi9；完整矩阵仍INCOMPLETE。

- 1a2bdaf后repair018真实App关闭/重开、侧栏点击恢复同nativeID、历史ACK界面可见均通过；恢复阶段0新投递，不升级业务恢复PASS。第1轮累计CLI9/App10/OC3/Pi9，账本冻结，完整矩阵仍INCOMPLETE。等待Owner更新本机OpenCode OAuth后新轮验收；正式认证配置保护保持，人工项仍在自动阶段之后。

- 第2轮e182827：OC明确认证阻塞且0输入；App/Pi self、工具回信、busy、同IDresume新投递、distinct-LAN丢ACK不重放实证通过，Issue3原会话恢复已关闭。CLI启动未发布端点，holder transfer未运行、全矩阵INCOMPLETE，人工仍待自动阶段完成。
- 同轮CLI有界唯一新标记诊断：start returned/register200/new codex-tui rollout但not_loaded，尚未证明embedded fallback或其他backend归属根因。初始诊断控制器错误/重复bootstrap拒绝均0新模型输入，保留原证据；最新账本CLI2/App8/OC0/Pi7，正式hash相同、资源/私有凭据回收。
- 同轮diag5成功、diag6再次失败并取得错误登记证据：延迟落盘App文件被当成CLI，实际CLI在官方loaded/list；修复以唯一bootstrap原生user文本关联native ID。8项相关零模型测试通过，生产行数不增加；待真实修复回归。当前CLI4/App10/OC0/Pi7，App本轮输入已停止，不重置账本。
- Issue4修复fe98cc8的真实CLI0.162.1专项启动、自投递、busy/followup、关闭恢复同nativeID后的新投递均PASS；迟到App/assistant/substring零模型负例通过。App达到10已停输入，未测holder transfer/LAN/人工，全矩阵仍INCOMPLETE。最新第2轮CLI9/App10/OC0/Pi7，资源和凭据副本回收，正式认证配置hash不变。OpenCode源认证仍过期，等待Owner重新登录。
- 第3轮1213e55：三端9格基础及两个工具环PASS，真实CLI→睡眠→App writer→CLI切换PASS，App/CLI/Pi同IDresume PASS。App FOLLOWUP native final/task_complete PASS而GUI false；截图旧滚动位置不能证明通知丢失。原编排先恢复App再关闭它做CLI交接，导致App ACK-loss发往旧端点、0预算/0native输入超时；原地将App恢复置最后，真实循环零模型红→绿，16项相关测试PASS，真实回归待执行。第3轮CLI8/App8/OC0/Pi8，全矩阵INCOMPLETE、正式hash不变/资源回收。
- [Issue5](https://github.com/ceasarXuu/RA2A/issues/5)编排修复edb936a等待真实CLI交接后App ACK-loss回归；[Issue6](https://github.com/ceasarXuu/RA2A/issues/6)长序列GUI false仍未根治。同轮两输入诊断短会话busy/unknown后唯一native final与精确P段落渲染PASS，无可滚动历史，因此不能反证原失败。账本CLI8/App10/OC0/Pi8冻结App额度。
- GUI判据另有确定假阳性：用户请求含ACK即可让旧substring返回true。原地检查native完整final的精确已渲染段落，combined final不再只检查FOLLOWUP token；真实JS负例红→绿，23项相关测试PASS，生产行数不增加。该修复不取消Issue6原失败，完整新判据回归待执行，人工留最后。

- 第4轮d2154bc真实回归：三端9格basic、两工具环、同ID恢复、CLI/App实际holder切换及三端distinct-LAN丢ACK不重放PASS。Issue5已关闭；App长序列GUI false保留，完整矩阵INCOMPLETE。CLI8/App9/OC0/Pi8，保护/清理通过。
- 第5轮App6输入专项：完整final在视口外，点击一次真实Scroll to bottom按钮后同ID/完整两ACK进入视口；截图独立核验，无后台刷新或模型新增。原检查器把视口外算可见的假阳性已原地修正；现以唯一可见原生导航控件显示回复，下一采样检查精确完整P及视口范围，24相关测试PASS，生产行数不增。Issue6真实修复回归待执行，自动跟随与导航后可见分开，人工仍待。
- Owner已将OpenCode模型路径改为OpenCode Go订阅；本机Go API凭据存在且本地catalog包含kimi-k3，下一轮私有显式选择该模型，CLI/App仍6.1-sol low，Pi现有kimi-k3；正式配置不修改。

- 第6轮631d230冻结真实四端矩阵自动化37/37 PASS：16基础格、4工具回信、四端忙时追加/同ID恢复/真实LAN丢ACK不重放及CLI/App持有者交接。OpenCode私有Go订阅kimi-k3成功；App完整final视口检查及原生导航后的截图实证通过，Issue6具体套件缺陷关闭。预算CLI10/App10/OC9/Pi9；正式8认证配置hash相同、owned/copies清理，源码运行期间未变。人工MANUAL_PENDING，不称整体用户验收全部结束。
- 第7轮人工专用一次性启动器已在本机证据目录准备、未启动；只启动四端并各提交一次人工标记，不重跑矩阵、不挪用第6轮用满的CLI/App额度。复用已有manual流程，零模型委托fixture通过。Owner应在本机交互终端观察真实TUI/专属App窗口，只有实际看到输入/完整回复/输入栏可继续才输入PASS。

### 2026-10-14 本轮收口

- 自动化：第6轮四端37/37 PASS；人工：Owner直接确认全部通过，并确认终端退出。按两类独立证据归档，不用收件ACK/DOM替代人工结论。
- 第7轮脚本自身原始结果NOT_RUN/RuntimeError，CLI1/App1/OC0/Pi0，没有内部人工结果；保留原始失败，不伪造manual标记或改判脚本PASS。Owner人工确认另列；原PID已退出，自有资源引用检查及账本原字节门禁后移除残留私有凭据树，恢复清理证据另存。共启7/10轮，无超额/退款/旧标记重放。
- 监督失败诊断缺口已原地修复：report保存既有safe pid/returncode/timed_out；零模型三路故障边界PASS，runtime5PASS/2SKIP，生产总行数不增加。第7轮历史退出原因UNKNOWN，不据新诊断追认。
- Linux本轮验收收口；Mac/Windows矩阵、真实跨设备网络验收、生产Issue2及版本发布不由此自动计作通过。
