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
Codex CLI 有健康的官方共享 daemon 时，自动安装的 wrapper 仍让普通 TUI 走原生启动；仅官方 daemon 不可用且 RA2A managed socket 可用时注入 managed 连接，并记录 `codex_wrapper_managed_fallback` 日志。
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
