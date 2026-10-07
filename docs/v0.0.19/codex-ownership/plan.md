# Codex 实际持锁归属修复方案

- 日期：2026-10-08。
- Product Authority：./prd.md#confirmed-product-decisions。
- Applicable Decisions：PD36、PD37。
- 设计基准：aabcf3c；只读 Desktop owner 查询已实现；后续独立 CLI 禁止投递前恢复已实现并隔离验证，完整仲裁未接入 / 未正式部署。
- Plan validity：valid-with-qualifications；跨平台实际持锁 PID 读取与 App 冷加载竞态已证存在、末端替代接口仍需验证。
- 证据主线：coe/2026-10-08-issue-2-unpublished-codex-session.md。

## Execution Contract

上述产品确认项是本主题唯一有效产品权威；修改须 Owner 明确确认，Agent 不得自批。工程证据可修订本方案，不能改变持锁归属 / 睡眠规则。每阶段前对当前实现和剩余工作 rebase，gate 为 pending / blocked-on-plan-approval 时不得开始依赖实施；重大方向 / 范围 / 副作用变化记录 Plan Delta 并取得批准。每阶段结束仅审计该阶段 Product Decision Delta；新产品选择需明确确认。当前不存在待确认的 CLI/App 固定优先级。

## 已确认事实与设计

1. 官方 0.160/0.161 使用 CODEX_HOME/thread-writer-locks/<ID>.lock 的跨进程排他锁。空文件没有宿主/PID元数据；正常释放关闭并移除，异常退出可能留下文件。
2. Linux 用 stat(device/inode) + /proc/locks 的 FLOCK WRITE PID +进程创建身份可只读获得实际持有者。原目标真实持有 PID888157，是 App 内置 app-server；睡眠原会话对照无锁，Desktop discovery 也没有 owner。隔离原语实验验证：额外打开不改变 holder，释放后残文件不是持有。
3. Darwin F_GETLK 对 flock 返回 PID=-1；FHASLOCK 是 sticky 的“曾锁定”。Windows LockFileEx 没有标准直接返回 holder PID 的字段。两平台必须单独验证等价 reader，不能拿打开文件者替代持锁者。
4. CLI 官方 turn/start 与 turn/steer 只读内存 thread_manager.get_thread，缺失即 ThreadNotFound；同步收件 response 不依赖 resume/subscription。去掉现有投递前 thread/resume，避免在睡眠或迁移窗口创建 writer。
5. Desktop discovery 只是 UI owner 路由证据；必须和实际持锁 backend 绑定。已安装 App follower start/steer 在多次 await 后可能经 Zb→resumeConversationForUnavailableOwner 冷恢复；pin targetClientId 不冻结 writer，JSON IPC 未提供 loadedOnly/expectedWriter 条件。因此当前 App IPC 不满足严格禁止唤醒门禁，不能直接集成此路径并声称原子安全。

### 归属对象

最小只读快照包含 CODEX_HOME、session ID、锁 device/inode、持锁 PID、进程创建身份、宿主种类及已核 socket / Desktop owner。快照代号由这些身份组合产生，不建立新的持久所有权数据库。cliSessions 保留为兼容候选提示，取消其永久排他作用；source/originator 和历史只供展示。

### 动态时序

- 本地发现：轻量周期刷新锁快照；文件事件仅用于触发提前刷新，因为原 inode 上的解锁/重锁未必有文件变更事件。暂定本地刷新目标不超过 1 秒，实际可达延迟由测试量化。
- 每次本地 / 远端发现请求复核当前状态；健康宿主不等于所有历史 session 可达。远端旧缓存仅供展示，不能授权发送。
- 每次发送：无条件重新解析真实持锁者；选择已核 backend / owner，再在写入入口复核锁和进程身份。
- 持有者改变且尚未写入：撤回旧快照，重新只读解析一次；稳定新 owner 可继续。确认无人持有返回睡眠；查询不确定返回未确定，均不通过 resume 获锁。
- 请求进入宿主后：不跨宿主重试。只有官方明确接收才 accepted；断线/超时保持 unknown。不能把前后两次锁读取描述为跨进程原子操作，原生调用自身必须保证只操作已有持锁 thread。
- 同 PID reuse、inode替换、server重启都使旧代号失效；原 RA2A 地址不变。

## 工作单元

| ID | Change Location / Target | Concrete Action | Resulting Behavior / Benefit | Side Effects | Verification / Safe Stop |
|---|---|---|---|---|---|
| W1 | writer锁 reader（位置在取证后确定） | 各平台验证只读 actual holder 和创建身份 | 将真实锁作为归属依据 | 小型平台适配，需权限边界与读取成本验证；不加服务/依赖 | 临时独占锁：获得→释放→残文件→另PID接手；未知PID和权限失败明确失败；不触碰正式锁 |
| W2 | cmd/ra2a/registry.go、codexcli/codexapp发布 | 用同一实时快照协调两适配器，撤销静态排他 | 消除旧登记导致双重排除，动态迁移不需restart | 影响发现/路由；只增加最小共享协调对象，保留原地址 | CLI/App/睡眠/切换/同名不同ID；其他Agent不变；无法匹配actual backend时停，不猜 |
| W3 | codexcli/delivery.go、desktopipc及App bridge | 投递复核，去cold resume；pin已证App owner | 不创建竞争writer、不向旧owner写入 | 调整现有写入前置条件；不用新队列/持久租约 | 在解析前、解析后、写入前后精确插入迁移；未写入更新、已写未知不重放；App coldload gate未证就不得集成 |
| W4 | 本地发现刷新与诊断 | 快照刷新及仅状态变化日志 | 及时展示迁移、定位sleep/unknown | 暂定1秒采样成本；不逐调用刷屏、不记录凭据 | 量化切换延迟，连续多次切换无错误宿主/重复消息；发现缓存不影响投递复核 |
| W5 | 原生隔离fixture及正式RA2A部署 | 验证后仅部署RA2A | 原目标无需手工归属命令、CLI/App继续正常使用 | 部署与服务重启必须有备份/进程保护 | 三平台原生、同ID CLI→App→CLI→sleep；M2→Ubuntu原链；失败只回滚RA2A产物，不操作官方会话 |

