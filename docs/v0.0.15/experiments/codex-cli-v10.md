# Codex CLI V10 官方 daemon 自挂接与投递契约验证

- 状态：核心结论通过（0.158.0，Ubuntu native）
- 执行日期：2026-09-28
- 执行阶段：Phase 0（替代 V9 的路线判定，并重定向 V8）
- 平台：Ubuntu 24.04（x86_64），宿主机 zhangxu-ubuntu
- Codex CLI：`0.158.0`（上游 stable，当日发布；tag `rust-v0.158.0`，commit `064c6b8c`）
- 对照版本：RA2A 既有实验为 `0.151.0` / `0.152.1` / `0.153.4` / 本机正式版 `0.155.1`
- 关联决策：PD29、PD30、PD31、PD32
- 前置：V9（`codex-cli-v9.md`）判定官方 daemon 自动挂接在 `0.153.4` 未生效，改用 wrapper 代传 `--remote`

## 1. 为什么做 V10

V9 的结论建立在 `0.153.4`：源码里存在 `maybe_probe_default_daemon_socket`，但发布版普通 `codex` TUI 实测未挂接官方 daemon，因此 Owner 选择了 wrapper 路线。上游在 `0.158.0` 已把该机制提升为默认开启的稳定特性，且新增了机器可读的 daemon 生命周期命令。V10 重新判定路线，目标是回答三个问题：

1. 普通用户零动作执行 `codex`，是否真的挂接到共享 app-server daemon，RA2A 能否作为第二个客户端接入同一 socket。
2. 真正的投递入口是 `thread/queue/*` 还是 `turn/start` / `turn/steer`，各自的前置条件能否在写入前探测。
3. 协议层是否存在可用的 thread 所有权判别字段（V6 的未解问题）。

## 2. 隔离与合规（PD32）

本机同时运行正式版 RA2A daemon（pid 2206，管理 `~/.codex/app-server-control/app-server-control.sock.ra2a-2206.sock`）与正式版 codex app-server，因此全部实验在完全隔离的环境内进行：

| 隔离项 | 取值 |
| --- | --- |
| `CODEX_HOME` | `<repo>/.cache/v10/home`（`.cache/` 已在 `.gitignore`） |
| codex 二进制 | `npm pack @openai/codex@0.158.0-linux-x64` 解包到 `.cache/v10/pkg`，不覆盖 `~/.local/bin/codex` |
| daemon socket | `.cache/v10/home/app-server-control/app-server-control.sock` |
| 真实 socket 目标 | `/tmp/codex-daemon-1000/<sha256>`（0700 目录 + 0600 socket，仅同 uid 可连） |
| 认证 | 无。未复制、未读取正式版 `~/.codex/auth.json`；账号用量无法在本环境核对（见 §7） |
| 未触碰 | 正式版 RA2A 进程、正式 socket、正式 `~/.codex`、正式 codex 进程 |

实验前关闭了 daemon 自动更新（`app-server-daemon/settings.json` 的 `updater.autoUpdateEnabled=false`），结束时 `codex app-server daemon stop` 并确认无残留进程与 socket。事后核对：正式 RA2A daemon 与其 socket 时间戳未变。

## 3. 结论摘要

| 验证项 | 0.158.0 实测结论 | 对计划的约束 |
| --- | --- | --- |
| 零动作挂接 | **通过**。daemon 不存在时，普通 `codex` 启动后约 5 秒内自行拉起 daemon 并建 socket，TUI 持有该 socket 的 ESTABLISHED 连接；daemon 已存在时直接接入 | wrapper 降级为兜底；PD29 的 `start_required` 由官方生命周期命令判定 |
| 第三方接入方式 | daemon 控制 socket 是 **AF_UNIX 上的 WebSocket**，同 uid 任意进程可作为普通 app-server 客户端接入，无 token/白名单 | RA2A 直接作为第二客户端，无需注入用户进程 |
| 投递入口 | `turn/start` / `turn/steer` / `thread/resume` 均为 stable；`thread/queue/*` 全部 experimental 且要求 thread 已 loaded | 投递不再需要 `experimentalApi`；`thread/queue/*` 退出主路径 |
| 投递确认 | **高风险已复现**：`turn/start` 先返回 turn ID（`status: inProgress`、`error: null`），失败在异步通知里才出现 | 必须等待 `turn/completed` 并检查 `turn.status`/`turn.error`，否则重演 Desktop 空 model 竞态 |
| 写入前能力探测 | `thread.canAcceptDirectInput` 可探测（需 `experimentalApi`），实测 `true` | 写入前门禁可用 |
| thread 所有权 | **协议仍不暴露**。`Thread.source` 恒为 `vscode`；`Thread.originator` 是 daemon 进程级全局值、first-writer-wins | `originator` 不可用作判别；必须由 RA2A 自建登记 |
| 多订阅者 | `turn/start` 的状态变化会广播给所有 `thread/resume` 订阅者 | TUI 能实时看到注入；同时需处理审批 fan-out 与退订时序 |

