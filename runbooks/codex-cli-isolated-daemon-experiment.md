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
- `Thread.source` 对所有 app-server 客户端恒为 `vscode`
- `Thread.originator` 是 daemon 进程级全局值、first-writer-wins（首个 `initialize` 的 `clientInfo.name` 决定后续所有 thread 的取值）
- 多订阅者 fan-out：`turn/start` 的 `thread/status/changed` 会广播给所有 `thread/resume` 订阅者
- 最后一个订阅者离开后 thread 卸载并广播 `notLoaded` + `thread/closed`

**投递确认必须等 `turn/completed`**：`turn/start` 先返回 turn ID（`status: inProgress`、`error: null`），未认证时随后出现 5 次 `Reconnecting... N/5`，最终 `turn/completed` 为 `status: failed`。这与 Desktop 空 model 竞态同类，适配器不得把请求响应当成功。

## 7. 需要认证才能验证的部分

真实 TUI thread、活跃回合 follow-up、人工继续、成功率与延迟，都要求隔离 `CODEX_HOME` 内完成**独立登录**。前置条件：

1. 用户在隔离环境完成登录（不得复制正式版 `auth.json`）。
2. 按 `runbooks/codex-account-usage-check.md` 核对 plan 桶用量；未认证时 `account/rateLimits/read` 返回 `codex account authentication required to read rate limits`，无法核对。

## 8. 收尾（必做）

```sh
"$CODEX_BIN" app-server daemon stop
pgrep -af "$WORK"                # 应为空
ls -la "$WORK/home/app-server-control/"   # 只剩 app-server-startup.lock
ls -la /tmp/codex-daemon-$(id -u)/        # 只剩 *.lock
# 复核正式版未受影响
pgrep -af "ra2a daemon"; ls -la ~/.codex/app-server-control/
```

实验产物（脚本与日志）留在 `.cache/` 下，不入库；入库的只有脱敏后的结论文档。

## 9. 已知踩坑

| 坑 | 表现 | 处理 |
| --- | --- | --- |
| 以为 socket 是 JSONL | 首次发送即 BrokenPipe，浪费一轮排查 | 直接上 WebSocket 客户端 |
| 用 `proxy --sock` 当 JSONL 桥 | 完全无输出 | 记住它是字节中继 |
| PTY 未设窗口大小 | TUI 只输出 splash，看不到登录页 | `TIOCSWINSZ` 设 50x200 |
| `daemon version` 当状态查询 | 未运行时命令失败而非返回 JSON | 这正是 `start_required` 判据 |
| 未关自动更新 | daemon 可能拉取新版本污染实验 | 预置 `settings.json` |
| 长 `CODEX_HOME`（Windows） | 超过 AF_UNIX 108 字节静默回退 embedded server | 用短路径，必要时缩短 `CODEX_HOME` |
| Windows 提升权限终端 | detached 启动被拒，回退 embedded | 用非提升 PowerShell |
