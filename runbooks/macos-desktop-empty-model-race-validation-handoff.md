# macOS → Windows Codex Desktop 空 model 竞态修复验收交接

## 目的

本文交接给 Mac 发送端操作员，用于验收 RA2A 在 Windows rog306 上修复的
Codex Desktop owner `start-turn` 设置竞态。Mac 只负责发起一次唯一测试消息并
记录发送证据；Windows 操作员负责观察官方 Codex Desktop 的实时 UI、日志和
原 session 手动续聊。

本交接针对空 model 竞态，和旧的 `text_elements` UI 验收是两个独立门禁：

- 通用 UI/text-elements 验收：
  [`macos-desktop-owner-ui-validation-handoff.md`](macos-desktop-owner-ui-validation-handoff.md)
- Windows transport、owner-first 和错误分类背景：
  [`windows-desktop-ipc-handoff.md`](windows-desktop-ipc-handoff.md)

## 版本与基线

```text
repository: https://github.com/ceasarXuu/RA2A.git
branch: main
required commit: 6df73fe fix(desktopipc): apply resolved model to owner settings
supporting commits: d3a0947（解析 model）/ 299890f（替换被 MCP 锁定的 Windows 二进制）
superseded attempts: 7ddb557 / bbc91b5（空 barrier）；d3a0947 单独使用（仅 start request.model）
target node: ra2a://rog306
known R8 thread: 019f43ef-d5a0-7910-bd34-c5c825d1e94a
```

验收以 `6df73fe` 是否为当前 `HEAD` 的祖先为准，不以版本标签为准。若工作树
有本地修改，先记录并停止，不要覆盖、reset 或清理这些修改。

## 根因与修复行为

R8 历史 thread 的持久化 model 是空字符串，Desktop owner 当前也可能没有可继承的
latest thread settings。旧实现只等待空设置 barrier；该 barrier 不会生成 model。
Desktop 先接受 `start-turn` 并返回 turn ID，随后异步失败：

```text
invalid_request_error: The '' model is not supported when using Codex with a ChatGPT account.
```

所以本文不再使用“设置已收敛”作为通过条件。当前实现的有界流程如下：

1. active turn 仍只走 steer，不读取或改变模型。
2. steer 明确 inactive 后，RA2A 通过受管 App Server 执行
   `thread/read(includeTurns=false)`；若 thread model 非空则使用它。
3. thread model 为空时执行 `model/list`，选择 `isDefault=true` 的当前默认模型；不得
   硬编码 `gpt-5.6-sol` 或其他名称。
4. 解析不到非空模型时，在 start 帧写出前明确失败。
5. `thread-follower-update-thread-settings` 的 `threadSettings.model` 必须携带解析结果，
   随后的 `thread-follower-start-turn` 在 `turnStart.request.model` 中携带同一值。
   只设置后一处已被 Windows R8 实机证明仍会产生空 `turn_context.model`。
6. 超时、断连、取消、响应缺少 turn ID、named-model/config 错误和其他拒绝均不
   重试，也不切换到第二个 writer；结果未知时不得重发。

## Mac 发送端预检

在 Mac 仓库工作树执行：

```sh
cd /path/to/RA2A
git status --short
git branch --show-current
git pull --ff-only
git merge-base --is-ancestor 6df73fe HEAD
git log -1 --oneline
go test ./...
go vet ./...
go test -race ./internal/desktopipc ./cmd/ra2a
./install.sh
~/.local/bin/ra2a version
launchctl print "gui/$(id -u)/com.ra2a.daemon"
```

确认当前分支是 `main`、祖先检查退出码为 0、daemon 属于独立 launchd 服务且
不是当前终端的子进程。若 Mac 仍运行旧二进制，先安装当前提交；不要为了本次
验收创建分支或修改 session/rollout 文件。

## Windows 端前置条件

由 rog306 操作员在正式 marker 前完成：

1. 安装并运行包含 `6df73fe` 的 RA2A；确认计划任务/服务指向该版本。
2. 官方 Codex Desktop 已打开，`\\.\pipe\codex-ipc` 可连接，Desktop owner
   IPC `initialize` 成功。
3. 只存在一个受管 App Server，不启动第二个 App Server 抢占 thread writer。
4. 先保存旧 R8 的错误/日志证据；如页面仍停在历史的“正在思考/系统错误”，
   可在正式 marker 前重启一次 Desktop 以清除旧 renderer 状态。正式测试开始后
   不再重启、切 tab 或编辑 session 文件。
5. 选择一个短、可安全验证的专用 session；只有 Windows 操作员明确同意时才用
   生产 R8：

   ```text
   ra2a://rog306/019f43ef-d5a0-7910-bd34-c5c825d1e94a
   ```

## Mac → Windows 单消息测试

Mac 操作员在发送前调用 `list_targets`，确认 `rog306` 在线且目标 thread/title
唯一。全流程只允许一条测试消息：

```text
[RA2A-WIN-EMPTY-MODEL-FINAL-<UTC>] 请先等待约30秒，再只回复 RA2A_WIN_EMPTY_MODEL_OK。
```