## 4. daemon 生命周期（机器可读）

`codex app-server daemon <cmd>` 每个命令向 stdout 输出**恰好一个 JSON 对象**。

```text
# 未启动
$ codex app-server daemon version
Error: failed to connect to <CODEX_HOME>/app-server-control/app-server-control.sock

# 启动（首次会自行把 CLI 包安装进隔离 CODEX_HOME）
$ codex app-server daemon start
{"status":"started","backend":"pid","pid":1852293,
 "managedCodexPath":"<CODEX_HOME>/packages/app-server-daemon/current/bin/codex",
 "managedCodexVersion":"0.158.0",
 "socketPath":"<CODEX_HOME>/app-server-control/app-server-control.sock",
 "cliVersion":"0.158.0","appServerVersion":"0.158.0"}

# 运行中
$ codex app-server daemon version
{"status":"running","backend":"pid", ... ,"appServerVersion":"0.158.0"}

# 停止
$ codex app-server daemon stop
{"status":"stopped","backend":"pid", ...}
```

要点：

- `version` 是**探测**语义：daemon 不在时命令失败并给出连接错误，不返回 JSON。RA2A 用它区分 `start_required` 与可用。
- `status` 取值实测：`running` / `started` / `stopped`（源码另有 `restarted` / `notRunning` / `alreadyRunning`）。
- socket 路径固定为 `$CODEX_HOME/app-server-control/app-server-control.sock`，符号链接到 `/tmp/codex-daemon-<uid>/<hash>`（规避 AF_UNIX 108 字节上限）。`stat` 实测：真实 socket `srw-------`、父目录 `drwx------`。
- 停止后符号链接被删除，仅残留 `app-server-startup.lock`。

## 5. 传输与握手：不是 JSONL，是 WebSocket

三次失败尝试后才确认真实传输，这是 RA2A 实现前必须知道的事实：

| 尝试 | 结果 |
| --- | --- |
| 向 socket 直接写 JSONL（raw） | 首次 `sendall` 即 `BrokenPipeError`，服务端立即关闭 |
| `codex app-server proxy --sock <path>` + JSONL | 无任何输出。`codex-rs/stdio-to-uds/src/lib.rs:12-45` 是纯字节中继，不做握手，因此调用方仍需自己说 WebSocket |
| 自写最小 WebSocket 客户端 over AF_UNIX | **成功**，`HTTP/1.1 101 Switching Protocols` |

握手请求与响应（路径 `/`，`Host: localhost`，`Sec-WebSocket-Version: 13`）：

```text
-> GET / HTTP/1.1 / Host: localhost / Upgrade: websocket / Connection: Upgrade
   / Sec-WebSocket-Key: <b64> / Sec-WebSocket-Version: 13
<- HTTP/1.1 101 Switching Protocols
```

`initialize` 响应（节选）：

```json
{"userAgent":"ra2a-v10-probe/0.158.0 (Ubuntu 24.4.0; x86_64) ... (ra2a-v10-probe; 0.0.0)",
 "codexHome":"<CODEX_HOME>","platformFamily":"unix","platformOs":"linux"}
```

