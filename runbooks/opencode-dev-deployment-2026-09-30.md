# OpenCode dev 修复三节点部署记录（2026-09-30）

## 固定代码基线

- 分支：`main`；源码修复提交：`68c5609c1298605d946fe354810369ec44aa253b`。
- 本次只部署 RA2A daemon；Codex 旧会话无须关闭或重启。本机 OpenCode TUI 必须重新启动，才能加载新版 wrapper 并建立附着租约。
- `ra2a version` 仍显示 `v0.0.15`，**不能用版本字符串判断 dev 是否已部署**；以运行中的可执行文件的 `go version -m` → `vcs.revision`、PID 和 SHA256 为准。

## ubuntu407（已核验）

1. 确认 `origin/main` 等于 `68c5609`；`PATH="/home/zhangxu/sdk/go/bin:$PATH" ./install.sh --opencode-wrapper`。本机 Go SDK 在 `/home/zhangxu/sdk/go/bin/go`，未列入当前 shell 的 PATH。
2. `~/.local/bin/ra2a restart`：daemon 原 PID `2571265` → 新 PID `2635686`；`systemctl --user show ra2a.service` 为 `ActiveState=active`。
3. `sha256sum /proc/2635686/exe ~/.local/bin/ra2a` 一致，均为 `f67a222cfbeff676ed58366f5760d107341a00575631eca4ec47eabda3b65177`；`go version -m /proc/2635686/exe` 的 `vcs.revision=68c5609`、`vcs.modified=false`。
4. 安装后新启动的 `opencode --yolo --ra2a` wrapper PID `2654475`，其 `/proc/2654475/exe` 与磁盘 `~/.local/bin/opencode` 的 SHA256 一致：`82b644eef7b561cbe63c1d068103ba1c0ad8b282ffcd247dc5e065203a069f9b`，`vcs.revision=68c5609`。
5. 新 TUI 固定附着 `:4099` 上的会话 `ses_f0d5dc6dfffepzX4QgYjMP0gsc`，租约文件为 `~/.config/ra2a/opencode-sessions/ses_f0d5dc6dfffepzX4QgYjMP0gsc.2654475`；`GET /v1/targets` 中 ubuntu407 317 个端点、其中 OpenCode 1 个。旧 TUI 未附着租约时只有 316 个端点，此差异符合新发布口径。

## rog306（远端会话回报，已核验）

- 用户指定的 Codex 会话：`ra2a://rog306/01a05d7e-b958-7df2-bd67-6eba9636fad6`（“测试session消息注入”）。部署指令投递返回 `accepted`；远端会话随后向上述 ubuntu407 OpenCode 会话回报 `ROG306-DEV-DEPLOY-DONE`。
- 远端回报：`git pull --ff-only origin main`；Go 构建新 Windows 可执行文件；禁用/停止 RA2A 计划任务并确认旧 PID `40312` 退出；旧 exe 退役保留，更新 `C:\Users\77585\.local\bin\ra2a.exe` 后重启。新 PID `60412`，运行中的 exe `go version -m` 为 `68c5609` / windows-amd64，SHA256 `70A69D710C7C42B9019A51ED8C6C8BA6A5094E8D9D4A390BEF5335562EBAAD82`。
- 对端 `list_targets` 报 rog306 `ready` 且 `sessionsStale=false`；Codex 会话未重启。未安装 OpenCode wrapper，不需要替换。已有未跟踪 `.commandcode/` 被保留；因此对端构建标记 `vcs.modified=true`，但 tracked diff 为 0，以 `vcs.revision`、运行进程和散列共同核验。
- 本端 mDNS 观察到 rog306 服务端口 `50343` → `59278`；端口变化仅是重启辅助信号，**不能单独证明版本**。

## macmini-m4（远端会话回报，已核验）

- 用户指定的 Codex 会话：`ra2a://macmini-m4/01a0eebf-9e9b-7f61-bb50-df18279b9085`（“配置联调专用 session”）。首次部署消息的请求返回 504，结果未知；后按用户指定会话发送了一条包含“已处理则不重复”的一次性指令，返回 `accepted`。
- 后续仅查询状态（不触发第二次安装）的消息也返回 `accepted`。用户现场确认该会话**看到了指令，正在执行**；不能将短时间无回报解释为未到达，不应继续重投。
- 该会话向本机 OpenCode 新会话回报：`RA2A dev 68c5609 部署状态：已完成`，当前 daemon PID `14581`；已安装并运行的二进制 `go version -m` 显示 `vcs.revision=68c5609c1298605d946fe354810369ec44aa253b`、`vcs.modified=false`；`launchctl` state=running、pid=14581、last exit code=0；本机 `/v1/targets` 为 macmini-m4 `ready`、25 个 Codex App 会话。该状态询问只读，没有重复安装或重启。
- 本端 mDNS 随后观察到 macmini-m4 的 RA2A 端口 `53657` → `62459`，与远端 daemon 重启相符；本端 `/v1/targets` 再次确认 macmini-m4 `ready`、25 个会话。Mac 上原 Codex 会话未要求重启。

## 本次操作经验

- RA2A `list_targets` 的节点 `ready` 和会话列表只证明可发现，不证明远端 daemon 正运行特定提交。
- 跨机投递返回 504 时结果未知；不要盲目重投同一动作。只有用户明确指定会话后，才发送带“若已处理则勿重复”的指令。
- 旧 OpenCode TUI 即使连接共享 server，也不会生成新版附着租约；需要用户主动重开，不能因 daemon 重启便声称旧 TUI 已升级。
- Windows 计划任务 `MultipleInstances=IgnoreNew`：重启命令成功不表示旧 PID 已退出，必须核验真实进程的 PID、路径与构建信息；详见 `runbooks/windows-managed-app-server-lifecycle-handoff.md`。