将 `<UTC>` 替换为实际 UTC 时间，例如 `20260904T031500Z`。等待要求用于让
Windows 操作员观察动态执行，不是让 RA2A 侧 sleep 或重试。记录：

- `message-id`；
- Mac 本地发送时间和目标 URI；
- 原始 sender result（accepted、明确拒绝或 `DELIVERY_UNKNOWN`）。

如果 sender 返回 `DELIVERY_UNKNOWN`、超时或连接断开，立即停止并把证据交回；
不得重发同一正文，也不得用新 message-id 猜测是否成功。

## Windows 观察与证据

Windows 操作员在 marker 前后至少各保留 5 分钟时间线，记录以下事件的时间和
request/turn 标识，不输出正文或 tool output：

```text
RA2A receive / message-id
thread-follower-steer-turn（若已有 active turn）
NoActiveTurn / active turn already ended（若 idle）
thread-follower-update-thread-settings（threadSettings.model 非空）
thread-follower-start-turn
turnStart.request.model（只记录模型名是否非空）
turn/start
task_started
task_complete
renderer/error-boundary 或 IPC reset
```

正常 idle 路径应为：

```text
receive → steer 明确 inactive 拒绝 → thread/read → 必要时 model/list
→ 携带非空 model 的 settings update → 一次携带同一 model 的 start-turn
→ turn/start → task_started → task_complete
```

正式验收不应再观察到空 model turn。最终只能有一个 start 请求、一个成功 turn，
不能出现异步 systemError、第二个成功 turn或第二个 writer。

### UI 门禁

Windows 操作员必须肉眼确认，不得只凭 rollout 或后台 `read_thread` 代替：

- Mac 消息在原目标 session 中实时出现；
- 可看到动态执行/思考状态变化，而不是永久停在“正在思考”；
- 最终显示 `RA2A_WIN_EMPTY_MODEL_OK`，没有“任务遇到系统错误”；
- 正式测试期间不需要重启 Desktop 或切换 tab；
- 在同一个原 session 的 Desktop 输入框手动发送：
  `本机手动续聊验证，请只回复 RA2A_WIN_MANUAL_OK`，并看到成功完成。

### 日志搜索

在故障 marker 的 ±5 分钟内搜索 Desktop 日志，保留路径和行号：

```powershell
$roots = @(
  "$env:LOCALAPPDATA\Codex\Logs",
  "$env:LOCALAPPDATA\Packages\OpenAI.Codex\_2p2nqsd0c76g0\LocalCache\Local\Codex\Logs"
)
$logs = Get-ChildItem $roots -Recurse -Filter *.log -ErrorAction SilentlyContinue
$logs | Select-String -Pattern `
  "thread-follower-start-turn|thread-follower-update-thread-settings|turn/start|task_started|task_complete|Cannot read properties|LocalConversationTurn|ThreadSummaryPanel|AppRoutes|Item not found|ipc-connection-reset|DESKTOP_OWNER_UNAVAILABLE" |
  Select-Object Path,LineNumber,Line
```

如果仍观察到空 model 错误，立即判定 FAIL；记录 start request 是否携带非空 model，
不要重试。thread model 为空和默认模型回退分支由自动化单元测试覆盖。

### Rollout 统计

仅报告目标 thread 对应 JSONL 的以下字段，不粘贴 prompt、工具输出或其他正文：

```text
文件路径
文件大小
总行数
最后修改时间
[RA2A message] 原始计数
JSON 全部可解析：yes/no
failed to parse rollout：计数
missing field：计数
unknown variant：计数
```

若 JSON 解析失败、出现 rollout 损坏或 `Item not found`，停止验收并保留原文件。

## Mac 返回模板

Mac 操作员将以下模板连同 Windows 证据返回；不要删掉 `message-id` 或时间戳：

```text
Mac model / macOS version:
Codex Desktop version / PID:
RA2A node ID / name:
git HEAD:
required commit is ancestor: yes/no
target Windows node / thread / title:
marker / sent at:
sender result:
message-id:
Windows UI visible live without restart/tab switch: yes/no
dynamic execution visible: yes/no
UI returned to completed state: yes/no
single successful turn ID:
empty-model rejection observed: yes/no
settings barrier observed: yes/no
resolved model non-empty: yes/no
model source: thread/read or model/list default
retry count:
manual continuation succeeded: yes/no
Desktop restart during formal test: yes/no
Item not found in turn state count:
Cannot read properties of undefined (reading 'length') count:
LocalConversationTurn error-boundary count:
ThreadSummaryPanel error-boundary count:
AppRoutes error-boundary count:
DELIVERY_UNKNOWN / retry:
rollout stats:
Desktop log paths and line numbers:
```

## 通过条件与失败处理

通过必须同时满足：提交祖先检查通过、只发送一条消息、没有未知结果重发、UI
实时可见且最终完成、成功 turn 只有一个、原 session 手动续聊成功、没有第二
writer/OOM/native crash，rollout 可解析且未被编辑。

任一门禁失败时，状态记为 `FAIL`：停止发送，保留日志和 rollout，不重启 Desktop，
不清理 session，不修改配置；把完整模板和原始路径/行号交回 Windows 修复方。