- `userAgent` 仍包含可解析版本号，V5 的版本探测口径继续有效。
- `codexHome` 回显，可用于校验连接的是不是目标隔离环境。
- 连接后会先收到 `configWarning`（本机为 bubblewrap/user namespace 提示）与 `remoteControl/status/changed`（`status: disabled`）。
- **`initialize` 不返回协议版本号**（`0.151.0` / `0.152.1` / `0.158.0` 的 `InitializeResponse` 字段完全一致），版本门槛只能靠 daemon JSON 或实测探测。

## 6. 投递契约（替代原 V8 的 queue 目标）

### 6.1 方法存在性与线程 ID 格式

以下方法在 `0.158.0` 全部存在（非 `method not found`）：`thread/loaded/list`、`thread/list`、`thread/read`、`thread/turns/list`、`thread/resume`、`turn/start`、`turn/steer`、`thread/queue/add`、`thread/unsubscribe`、`account/rateLimits/read`。

`threadId` 必须是 UUID（可带 `urn:uuid:` 前缀），否则同步拒绝：

```text
thread/read        -> -32600 invalid thread id: invalid character: expected an optional prefix of `urn:uuid:` ...
thread/resume      -> -32600 invalid session id: ...
```

### 6.2 未知 thread 的同步错误形态

用合法但不存在的 UUID 探测，全部是**同步** JSON-RPC 错误，不存在"先 accept 再异步失败"：

| 方法 | code | message |
| --- | --- | --- |
| `thread/read` | -32600 | `thread not loaded: <id>` |
| `thread/resume` | -32600 | `no rollout found for thread id <id>` |
| `turn/start` | -32600 | `thread not found: <id>` |
| `turn/steer` | -32600 | `thread not found: <id>` |
| `thread/queue/add` | -32603 | `failed to read thread: invalid thread-store request: no rollout found for thread id <id>` |
| `thread/unsubscribe` | — | 正常返回 `{"status":"notLoaded"}` |

### 6.3 experimentalApi 门禁

| 连接能力 | `thread/queue/add` | `turn/start` |
| --- | --- | --- |
| `experimentalApi: true` | 进入 thread 解析 | 进入 thread 解析 |
| `experimentalApi: false` | -32600 `thread/queue/add requires experimentalApi capability` | 仍进入 thread 解析（**不受门禁**） |

即 `thread/queue/*` 是 experimental 专属能力，而投递主路径 `turn/start` 不需要该能力。

### 6.4 关键风险：`turn/start` 的成功响应不代表投递成功

未认证环境下真实执行 `turn/start`，实测时间线：

```text
turn/start 响应 -> {"turn":{"id":"01a0e788-a0c9-...","status":"inProgress","error":null}}   # 看起来成功
随后异步通知:
  error            "Reconnecting... 4/5"  codexErrorInfo.responseStreamDisconnected.httpStatusCode=401
  error            "Reconnecting... 5/5"  ...
  thread/status/changed  {"type":"systemError"}
  error            "unexpected status 401 Unauthorized: Missing bearer or basic authentication ..."
  turn/completed   {"turn":{"id":"01a0e788-a0c9-...","status":"failed","error":{...401...}}}
thread/turns/list -> 该 turn 的 items 含 userMessage，content[0].text_elements == []
```

结论（与 v0.0.12/v0.0.13 Desktop 空 model 竞态教训同源）：

1. `turn/start` 的响应**不可作为投递确认**。必须等 `turn/completed` 并检查 `turn.status` / `turn.error`。
2. 宿主内置 5 次重连（`Reconnecting... N/5`），失败窗口是秒级，不是立即。适配器不得在此窗口内重试或切换投递路径。
3. `turn/completed` 才是终态；`thread/status/changed: systemError` 是先行信号。

### 6.5 写入前能力探测

`thread/read` 在 `experimentalApi: true` 下返回 `canAcceptDirectInput: true`（`None` 表示能力不可用，例如未 loaded 的存储 thread）。实测 `thread/loaded/list` 会返回本连接创建的 thread id，可作为"已 loaded"判据。

`UserInput::Text.text_elements` 在协议里是 `#[serde(default)]`（可省略），但沿用 Desktop IPC 已修复的契约，RA2A 仍显式发送 `textElements: []`。

## 7. thread 所有权：协议仍不可解

