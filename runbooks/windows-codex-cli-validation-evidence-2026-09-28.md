# Windows Codex CLI 适配验证记录（2026-09-28）

对应步骤：[`windows-codex-cli-validation-checklist.md`](windows-codex-cli-validation-checklist.md)。本轮使用 Windows 11 26200、非提升 PowerShell、Codex CLI 0.158.0。仓库 `main` 已推进至 `8dd6db1`。既有全局 CLI 0.153.4 与默认 `~/.codex` 未替换；0.158.0 安装在仓库忽略的 `.cache/win-verify/cli`，实验 `CODEX_HOME=C:\Users\77585\.ra2a-win-verify`（31 字符）。用户批准后，正式 RA2A 服务已升级到 `8dd6db1`。

| 项 | 结果 | 证据与范围 |
| --- | --- | --- |
| W1 构建、自检 | PASS | Go 1.27.0/windows-amd64；`go build ./...`、`go test ./...` 全绿；隔离构建的 `ra2a selftest --pin <测试 PIN> --id win-node --name rog306` 返回 `selftest=ok`。Go 版本高于清单记载的 1.24.x。 |
| W2 正式版 Desktop 回归 | 本机正式服务 PASS，LAN/UI 直观确认待补 | 正式服务升级后，经 `POST /v1/send` 向专用 Desktop session `01a05d7e-b958-7df2-bd67-6eba9636fad6` 投递空闲 marker `RA2A-W2-DESKTOP-20260928-2308`，HTTP 200 `accepted`，该 session 仅产生一个 turn `01a0e88e-c3b4-7f23-a2f4-48405de2c5d0`，回复 `RA2A_W2_OK`。活跃 marker `RA2A-W2-ACTIVE-FIRST-20260928-2310` 与 `RA2A-W2-ACTIVE-FOLLOWUP-20260928-2310` 均 HTTP 200，两个 user message 位于同一 turn `01a0e88f-c602-7233-b9dc-eeca364405fc`，最终回复 `RA2A_W2_ACTIVE_OK`。Desktop 日志对该会话记录空闲 `turn/start` 一次、活跃 `turn/start` 和 `turn/steer` 各一次，并有 `rendererWindowVisible=true` 与 `turn-complete`。通过 `read_thread` 核对了消息及 turn；未从另一台机器发起，也未直接截图确认视觉呈现。 |
| W3 daemon/socket | PASS | daemon `version` 返回 `running`、`appServerVersion=0.158.0`；控制 socket 为 `C:\Users\77585\.ra2a-win-verify\app-server-control\app-server-control.sock`，74 字符；ACL 仅当前用户。 |
| W4 零动作挂接 | PASS | daemon 停止后，普通 `codex` 启动自动出现 daemon 与 socket；TUI 正常显示。`--no-daemon`、`-c model="x"`、`--profile p1`、设置 `CODEX_EXEC_SERVER_URL=ws://127.0.0.1:1` 分别启动后 daemon 均缺席；profile 使用隔离目录内的 `p1.config.toml`。 |
| W5 免登录注入 | 本机适配器 PASS，LAN 待验证 | 隔离 TUI 自建 thread `01a0e872-95d4-78a2-83e4-a6ce84096c69`；手工首轮实时显示 `MOCK-REPLY-1`。第二客户端通过 `internal/codexcli.Adapter` 注入 `WINDOWS-CLI-INJECT-20260928-2238`，返回 `delivered`，TUI 实时显示 marker 与 `MOCK-REPLY-3`，日志有 `cli_turn_delivered`、`cli_unsubscribed`。未由另一台机器经 RA2A LAN 路径注入。 |
| W6 活跃 follow-up | 本机适配器 PASS | 延迟 mock 下，`WINDOWS-CLI-HANG2-FIRST-20260928` 与 `WINDOWS-CLI-HANG2-FOLLOWUP-20260928` 均返回 `delivered`，日志分别为 `mode=start`、`mode=steer`；rollout 两条 user message 均属于 turn `01a0e878-46ab-76e3-a00c-e753f7a25e0d`，该 turn 只有一次 `task_complete`；TUI 显示两次回复并回到可输入状态。 |
| W7 `START_REQUIRED` | 本机适配器 PASS | 停止隔离 daemon 后向已登记 thread 投递，返回 `code=start_required`；`daemon version` 仍无法连接，无新隔离 daemon 进程。LAN 调用方返回值未复验。 |
| W8 边界与降级 | 部分完成 | 单元测试覆盖未登记/非法 thread、旧节点缺 `agent` 字段等。正式服务实测不存在的 UUID 返回 HTTP 404 `TARGET_NOT_FOUND`；未登记的非法 thread 文本也返回 `TARGET_NOT_FOUND`，与清单预期的 `TARGET_UNSUPPORTED` 不一致，原因是端点查找先于 CLI ID 校验。断 LAN、daemon 中途退出和新旧节点真实混用未做实机验证。 |
| W9 收尾 | PASS | 隔离 TUI、daemon 与 mock Python 进程均已停止；实验目录与 rollout 留存供复验。正式 RA2A 服务运行新版本，默认 Codex 环境未替换；旧服务二进制保留备份。 |

## 发现与修复

