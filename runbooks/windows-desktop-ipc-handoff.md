# Windows Codex Desktop IPC 修复交接

## 目标

在 Windows rog306 上完成 RA2A 对 Codex Desktop-owned session 的消息注入：目标 thread 存在 active writer 时，不启动第二个 writer，而是连接 Desktop IPC owner 创建新 turn。

当前基线：

```text
commit: 15b47d819aac42e189522e61475d9282501fb367
tag: v0.0.5
target: ra2a://rog306/019f43ef-d5a0-7910-bd34-c5c825d1e94a
```

完整证据见 [`coe/2026-09-02-00-49-windows-desktop-ipc.md`](../coe/2026-09-02-00-49-windows-desktop-ipc.md)。

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
5. 不修改 active-writer 判定、不增加消息队列、不在结果未知后重试。

在 Windows 实机核对 `go-winio` 当前 API 后再 pin 依赖；不要凭 Mac 交叉编译结果宣称完成。

## 测试顺序

### 1. Windows named-pipe transport 测试

- 使用临时唯一 pipe 名，避免占用真实 `codex-ipc`。
- fake pipe server 接受连接，验证双向读写、context 取消、deadline 和 close。
- 测试无 server、超时和 server 提前关闭的错误。

### 2. Desktop IPC 客户端集成测试

fake server 按现有 4 字节 framing：

1. 接收 `initialize`，返回带 `clientId` 的成功响应。
2. 接收 `thread-follower-start-turn`，校验 conversation/thread/message ID 和正文。
3. 返回带 turn id 的成功响应。
4. 覆盖 `client-discovery-request` 插入在响应之前的情况。

### 3. 真实 Codex Desktop probe

前提：rog306 上 Codex Desktop 已打开 R8。

1. 确认 pipe 存在并可由当前用户连接：`\\.\pipe\codex-ipc`。
2. 运行 `desktop-ipc-probe` 或等价最小 probe 完成 initialize。
3. 向专用测试 thread 调用 StartTurn，记录 request ID、client ID、turn ID。
4. 验证 Desktop UI 实时显示消息。
5. 验证用户可在同一 session 继续手动输入，不出现“已在另一个应用中打开”。

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
