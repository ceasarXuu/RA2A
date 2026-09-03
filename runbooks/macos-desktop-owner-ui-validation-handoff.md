# macOS Codex Desktop `text_elements` UI 回归交接

## 目标

在 Mac 官方 Codex Desktop 上验证 `main` 中的 Desktop IPC 文本输入修复：空闲目标的第一条远端消息通过 `turn/start` 创建 turn，活跃期第二条消息通过 `turn/steer` 进入同一 turn；两条 input 均必须携带 `text_elements: []`，Desktop UI 全程可见、可恢复、可手动续聊，不需要重启 App。

本次只安装并验证 `main` 源码，不创建分支、不改代码、不发布版本。测试完成后回传证据，由 Windows 端汇总结果。

## 固定基线

```text
repository: https://github.com/ceasarXuu/RA2A.git
branch: main
required commit: 78b29827568541c2a8f3c60b150b041cc09fb5ab
change: fix(desktopipc): include text elements in messages
```

该修复尚未发布新 tag。`ra2a version` 仍显示 `v0.0.10`，不能用 `ra2a update` 或 latest Release 代替本次源码安装；必须用 Git 祖先关系证明安装源码包含上述提交。

## 回归背景与正确行为

上一次 active-turn 修复已解决重复 start 造成的 `Item not found in turn state`。本次新回归针对另一个已证实问题：RA2A v0.0.10 的 start/steer 文本 input 省略 `text_elements`，Codex Desktop release 26.901.20858 在 `LocalConversationTurn` 中直接读取 `text_elements.length`，导致 `Cannot read properties of undefined (reading 'length')`，并可上浮至 `ThreadSummaryPanel` 和 `AppRoutes`。后端 turn 仍可完成，所以不能只用 rollout 或 `read_thread` 判定通过。

修复后的规则：

1. Desktop IPC 初始化成功后，先调用 `thread-follower-steer-turn`（version 1）。
2. active turn 存在时，消息必须进入该 turn，不能再调用 `start-turn`。
3. 只有 Desktop 明确报告 `NoActiveTurn`、`SteerTurnInactiveError` 或“active turn already ended”时，才调用 `thread-follower-start-turn`（version 2）。
4. steer 或 start 写出后的超时、断连、取消或缺少 turn ID 均属于 `DELIVERY_UNKNOWN`，不得换路径、回退或重试。
5. rollout 或后台 API 中存在完整 turn 不能替代 UI 实时刷新验收。
6. start 和 steer 的文本 input 必须显式带 `text_elements: []`，不能省略或传 `null`；否则新版 Desktop 可在 `LocalConversationTurn` 读取 `text_elements.length` 时进入 error boundary。

Windows 官方 Codex Desktop 已在提交 `78b2982` 上验证 idle start、active steer、活跃期打开长历史 R8、切离再切回，同一时间窗内上述三个 error boundary 均为 0。本交接负责验证 macOS 使用同一协议时没有平台回归。

## 1. 拉取并确认源码

保留开始前已有或来源不明的本地修改，不要 reset、覆盖或擅自提交。如果工作区不干净且会影响安装或验证，停止并回传 `git status --short`。

```bash
cd /path/to/RA2A
git status --short
git branch --show-current
git pull --ff-only
git merge-base --is-ancestor 78b29827568541c2a8f3c60b150b041cc09fb5ab HEAD
git log -1 --oneline
```

必须满足：

- 当前分支是 `main`；
- `git pull --ff-only` 成功；
- `git merge-base --is-ancestor ...` 退出码为 0；
- 没有创建新分支，也没有修改源码。

## 2. 本地验证、安装并重启服务

需要 Go 1.24+。源码安装只替换可执行文件，不应改变现有 node ID、名称、PIN 或 Codex 路径。

```bash
go version
go test ./...
go vet ./...
go test -race ./internal/desktopipc ./cmd/ra2a
./install.sh
~/.local/bin/ra2a restart
~/.local/bin/ra2a version
launchctl print "gui/$(id -u)/com.ra2a.daemon"
```

检查：

