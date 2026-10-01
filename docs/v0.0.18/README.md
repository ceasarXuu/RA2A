# RA2A v0.0.18 修复目标

## FIX-001：Windows Codex 控制目录权限兼容性

- 登记日期：2026-10-01。
- 状态：代码已实现，Linux 回归及 Windows 交叉编译通过；Windows 实机验收待完成。此前 Windows 本机临时权限修复不能代替本次验收。
- 影响：普通 Codex CLI 无法启动共享后台服务；用户输入停留在未发送草稿。

### 问题与已确认事实

在 Windows 上执行 `codex --yolo`，出现以下错误：

```text
Error: app server did not become ready on C:\Users\77585\.codex\app-server-control\app-server-control.sock
Error: socket directory is not private to the current user
```

本次环境为 Codex CLI `0.159.0`、托管 app-server `0.159.3`。检查发现 `app-server-control` 目录未禁用权限继承，包含其他账户与 `CodexSandboxUsers` 的访问权限；原访问规则均为继承规则。备份权限后，仅将该目录 ACL 收紧为当前用户 FullControl 并禁用继承，执行 `codex app-server daemon start` 成功，`daemon version` 返回 `status: running`。这确认了本次启动失败的直接原因是控制目录不符合 Codex 的私有权限要求。

RA2A 的[默认 socket 路径](../../internal/codexhost/owner.go)与官方 daemon 共用 `CODEX_HOME/app-server-control` 父目录；修复前，[受管宿主启动逻辑](../../internal/codexhost/host.go)及 owner lease 写入仅使用 `os.MkdirAll(..., 0o700)` 创建目录。本机 Go 的 Windows 实现调用 `CreateDirectory(..., nil)`，不能将 `0700` 转换为仅当前用户可访问的 ACL，因此 RA2A 原实现存在目录权限兼容性缺口。

归因边界：尚无证据证明 RA2A 主动放宽过 ACL，或本次目录由 RA2A 首次创建。不能把“RA2A 直接改坏权限”作为已确认结论。

[2026-09-28 Windows 验证记录](../../runbooks/windows-codex-cli-validation-evidence-2026-09-28.md)的“发现与修复”第 4 项已记载同类错误，当时只收紧了隔离实验目录及其 `app-server-daemon` 子目录权限，正式目录未处理。

### 修复目标与范围

- 消除 RA2A 与官方 Codex 共用控制目录时的 Windows 权限兼容性缺口，保证 RA2A 启动、重启及升级后，普通 Codex CLI 仍可正常启动或接入官方 daemon。
- 覆盖首次创建目录和已有目录继承了额外权限两种场景；实施前核实目录创建与权限变化路径，再确定采用目录隔离还是最小范围的权限处理。
- 保留 Codex 配置、登录状态、会话历史及已有 socket 所有权边界；不递归修改整个 `CODEX_HOME` 的 ACL，不停止或清理非 RA2A 所有的进程与文件。
- `--no-daemon` 可作临时绕行，不作为修复完成标准。

### 实现与验证进展（2026-10-01）

