# OpenCode CLI 共享会话与权限验证

适用范围：`opencode --ra2a`、`opencode --yolo --ra2a`、OpenCode wrapper 升级和卸载。

## 构建与检查

本机 Go SDK 位于 `/home/zhangxu/sdk/go/bin/go`；仅凭 shell 的 `PATH` 找不到 `go`
不能推断未安装 Go。先检查 SDK 路径，再执行：

```sh
/home/zhangxu/sdk/go/bin/go test ./...
/home/zhangxu/sdk/go/bin/go test -race ./cmd/oc-wrapper ./internal/opencode ./internal/ocsession ./internal/ochost ./installer
OPENCODE_TEST_INTEGRATION_BINARY=/home/zhangxu/.opencode/bin/opencode /home/zhangxu/sdk/go/bin/go test -race -count=1 -run TestNativeOpenCodeSessionAndPermissionReply ./cmd/oc-wrapper
GOOS=windows GOARCH=amd64 /home/zhangxu/sdk/go/bin/go build ./cmd/ra2a ./cmd/oc-wrapper
pwsh -NoProfile -Command '$tokens=$null; $errors=$null; [System.Management.Automation.Language.Parser]::ParseFile((Resolve-Path ./install.ps1), [ref]$tokens, [ref]$errors) > $null; if ($errors.Count) { $errors; exit 1 }'
```

## 运行时判断

- OpenCode 的会话存储是全局的；`GET /session` 中出现会话并不表示共享 server
  拥有该会话的执行权。只有存活的 `--ra2a` TUI 建立的附着租约，才发布为可投递端点；
  TUI 退出后不再发布。投递前再次检查租约，避免返回虚假的成功。
- wrapper 为新 TUI 在共享 server 创建会话，为 `--continue` 选择当前目录最近的会话，
  并通过 `attach --session` 固定会话。`--fork` 暂不接受，因为无法证明新会话的执行归属。
- `--yolo` / `--auto` 是当前附着会话的权限请求自动应答，不是修改整个 server 的权限。
  仅自动应答 `permission.asked` / `permission.v2.asked`；显式拒绝不会产生 ask 事件。
  检查 stderr 中的 `opencode_permission_auto_approved`（含 session/request ID）；
  `opencode_permission_reply_failed` 和 `opencode_permission_stream_dropped` 用于定位异常。
- wrapper 重建后，已经运行的 TUI/daemon 仍使用旧进程；需重启二者并确认实际 PID
  与可执行文件路径。不要在用户正在工作的 TUI 上进行自动重启。
- `ra2a stop` / `exit` 根据 owner 文件回收共享 server；TUI 退出不应移除 owner 文件。
- 安装脚本在同一目录发现原生 `opencode` 时保存为 `opencode.real`（Windows 为
  `opencode.real.exe`）；再次安装不覆盖备份；卸载恢复原生可执行文件并移除 RA2A
  的 MCP 条目。Windows 包装器正在运行时，替换的旧镜像会被重命名为 retired 文件。

## 本次发现及验证边界

2026-09-30 现场进程检查：共享 server 的两个 PID 没有 `OPENCODE_PERMISSION`，
而 `opencode --yolo --ra2a` 的 attach 子进程确实有 allow-all 环境变量。这证明旧
wrapper 把策略设置给了错误的进程。新版改用 server 暴露的按会话权限事件和 reply API；
本地 `httptest` 覆盖旧版及 v2 事件、其他会话不被自动批准；使用隔离 HOME/端口
启动真实 OpenCode 1.18.33 server 并发送 `external_directory` ask 的端到端
权限应答测试通过，跨平台构建检查通过。
本次未重启用户正在运行的 OpenCode TUI，因此现场权限提示是否消失须在用户主动重启
新版 wrapper 后核验。


## CLI↔OpenCode验收已验证经验（2026-10-04）

- `ra2a opencode`无子命令会启动/附着TUI，不能当只读状态查询。只读核验使用既有owner/lease文件、PID/监听归属及已知共享server的GET session/message/status/permission，不猜启动器命令。误启动可能留下空session及dead lease；保留证据，不擅自删历史会话。
- Windows wrapper PATH测试夹具应生成`opencode.exe`，与生产.exe/.cmd候选一致；无扩展名Unix fixture在Windows会得到空路径，并不证明生产解析错误。
- 空闲组单独观察ready后发消息；工作中样例应在工具内确认busy后只发一次，不依赖主Agent处理业务回信的速度命中短等待窗口。错过busy则保持零追加写入，新nonce独立样例另记，不能伪称活跃通过。
- OpenCode仅声明receiveText/replyAddress。实测工作中追加后assistant parent切到新user，原两次等待仍完成且无重复，双marker最终保留；报告原生parent/finish/time，不套用Codex同turn steer含义。