- LaunchAgent 正常运行，程序路径是 `~/.local/bin/ra2a`；
- 关闭安装终端后 daemon 仍在运行；
- 原 node ID、名称和 PIN 未变化；
- `~/.config/ra2a/logs/ra2a.err.log` 没有持续启动失败；
- 即使版本字符串仍是 `v0.0.10`，也以本节的 Git 祖先校验和源码安装时间为准，不改 tag、不发版。

## 3. 准备专用测试 task

1. 在 Mac 官方 Codex Desktop 中创建或选择一个不承载生产工作的专用 task，并保持它在当前窗口打开。
2. 记录 task ID、标题、Mac RA2A node ID/name、Codex Desktop 版本、进程 PID 和测试开始时间。
3. 手动发送一条普通消息，确认该 task 当前可交互；等它完成后再开始远端测试。
4. 测试开始时 task 必须为 idle，且历史中不存在本次 marker。
5. 验证期间不要重启 Codex Desktop、切换 task、编辑 rollout/session 文件或启动第二个 App Server。

如当前页面已经因旧版本卡在“思考中”或“此轮内容无法显示”，先保留当前 Desktop 日志、故障时间和目标 thread/turn ID，然后才重启一次 Codex Desktop 清除旧 renderer 状态，再选择新的专用 task。该重启只能发生在正式计时和 marker 发送之前。

### 3.1 向 rog306 报告 READY

Mac 完成安装、选定专用 task 并确认它已 idle 后，通过 RA2A 向 Windows 协调 task 发送一条 READY：

```text
to: ra2a://rog306/01a05964-0476-7ed2-a2d3-3b4ec5727b3f
text: [RA2A-MAC-TEXT-ELEMENTS-READY-<UTC时间>] HEAD=78b29827568541c2a8f3c60b150b041cc09fb5ab; target=ra2a://<mac-node-id>/<mac-task-id>; Desktop=<version>; PID=<pid>; task=idle; UI=visible
```

发送 READY 后立即停止操作，保持目标 task 在前台，等待 rog306 发送两条受控消息。不得自行向测试 task 注入 marker，不得为了加速而重启 Desktop。

## 4. 从 rog306 发送两条唯一消息

先在 rog306 的 RA2A MCP 中调用 `list_targets`，确认 Mac node 和目标 task ID 唯一可见。随后执行一次计划内的两消息序列，不发送第三条消息。

### 4.1 空闲首条消息

调用一次 `send_message`：

```text
to: ra2a://<mac-node-id>/<mac-task-id>
text: [RA2A-MAC-TEXT-ELEMENTS-BASE-<UTC时间>] 请立即运行 sleep 30；等待结束后回复 RA2A_MAC_TEXT_ELEMENTS_BASE_OK。
```

记录 sender 原始结果和发送时间。返回 `accepted` 后，应在 Mac UI 立即看到该用户消息和执行动态，task 进入 active 状态。

### 4.2 active follow-up

确认 Mac UI 已显示首条消息且 Agent 仍在执行 `sleep 30` 后，立即调用一次 `send_message`：

```text
to: ra2a://<mac-node-id>/<mac-task-id>
text: [RA2A-MAC-TEXT-ELEMENTS-FOLLOWUP-<同一UTC时间>] 这是 active-turn follow-up；当前等待结束后请只回复 RA2A_MAC_TEXT_ELEMENTS_FOLLOWUP_OK。
```

要求：

- 两条 marker 使用同一个 UTC 时间后缀，并完整记录；
- 第二条必须在第一条 turn 完成前发送；
- 每条消息只调用一次 `send_message`；
- 任一请求返回超时、`DELIVERY_UNKNOWN` 或其他错误时不得重试，立即保留现场并收集证据；
- 不运行 `desktop-ipc-probe --allow-write`，否则不能证明正式 LAN/MCP 接收链。

若第一条 turn 在第二条发送前意外完成，本次样本无效但不代表修复失败。停止发送，记录时序并回传，不要在同一 task 临时追加消息补测。

## 5. UI 与 turn 验收

保持同一个 Codex Desktop 进程和同一个打开页面，依次确认：