- 受管宿主启动、自动重启和 owner lease 写入共用平台目录准备逻辑。Unix 保持既有 `0700` 创建行为；Windows 首次创建直接传入与官方 Codex `0.159.3` 等价的当前用户 owner 与 protected、单条 OICI FullControl DACL。
- 已有目录必须是当前用户拥有的真实目录；拒绝普通文件与 reparse point，保留其他用户的 owner。已满足官方私有权限契约时不写 ACL，因此可与官方 daemon 持有的目录保护句柄共存。
- 已有坏 ACL 仅调整该目录的 DACL。修复句柄使用 `MAXIMUM_ALLOWED`，依据 [Microsoft SetSecurityInfo 文档](https://learn.microsoft.com/en-us/windows/win32/api/aclapi/nf-aclapi-setsecurityinfo)避免将权限变化传播到现有子对象；不设置 owner、SACL，不移动或删除既有 socket。
- 已新增 5 项 Windows 专用测试：首次创建与官方式目录保护句柄共存；坏继承 ACL 修复、重复执行及父目录/子文件/兄弟文件权限与内容保留；非目录拒绝；reparse point 拒绝；owner 不匹配拒绝。Linux `go test ./internal/codexhost` 通过，Windows amd64 测试二进制交叉编译通过。
- Wine 11.0 的独立临时 prefix 运行尝试在初始化阶段超时，未产生测试结果；不计入 Windows 验收，也未因此引入生产特殊处理。验证入口及证据格式见 [Windows 验证记录补充](../../runbooks/windows-codex-cli-validation-evidence-2026-09-28.md#fix-001-实现验证补充2026-10-01)。

官方权限依据：[0.159.3 目录创建源码](https://github.com/openai/codex/blob/rust-v0.159.3/codex-rs/uds/src/windows_security.rs)、[既有目录校验源码](https://github.com/openai/codex/blob/rust-v0.159.3/codex-rs/uds/src/windows_socket_validation.rs)。Windows 实机仍须执行下列五项验收，完成前不标记 FIX-001 fixed。

### 验收条件

1. **RA2A 先启动**：在具有额外继承权限的隔离 `CODEX_HOME` 中，先启动 RA2A，再执行普通 `codex` / `codex --yolo`；官方 daemon 正常启动，TUI 可输入，无上述权限错误。
2. **已有目录与升级**：复现控制目录已经存在且继承其他账户权限的环境，验证修复后的 RA2A 启动、重启及升级路径；官方 `daemon version` 返回 `running`，无需手工修 ACL。
3. **官方 daemon 先启动**：先启动官方 daemon，再启动和重启 RA2A；官方 daemon 与 RA2A 的受管宿主均保持各自所有权，既有 Codex 会话仍可继续使用。
4. **边界回归**：已满足私有权限要求的 Windows 环境继续正常工作；macOS/Linux 的相关宿主生命周期检查通过，配置与会话数据保留。
5. **证据交付**：记录验证使用的 CLI/app-server 版本、启动顺序、目录 ACL 与 daemon 状态，并更新现有 Windows runbook；当前本机临时修复不能替代上述复现与验收。


## Codex CLI 实现缺陷修复（2026-10-01）

以下缺陷先通过隔离诊断复现，再实现局部修复；当前为源码回归通过，尚未发布 v0.0.18，也未部署到本机正式服务。

| 缺陷 | 修复结果 | 验证范围 |
| --- | --- | --- |
| 已登记 CLI 被共享 App 历史覆盖，走错写入通道 | App 枚举排除 CLI 成功登记的 IDs；CLI 未加载也不回退 App | 真实 Registry 的 Lookup/Deliver、buildRegistry 接线、未登记 Desktop 与无效登记边界。提交 `e7aa4ae`。 |
| CLI 输入只含正文，丢失来源和消息 ID | start/steer 统一使用 `RenderIncomingText` | 捕获实际 WebSocket 输入并核对完整包络。提交 `dd02ee1`。 |
| 终态先于提交响应到达，确认被丢弃 | 短期保留提前到达的终态 | fake 宿主先通知、后 response；移除原固定 30ms 延迟。提交 `dd02ee1`。 |
| 同一 turn 多等待者互相覆盖 | 共享终态并广播确认 | completed/failed、单等待取消、Close 释放、重复通知及过期保留；验证限定于等待器层。提交 `dd02ee1`。 |
| 断线后一直复用失效 RPC | 后续操作重建连接；不重放不确定写入 | 断线后恢复端点、已提交写入仅发生一次且返回 unknown；同时修正通知 handler 同步。提交 `dd02ee1`。 |

本次验证：

- `go test -race -count=1 ./internal/codexcli ./internal/codexapp ./internal/agentbridge ./cmd/ra2a ./cmd/codex-wrapper` 通过。
- `go test -count=1 ./internal/codexhost ./internal/appserverprobe ./internal/lannode ./internal/control ./internal/mcpserver ./internal/operator` 通过。
- Windows arm64、Darwin arm64 的 `ra2a` / `codex-wrapper` 交叉构建通过；Windows amd64 ACL 测试编译通过。均不替代目标平台原生运行。

上述回归不覆盖真实宿主的并发订阅生命周期、跨设备 App/CLI 四方向互通、20+ 多轮、人工继续和恢复矩阵。[PD31 与原互通验收](../v0.0.15/engineering-plan.md)仍需现场完成，不能将局部缺陷修复等同于 CLI 正式支持准入。

## 本机 Codex App 地区登录错误调查（2026-10-01）

- 环境：Ubuntu，App `26.924.22138`，bundled Codex `0.158.0-alpha.2.1`；正常 CLI / 官方 daemon 为 `0.159.3`。
- 直接失败证据：浏览器 OAuth callback 成功，App 自带原生后端随后三次在 token 兑换收到 HTTP403、`unsupported_country_region_territory`。因此拒绝发生在后端兑换阶段。
- 网络对照：App Chromium 使用 GNOME 的现有 localhost 代理，原生后端没有继承 HTTP(S) proxy 环境；CLI 后端使用该现有代理。公开无凭据认证元数据 GET 经现有代理为 HTTP200、直连为 HTTP403，但 GET 拒绝页本身不作为 OAuth 地区 code 的替代证据。
- RA2A 归因边界：App 直接启动自己的 bundled 后端，进程树和启动日志不经过 RA2A wrapper/managed host；未发现 RA2A 产品代码写认证文件或调用登录/退出接口的因果证据。当前证据支持 App 原生网络环境差异，而非 RA2A 启动接管。
- 网络状态：已新建用户级桌面入口，原生后端使用现有代理，本地账号读取从约15秒恢复到5–30毫秒。Node 环境代理开关无效并已移除；App 入口加载用户目录的标准 TCP 代理库后，真实 durable WebSocket 初始化成功、状态 connected。默认线程 DNS 模式未通过 zygote 启动对照，已回退为验证成功的 TCP-only 配置，保留完整沙箱。
- **整体任务仍未解决**：用户发送消息仍卡启动，未见对应 `turn/start`。真实 App 的 Git 检查子进程成功 exit 0，但 App 内部仍将其判为不可用；错误早于代理修复，不能据标签认定 Git 缺失或 RA2A 侵入。临时 debug/进程跟踪已移除，恢复正常入口启动。
- 版本边界：系统仍为 `26.924.22138`、安装文件校验正常。官方新版 `26.928.31416` 已下载校验并仅提取进行只读对比，相关本地 Git/RPC 逻辑没有针对性修复证据，尚未升级或重装。未直接改写凭据、删除缓存、改 App 包源码或重启正式 RA2A/CLI 服务。

复用诊断步骤见 [Harness runbook](../../runbooks/auto-harness-install.md#linux-codex-app-登录地区错误的诊断边界)。
