# Pi 原生扩展接入与隔离验证

## 接入边界

Pi 1.0 的公开扩展 `sendUserMessage()` 是异步提交入口，不返回原生队列确认。RA2A 的确认表示 **Pi 进程内专属扩展明确收到**：目标身份已核对，原生 session entry tree 中已存在 `ra2a-received` 记录。模型处理另行进行；工作中采用 followUp，不取消原工作。新会话记录可能由 Pi 延迟落盘，不承诺断电耐久或自动重放。

原生 TUI 展示收到标记；模型可见输入仍使用统一 `[RA2A message]` 来源和 message-id。原生输入被其他扩展拦截、认证失败等不改写已有收件结果。声明能力仅 receiveText/replyAddress；尚未声明 steerActiveTurn/interactiveSafe。

## 安装与关闭

- RA2A setup/restart 的安装流程检测 Pi，写入 `~/.pi/agent/extensions/ra2a.js`；`PI_CODING_AGENT_DIR` 可覆盖 agent 目录。
- 正常 `pi` 启动加载该文件；`ra2a pi` 是显式安装并启动入口。没有替换官方 pi，也不修改 settings/auth/models/MCP 配置。
- 同名非 RA2A 文件拒绝覆盖；重复安装同一内容保持文件不动。更新只替换本产品拥有的专属文件。
- `ra2a pi-unregister` 只移除产品拥有的扩展；安装脚本卸载及 `ra2a exit` 调用归属保护的移除路径。已运行的 Pi 不被强制关闭，扩展随该进程退出释放。
- **升级边界：**安装脚本运行新 RA2A 的 setup/restart 时更新插件已接线；旧版本自身执行 `ra2a update` 的进程仍是旧程序，尤其 Windows 延迟替换仅重启计划任务，不能据此保证新插件已安装/更新。首个升级必须用安装脚本或在替换后调用新二进制 `ra2a restart`；自更新全链路仍列为后续验收项，不宣称已通过。

## 实例所有权

每个 Pi 进程在私有目录创建 `attachment.<pid>.json`，包含当前原生 session ID、loopback URL、随机鉴权 token、状态与三秒有效期；每秒刷新。文件替换跟随原生 new/resume/reload 生命周期，shutdown 撤回。异常退出最多等待三秒到期；同 session 多条有效记录时适配器拒绝模糊路由。发现与发送之间发生切换时，接收端再次核对目标 session，拒绝旧目标。

记录目录默认 `~/.config/ra2a/pi-sessions`，测试可用 `RA2A_PI_SESSION_DIR`。Token 不出现在目标列表或模型工具结果中。通道只允许 IPv4 loopback，适配器不使用 HTTP 代理、不跟随重定向。Mac/Windows 真实权限与生命周期尚待各平台原生验收。

## 隔离原生测试

单元验证：

```sh
go test -race ./internal/pi ./internal/agentbridge ./internal/operator ./cmd/ra2a
```

原生 SDK + Go Adapter + Unix 原生 TUI，先在测试进程设置独立 HOME/USERPROFILE/APPDATA/LOCALAPPDATA/CODEX_HOME、所有 XDG 目录、PI_CODING_AGENT_DIR、RA2A_PI_SESSION_DIR；保留既有编译缓存，使用假凭据和内存 mock provider：

```sh
RA2A_PI_TEST_PACKAGE=/absolute/path/to/@earendil-works/pi-coding-agent \
RA2A_PI_TEST_BINARY=/absolute/path/to/pi \
GOPROXY=off go test -race -count=1 -run TestNativePiExtension -v ./internal/pi
```

`RA2A_PI_TEST_PACKAGE` 未设置时原生测试明确 skip。Windows 此测试运行 SDK 部分，不运行 Unix Python PTY 部分，不能冒充 Windows TUI 验收。不能执行裸 `pi` 或登录命令作为版本查询。

已验证的 Linux 1.0 fixture 包含：真实插件与 Adapter 确认、22 条独立用户输入、模型挂起时接收后 followUp、无凭据时插件收到而原生执行失败、来源身份、错误鉴权、错会话拒绝、真实 TUI 收到显示、切换/resume 和异常退出租约到期。Go 单元另覆盖矛盾/缺失 ACK 不重放、取消/断连、重复 owner、非 loopback、重定向、安装/卸载归属保护。

## 发布前剩余验收

Pi ↔ Codex App / Codex CLI / OpenCode / Pi 全交叉、真实 Mac/Windows 原生行为及私有权限、双真实 Pi 实例同会话竞争、升级全链路，以及 Owner 的可见/人工继续操作尚未完成。收件 ACK、模型回复和执行结束分别保存证据。未完成这些门槛不得列为正式支持。

## Windows SDK 夹具启动诊断

Windows 的动态 `import()` 不能直接使用 `C:\...` 文件路径；夹具统一以 Node `pathToFileURL()` 转换所有绝对模块路径。子进程启动失败后，应先终止本测试拥有的子进程并 `Wait()`，待 stderr copier 完成后再读取诊断缓冲区，避免错误报告自身产生 race。

ROG 的 Pi 1.0.2 首轮四包 race 通过，但显式原生 SDK 测试在上述夹具启动路径失败，尚未进入收件验证，不能作为 1.0.2 兼容证据。正式 App 活动期间 `.codex/models_cache.json` 发生变化，来源未归属；保留原差异，不宣称整个保护基线完全不变。修正夹具后使用新绑定提交独立复验，不部署失败产物。

## 自动加载入口

Pi 1.0 原生全局扩展目录自动扫描 `.js` / `.ts`，不扫描 `.mjs`。专属安装文件为 `ra2a.js`；仓库中的嵌入源文件 `extension.mjs` 不需要改名。原生 SDK 及 PTY 夹具使用真实 Install 产物的默认发现路径，避免用显式 `--extension` 掩盖安装缺陷。早期部署的 `ra2a.mjs` 可保留为证据，它不会自动加载；不得仅因后缀相同删除用户其他文件。