1. 第一条用户消息无需重启或切换 task 即实时出现。
2. sleep 执行期间可以看到正常的思考、工具调用或状态变化，不是静止的假“思考中”。
3. 第二条 follow-up 无需重启或切换 task 即出现在同一 active turn。
4. 全程只创建一个 turn，最终回复为 `RA2A_MAC_TEXT_ELEMENTS_FOLLOWUP_OK`。
5. turn 完成后 UI 从执行态收敛到完成态，没有持续“思考中”。
6. 不重启 App，在输入框手动发送：

   ```text
   本机手动续聊验证，请只回复 RA2A_MAC_MANUAL_OK
   ```

7. 同一 task 正常回复 `RA2A_MAC_MANUAL_OK`，没有“已在另一个应用中打开”、writer 冲突或输入框失效。

必须由 Mac 操作者明确记录动态过程和完成状态。只看到最终消息、rollout 中存在 turn，或后台 API 返回 completed，都不能单独判定 UI 验收通过。

## 6. 协议与日志证据

在测试时间窗内收集 RA2A daemon 日志和 Codex Desktop 日志。日志位置随 Codex Desktop 版本可能变化，不要移动或修改原文件；按测试时间和 marker 定位当前进程的日志即可。

期望同一目标 task 出现：

```text
IpcRouter count: 2
turn/start count: 1
turn/steer count: 1
task_started count: 1
user_message count: 2
task_complete count: 1
Item not found in turn state count: 0
Cannot read properties of undefined (reading 'length') count: 0
LocalConversationTurn error-boundary count: 0
ThreadSummaryPanel error-boundary count: 0
AppRoutes error-boundary count: 0
```

关键约束：

- 首条空闲消息对应 `turn/start`；
- 第二条 active follow-up 对应 `turn/steer`；
- 两条 user message 和最终回复使用同一个 turn ID；
- 不存在第二个 `turn/start`、第二个 task、重复 marker 或 `DELIVERY_UNKNOWN` 后重试；
- 如 Codex Desktop 日志格式变化，保留原始相关行，不要为了满足上述名称改写证据。

## 7. 回传证据包

按以下模板回传：

```text
Mac model / macOS version:
Codex Desktop version / PID:
RA2A node ID / name:
git HEAD:
required commit is ancestor: yes/no
target task ID / title:
base marker / sent at:
follow-up marker / sent at:
base sender result:
follow-up sender result:
single turn ID:
base visible live without restart/tab switch: yes/no
follow-up visible live without restart/tab switch: yes/no
dynamic execution visible: yes/no
UI returned to completed state: yes/no
manual continuation succeeded: yes/no
Codex Desktop remained same process: yes/no
turn/start count:
turn/steer count:
Item not found in turn state count:
Cannot read properties of undefined (reading 'length') count:
LocalConversationTurn error-boundary count:
ThreadSummaryPanel error-boundary count:
AppRoutes error-boundary count:
Desktop restarted during test: yes/no
```

同时附上：

```bash
git status --short
launchctl print "gui/$(id -u)/com.ra2a.daemon"
tail -n 200 ~/.config/ra2a/logs/ra2a.log
tail -n 200 ~/.config/ra2a/logs/ra2a.err.log
```

再附 marker 前后同一时间窗的 Codex Desktop 日志原始片段，以及能够证明两条消息同属一个 turn 的 rollout 摘要。敏感正文可以遮盖，但不要遮盖 task ID、turn ID、marker、时间、方法名和错误类型。

## 通过标准

以下条件必须全部满足：

- Mac 安装源码包含 `78b29827568541c2a8f3c60b150b041cc09fb5ab`；
- rog306 严格发送两条计划内的唯一消息，无重试、无第三条测试消息；
- 第一条空闲消息只触发一次 `turn/start`；
- 第二条 active follow-up 只触发一次 `turn/steer`；
- 两条消息进入同一个 turn，且只有一次 task start/complete；
- Desktop UI 无需重启或切换 task 即显示两条消息、动态执行和最终完成状态；
- `Item not found in turn state` 为 0；
- `Cannot read properties of undefined (reading 'length')` 为 0，且 `LocalConversationTurn`、`ThreadSummaryPanel`、`AppRoutes` 均无新 error boundary；
- 用户能在原 task 手动继续对话；
- 没有 writer 冲突、重复 turn 或不确定投递后的重试。

任一条件失败都不能判定 macOS 回归通过。保留现场，不重启 App、不重发消息、不编辑 session 文件，直接回传完整证据包。