### 7.1 `source` 无效（复现 V6 结论）

`thread/start` 由 app-server 客户端创建，`Thread.source` 实测仍为 `"vscode"`，与 Codex Desktop、remote TUI 无法区分。

### 7.2 `originator` 是进程级全局值，first-writer-wins

`initialize` 会把 `clientInfo.name` 写成 daemon 进程级默认 originator（源码 `app-server/src/request_processors/initialize_processor.rs:19,132,166-194`，非 `codex_app_server_daemon` / `codex-backend` 时生效）。实测三连接序列：

| 步骤 | 连接 `clientInfo.name` | 新建 thread 的 `originator` |
| --- | --- | --- |
| 1（首个 initialize） | `ra2a-v10-probe` | `ra2a-v10-probe` |
| 2 | `v10-alpha` | `ra2a-v10-probe` |
| 3 | `v10-beta` | `ra2a-v10-probe` |
| 4 | `codex_app_server_daemon` | `ra2a-v10-probe` |
| 5（重读步骤 2 建的 thread） | — | 仍为 `ra2a-v10-probe` |

两个直接后果：

1. `originator` **不能**用于区分"哪个连接拥有这个 thread"，它在同一 daemon 进程内对所有 thread 相同。
2. RA2A 若以自有 `clientInfo.name` 作为首个连接接入，会把该 daemon 之后创建的所有 thread（含用户 TUI 新建 thread）的 `originator` 永久写成 RA2A 的名字。这是跨客户端副作用，必须在适配器设计里显式处理（连接顺序、命名、以及是否接受该污染）。

服务端确实维护了"thread → connection 集合"的映射（`app-server/src/thread_state.rs:316-347`），但为 `pub(crate)`，未映射到任何方法。`thread/loaded/list` 只返回裸 `Vec<String>`，无 owner 字段。

### 7.3 多订阅者 fan-out（已验证）

两个客户端都对同一 thread 执行 `thread/resume` 后，其中一个 `turn/start`：

```text
发起方收到: error x N, warning, thread/status/changed(active), ...
订阅方收到: thread/status/changed {"type":"active"}        # 同一 threadId
```

- TUI 作为订阅者会实时看到 RA2A 注入的 turn（正向价值）。
- 审批类服务端请求会 fan-out 给所有订阅者，RA2A 必须显式不代答（与 Desktop 侧 owner 语义同类问题）。
- 订阅会钉住 thread 内存：实测某订阅者断开后，thread 转为 `notLoaded` 并广播 `thread/status/changed: notLoaded` + `thread/closed`，随后卸载。RA2A 必须在不用时 `thread/unsubscribe`。

## 8. 零动作挂接验证（V9 替代结论）

### 8.1 daemon 已存在

普通 `codex`（无任何 flag）在 pty 中启动后：

```text
daemon: pid 1852293 ... app-server --listen unix:// --managed-daemon
ss -xap: u_str ESTAB ... /tmp/codex-daemon-1000/<hash>  users:(("codex",pid=1852293,fd=36))
TUI /proc/<tui_pid>/fd: 37 -> socket:[61879277]      # 与 daemon ESTAB 对端 inode 一致
```

即 TUI 与 daemon 之间的 AF_UNIX 连接成立，TUI 未另起 embedded server。

### 8.2 daemon 不存在

先 `daemon stop`（确认无残留进程、socket 符号链接被删除），再启动普通 `codex`：

```text
t=0s    daemon 进程: 无     socket 存在: 否
t=5s    daemon 进程: 有     socket 存在: 是
t=5..25s 持续存在
ss -xap: u_str ESTAB ... /tmp/codex-daemon-1000/<hash> users:(("codex",pid=1855374,fd=36))
TUI /proc/<tui_pid>/fd 包含 socket:[62014296]        # 与 daemon ESTAB 对端 inode 一致
```

**结论：0.158.0 上普通 `codex` 零动作即可挂接共享 daemon，V9 的阻塞已解除，wrapper 不再是主路径。**

