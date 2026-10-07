# Codex CLI 隔离 daemon 实验标准流程

适用于所有需要在 RA2A 正式版共存机器上验证 Codex CLI / app-server 行为的实验（V6/V8-R/V10/V11 等）。依据 PD32 与 `docs/v0.0.15/experiments/codex-cli-v10.md` 的实操经验固化。

## 0. 为什么需要独立 CODEX_HOME

`0.158.0` 起 `CODEX_HOME` 同时决定三件事：

| 资源 | 路径 |
| --- | --- |
| daemon 控制 socket | `$CODEX_HOME/app-server-control/app-server-control.sock` |
| session / rollout 存储 | `$CODEX_HOME/sessions/` |
| 配置与认证 | `$CODEX_HOME/config.toml`、`auth.json` |

因此**换一个 `CODEX_HOME` 就等于同时隔离了 daemon、socket 与 session 存储**，不再需要 `-c ephemeral=true` 这类手段（V6 已实测其无效，仍产生 `ephemeral=false` 的持久 thread）。

Go 适配器实验可以显式传 `codexcli.Config.CodexHome`。2026-10-02 的双 fake 宿主回归已确认：该值同时限定 daemon 探测子进程与缺省 socket，连接、重连和 Health 均使用同一目录，指定实例未运行时不回退默认实例。公开 `DetectDaemon` 仍使用调用进程环境；实验调用它时必须在独立子进程中设置 `CODEX_HOME`，不要修改正在提供服务的进程环境。

注意符号链接：真实 socket 位于 `/tmp/codex-daemon-<uid>/<sha256>`（规避 AF_UNIX 108 字节上限），`$CODEX_HOME` 下是符号链接。校验权限要看真实目标：

```sh
stat -c '%a %U %n' "$CODEX_HOME/app-server-control" "$(readlink -f "$CODEX_HOME/app-server-control/app-server-control.sock")"
# 期望：目录 700，socket 600，同 uid
```

## 1. 准备隔离环境

```sh
REPO=<repo root>
WORK=$REPO/.cache/exp-<tag>          # .cache/ 已 gitignore
mkdir -p "$WORK/home/app-server-daemon" "$WORK/logs"

# 关闭 daemon 自动更新，避免实验期间拉取新版本
cat > "$WORK/home/app-server-daemon/settings.json" <<'JSON'
{"remoteControlEnabled":false,"shutdownGraceSeconds":10,
 "updater":{"autoUpdateEnabled":false,"updateIntervalMinutes":1440}}
JSON
```

获取目标版本二进制（不覆盖系统 codex）：

```sh
cd "$WORK"
npm pack @openai/codex@<version>              # 4KB 壳包
npm pack @openai/codex@<version>-linux-x64    # 平台包，约 160MB
tar xzf openai-codex-<version>-linux-x64.tgz -C pkg
chmod +x pkg/package/vendor/x86_64-unknown-linux-musl/bin/codex
export CODEX_HOME=$PWD/home
export CODEX_BIN=$PWD/pkg/package/vendor/x86_64-unknown-linux-musl/bin/codex
"$CODEX_BIN" --version
```

## 2. 启动前冲突检查（必做）

```sh
# 正式版进程与 socket 必须在实验前后保持不变
pgrep -af "ra2a daemon"
ls -la ~/.codex/app-server-control/
pgrep -af "app-server"
```

记录这些输出的时间戳/进程号，实验结束后逐一比对。若本机已有正式版 RA2A daemon 在跑（`app-server-control.sock.ra2a-<pid>.sock`），**不要**用无后缀的官方 socket 名，避免与正式版混淆。

## 3. daemon 生命周期

```sh
"$CODEX_BIN" app-server daemon version     # 未运行：报错并给连接错误（探测语义）
"$CODEX_BIN" app-server daemon start      # 首次会把 CLI 包安装进隔离 CODEX_HOME
"$CODEX_BIN" app-server daemon version    # {"status":"running",...,"appServerVersion":"..."}
"$CODEX_BIN" app-server daemon stop       # 结束时必须执行
```

