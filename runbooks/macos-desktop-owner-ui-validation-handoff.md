# macOS Codex Desktop owner-first UI 验证交接

## 目标

在 Mac 官方 Codex Desktop 上验证 RA2A 的 Desktop-owner-first 修复：从 rog306 通过正式 LAN/MCP 路径只发送一条唯一消息后，目标 task 应在 **不重启 Codex Desktop** 的情况下立即显示新用户消息、创建并完成 turn，且用户仍可在同一 task 手动继续对话。

本次只做 `main` 上的回归验证，不创建分支、不发布版本。

## 固定基线

```text
repository: https://github.com/ceasarXuu/RA2A.git
branch: main
required commit: 11f44361f8db8e7ccad1346df24fc56171b83f95
change: fix(desktopipc): prefer Desktop owner for turns
```

该提交尚未发布新 tag，二进制仍可能显示 `v0.0.8`。不能用 `ra2a update` 或 latest Release 代替本次源码安装；必须确认安装的源码包含上述提交。

## 问题与修复边界

旧路由先调用受管 Codex App Server。目标 task 空闲时，该调用可能成功创建并持久化完整 turn，却绕过当前 Codex Desktop renderer 的事件链；因此切换 tab 仍看不到消息，只有重启 App 重新水合后才出现。

修复后的规则：

1. 只要 Desktop IPC 可用，始终先通过 `thread-follower-start-turn` 交给当前 Desktop owner 创建 turn。
2. 仅在连接、initialize 或 Desktop 明确拒绝等“确认未投递”结果下回退受管 App Server。
3. StartTurn 写出后的超时、断连、取消或成功响应缺少 turn ID 均为 `DELIVERY_UNKNOWN`，不得回退或重试。
4. rollout 中存在 turn 只证明已持久化，不能替代当前 Desktop UI 的实时可见性验收。

Windows 已完成真实 LAN 验证；本交接负责补齐同一共用路由在 macOS 上的真实 UI 回归。

## 1. 拉取并验证源码

保留开始前已有或来源不明的本地修改，不要 reset、覆盖或擅自提交它们。如果工作区不干净且会与本次安装冲突，停止并报告。

```bash
cd /path/to/RA2A
git status --short
git branch --show-current
git pull --ff-only
git merge-base --is-ancestor 11f44361f8db8e7ccad1346df24fc56171b83f95 HEAD
git log -1 --oneline
```

必须满足：

- 当前分支为 `main`；
- `git pull --ff-only` 成功；
- `git merge-base --is-ancestor ...` 退出码为 0；
- 不创建新分支，不改代码。

## 2. 安装源码版本并重启 LaunchAgent

需要 Go 1.24+。无参数源码安装只替换命令，不改现有 node ID、名称、PIN 或 Codex 路径：

```bash
go version
go test ./...
./install.sh
~/.local/bin/ra2a restart
~/.local/bin/ra2a version
launchctl print "gui/$(id -u)/com.ra2a.daemon"
```

检查：

- LaunchAgent 状态正常且进程使用 `~/.local/bin/ra2a`；
- 关闭执行安装的终端不会停止服务；
- `~/.config/ra2a/logs/ra2a.err.log` 没有持续启动失败；
- 若 `ra2a version` 仍显示 `v0.0.8`，以 Git 祖先校验和本次源码构建为准，不要因此改 tag 或发版。

## 3. 准备目标 task

1. 在 Mac 官方 Codex Desktop 中创建或选择一个专用测试 task，保持该 task 打开。
2. 记录准确的 task ID、标题、Mac RA2A node ID 和验证开始时间。
3. 开始前确认 task 空闲、可手动发送消息，且没有同名测试消息。
4. 验证期间不要退出或重启 Codex Desktop，也不要先切换 tab 触发重新加载。

不要使用重要生产 task。不要编辑 rollout/session 文件，也不要使用独立 App Server 或第二 writer 模拟成功。

## 4. 通过正式 LAN 路径只发送一条消息

先在 rog306 的 RA2A MCP 中调用 `list_targets`，确认 Mac 节点和上述 task ID 可见。随后调用一次且仅一次 `send_message`：

```text
to: ra2a://<mac-node-id>/<mac-task-id>
text: [RA2A-MAC-DESKTOP-FIRST-<UTC时间>] 请只回复 RA2A_MAC_DESKTOP_FIRST_OK
```

要求：

- `<UTC时间>` 使用本次验证生成的唯一值，并完整记录；
- 只允许这一条跨设备测试消息；
- 若返回超时、`DELIVERY_UNKNOWN` 或其他错误，不得重试，先收集证据；
- 不要额外运行 `desktop-ipc-probe --allow-write`，否则无法证明正式 LAN/MCP 路径。

## 5. UI 与可继续沟通验收

发送后保持 Codex Desktop 原进程不变，按顺序确认：

1. 当前打开的 Mac task 无需重启 App 即出现唯一 marker 对应的用户消息。
2. 同一 task 创建一个且仅一个新 turn，并回复 `RA2A_MAC_DESKTOP_FIRST_OK`。
3. 不切换 task、不重启 App，在输入框手动发送：

   ```text
   本机手动继续验证，请只回复 RA2A_MAC_MANUAL_OK
   ```

4. 同一 task 正常回复 `RA2A_MAC_MANUAL_OK`，且没有“已在另一个应用中打开”或 writer 冲突。

必须由操作者明确记录“无需重启即可看到”或“必须重启才能看到”。仅凭后台 API、rollout 或 turn ID 不算 UI 验收通过。

## 6. 证据包

回传以下最小证据，敏感内容可遮盖，但不要省略 ID、时间顺序和错误类型：

```text
Mac model / macOS version:
Codex Desktop version:
RA2A node ID / name:
git HEAD:
target task ID / title:
unique marker:
sender result:
created turn ID:
message visible without restart: yes/no
single turn only: yes/no
manual continuation succeeded: yes/no
Codex Desktop remained same process: yes/no
```

同时附上：

```bash
git status --short
launchctl print "gui/$(id -u)/com.ra2a.daemon"
tail -n 200 ~/.config/ra2a/logs/ra2a.log
tail -n 200 ~/.config/ra2a/logs/ra2a.err.log
```

如能定位当前 Codex Desktop 日志，再截取 marker 前后同一时间窗，证明存在 Desktop IPC 路由、目标 `turn/start` 和完成/renderer 通知。不要用“rollout 中有完整 turn”单独宣告通过。

## 通过标准

以下条件必须全部满足：

- Mac 安装源码包含 `11f44361f8db8e7ccad1346df24fc56171b83f95`；
- rog306 只发送了一条唯一远端测试消息；
- 消息在未重启 Codex Desktop 的情况下实时出现在原 task；
- 只创建一个 turn 且回复正确；
- 用户可在原 task 手动继续对话；
- 没有 writer 冲突、重复 turn 或 `DELIVERY_UNKNOWN` 后重试。

任一条件失败即不能判定修复完成。保留现场，不重启 App、不重发消息，回传证据包供后续定位。
