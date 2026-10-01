# RA2A 已支持 Harness 自动安装与核验

适用：源码 `install.sh` / `install.ps1`，以及自 v0.0.17 起的发布资产 `install-ra2a.sh` / `install-ra2a.ps1`。旧版 Release 安装器不具备自动 wrapper 安装能力。

## 安装契约

| 本机可运行的宿主 | 正常安装后的行为 | 初始化要求 |
| --- | --- | --- |
| Codex CLI | 安装 `codex` wrapper；原生命令保留为 `codex.bin` 或记入 native-path 文件；非 TUI 透传 | 可单独初始化 |
| OpenCode | 安装 `opencode` wrapper；普通 TUI 自动连接共享 server、建立会话租约，子命令透传 | 可单独初始化，不要求 Codex |
| Codex App 内置的 Codex 可执行文件 | 注册 Codex MCP 并启动适配器；无独立 CLI 时不需要 CLI wrapper | 可单独初始化 |
| 无可用宿主 | 安装 RA2A 命令，但提示尚无接入对象 | setup 明确失败，不报告服务就绪 |

首次设置后，重复安装会重新检测当前宿主并刷新配套；已有配置的 daemon 重启后也会注册新检测到的适配器。Codex App 与 OpenCode 混用时两者独立接入。已经运行的 TUI 不会因更换 wrapper 而自行升级，必须由用户主动重开。
Codex CLI 有健康的官方共享 daemon 时，自动安装的 wrapper 仍让普通 TUI 走原生启动；仅官方 daemon 不可用且 RA2A managed socket 可用时注入 managed 连接，并记录 `codex_wrapper_managed_fallback` 日志。纯 flag 启动（如 `codex --yolo`）同样按 TUI 处理；`--help`/`--version` 一类信息 flag 始终透传。
注入前必须通过可用性门禁：托管 host 需报告与调用方一致的 Codex home，并在限时内完成一次账号读取（`account/rateLimits/read`）。门禁失败或 socket 不可用时保持原生启动并记录 `codex_wrapper_managed_skipped`。这样当 RA2A 服务环境与用户 shell 环境不一致（典型为代理变量只导出在 `~/.bashrc`）时，用户 TUI 不会被静默切到连不上账号后端的 host。Codex 0.159+ 将真实 UDS 放在 `/tmp/codex-daemon-<uid>/` 并在控制目录留下符号链接，wrapper 读取 owner lease 时先解析符号链接，再校验 socket 类型与连通性，并把这个解析后的路径交给 `--remote`。
曾经检测到的宿主若后来被卸载，下一次安装会将没有 backing native 的 RA2A launcher 移入带时间戳的备份，而不是阻止其他宿主；daemon 配置只保留仍能运行的可执行文件。

## 发布资产和入口一致性

发布流水线为 linux/darwin/windows 的 amd64/arm64 同时构建 `ra2a`、`codex-wrapper`、`opencode-wrapper`，分别附 SHA256。Release 下载器先验证所有将安装的资产再替换本地文件；校验失败保留旧二进制。只有检测到对应宿主才下载 wrapper，用户不必安装 Go。
源码安装也在替换现有 RA2A 可执行文件前先完成所有要安装的 wrapper 构建，避免 wrapper 构建失败造成半升级。

Windows npm 安装可能只有 `.cmd` shim；安装器记录原生 `.exe`/`.cmd` 路径，wrapper 调用 `.cmd` 时使用 `cmd.exe`。已经安装 Codex App、但 CLI 不在 PATH 时，Windows 安装器还尝试读取官方 Desktop App Server 进程的真实可执行文件路径。无法发现实际可运行命令时应解释原因，不能把检测成功当作凭空存在的二进制。
Windows 共享 OpenCode server 若经 `.cmd` shim 启动，结束服务时须通过 `taskkill /T` 回收整个原生子进程树，不能只终止 `cmd.exe` 父进程。

卸载通过对应安装器的 `--uninstall` / `-Uninstall` 恢复已保存的原生命令、移除本次安装的 wrapper 和 RA2A 服务。Windows 正在被调用的可执行映像先重命名为 retired 备份，再恢复原生命令；不要通过覆盖正在运行的映像碰运气。

## 验证方法

```sh
/home/zhangxu/sdk/go/bin/go test -count=1 ./...
/home/zhangxu/sdk/go/bin/go test -race -count=1 ./installer ./internal/operator ./cmd/ra2a ./cmd/oc-wrapper ./cmd/codex-wrapper ./internal/control ./internal/mcpserver
GOOS=windows GOARCH=amd64 /home/zhangxu/sdk/go/bin/go build ./cmd/ra2a ./cmd/oc-wrapper ./cmd/codex-wrapper
sh -n install.sh && sh -n install-remote.sh
```

`installer` 用隔离 HOME、假宿主命令、假 Go 构建器、HTTP Release 服务器模拟本地/远程下载、重复升级、SHA256 失败与卸载；Linux 上安装了 `pwsh` 时还通过真实 PowerShell 执行两个 Windows 安装脚本的安装/重复安装/卸载分支，Windows 服务命令在测试进程中被桩替换。跨平台构建只证明可编译，不代替 Windows 原生进程生命周期验收。

## 操作经验