每个命令输出恰好一个 JSON 对象。`version` 用作 `start_required` 判定：命令失败即 daemon 不可达。

## 4. 传输：WebSocket over AF_UNIX

控制 socket **不是** JSONL。已验证的三种认知：

| 做法 | 结果 |
| --- | --- |
| 直接往 socket 写 JSONL | 首次 `sendall` 即 `BrokenPipeError` |
| `codex app-server proxy --sock <path>` + JSONL | 无输出；`codex-rs/stdio-to-uds/src/lib.rs` 是纯字节中继，调用方仍需自己说 WebSocket |
| 自写 WebSocket over AF_UNIX 客户端 | 成功，`HTTP/1.1 101 Switching Protocols` |

握手：`GET / HTTP/1.1`，`Host: localhost`，`Upgrade: websocket`，`Connection: Upgrade`，`Sec-WebSocket-Key: <b64 16 bytes>`，`Sec-WebSocket-Version: 13`。客户端帧必须掩码。

RA2A 实现时用已有的 `gorilla/websocket`，通过 `NetDial` 挂 UDS，无需自写握手。

`initialize` 不返回协议版本号；版本只能从 `initialize.userAgent` 解析，或用 `daemon version` 的 `appServerVersion`。

## 5. 验证 TUI 零动作挂接

```sh
# daemon 不存在时启动普通 codex，观察自动拉起
python3 .cache/v10/probe_autostart.py     # 记录时间线：daemon 进程 + socket 出现

# 已有 daemon 时启动 TUI，比对 socket inode
ss -xap | grep codex-daemon-$(id -u)      # daemon 侧 ESTAB 的对端 inode
ls -l /proc/<tui_pid>/fd | grep socket     # TUI fd 应出现同一 inode
```

socket inode 双向一致才算挂接成立。仅看到 daemon 进程存在不足以证明 TUI 已接入。

PTY 注意事项：不设窗口大小时 TUI 可能只输出 splash；用 `fcntl.ioctl(fd, TIOCSWINSZ, ...)` 设成 50x200。读取端要容忍 `OSError`。

## 6. 不需要认证也能验证的契约

以下实测不依赖登录，可直接用于契约测试：

- 方法存在性：`thread/loaded/list`、`thread/list`、`thread/read`、`thread/turns/list`、`thread/resume`、`turn/start`、`turn/steer`、`thread/queue/add`、`thread/unsubscribe`、`account/rateLimits/read`
- `threadId` 必须是 UUID（可带 `urn:uuid:`），否则同步 -32600
- 未知 thread 的错误形态（全部同步）：`thread not loaded` / `no rollout found` / `thread not found` / `failed to read thread`
- `thread/unsubscribe` 永不报错，返回 `notLoaded` / `notSubscribed` / `unsubscribed`
- `experimentalApi: false` 时 `thread/queue/add` 被拒（`requires experimentalApi capability`），`turn/start` 不被拒
- `thread/start` 无需认证即可创建 thread 并返回 `id`、`sessionId`、`path`
- `0.159.3` 实测新线程在首个回合持久化前，第二客户端的 `thread/resume` 返回 `no rollout found`；验证已有会话收件时，须由原客户端先完成种子回合，不能只用新建线程的 loaded 状态代替真实会话前置条件。
- `Thread.source` 对所有 app-server 客户端恒为 `vscode`
- `Thread.originator` 是 daemon 进程级全局值、first-writer-wins（首个 `initialize` 的 `clientInfo.name` 决定后续所有 thread 的取值）
- 多订阅者 fan-out：`turn/start` 的 `thread/status/changed` 会广播给所有 `thread/resume` 订阅者
- 最后一个订阅者离开后 thread 卸载并广播 `notLoaded` + `thread/closed`

**收件与执行分别确认（PD33）**：`turn/start` 返回有效 turn ID 且无嵌入错误即确认收件；`turn/steer` 必须返回预期活动 turn ID。未认证时后续执行仍可能失败，应单独记录 `turn/completed`，不能将执行结果耦合到收件 ACK。缺失或不明确的输入 ACK 保持 unknown，不重放。

## 7. 免登录验证：本地 mock 模型端点

