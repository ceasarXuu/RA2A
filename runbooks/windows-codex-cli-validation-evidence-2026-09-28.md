# Windows Codex CLI 适配验证记录（2026-09-28）

对应步骤：[`windows-codex-cli-validation-checklist.md`](windows-codex-cli-validation-checklist.md)。本轮使用 Windows 11 26200、非提升 PowerShell、Codex CLI 0.158.0。仓库 `main` 最终为 `ccf97c7`。既有 CLI 0.153.4、正式 RA2A 服务与默认 `~/.codex` 未替换；0.158.0 安装在仓库忽略的 `.cache/win-verify/cli`，实验 `CODEX_HOME=C:\Users\77585\.ra2a-win-verify`（31 字符）。

| 项 | 结果 | 证据与范围 |
| --- | --- | --- |
| W1 构建、自检 | PASS | Go 1.27.0/windows-amd64；`go build ./...`、`go test ./...` 全绿；隔离构建的 `ra2a selftest --pin <测试 PIN> --id win-node --name rog306` 返回 `selftest=ok`。Go 版本高于清单记载的 1.24.x。 |
| W2 正式版 Desktop 回归 | 待验证 | 本轮未升级正式 RA2A 服务，也未从对端向专用 Desktop 测试 session 发送 marker。 |
| W3 daemon/socket | PASS | daemon `version` 返回 `running`、`appServerVersion=0.158.0`；控制 socket 为 `C:\Users\77585\.ra2a-win-verify\app-server-control\app-server-control.sock`，74 字符；ACL 仅当前用户。 |
| W4 零动作挂接 | PASS | daemon 停止后，普通 `codex` 启动自动出现 daemon 与 socket；TUI 正常显示。`--no-daemon`、`-c model="x"`、`--profile p1`、设置 `CODEX_EXEC_SERVER_URL=ws://127.0.0.1:1` 分别启动后 daemon 均缺席；profile 使用隔离目录内的 `p1.config.toml`。 |
| W5 免登录注入 | 本机适配器 PASS，LAN 待验证 | 隔离 TUI 自建 thread `01a0e872-95d4-78a2-83e4-a6ce84096c69`；手工首轮实时显示 `MOCK-REPLY-1`。第二客户端通过 `internal/codexcli.Adapter` 注入 `WINDOWS-CLI-INJECT-20260928-2238`，返回 `delivered`，TUI 实时显示 marker 与 `MOCK-REPLY-3`，日志有 `cli_turn_delivered`、`cli_unsubscribed`。未由另一台机器经 RA2A LAN 路径注入。 |
| W6 活跃 follow-up | 本机适配器 PASS | 延迟 mock 下，`WINDOWS-CLI-HANG2-FIRST-20260928` 与 `WINDOWS-CLI-HANG2-FOLLOWUP-20260928` 均返回 `delivered`，日志分别为 `mode=start`、`mode=steer`；rollout 两条 user message 均属于 turn `01a0e878-46ab-76e3-a00c-e753f7a25e0d`，该 turn 只有一次 `task_complete`；TUI 显示两次回复并回到可输入状态。 |
| W7 `START_REQUIRED` | 本机适配器 PASS | 停止隔离 daemon 后向已登记 thread 投递，返回 `code=start_required`；`daemon version` 仍无法连接，无新隔离 daemon 进程。LAN 调用方返回值未复验。 |
| W8 边界与降级 | 部分完成 | 单元测试覆盖未登记/非法 thread、旧节点缺 `agent` 字段等。断 LAN、daemon 中途退出和新旧节点真实混用未做实机验证。 |
| W9 收尾 | PASS | 隔离 TUI、daemon 与 mock Python 进程均已停止；实验目录与 rollout 留存供复验。正式 RA2A 与默认 Codex 环境未替换。 |

## 发现与修复

1. Windows 测试夹具原先用 Unix shell 假可执行文件及过长 socket 路径，导致 `go test ./...` 失败。改为 Windows `.cmd` 夹具与短路径后全绿（`d059ff4`）。
2. 官方 0.158.0 在本机 `initialize.userAgent` 返回 `Codex Desktop/0.158.0 (Windows 10.0.26200; x86_64) ...`；旧解析器得到 `Codex`，绕过最低版本校验。修复后真实连接记录 `app_server_version=0.158.0`（`4e17361`）。
3. 官方 `turn/steer` 响应携带顶层 `turnId`；旧适配器按 `turn.id` 解码，造成成功注入却返回 `turn_unconfirmed`。修复假服务契约与解码后，活跃 follow-up 返回同一 turn ID 的 `delivered`（`ccf97c7`）。
4. Windows daemon 要求 `CODEX_HOME/app-server-daemon` 目录私有。仓库盘以及新建用户目录默认继承其他组权限时，普通 `codex` 报 `socket directory is not private to the current user`。仅将隔离实验目录及其 `app-server-daemon` 子目录 ACL 收紧为当前用户 FullControl 后，零动作挂接成功。正式目录未改动。

## 待接手事项

- 使用当前 `main` 的正式 RA2A 服务及专用 Desktop 测试 session 完成 W2，并确认 UI 实时显示、空闲 start 一次、活跃 steer 一次。
- 从另一台机器完成 W5/W6/W7 的 LAN 端返回值、`list_targets.agent=codex-cli` 与日志联验。
- 补齐 W8 真实网络/版本混用场景。
