# Windows Codex Desktop IPC 修复交接

## 目标

在 Windows rog306 上完成 RA2A 对 Codex Desktop-owned session 的消息注入：目标 thread 存在 active writer 时，不启动第二个 writer，而是连接 Desktop IPC owner 创建新 turn。

当前基线：

```text
commit: 15b47d819aac42e189522e61475d9282501fb367
tag: v0.0.5
target: ra2a://rog306/019f43ef-d5a0-7910-bd34-c5c825d1e94a
```

原始 Chain-of-Evidence 现场记录保存在开发者本地且被仓库忽略的 `/coe` 目录；本 runbook 记录可转交、可复现的结论和验收要求。

## 已确认根因

[`internal/desktopipc/dial.go`](../internal/desktopipc/dial.go) 在 Windows 上无条件返回：

```text
Windows named-pipe dialing is not part of the macOS experiment
```

网络、发现、PIN、DTLS、CoAP 和寻址均已排除：请求已经到达 rog306，并取得 active-writer 业务响应。

OpenAI Codex 源码确认 Windows Desktop IPC 路径和帧格式：

- pipe：`\\.\pipe\codex-ipc`
- frame：4 字节 little-endian payload 长度 + JSON payload
- 来源：<https://github.com/openai/codex/blob/main/codex-rs/tui/src/ide_context/ipc.rs>

RA2A 的 [`internal/desktopipc/client.go`](../internal/desktopipc/client.go) 已实现相同 framing、initialize 和 `thread-follower-start-turn`；macOS 已通过实机验证。Windows 尚未证明 follower 方法版本和参数完全兼容。

## 最小实现方向

1. 先写 Windows 失败测试，再写生产代码。
2. 用 build tags 拆分平台 transport，保持 `client.go` 不分叉：
   - `dial_unix.go`：现有 Unix socket 实现。
   - `dial_windows.go`：Windows named-pipe 实现。
   - 公共 `DialContext` 只负责候选地址与错误聚合。
3. 优先使用成熟的 `github.com/Microsoft/go-winio`（当前最新可见版本 `v0.6.2`）及其 context-aware named-pipe dial，避免自行封装 Win32 overlapped I/O 和 deadline。
4. Windows transport 必须返回真正支持 `SetDeadline` 的 `net.Conn`，因为 `client.call` 依靠 connection deadline 限制 initialize/StartTurn。
5. 不增加消息队列，不在结果未知后重试。

在 Windows 实机核对 `go-winio` 当前 API 后再 pin 依赖；不要凭 Mac 交叉编译结果宣称完成。

## 后续修正：Desktop owner 必须优先

后续实机发现，仅在 managed App Server 返回 active writer 后才回退 Desktop IPC 并不充分：目标空闲时 managed 路径可能直接成功，turn 虽然完整落盘，但当前 Desktop renderer 没有参与 `turn/start`，切换 task 也不会重新水合，必须重启 App 才能看到。

发送顺序必须是：

1. 有 Desktop 集成时先调用 `thread-follower-steer-turn`（version 1）。若 thread 已有 active turn，消息必须追加到该 turn，不能再调用 `start-turn`。
2. 只有 Desktop 明确返回 `NoActiveTurn`、`SteerTurnInactiveError` 或等价的“active turn already ended”拒绝时，才调用 `thread-follower-start-turn`（version 2）。
3. 连接或 initialize 失败、Desktop 明确确认请求未投递时，才允许回退 managed App Server。
4. SteerTurn 或 StartTurn 帧写出后，断连、超时、取消或成功响应缺少 turn ID 都属于 `DELIVERY_UNKNOWN`，不得切换方法、回退或重试。
5. 真实 UI 验收必须检查同一进程内出现 `IpcRouter`、目标 `turn/start` 或 `turn/steer` 和 renderer/完成通知，不能仅以 rollout 或后台 `read_thread` 存在 turn 代替。

### Desktop 文本输入契约

`thread-follower-start-turn` 和 `thread-follower-steer-turn` 的每个文本 input 必须显式包含非 `null` 的 `text_elements` 数组：

```json
{"type":"text","text":"...","text_elements":[]}
```

Codex Desktop release 26.901.20858 的 `LocalConversationTurn` 会直接读取 `text_elements.length`。省略该字段会使后台 turn 正常执行，但 UI 报 `Cannot read properties of undefined (reading 'length')`，随后可上浮到 `ThreadSummaryPanel` 和 `AppRoutes`。单元测试必须同时校验 start 与 steer 序列化为空数组，不能只校验 `type` 和 `text`。

### active turn UI 卡死回归（v0.0.9）