配置自定义 `model_providers` 指向本地 mock 后，**协议层与 TUI 层的绝大部分验证都不需要账号**：

```toml
# $CODEX_HOME/config.toml
model = "mock-model"
model_provider = "ra2a-mock"
model_reasoning_effort = "none"

[model_providers.ra2a-mock]
name = "RA2A Mock"
base_url = "http://127.0.0.1:8931/v1"
wire_api = "responses"
env_key = "RA2A_MOCK_KEY"
requires_openai_auth = false
```

- `env_key` 指向任意假值即可（请求头会带 `Authorization: Bearer <假值>`）。
- `requires_openai_auth = false` 使其不经过 OpenAI 登录。
- **provider 的环境变量由 daemon 继承**，必须在 `codex app-server daemon start` 之前导出 `RA2A_MOCK_KEY`，事后再导出无效。

mock 端点只需实现 `POST /v1/responses` 的 SSE 流，事件形状见 `codex-rs/core/tests/common/responses.rs`（tag `rust-v0.158.0`）：

```text
event: response.created
data: {"type":"response.created","response":{"id":"resp_1"}}

event: response.output_item.done
data: {"type":"response.output_item.done","item":{"type":"message","role":"assistant","id":"msg_1","content":[{"type":"output_text","text":"MOCK-REPLY-1"}]}}

event: response.completed
data: {"type":"response.completed","response":{"id":"resp_1","usage":{...}}}
```

参考实现见 `.cache/v10/mock_responses.py`，支持 `MOCK_MODE=fast`（立即回复）与 `MOCK_MODE=hang`（`MOCK_HANG_SECONDS` 秒后回复，用于保持回合活跃以测 `turn/steer`）。

免登录已验证：TUI 越过登录门槛并自建 thread、完成完整回合并渲染 `MOCK-REPLY-N`、外部客户端注入到 **TUI 拥有的 thread** 并被 TUI 实时渲染、活跃回合 `turn/steer` follow-up 在同一 thread 内各执行一次、完整通知序列（`turn/started` → `item/*` → `turn/completed`）。

### 时序约束（踩过）

`thread/resume` 的响应到达之前就发 `turn/start`，调用方**收不到任何回合通知**（`turn/started` / `item/*` / `turn/completed` 全部丢失），投递无法确认。必须串行：先 `thread/resume` 并等响应，再 `turn/start`。

### PTY 自动化注意

文本与回车必须**分两次写入**（中间至少 0.5s）。一次性写入 `text\r` 会让 TUI 只把文本填进输入框而不提交，实测不产生新 thread 与新回合。

### 自动验证真实 Adapter 协议（2026-10-02）

Linux 可直接运行已纳入仓库的 opt-in 验证，默认日常测试跳过，不启动 daemon：

```sh
RA2A_TEST_CODEX_BIN=/absolute/path/to/native/codex \
  go test -race -count=1 -timeout 180s -v \
  -run '^TestNativeCLIIsolatedDelivery$' ./internal/codexcli
```

必须指定原生二进制的绝对路径，不覆盖正式 CLI 安装。测试自动创建独立 home、loopback 模拟模型与假凭据，关闭实验 daemon 自动更新/远程控制，过滤子进程账户密钥和代理注入环境；通过显式 `Config.CodexHome` 连接实验实例，结束时仅停止该 home 的 daemon。不需要登录或复制 `auth.json`。

`0.159.3` 已通过：22 轮投递及来源保留、独立 RPC 拥有者继续、活跃回合 steer、实验 daemon 重启恢复、模型保留及未认证检查。独立 RPC 客户端保持线程订阅，不使用正式 TUI。此测试不覆盖 TUI 人工继续、真实模型与限流、跨设备通信或 Windows/macOS 原生验收。

Go 临时 home 在 `/tmp` 时，Codex 会提示拒绝在临时目录创建 PATH helper aliases；上述仅输出文本的模拟模型流程已通过该环境，不将此结果扩展到工具执行能力。

## 8. 仍需账号才能验证的部分