源码侧的对应条件（`codex-rs/tui/src/daemon_startup.rs:25-104`）：`--no-daemon`、`--oss`、workload identity、`CODEX_EXEC_SERVER_URL`、`--profile`、`-c/--enable/--disable/--search`、自定义 config loader、`--strict-config`、`--dangerously-bypass-hook-trust`、Bedrock 首次向导、Windows 非提升终端/`DetachedLaunchRestricted` 会被排除；`--remote` 显式指定时不探测。排除场景正是 wrapper 的兜底范围。

## 9. 对实现的约束

1. RA2A 作为**普通 app-server 客户端**接入官方 daemon socket，不再需要向用户 TUI 进程注入 `--remote`。
2. 传输层实现 WebSocket over AF_UNIX（RA2A 已依赖 `gorilla/websocket`，可用 `NetDial` 挂 UDS）；`codex app-server proxy` 不能替代，它要求调用方自己说 WebSocket。
3. 投递入口固定为 `thread/resume`（建立订阅）+ `turn/start`（空闲）/ `turn/steer`（活跃，需 `expectedTurnId`）。`thread/queue/*` 不进主路径。
4. **投递确认必须基于 `turn/completed`**；`turn/start` 响应只用于拿到 turn ID。任何 `DELIVERY_UNKNOWN` 一律不重试、不切路径。
5. 写入前门禁：thread 已 loaded、`canAcceptDirectInput`、thread ID 为 UUID、`textElements: []` 显式携带、活跃回合用 `turn/steer` 并带 `expectedTurnId`。
6. 端点归属只能由 RA2A 侧登记表建立（记录本连接 create/resume 的 thread ID）；`source` 与 `originator` 均不可用。未知归属 thread 不得发布为 ready。
7. 生命周期纪律：不用即 `thread/unsubscribe`；不代答审批类服务端请求；容忍并忽略与本次投递无关的 `error` / `warning` 通知。
8. 版本门槛：`0.158.0` 是当前唯一验证过的版本；daemon crate 自述 experimental，须 pin 下限并做契约测试。
9. 已知宿主约束：注入的 turn 运行在 **daemon 启动时的环境变量**下，不是用户终端环境（`app-server-daemon/README.md:22-24`）；Windows 需非提升终端且 `CODEX_HOME` 路径需满足 AF_UNIX 108 字节限制。

## 10. 本轮未覆盖（需后续实验）

| 项 | 阻塞原因 | 解除方式 |
| --- | --- | --- |
| 真实 TUI thread 上的注入与实时显示 | 隔离环境无认证，TUI 停在登录页，未创建 thread | 用户在隔离 `CODEX_HOME` 完成独立登录 |
| 真实回合的投递成功率与延迟 | 同上；且需按 `runbooks/codex-account-usage-check.md` 先核对 plan 桶用量 | 独立认证 + 用量门禁 |
| 三平台（macOS/Windows）复现 | 本轮仅 Ubuntu | 各平台按本报告方法复跑 §4/§6/§8 |
| RA2A 与官方 daemon 的 owner 归属 | 属架构决策，非实验问题 | Owner 决策：RA2A 是否改用官方 daemon 取代 `internal/codexhost` 的 app-server 所有权 |
| RA2A 连接对 TUI 新建 thread 的 `originator` 污染 | 需 TUI 真实创建 thread | 独立认证后在同一 daemon 内对比 RA2A 先/后连接两种顺序 |

## 11. 复现材料

本轮脚本与日志位于工作区 `.cache/v10/`（已 gitignore，不入库）：

| 文件 | 用途 |
| --- | --- |
| `probe_ws.py` | 最小 WebSocket over AF_UNIX JSON-RPC 客户端（无第三方依赖） |
| `probe_delivery.py` | 投递契约：能力字段、同步/异步失败、终态通知 |
| `probe_ownership.py` | originator 全局污染 + 多订阅者 fan-out |
| `probe_tui_attach.py` / `probe_tui2.py` / `probe_tui3.py` | TUI 零动作挂接（socket inode 比对、`ss -xap`） |
| `probe_autostart.py` | daemon 不存在时的自动拉起时间线 |
| `logs/01..19-*.json` | 各步骤原始输出 |

复现步骤与隔离口径见 `runbooks/codex-cli-isolated-daemon-experiment.md`。
