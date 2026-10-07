# v0.0.19 新增 issue 排查与修复验收

- 日期：2026-10-08。
- 范围：Owner 要求拉取最新两个 issue 排查并加入 v0.0.19 修复目标；本阶段为诊断与目标登记，无生产代码修复。
- 基准：正式版 `v0.0.18`；远端 main `e891277`。当前原 checkout 为 `ffb52fd`，与远端分叉，不能用于判断正式版行为。本记录基于独立 detached worktree，未改原分支绑定。
- GitHub：[v0.0.19 milestone](https://github.com/ceasarXuu/RA2A/milestone/1)。

## Issue #1：双网卡 DTLS

[Issue #1](https://github.com/ceasarXuu/RA2A/issues/1) 与 [证据案例](../../coe/2026-10-08-issue-1-multihomed-dtls.md)。

**结论：源地址丢失机制得到 issue 实测与正式版依赖源码的相互支持；尚未修复。**

`internal/lannode/node.go` 的 listener 绑定 `0.0.0.0:0`。go-coap v3.5.4 `net/dtlslistener.go` 调用 Pion DTLS v3.1.8 listener，后者使用自己的 `internal/net/udp/packet_conn.go`：接收只调用 `ReadFrom`，不保存本地目标 IP；发送在原 wildcard socket 上调用 `WriteTo`。客户端使用 connected UDP，其对端是请求目标地址，因此另一地址的回包不满足对端匹配。该路径解释 issue 探针请求 Wi-Fi 地址却收到 Ethernet 地址回复的结果；延长超时不改变此机制。

修复候选优先验证按本地 IPv4 分别绑定 listener、共用服务端口，并仅广告成功监听地址。必须管理地址增删、接口重连、睡眠唤醒及 loopback，不能仅在启动时固定地址列表。wildcard + packet-info 需要自定义接收/发送及连接归属；Pion 仅设置 socket option 无法保留当前路径丢失的元数据。x/net v0.49.0 的 Windows IPv4 ControlMessage 读写未实现，不能作为三平台统一实现。

验收：

- 每个发布地址的 ClientHello 回复源 IP 等于请求目标 IP；使用 issue 中不含 PIN 的只读探针。
- 同一子网双网卡的每个地址均通过 DTLS + CoAP sessions 查询；保留 DTLS 对端校验。
- 单网卡、loopback 无回归；地址变化、接口重连和唤醒后 listener 与 mDNS 广告一致。
- 在指定且经授权的会话间双向投递，确认目标收到与返回结果；仅 `accepted` 不足以通过。
- macOS、Linux、Windows 均验证；不以关闭网卡、关闭 DTLS 或接受任意回包源地址代替修复。

## Issue #2：会话未发布

[Issue #2](https://github.com/ceasarXuu/RA2A/issues/2) 与 [证据案例](../../coe/2026-10-08-issue-2-unpublished-codex-session.md)。

**结论：故障存在已确认，目标缺失根因尚未确认；缺失目标被包装为投递未知的次级问题已定位。**

issue 的节点 ready、非 stale、319 个 session 不含目标，与 Ubuntu 返回业务错误共同表明网络已往返，不能归入 #1。当前 issue 已匿名化原目标 ID；不能凭同名或较新的会话替代目标，也不能推断会话关闭或用户 ID 错误。

正式版与远端 main 的枚举路径相同：`internal/appserverprobe/client.go` 设置 `archived=false`、`useStateDbOnly=true`、sourceKinds 并分页。`internal/codexapp/adapter.go` 排除已由 CLI 接受注册的 thread ID；`cmd/ra2a/registry.go` 的 `buildRegistry` 把 CLI 所有权传给 App adapter。CLI 未发布时不能据此由 App 接管，否则违反 writer 所有权边界。managed App Server 默认继承 daemon 环境，故需对比实际用户和有效 CODEX_HOME。

`internal/agentbridge/registry.go` 的 Lookup 重新枚举，忽略枚举 problems 后可返回 not found。`cmd/ra2a/registry.go` 的 `deliverOverLAN` 仅区分 delivered / start_required，其余结果统一包装为 `ErrDeliveryUnknown`。因此未找到目标、尚未调用宿主也可报投递未知。这是独立于枚举缺失的分类问题，不能用分类修复冒充主故障修复。

下一轮 Ubuntu 只读诊断：

1. 核对原目标真实 ID、宿主类型、daemon 版本/用户/二进制与有效 CODEX_HOME；仅采集必要字段，不输出 PIN、认证或完整环境。
2. 同一时点比较原始 thread/list 全分页、只读 thread/read 与 registry 发布列表。对 sourceKinds / archived / useStateDbOnly 做单变量对照；不要执行 resume/start 或接管 writer。
3. 若原始列表含目标，检查 CLI 所有权排除、CLI 发布状态、endpoint 校验和枚举 problems；若原始列表不含目标，继续区分数据根目录、状态库与过滤问题。
4. 本 issue 未提供可安全定位的原目标 ID，以上现场证据仍待补齐。本轮不向其他 session 发送诊断消息。

验收：

- 原目标在本机和远端 list_targets 出现，可直接使用返回地址投递，目标收到并能返回。
- 不创建替代会话、不误投其他会话、不引入竞争 writer。
- 缺失目标明确返回 not_found；真实已写入但结果不确定仍保持 unknown，不能盲目重试。
- 枚举失败与真正无目标可区分；过滤、分页与 App/CLI 所有权无回归。

## 本阶段验证

缺失端点诊断测试通过；已有分页、registry 路由和 CLI 所有权过滤测试通过。精确诊断结果见对应 coe 案例。这些测试不证明 Ubuntu 原目标已发布，也不替代双网卡实机验收。本阶段未安装测试二进制、修改节点配置、重启正式服务或实施生产修复。GitHub issue 保持 open；后续补齐现场证据及修复验证后才能关闭。

## 2026-10-08 实现推进

Owner 已授权先修问题，本轮在 Ubuntu main 实施；初始工作区 clean，已快进到 180afda。新增手写生产代码合计不到 200 行，单源文件均低于 500 行。

- #1：逐 IPv4 绑定同端口、5 秒同步地址、只广告成功监听地址，绑定失败继续重试。Linux 两个 loopback 地址真实 DTLS/CoAP 和地址生命周期测试通过；Darwin/Windows 仅交叉构建，双网卡实机/唤醒/双向业务仍待验收，issue 不关闭。
- #2：确定缺失通过 LAN 返回 TARGET_NOT_FOUND；枚举失败保留 enumeration_failed/unknown，不重放未知投递。四包 race 回归通过。
- 当前开发会话未发布已定位到旧 CLI 登记与现 App 宿主冲突；匿名 issue 与该 ID 的关联需 Owner 确认后再使用既有 release-cli 显式转交。尚未操作正式配置、服务或官方宿主，不能宣称原目标已修复。

详细证据增补到对应 coe 案例；历史诊断结论作为当时状态保留。本轮未发版。

## Issue #2 原目标恢复（2026-10-08）

Owner 明确原始来源为 MacMini M2，原目标为当前 `ra2a://ubuntu407/01a0f6ff-b902-7970-acba-ef8d3451c6f7`。旧 CLI 登记与现 App 宿主冲突确认为该目标根因。备份后执行既有 release-cli，只重启 RA2A；原 ID 现发布为 codex-app，单次收件标记已真实进入原 App 会话。官方 CLI/App/daemon 进程和 config/auth/launcher 保持，RA2A 配置只改变 cliSessions，PIN/节点身份保留；二进制未升级。

M2 当前不可达，原始跨设备链复验待其恢复在线。issue 保持 open，不用本机自投递替代 M2 验收。保护与原始收件证据见 coe E-006。
