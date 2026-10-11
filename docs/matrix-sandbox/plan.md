# Linux 矩阵沙盒工程设计

- Product Authority：[PRD Confirmed Product Decisions](prd.md#confirmed-product-decisions)。
- Applicable Decisions：PD1、PD2、PD3；Codex 所有权语义引用既有 PD36/PD37。
- 状态：设计 drafted；实现 not-started；有效性 valid-with-qualifications。
- 更新：2026-10-12。基础设施主题独立于版本目录，不自行指定发布版本。

## Execution Contract

产品权威仅限用户直接确认行，Agent 不自批准。当前工程证据可修订方案，不能改变产品权威；新增核心规则列入待决。每个实现阶段前按实际实现 rebase，pending/blocked-on-plan-approval 不执行；重大方向/范围变化记录 Plan Delta 并取得批准。阶段后仅审计本阶段 Product Decision Delta。当前授权是设计，不复制认证、不启动环境、不安装或部署。

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
- 每类Agent可创建第二个来源会话验收同类互通；该类接收总预算不增加（待PD统计桶确认）。
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

不继承host Secret Service/DBus解锁会话作为快捷方式。优先验证文件认证可复用；keyring-only或设备绑定情况做单独Discovery：若无法隔离复用，报告该Agent AUTH_BLOCKED并停止收费用例，不能擅自更改正式store。复制token可能触发账户刷新互斥/失效，需小规模验证并报告；是否写回仍待产品确认。

真实凭据用于真实客户端小规模矩阵；mock fixture保留用于不限真实额度的计数并发/故障回归。日志只白名单采集协议状态、marker、原生事件ID和时间，不导出完整会话thinking、auth、headers或环境块；trace/screenshots可能有敏感内容，人工UI证据按需保存。

## 5. 预算必须在接收端可信入口落实

候选新增test专用运行器 `cmd/ra2a-sandbox`，复用registry/Coordinator/LAN构造（位置重用可行性先Discovery）；正式serve默认行为不加沙盒分支。

所有沙盒接收路径在进入Adapter.Deliver/原生输入前向可信controller预留额度。Agent无账本写权限、不能选择预算桶；桶由启动时登记的harness身份决定。MCP/control、LAN回信、直接sandbox CLI send均须同一接收门禁。原生客户端自身启动提示/人工输入还需按相同账本调度；无法阻止绕过的路径不得开放为自动验收入口。

预留记录至少run_id、bucket、case_id、message_id、状态、时间；不得保存秘密或全文。原子持久预留只提交≤10，计数服务停止/损坏时拒绝收费输入。restart/resume重新关联同桶；已有message-id返回记录状态，不重新写入。写前拒绝与写后unknown分别报告；ACK超时不退款/不重发的建议待确认。

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
- 执行verified（本次无Agent基础探针范围）；D1真实三节点LAN、D2显示/DBus/bwrap原语通过，D2真实App与单实例验收尚not-started，不能整体声明GUI已兼容。预算桶/写回规则确认后才D3。
- 建议阶段生产新增≤250行；主要一次性诊断脚本/报告，不把阶段验证扩成完整部署工具。依赖安装、特权常驻服务或VM变更另行评审。
- 2026-10-12本机实际结果：[复现与证据摘要](../../experiments/sandbox/README.md)。214行一次性探针，无新增依赖；unprivileged namespace可建私有bridge/3个net，真实mDNS+DTLS/CoAP环形投递通过；bwrap隔离HOME/运行目录与私有passwd支持D-Bus，Xephyr单socket/new-cookie下xdpyinfo通过。正式Agent、Auth与模型未运行，收费消息0；原语方向direction-supported，不等同完整App/预算实现。

### Phase B：预算与最小真实矩阵

- Pre-Phase Plan Rebase Gate：pending；必须依据A结果选择一种隔离机制及核已集成适配器。
- 执行not-started；W1先mock验证，再W2/W3真实凭据；建议每个批准实现阶段新增生产≤500行、单文件≤500，超过就先简化并请求批准，不借测试分类隐藏生产运行器。
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