| 项 | 原因 |
| --- | --- |
| 真实模型行为 | mock 只覆盖协议形状，不覆盖真实 tool call、长上下文、限流 |
| 账号用量门禁 | `account/rateLimits/read` 未认证时返回 `codex account authentication required` |
| plan 级 rate-limit 弹条 | 依赖真实账号 `codex` 桶（阈值 90） |
| 端到端成功率与延迟 | 需要真实后端 |

需要这些时：在隔离 `CODEX_HOME` 内由用户完成**独立登录**（不得复制正式版 `auth.json`），再按 `runbooks/codex-account-usage-check.md` 核对 plan 桶用量。

## 9. 收尾（必做）

```sh
"$CODEX_BIN" app-server daemon stop
pgrep -af "$WORK"                # 应为空
ls -la "$WORK/home/app-server-control/"   # 只剩 app-server-startup.lock
ls -la /tmp/codex-daemon-$(id -u)/        # 只剩 *.lock
# 复核正式版未受影响
pgrep -af "ra2a daemon"; ls -la ~/.codex/app-server-control/
```

实验产物（脚本与日志）留在 `.cache/` 下，不入库；入库的只有脱敏后的结论文档。

## 10. 已知踩坑

| 坑 | 表现 | 处理 |
| --- | --- | --- |
| 以为 socket 是 JSONL | 首次发送即 BrokenPipe，浪费一轮排查 | 直接上 WebSocket 客户端 |
| 用 `proxy --sock` 当 JSONL 桥 | 完全无输出 | 记住它是字节中继 |
| PTY 未设窗口大小 | TUI 只输出 splash，看不到输入框 | `TIOCSWINSZ` 设 50x200 |
| 文本与回车一次写入 | TUI 只填入输入框，不提交、不建 thread | 分两次写，中间 ≥0.5s |
| `resume` 未等响应就 `turn/start` | 收不到任何回合通知，无法确认投递 | 串行：等 resume 响应再 start |
| provider env 事后才导出 | daemon 已继承旧环境，`env_key` 取不到 | 在 `daemon start` 之前导出 |
| `daemon version` 当状态查询 | 未运行时命令失败而非返回 JSON | 这正是 `start_required` 判据 |
| 未关自动更新 | daemon 可能拉取新版本污染实验 | 预置 `settings.json` |
| 长 `CODEX_HOME`（Windows） | 超过 AF_UNIX 108 字节静默回退 embedded server | 用短路径，必要时缩短 `CODEX_HOME` |
| Windows 提升权限终端 | detached 启动被拒，回退 embedded | 用非提升 PowerShell |

## 跨设备验收协调会话的登记检查（2026-10-02）

正式 CLI 会话也可能出现在共享 App 历史中。`list_targets` 标为 `codex-app` 不足以证明 Desktop 归属；本次现场未登记的 Warp CLI 被投递到 Desktop，返回 `no-client-found`。先结合实际客户端、官方 daemon loaded list 和明确的 `canAcceptDirectInput=true` 核对线程，不能单凭 `source` 或 `originator` 自动归属。

确认属于 CLI 后，备份 RA2A 二进制与配置，部署含 `e7aa4ae` 排除修复的已验证版本，再执行 `ra2a adopt-cli <完整 thread ID>`。该命令仅保存配置，现有 registry 不热加载，需仅重启 RA2A 服务，再检查端点为 `codex-cli`。重启前核对正式 CLI/App 是否在服务 cgroup 外；重启后核对关键 PID 和配置/认证/代理文件元数据。不要通过重启 App 或开启 Desktop 失败后的 managed fallback 来绕过错误归属。

按阶段分别记录收件 ACK、接收标记和执行完成。协调会话正在执行长任务时，收件仍应及时返回；后续回复属于独立消息。unknown 不自动重发。互发测试限定消息数量和 hop，避免无限回信。

## Windows 测试写配置的隔离要求（2026-10-02）

Windows 的 `os.UserHomeDir()` 读取 `USERPROFILE`，仅设置 `HOME` 无法隔离 `operator.Save`。ROG 实际出现 OpenCode-only 测试写入 `open-node` 与测试 PIN、导致 DTLS 超时；恢复正式配置后通知恢复。