## 分阶段验证与 gates

### Phase A：关键接口验证（W1、W3发现项）

- Pre-Phase Plan Rebase Gate：ready；现实现=aabcf3c，根治未接入；Material plan delta=none（首次按Owner新规则设计）；User approval=not-required（已授权根治及明确持锁/动态规则）。
- 最小投入：Linux actual holder 已证；补平台读 API 与 native existing-thread 投递时序，禁止修改正式配置/宿主或获取正式锁。跨平台 holder 不能证明就标 blocked-on-discovery，不允许包装为通过。
- Tracking：Linux锁与CLI末端已证；Mac/Windows实际holder和App安全末端 blocked-on-discovery。不得部署返回unknown的占位reader来破坏当前正常使用。

### Phase B：最小实现（W2、W3、W4）

- Pre-Phase Plan Rebase Gate：pending；进入前以 A 的真实接口 / 代码预算重新核对，重大变化才走批准门禁。
- 预算：本批准根治阶段新增手写生产总代码≤500行，IPC基础已用保守87行；CLI独立子项新增2行注释、删除旧恢复/订阅流程，累计新增保守89行；所有单源文件≤500。优先删静态排除/冷resume，再改现有路径。若跨平台方案需新代理/完整lease系统或超过预算，先停止扩张并提出最小替代。
- Tracking：完整集成 not-started；CLI独立子项 completed；不让Linux实现的成功替代Mac/Windows适用性。
- 可独立先行项：W3 的 CLI 删除投递前 resume/unsubscribe，固定原生源码证明已有内存 thread 足以同步收件；此子项独立 rebase 为 ready，未新增产品决策或扩大范围，不依赖未确定的平台 reader 或 App 末端。验证已覆盖已有线程收件、睡眠线程不唤醒、读后卸载不重放；internal/codexcli -race通过，真实0.161.0隔离原生22收件/owner继续/活跃收件/恢复/CoAP丢ACK不重放通过。其他 W2/W3/W4 集成仍 pending。

### Phase C：原生与正式现场验收（W5）

- Pre-Phase Plan Rebase Gate：pending；只在实现风险匹配回归和原生门禁通过后进入。
- Tracking：not-started；当前App原地址已单次恢复不代表动态规则已runtime verified。
- 保留配置/auth/launcher/proxy及原CLI/App/daemon关键身份基线；人工正常关闭/打开/resume留在自动验证之后。

## Product Decision Delta

| Phase | Decision Surface | Observed Semantics | Authority Coverage | Classification | Required Action |
|---|---|---|---|---|---|
| 已完成只读IPC基础 | 查询与显式定向，尚未路由 | 不选固定宿主、不启动任务 | PD36/37前置技术能力 | engineering-only | 保留回归，等待实际锁接入 |
| A | 锁证据/现有API契约 | 读取正式原目标和隔离锁，无正式写入 | PD36 | covered | 跨平台holder/App安全末端仍未证 |
| B独立CLI子项 | 禁止投递前恢复 | 已有线程直接收件，卸载不恢复，无订阅变化 | PD36/37 | covered | 代码和隔离测试完成；不等同完整仲裁通过 |

## Pending Product Decisions

无待确认固定优先级；Owner 已明确以实际锁为准。工程未决：Mac/Windows reader、App handler 已证冷resume分支的安全替代、跨进程最终写入前置条件、刷新延迟实测。这些不是改用静态宿主优先级的授权。


## 技术接口限制（2026-10-08 取证补充）

- Darwin 固定 XNU f6217f8 的 F_FLOCK 以 fileglob 持有、lf_owner=NULL，F_GETLK 返回 PID=-1；现代 lsof 1ebf257 的 Darwin 路径仅枚举 vnode/openflags，没有实际锁owner读取。文件打开者或曾锁位不能满足本契约。
- Windows LockFileEx 没有owner返回；FileProcessIdsUsingFileInformation/RmGetList枚举文件使用者，不能证明byte-range实际持锁者；FILE_LOCK_INFO虽然有ProcessId却属于内核system-use，不是已证可部署用户态reader。
- App backend现有匿名stdio由App独占协议流，没有已证外部可另接入原生已有thread-only投递入口；禁止通过复制其FD注入/争读响应。当前外部IPC仍带UI恢复分支。
- ROG实际Appx26.1002.7124.0/asar26.1002.52244/backend0.162.0-alpha.2的只读核验也证实Yv在follower no-client-found后调用resumeConversationForUnavailableOwner，最终thread/resume。targetClientId仍仅固定IPC client。完整schema未取得，不能据此宣称所有可能接口不存在；已检查入口不具备已证禁止恢复保障，Mac独立新版本核验尚待返回。
- 因此当前无法声明三平台严格契约已可实现。下一技术选项必须先证明官方owner接口/禁止恢复入口或受批准的宿主协作机制；不以驱动、句柄劫持或静态优先级绕过限制。