- 本机 `go` 位于 `/home/zhangxu/sdk/go/bin/go`，shell PATH 中找不到 `go` 时先查 SDK，不要误判为未安装。
- Go 测试缓存不会追踪被 shell 测试调用的外部安装脚本。改动 `install*.sh` 后要用 `go test -count=1 ./installer`，不能以 `(cached)` 结果为准。
- `list_targets` 的会话 ID 不是供 Agent 拼接的地址；返回数据现在包含完整 `address`，MCP `send_message` schema 也允许传 `from`。多 OpenCode 会话时必须提供本会话的 `from`，否则来源无法自动识别。
- 每次升级后以运行进程的可执行路径、PID 和构建提交核验；仅看磁盘二进制、版本字符串或任务启动命令的成功输出都可能误判。

## Linux Codex App 登录地区错误的诊断边界

2026-10-01 在 Ubuntu、App `26.924.22138` 上确认过以下组合：CLI 和网页可用，但 App 在浏览器授权返回后提示地区不符合条件。App 的 Chromium 使用 GNOME 系统代理，独立 Rust 后端却没有继承 shell 中的 `HTTP_PROXY` / `HTTPS_PROXY`。原生日志记录 `oauth token exchange returned non-success status`、HTTP 403 和 `unsupported_country_region_territory`；浏览器 callback 的 `state_valid=true`、`has_error=false`。不能把此错误自动归因于 RA2A。

按以下顺序收集证据：

1. 比较 App、其 bundled `codex app-server` 和正常 CLI 后端的**代理变量存在性**、实际连接路径。桌面系统代理设置不能证明原生子进程也使用代理。
2. 用 App `stdio_transport_spawned` 日志、父 PID 与 `/proc/<pid>/exe` 确认后端是否来自 App 自带路径，或确实经过 RA2A wrapper/managed host。
3. 在原生日志中区分浏览器 callback 失败、token 兑换失败与账号读取失败；按 App 后端 PID 和时间筛选，避免混入 CLI 的日志。当前安装的原生日志为 `~/.codex/logs_2.sqlite`，以只读方式查询；日志格式和位置需随版本核实。
4. 可对公开无凭据认证元数据端点作正常路径与直连对照，记录 HTTP 状态即可。公开 GET 的拒绝页不能替代 OAuth 错误 code 的证据。不要输出 OAuth code、token、Authorization 或完整认证文件。
5. 若确认 App 未继承用户正常 CLI 的现有网络环境，候选操作是保存当前工作、关闭 App 后从该 shell 重新启动，再由用户实际登录验证。本次已完成正常重启和后端路径验证，实际 OAuth 登录仍待用户操作；不能据此标记登录已恢复，不要删除 `auth.json` 或会话缓存。

本次进程树确认 App 直接启动 `/usr/lib/chatgpt/resources/codex`，不经过 RA2A wrapper；未发现 RA2A 产品代码写认证文件或调用登录/退出接口。共享后端可能自动刷新凭据，因此结论限于当前有证据的失败路径。官方认证缓存说明见 [OpenAI Docs](https://learn.chatgpt.com/docs/auth)。

### 本机入口修复与验证（2026-10-01）

- 在 `~/.local/share/applications/chatgpt.desktop` 新建同 ID 的用户入口，保留系统入口其余字段，只将 `Exec` 改为 `/usr/bin/env` 携带当前正常 CLI 的 `HTTP_PROXY`、`HTTPS_PROXY`、小写同名键和 localhost `NO_PROXY`，再执行原 `/usr/bin/chatgpt %U`。本机现有代理为 `http://127.0.0.1:7890`；不得把此地址当成所有设备的默认值。用户入口优先级依据 [Desktop Entry Specification](https://specifications.freedesktop.org/desktop-entry/latest-single/#desktop-file-id)。
- `desktop-file-validate` 通过。移除启动 GIO 进程的代理变量后，用该入口正常启动 App；原生后端仍携带代理并实际连接 `127.0.0.1:7890`。`Gio.DesktopAppInfo.new('chatgpt.desktop').get_filename()` 解析到用户入口，验证了后续菜单启动所用文件。本地 `getAuthStatus` 从约 15 秒缩短到 5–30 毫秒。
- App 已正常重启；RA2A 服务和正常 CLI daemon 的 PID 保持。认证文件与配置文件 metadata 保持，未复制、删除或直接改写凭据。
- 当前 App 的 durable 云端 WebSocket 是另一条 Node 网络路径，仍有 `open_timeout`。虽然运行时二进制包含 Node 环境代理功能，试用 `NODE_USE_ENV_PROXY=1` 后实测仍直连，已移除此无效开关；未改 ASAR、关闭 TLS 校验或改全局 DNS/代理。不能以本地账号读取恢复代表云端连接和实际登录均已验收。
- 实际登录验证需要用户查看已打开 App，必要时点击登录并完成浏览器授权；以 native 的成功 token 兑换、App 登录完成事件或用户实际使用结果为完成依据。当前尚未获得这项反馈。
- 回退该本机入口时，将新建用户文件移到带时间戳的 `.disabled` 备份名，使菜单恢复系统入口；保留文件便于再恢复。若 App 包升级改变了系统入口字段，应重新核对用户副本。
