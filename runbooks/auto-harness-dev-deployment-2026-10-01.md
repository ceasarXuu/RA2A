# Harness 自动安装 dev 三节点部署记录（2026-10-01）

## 代码与范围

- 产品决策：`prd/2026-09-30-auto-harness-install.md`。
- 功能提交：`56264fb`（按已安装宿主接入、普通 OpenCode TUI 自动附着、地址与来源 MCP 契约）；安装提交：`08de6c409f572e2885877e711558920dee702814`（源码/Release 检测、wrapper 打包、升级卸载恢复）。
- 此次从 `main` 源码安装 **dev**，没有发布新正式 Release；正式 Release 下载器与发布流水线已更新，将在后续正式版本发布时生效。
- 保持现有 Codex/OpenCode TUI 不被强制终止；旧 TUI 只有在用户主动重开后才会加载新 wrapper。只重启各节点 RA2A daemon。

## ubuntu407：本机运行态

| 项目 | 证据 |
| --- | --- |
| 安装命令 | `PATH="/home/zhangxu/sdk/go/bin:$PATH" ./install.sh`，**没有 wrapper 参数** |
| 检测结果 | Codex CLI `/home/zhangxu/.local/bin/codex`、OpenCode `/home/zhangxu/.opencode/bin/opencode` |
| daemon | 旧 PID `2635686` → 新 PID `2748315`，systemd user `ActiveState=active` |
| 运行版本 | `/proc/2748315/exe` 与磁盘 `~/.local/bin/ra2a` SHA256 同为 `f2bccc7192503e3e8c4e4a3cdfa98b5c437649f4ac2c1e716f4bd07a14289ac7`，`go version -m` 报 `vcs.revision=08de6c4` / `vcs.modified=false` |
| wrapper | `~/.local/bin/codex` 和 `~/.local/bin/opencode` 的 `go version -m` 均为 `08de6c4`；原有自定义 Codex 启动脚本移至 `codex.bin`，安装前后 SHA256 都是 `54b67dadd227bddb30fd6414aacb4b4ed688a6e7ffe29d82655d924b758b20bf` |
| 冒烟 | `codex --version` → `codex-cli 0.155.1`；`opencode --version` → `1.18.33`；`config.json` 同时保留 Codex/OpenCode，OpenCode MCP `ra2a` 指向本次安装二进制 |

安装器一次构建全部要安装的 wrapper，再发布新的 RA2A 命令，避免 wrapper 构建失败造成半升级。安装时日志中的 `detected harness:` 可用于审计本机实际识别结果。daemon 重启后节点 `ready`，仍发布 1 个当前存活的 OpenCode 会话。

## rog306：Windows 会话回报

- 投递目标：`ra2a://rog306/01a05d7e-b958-7df2-bd67-6eba9636fad6`（“测试session消息注入”）；回报标记 `ROG306-AUTO-INSTALL-DONE`。
- `git pull --ff-only origin main` 至 `08de6c4`，执行 `powershell -NoProfile -ExecutionPolicy Bypass -File E:\RA2A\install.ps1`，无 wrapper 参数。自动检测：Codex 原生 `C:\Users\77585\AppData\Local\OpenAI\Codex\bin\ca9abb0b4d8ac692\codex.exe`，OpenCode 原生 `C:\Users\77585\AppData\Roaming\npm\opencode.cmd`；两个 launcher 均安装到 `.local\bin`。
- 旧 daemon PID `60412` 已退出；新 PID `35296`，实际运行 exe `vcs.revision=08de6c4`；`list_targets` rog306 `ready`、`sessionsStale=false`、287 会话。
- Codex/OpenCode 原生及 wrapper 的版本检查分别为 `0.159.0`、`1.18.33`；原生路径由 native-path 文件保存、旧 RA2A exe 退役备份。已有未跟踪 `.commandcode/` 保留（本机源码未被覆盖），Codex 会话未重启。

## macmini-m4：macOS 会话回报

- 投递目标：`ra2a://macmini-m4/01a0eebf-9e9b-7f61-bb50-df18279b9085`（“配置联调专用 session”）。
- 该机本地 `main` 与远端已经分叉，`git pull --ff-only origin main` 返回 `fatal: Not possible to fast-forward, aborting.`。为保留本地 5 个提交和未跟踪文件，远端会话另从远端 `main` 临时浅克隆，核对临时 HEAD=`08de6c4`，从临时检出运行 `./install.sh`（无 wrapper 参数）。**本地原项目 HEAD 仍为 `ffb52fd`，不要误写为已快进。**
- 安装器自动检测 Codex CLI 和 OpenCode，安装两个 launcher，只重启 RA2A daemon 一次。旧 PID `14581` → 新 PID `38770`；新进程实际映像 inode 与安装后 ra2a 一致，`go version -m` 显示 `vcs.revision=08de6c4`、`vcs.modified=false`。
- 本机 `/v1/targets` 与发送端均观察 macmini-m4 `ready`、`sessionsStale=false`、30 个会话；原生命令与 wrapper 版本分别一致：Codex CLI `0.155.1`、OpenCode `1.18.33`，Codex 原生入口由 `codex.bin` 保存。旧 Codex 会话未重启。

## 验证边界与经验

1. `go test -count=1 ./...`、相关 `-race`、`go vet ./...`、Linux/macOS/Windows 六种 Release 架构交叉构建通过；隔离原生 OpenCode 1.18.33 的权限 ask→reply 测试通过。`installer` 还验证了源码及校验下载两条安装入口、重复安装、OpenCode-only、Windows PowerShell 分支和卸载恢复。
2. Go 测试缓存不会追踪外部安装脚本；修改安装脚本后必须使用 `go test -count=1 ./installer`。Windows 隔离测试模拟了脚本分支，但**不能代替 Windows 真机进程核验**；rog306 本次回报提供了后者。
3. 不要用 `ra2a version`（目前仍为 `v0.0.15`）、磁盘文件时间戳或 mDNS 端口变化代替运行进程的 `vcs.revision`、PID 与映像校验。
4. macmini-m4 的本地分叉分支尚未被合并/清理；运行服务来自临时 clone 的正确提交。这是环境事实，不在本次部署中擅自修改用户 Git 历史。
5. 已运行的 OpenCode TUI 仍可能持有旧 wrapper/MCP 进程，普通 `opencode` 自动接入及新 `from` 工具 schema 的现场验收要在用户主动重开对应 TUI（必要时重启共享 server）后做，不能把 daemon 已升级当作 TUI 已升级。