需要调用 operator 或 RA2A run 路径的测试同时设置临时 `HOME`、`USERPROFILE`、`LOCALAPPDATA`，并在任何 Save/Load 前断言 `operator.ConfigPath()` 落在临时目录。`LOCALAPPDATA` 用于旧 Windows 配置读取与迁移，也必须隔离；fake 宿主并不自动隔离配置和 mailbox。复用 `cmd/ra2a` 的 `isolatedOperatorHome` fixture。Windows 交叉编译只证明可构建，原生执行结果另行记录。

## 收件与执行验证分开（2026-10-03，PD33）

Owner 已纠正旧的完成确认设计。CLI Deliver 的成功只依据有效 start/steer 输入 ACK，不等待 turn/completed；RPC返回不明确时仍unknown且不重放。不要延长超时来掩盖收件与工作量的耦合。

真实fixture中由独立owner observer另行等待完成，确保每轮模型输入和后续人工客户端调用有序；这属于执行验证，不是Deliver的等待条件。活跃测试保持mock模型阻塞，先断言Deliver已返回成功且turn ID不变，再释放模型、观察完成。observer重连后必须重新绑定完成处理器。v0.0.19修复后RA2A直接给已有内存thread输入，不在投递前resume或建立/释放订阅；独立owner持续观察，不得关闭或退订真实TUI。

## 短收件限时测试的准备阶段（2026-10-04）

Mac 的旧 shell fixture 冷探测实测需 406–449ms，将其包含在 300ms 收件限时内会在写入前失败。先用 ListEndpoints 建立 fake 连接并校验目标，再开始收件计时；保留原 300ms、禁止完成事件、一次写入和有效 ACK 断言。诊断四次收件均低于 2ms，不能把冷探测失败归为收件耦合，也不能通过放宽收件限时掩盖问题。

跨平台 daemon probe fake 使用原生测试可执行文件和相邻 JSON，不使用 Windows .cmd；仅处理固定 version 参数，缺失 fixture 或异常参数必须拒绝。race 子进程退出等待通过测试局部 GORACE 设置隔离。Linux 通过与 Windows 交叉编译均不能替代 Windows 原生执行。

## Windows native实验的路径与命令结果（2026-10-04）

- 临时home要短，启动前检查control socket不超过107字节。HOME/USERPROFILE/LOCALAPPDATA/APPDATA/CODEX_HOME/TEMP/TMP全部隔离；测试凭据store明确file，不读取系统keyring。新state目录使用当前SID的protected OICI私有DACL，不能依赖Go的mkdir0700在Windows设置ACL。只创建fresh目录，不修已有生产ACL。
- 官方0.160.0在Windows创建current junction；ROG现场Go1.27 EvalSymlinks对原/规范路径均失败，而Win32 GetFinalPathNameByHandle解析成功、current/release/实际exe hash一致。不能据Go解析失败判断junction不存在，也不能仅修斜杠后放宽归属。Win32只读handle解析真实路径，匹配native FILETIME与PID记录及实际exe，再initialize指定temp socket核home；全部通过后才允许停止该实验PID。Unix保留已有canonical gate。
- PowerShell将native stderr warning提升为终止异常，会丢失真实ExitCode。临时home的PATH alias拒绝提示已实测出现，但不等于停止失败；使用.NET ProcessStartInfo、UseShellExecute=false、stdout/stderr独立异步读取，以真实ExitCode和PID/socket状态判断。ROG保留实例经过完整gate后这样停止，exit0、PID/记录/socket退出且正式保护未变。
- start归属或stop验证失败时保留temp home/PID/原始日志；不得为了cleanup绕gate按group/job杀进程。保留实例清理与消息投递是不同操作，unknown消息不重发。