1. Windows 测试夹具原先用 Unix shell 假可执行文件及过长 socket 路径，导致 `go test ./...` 失败。改为 Windows `.cmd` 夹具与短路径后全绿（`d059ff4`）。
2. 官方 0.158.0 在本机 `initialize.userAgent` 返回 `Codex Desktop/0.158.0 (Windows 10.0.26200; x86_64) ...`；旧解析器得到 `Codex`，绕过最低版本校验。修复后真实连接记录 `app_server_version=0.158.0`（`4e17361`）。
3. 官方 `turn/steer` 响应携带顶层 `turnId`；旧适配器按 `turn.id` 解码，造成成功注入却返回 `turn_unconfirmed`。修复假服务契约与解码后，活跃 follow-up 返回同一 turn ID 的 `delivered`（`ccf97c7`）。
4. Windows daemon 要求 `CODEX_HOME/app-server-daemon` 目录私有。仓库盘以及新建用户目录默认继承其他组权限时，普通 `codex` 报 `socket directory is not private to the current user`。仅将隔离实验目录及其 `app-server-daemon` 子目录 ACL 收紧为当前用户 FullControl 后，零动作挂接成功。正式目录未改动。
5. 首次升级正式服务后，Codex Desktop 会话数从约 289 降为 0；`agentbridge.Endpoint.Validate` 拒绝状态为 `busy` 的会话，而 Desktop 适配器会把非 idle 会话标记为 busy。同时，本机 mDNS 发现结果与本机端点再次合并，`rog306` 出现两次。立即恢复旧服务后修复这两处，相关测试和 `go test ./...` 全绿（`8dd6db1`）。重新升级后 `/v1/targets` 恰有一个 `rog306`，包含 289 个会话；另有 `macmini-m4` 22 个、`ubuntu407` 316 个。

## 正式服务状态与回退点

- 正式路径：`C:\Users\77585\.local\bin\ra2a.exe`；升级后 SHA-256 为 `476C311382C309053CB97B92869D5094B536410A9B13ED51682791172BF2955B`。
- 升级前二进制备份：`C:\Users\77585\.local\bin\ra2a.exe.retired-win-verify-20260928230711`。首次升级失败的二进制另留 `ra2a.exe.failed-win-verify-20260928230520`，未删除。
- 实测投递使用正式服务本机控制端点 `127.0.0.1:47321`，没有绕过 RA2A 控制层；它不等同于另一台机器发起的 LAN 端到端验证。

## 待接手事项

- 从另一台机器向专用 Desktop 测试 session 完成 W2 的 LAN 投递，并直接确认 UI 显示；本机正式控制端点与 Desktop turn 日志已通过。
- 从另一台机器完成 W5/W6/W7 的 LAN 端返回值、`list_targets.agent=codex-cli` 与日志联验；需要在不影响正式 Desktop 配置的前提下，把隔离 CLI thread 接入 LAN 节点。
- 补齐 W8 真实网络/版本混用场景。

## FIX-001 实现验证补充（2026-10-01）

来源：[v0.0.18 FIX-001](../docs/v0.0.18/README.md)。本节记录实现阶段，**尚未完成修复后的 Windows 实机验收**；不改变上文 2026-09-28 的原始验证范围。

| 验证项 | 结果 | 范围 |
| --- | --- | --- |
| Linux 宿主生命周期回归 | PASS | `go test ./internal/codexhost`；Unix 目录行为不变。 |
| Windows amd64 测试编译 | PASS | `GOOS=windows GOARCH=amd64 go test -c ./internal/codexhost`；包含 5 项新增 Windows ACL 测试，编译不能代替运行。 |
| Wine 11.0 补充尝试 | 无测试结果 | 全新临时 prefix 在初始化阶段 90 秒超时；该 prefix 的 Wine 进程已停止。未运行官方 Windows daemon，不计实机验收。 |
| Windows 首次目录/坏继承 ACL/daemon 先启动 | 待验证 | 下列 Windows 测试与真实启动顺序都需要执行。 |

Windows 非提升 PowerShell 中先运行：

```powershell
go test ./internal/codexhost -run '^TestControlDirectory' -v
```

通过标准：5 项测试通过，`TestControlDirectoryRejectsReparsePoint` 若因符号链接权限跳过，需用隔离 junction 场景补测后才完成该边界。测试仅操作临时夹具。测试分别检查官方等价的 protected、单 OICI FullControl DACL，官方式不允许删除共享的目录句柄共存，当前 owner 保留，以及已有子文件/兄弟文件/父目录的完整安全描述符与内容不变。

随后按 FIX-001 的验收条件，在全新短路径的隔离 `CODEX_HOME` 中补真实 CLI/daemon/RA2A 顺序验证。使用独立 `ra2a serve` 实例与独立控制端口，避免对正式服务执行 `stop/restart/selftest`；沿用清单第 5 节的 mock 配置，避免复制真实认证资料。制造额外继承 ACL 仅限新建实验目录，禁止针对真实 `~/.codex` 或整个 `CODEX_HOME` 递归收紧权限。

每个场景保留以下证据：

```powershell
codex --version
codex app-server daemon version
$acl = Get-Acl (Join-Path $env:CODEX_HOME 'app-server-control')
$acl | Select-Object Owner, AreAccessRulesProtected, Sddl
$acl.Access | Select-Object IdentityReference, FileSystemRights, IsInherited, InheritanceFlags, PropagationFlags
```

回传应明确：CLI 与 daemon 版本、测试提交、独立实例 PID/控制端口、启动顺序、修复前后 ACL、官方 daemon 状态、普通 `codex`/`codex --yolo` 的 TUI 输入结果、RA2A 重启及升级结果、既有 socket/会话保留情况，以及是否出现跳过测试。正式环境的临时手工修 ACL 和交叉编译都不计入修复验收。
