# RA2A v0.0.18 修复目标

## FIX-001：Windows Codex 控制目录权限兼容性

- 登记日期：2026-10-01。
- 状态：已纳入 v0.0.18 修复目标，待实现。本机权限已临时修复，RA2A 代码尚未修复。
- 影响：普通 Codex CLI 无法启动共享后台服务；用户输入停留在未发送草稿。

### 问题与已确认事实

在 Windows 上执行 `codex --yolo`，出现以下错误：

```text
Error: app server did not become ready on C:\Users\77585\.codex\app-server-control\app-server-control.sock
Error: socket directory is not private to the current user
```

本次环境为 Codex CLI `0.159.0`、托管 app-server `0.159.3`。检查发现 `app-server-control` 目录未禁用权限继承，包含其他账户与 `CodexSandboxUsers` 的访问权限；原访问规则均为继承规则。备份权限后，仅将该目录 ACL 收紧为当前用户 FullControl 并禁用继承，执行 `codex app-server daemon start` 成功，`daemon version` 返回 `status: running`。这确认了本次启动失败的直接原因是控制目录不符合 Codex 的私有权限要求。

RA2A 的[默认 socket 路径](../../internal/codexhost/owner.go)与官方 daemon 共用 `CODEX_HOME/app-server-control` 父目录；[受管宿主启动逻辑](../../internal/codexhost/host.go)及 owner lease 写入仅使用 `os.MkdirAll(..., 0o700)` 创建目录。本机 Go 的 Windows 实现调用 `CreateDirectory(..., nil)`，不能将 `0700` 转换为仅当前用户可访问的 ACL，因此 RA2A 存在目录权限兼容性缺口。

归因边界：尚无证据证明 RA2A 主动放宽过 ACL，或本次目录由 RA2A 首次创建。不能把“RA2A 直接改坏权限”作为已确认结论。

[2026-09-28 Windows 验证记录](../../runbooks/windows-codex-cli-validation-evidence-2026-09-28.md)的“发现与修复”第 4 项已记载同类错误，当时只收紧了隔离实验目录及其 `app-server-daemon` 子目录权限，正式目录未处理。

### 修复目标与范围

- 消除 RA2A 与官方 Codex 共用控制目录时的 Windows 权限兼容性缺口，保证 RA2A 启动、重启及升级后，普通 Codex CLI 仍可正常启动或接入官方 daemon。
- 覆盖首次创建目录和已有目录继承了额外权限两种场景；实施前核实目录创建与权限变化路径，再确定采用目录隔离还是最小范围的权限处理。
- 保留 Codex 配置、登录状态、会话历史及已有 socket 所有权边界；不递归修改整个 `CODEX_HOME` 的 ACL，不停止或清理非 RA2A 所有的进程与文件。
- `--no-daemon` 可作临时绕行，不作为修复完成标准。

### 验收条件

1. **RA2A 先启动**：在具有额外继承权限的隔离 `CODEX_HOME` 中，先启动 RA2A，再执行普通 `codex` / `codex --yolo`；官方 daemon 正常启动，TUI 可输入，无上述权限错误。
2. **已有目录与升级**：复现控制目录已经存在且继承其他账户权限的环境，验证修复后的 RA2A 启动、重启及升级路径；官方 `daemon version` 返回 `running`，无需手工修 ACL。
3. **官方 daemon 先启动**：先启动官方 daemon，再启动和重启 RA2A；官方 daemon 与 RA2A 的受管宿主均保持各自所有权，既有 Codex 会话仍可继续使用。
4. **边界回归**：已满足私有权限要求的 Windows 环境继续正常工作；macOS/Linux 的相关宿主生命周期检查通过，配置与会话数据保留。
5. **证据交付**：记录验证使用的 CLI/app-server 版本、启动顺序、目录 ACL 与 daemon 状态，并更新现有 Windows runbook；当前本机临时修复不能替代上述复现与验收。