固定源码：[Windows junction选择](https://raw.githubusercontent.com/openai/codex/rust-v0.160.0/codex-rs/app-server-daemon/src/prepare_install_windows.rs)、[PID启动](https://raw.githubusercontent.com/openai/codex/rust-v0.160.0/codex-rs/app-server-daemon/src/backend/pid_start.rs)、[Windows进程身份检查](https://raw.githubusercontent.com/openai/codex/rust-v0.160.0/codex-rs/app-server-daemon/src/backend/windows.rs)。


### Windows短TEMP与清理结果

- 在启动go测试子进程前，为其TEMP/TMP/TMPDIR指定全新短且可写的目录；fixture调用os.MkdirTemp早于t.Setenv，不能把父TEMP放在长证据目录。先按随机home最坏名字核socket长度<=107，不放宽门禁。日志继续写外部证据目录。
- native stop成功、PID记录/socket/package退出与整个home目录清理是独立事实；os.RemoveAll错误必须显式报告，不能忽略后宣称清理成功。Windows实测功能PASS后仍可能留下 `.tmp`/`app-server-daemon`，先保留错误、目录和只读资源清单，不自行删证据或修ACL。

- 隔离投递fixture应在全新home配置 `[features] plugins = false`，避免默认插件启动同步引入外部Git下载和Windowsindex.lock共享句柄，不能修改正式用户插件设置。d13d656实测Linux0.159.3/0.160.0与Windows0.160.0保持全部投递/恢复覆盖，新Windowshome完整清理通过；插件同步自身不计入此fixture覆盖。旧失败目录继续保存。

## 同一 thread 从 CLI 转到 App 后不发布

2026-10-08 Ubuntu 现场验证：旧 `cliSessions` 登记会同时阻止未加载 CLI 发布及 App 历史发布。官方只读 `thread/read` 可找到 thread，但 `thread/loaded/list` 为空；source/originator 不能替代当前宿主证据。

先由 Owner 确认同一 ID 当前已在 App 打开且 CLI 不再承载它；备份 RA2A 配置后执行 `ra2a release-cli <完整ID>`，仅重启既有 RA2A 服务。命令不更改原生会话，地址 ID 保持不变。检查 `list_targets` 的该 ID 为 codex-app，并在原 App 窗口独立确认一次授权标记收件。未经宿主确认不得根据 CLI 未加载自动转交 App；App 历史记录存在也不证明可投递。


## Linux writer归属只读查询（2026-10-08）

internal/codexowner.ReadWriter以lock文件device/inode匹配/proc/locks的FLOCK ADVISORY WRITE，结合PID、start ticks及exe并复读身份；不获取锁。文件残留、额外打开者不证明持锁。错误及unsupported不能当睡眠；该reader与被查进程必须处于同一PID/mount视图，快照不能取代写入末端条件。

最小验证：`GOPROXY=off go test -race ./internal/codexowner -count=1`。fixture只在临时文件用独立测试子进程持锁，覆盖释放但保持文件打开、同inode接手、进程退出和路径inode替换。显式查询耗时不是后台发现刷新延迟，Mac/Windows交叉构建不证明实际holder可读取。该模块当前尚未接入正式路由。

## 新建stdio连接的隔离桥实验（2026-10-08）

`internal/codexstdio`仅用于新建模拟App与backend的四条管道，不能复制正式App描述符或与原读者争抢stdout。保留原客户端唯一initialize/capabilities及initialized通知；区分App请求、桥请求与原生server请求，迟到桥响应不能落入App。取消后不重放；桥接返回原生RPC结果，验证者还须检查有效turn ID及独立完成记录。

真实隔离命令：`RA2A_TEST_CODEX_BIN=<官方绝对路径> GOPROXY=off go test -race ./internal/codexcli -run '^TestNativeStdioBridgeReceiptAndOwnerContinuation$' -count=1 -timeout 90s -v`。fixture直接启动自己的stdio app-server，8个profile/temp环境隔离、file mock凭据、plugins=false，不走正式daemon/IPC。thread_unload_delay_secs=1仅写入临时配置，用于退订后等待卸载并核不唤醒；不调整正式用户延迟。

Linux0.161.0已验证输入ACK/通知/原客户端继续/卸载拒绝。该实验不证明真实Desktop屏幕、权限审批交互或跨平台holder仲裁；Windows固定codex-ipc命名管道未隔离，不能因此启动第二个Desktop App。模块不接正式发现/投递，不部署。
