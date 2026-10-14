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
  并通过 `attach --session` 选择启动会话。当前会话由客户端原生 TUI 插件的
  `api.route.current` 跟踪，resume/切换后原子更新 `attachment.<wrapperPID>` 租约。
  首页/其他 route 不发布端点，插件失联超过3秒撤销；不能把启动参数当当前选择。
  `--fork` 暂不接受，因为无法证明新会话的执行归属。
- `--yolo` / `--auto` 是当前附着会话的权限请求自动应答，不是修改整个 server 的权限。
  仅自动应答 `permission.asked` / `permission.v2.asked`；显式拒绝不会产生 ask 事件。
  检查 stderr 中的 `opencode_permission_auto_approved`（含 session/request ID）；
  `opencode_permission_reply_failed` 和 `opencode_permission_stream_dropped` 用于定位异常。
  自动应答也跟随当前 focus，不继续批准启动会话的请求。
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
- 重启后TUI内resume必须保持原native session ID对应的地址。旧wrapper仅登记startup session，导致恢复旧会话后消息进入隐藏的新会话；原有固定会话22轮测试不能覆盖这个场景。新wrapper将临时插件配置只传给attach子进程；已有OPENCODE_TUI_CONFIG复制到同目录以保留相对路径与原插件，原文件不改。该目录须可创建临时文件；解析/写入失败明确报错，不静默替换用户设置。新wrapper与新RA2A必须一起部署，旧TUI须由用户最后重开；禁止用唯一在线端点猜用户当前会话。
- 原生TUI插件接口在固定OpenCode1.18.33/1.18.34源码确认，Solid共享runtime在1.18.34隔离原生attach实测。模板只用内置runtime和node:fs；旧版或禁用该插件时不发布虚假startup端点。原生插件加载器仍可能按自己的策略安装既有.opencode目录依赖，部署验证应核对保护基线。
- Windows wrapper PATH测试夹具应生成`opencode.exe`，与生产.exe/.cmd候选一致；无扩展名Unix fixture在Windows会得到空路径，并不证明生产解析错误。
- 空闲组单独观察ready后发消息；工作中样例应在工具内确认busy后只发一次，不依赖主Agent处理业务回信的速度命中短等待窗口。错过busy则保持零追加写入，新nonce独立样例另记，不能伪称活跃通过。
- OpenCode仅声明receiveText/replyAddress。实测工作中追加后assistant parent切到新user，原两次等待仍完成且无重复，双marker最终保留；报告原生parent/finish/time，不套用Codex同turn steer含义。