v0.0.9 的 owner-first 实现对每条消息都调用 `start-turn`。当同一 session 正在执行时，后续消息虽被后端合并进原 turn，但 Desktop renderer 已预建了另一个 in-progress turn，随后持续出现 `Item not found in turn state`，UI 会停在“思考中”直到重启 App。

修复后的 Windows 实机样本：

```text
thread: 01a05d7e-b958-7df2-bd67-6eba9636fad6
turn: 01a05f67-c2b8-72e0-b9c3-bc8b2299af18
base marker: RA2A-STEER-FIX-BASE-20260902-0755
follow-up marker: RA2A-STEER-FIX-FOLLOWUP-20260902-0755
Desktop log: turn/start=1, turn/steer=1, Item not found in turn state=0
rollout: task_started=1, user_message=2, task_complete=1
reply: RA2A_STEER_FOLLOWUP_OK
```

该样本证明空闲消息只创建一个 turn，执行期间的后续消息进入同一 turn，并由同一 renderer 正常完成。UI 肉眼动态刷新仍需在目标 Desktop 页面现场确认，不能由日志替代。

Windows 实机验证样本：

```text
marker: RA2A-DESKTOP-FIRST-LAN-20260902-0620
turn: 01a05f04-34b3-77e3-a0a4-42b24d6f5040
result: IpcRouter -> turn/start -> turn-complete
reply: RA2A_DESKTOP_FIRST_LAN_OK
```

## 测试顺序

### 1. Windows named-pipe transport 测试

- 使用临时唯一 pipe 名，避免占用真实 `codex-ipc`。
- fake pipe server 接受连接，验证双向读写、context 取消、deadline 和 close。
- 测试无 server、超时和 server 提前关闭的错误。

### 2. Desktop IPC 客户端集成测试

fake server 按现有 4 字节 framing：

1. 接收 `initialize`，返回带 `clientId` 的成功响应。
2. 接收 `thread-follower-steer-turn`，校验 conversation/thread/message ID、正文和 restore message。
3. active turn 成功时返回 turn id，并确认没有额外 `start-turn`。
4. idle turn 必须先收到明确 inactive 拒绝，再接收 `thread-follower-start-turn`。
5. steer 写出后断连或超时时必须返回 `DELIVERY_UNKNOWN`，不能尝试 `start-turn`。
6. 覆盖 `client-discovery-request` 插入在响应之前的情况。

### 3. 真实 Codex Desktop probe

前提：rog306 上 Codex Desktop 已打开 R8。

1. 确认 pipe 存在并可由当前用户连接：`\\.\pipe\codex-ipc`。
2. 运行 `desktop-ipc-probe` 或等价最小 probe 完成 initialize。
3. 向空闲的专用测试 thread 发送第一条消息，记录 request ID、client ID、turn ID，并确认只调用一次 StartTurn。
4. 在该 turn 执行期间发送第二条消息，确认调用 SteerTurn 且两条消息属于同一个 turn。
5. 验证 Desktop UI 实时显示两条消息、执行动态和完成状态，不需要重启 App。
6. 验证用户可在同一 session 继续手动输入，不出现“已在另一个应用中打开”。

真实 R8 在最终验证前不要反复注入，以免制造重复任务。

### 4. 双机验收

从 Mac 向以下地址仅发送一条带唯一 handoff ID 的短测试消息：

```text
ra2a://rog306/019f43ef-d5a0-7910-bd34-c5c825d1e94a
```

验收必须同时满足：

- Mac `send_message` 返回 accepted。
- Windows R8 出现且执行该消息。
- Windows 用户随后可在 R8 手动继续沟通。
- daemon 日志没有第二 writer、重复 turn 或 `DELIVERY_UNKNOWN`。

## 回归门禁

```text
go test ./...
go vet ./...
go test -race ./internal/desktopipc ./cmd/ra2a
```

并验证：

- Windows amd64/arm64 原生构建和实机测试。
- macOS Desktop IPC 现有测试与实机行为不回归。
- Linux 构建不引入 Windows-only 依赖代码。
- 单个手写生产文件不超过 500 行，本阶段新增生产代码不超过 500 行。

## 不要做

- 不要照搬 Paseo 作为 Desktop 注入方案；Paseo 启动并拥有自己的 Codex App Server。
- 不要用第二个 App Server 抢占 Desktop thread writer。
- 不要编辑 rollout/session 文件伪造用户消息。
- 不要在 StartTurn 写出后超时自动重试。
- 不要只凭交叉编译或 fake pipe 测试发布；必须有 rog306 + 官方 Codex Desktop 实机证据。
