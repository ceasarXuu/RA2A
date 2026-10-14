# Windows 侧 Codex CLI 适配验证清单

2026-09-28 Windows 实机执行结果见 [`windows-codex-cli-validation-evidence-2026-09-28.md`](windows-codex-cli-validation-evidence-2026-09-28.md)。

- 目标版本：`main` @ `dff78ec` 之后
- 依据：`docs/v0.0.15/experiments/codex-cli-v10.md`（Ubuntu 0.158.0 已通过）、`runbooks/codex-cli-isolated-daemon-experiment.md`
- 平台要求：Windows 11，**非提升** PowerShell（提升终端会导致 daemon detached 启动被拒）
- 关键风险：AF_UNIX 108 字节地址上限、daemon 自动挂接、Desktop 回归

## 0. 前置

```powershell
git pull
go version          # 需要 1.24.x
go test ./...       # 必须全绿再开始
codex --version     # 目标 0.158.0 或更高
```

记录基线：

```powershell
$env:CODEX_HOME.Length          # 必须 < 90，否则 W3 会失败
codex --version
$env:CODEX_HOME
```

`CODEX_HOME` 未设置时默认 `$HOME\.codex`。若路径过长，后续 W3 用短的 `CODEX_HOME` 复测。

## 1. 构建与自检

```powershell
go build ./...
.\installer\install.ps1 -CodexPath (Get-Command codex).Source
ra2a version
ra2a selftest --pin A1B2C3 --id win-node --name rog306
```

**通过标准**：`selftest=ok`，`go test ./...` 全绿。

## 2. 正式版 Desktop 回归（最高优先级，先确认没打破现有能力）

```powershell
Get-Process ra2a -ErrorAction SilentlyContinue
codex app-server daemon version        # 预期：未运行时命令失败（探测语义）
```

在 Codex Desktop 打开一个 session，从另一台已验证的机器（Ubuntu）向
`ra2a://win-node/<thread-id>` 发送一条带唯一 marker 的消息。

**通过标准**：Desktop 侧实时显示该 marker，且空闲消息只创建一次 turn、
执行中 follow-up 只 steer 一次。失败即停止，先修回归。

## 3. daemon 探测与 socket 路径

```powershell
codex app-server daemon version
```

**通过标准**：输出单个 JSON 对象，且 `status` 为 `running`，
`socketPath` 指向 `$CODEX_HOME/app-server-control/app-server-control.sock`，
`appServerVersion` ≥ `0.158.0`。

未运行时先启动一个 codex TUI 再复查。检查权限与真实路径：

```powershell
$sock = (codex app-server daemon version | ConvertFrom-Json).socketPath
Get-Item $sock | Select-Object FullName, LinkType, Target
$real = (Get-Item $sock).Target
(Get-Acl $real).Access | Select-Object IdentityReference, FileSystemRights
```

**通过标准**：真实 socket 仅当前用户可访问（Unix 语义下为 0600 等价），
`$real.Length` ≤ 107。

## 4. 零动作挂接（本次最关键项）

先确认 daemon 不在运行：

```powershell
codex app-server daemon stop
Get-Process codex -ErrorAction SilentlyContinue | Where-Object { $_.Path -like "*app-server*" }
Test-Path "$env:CODEX_HOME\app-server-control\app-server-control.sock"
```

然后**不带任何参数**启动 TUI，观察约 10 秒后另开一个 PowerShell 窗口：

```powershell
codex app-server daemon version
Get-Process codex | Select-Object Id, Path
```

同时确认 TUI 进程与 daemon 之间确有连接（`Get-NetTCPConnection` 不适用于
AF_UNIX，用 socket 占用情况判断）：

```powershell
$p = Get-Process codex | Where-Object { $_.Path -like "*app-server*" }
$p | Select-Object Id, StartTime
```

**通过标准**：
1. TUI 启动后 daemon 进程与 socket 自动出现（无需 `--remote`、无需 wrapper）。
2. `daemon version` 返回 `running`。
3. TUI 界面正常渲染，无 error boundary、无 "embedded" 回退提示。
4. 若出现静默回退到 embedded server（`daemon version` 仍失败），记录
   `$real.Length` 与 `CODEX_HOME` 长度，按 W3 处理。

**排除场景记录**：分别用下列参数各试一次，确认行为符合预期（这些都是
官方 daemon 自挂接的排除条件，RA2A wrapper 在这些场景才是兜底）：
`--no-daemon`、`-c model="x"`、`--profile p1`、`$env:CODEX_EXEC_SERVER_URL="..."`。

## 5. 免登录注入验证（用 mock 端点，不消耗账号额度）

在独立目录准备 mock 与配置（Linux 版流程见 runbook，Windows 用 PowerShell 启动 python）：

```powershell
$exp = "$PWD\.cache\win-verify"
New-Item -ItemType Directory -Force -Path "$exp\home\app-server-daemon" | Out-Null
'{"remoteControlEnabled":false,"shutdownGraceSeconds":10,"updater":{"autoUpdateEnabled":false,"updateIntervalMinutes":1440}}' |
  Set-Content "$exp\home\app-server-daemon\settings.json" -Encoding utf8
@'
model = "mock-model"
model_provider = "ra2a-mock"
model_reasoning_effort = "none"

[model_providers.ra2a-mock]
name = "RA2A Mock"
base_url = "http://127.0.0.1:8931/v1"
wire_api = "responses"
env_key = "RA2A_MOCK_KEY"
requires_openai_auth = false
'@ | Set-Content "$exp\home\config.toml" -Encoding utf8
```

启动 mock（脚本见 `.cache/v10/mock_responses.py`，随仓库分发到验证机器）：

```powershell
$env:RA2A_MOCK_KEY = "verify"
python .\mock_responses.py
```

另开窗口，用隔离 `CODEX_HOME` 启动 TUI：

```powershell
$env:CODEX_HOME = "$exp\home"
codex app-server daemon start
codex
```

在 TUI 里手动发送一条消息（文本与回车分两次敲），确认渲染出 `MOCK-REPLY-N`。
记录 TUI 自建 thread ID（`codex app-server daemon` 侧或 rollout 文件名）。

从第二个客户端注入：

```powershell
ra2a adopt-cli <thread-id>
ra2a restart
```

再从另一台机器向 `ra2a://win-node/<thread-id>` 发送带唯一 marker 的消息。

**通过标准**：
1. TUI 无登录越过门槛，完整渲染首轮 `MOCK-REPLY-N`。
2. RA2A 注入的 marker 在 TUI 实时显示，模型回复同样显示。
3. `list_targets` 中该 thread 的 `agent` 为 `codex-cli`。
4. daemon 日志出现 `cli_turn_delivered`，**没有** `cli_turn_accepted_unconfirmed`。
5. 出现 `cli_unsubscribed`（每次投递后主动退订）。

## 6. 活跃回合 follow-up

把 mock 改为 hang 模式（`MOCK_MODE=hang`、`MOCK_HANG_SECONDS=10`）重启 mock，
然后：先发一条消息触发长回合，回合执行中再注入第二条。

**通过标准**：两条消息在同一 thread 内各执行一次、顺序正确、TUI 无重复
turn、无永久 thinking，注入后仍可人工继续输入。

## 7. start_required 语义（PD29）

```powershell
codex app-server daemon stop
# 保持 RA2A 运行，从另一台机器向该 CLI thread 发消息
```

**通过标准**：调用方收到 `START_REQUIRED`，且 **RA2A 没有自动拉起 daemon**。
用 `Get-Process` 确认无新 app-server 进程。

## 8. 边界与降级

| 场景 | 预期 |
|---|---|
| 投递到未登记 thread | `TARGET_NOT_FOUND`，不写宿主 |
| 投递到非法 thread ID | `TARGET_UNSUPPORTED` |
| 断开 LAN 后投递 | `TARGET_UNREACHABLE`，恢复后可重试 |
| daemon 中途退出 | RA2A 不崩，`list_targets` 中 CLI 端点消失，`START_REQUIRED` |
| v0.0.14 旧节点与新节点混用 | 不误投、不崩溃；旧节点 session 缺 `agent` 字段按 Codex App 读取 |

## 9. 收尾

```powershell
codex app-server daemon stop
Get-Process codex | Where-Object { $_.Path -like "*$exp*" }   # 应为空
```

**回传模板**

```text
主机 / OS 版本：
codex 版本 / daemon appServerVersion：
CODEX_HOME 路径与长度 / 真实 socket 路径与长度：
go test ./... 结果：

W1 自检：PASS/FAIL
W2 Desktop 回归：PASS/FAIL（marker：）
W3 daemon 探测：PASS/FAIL（status / socketPath / 权限）
W4 零动作挂接：PASS/FAIL（daemon 是否自动出现 / 排除场景行为）
W5 免登录注入：PASS/FAIL（marker：/ 日志事件）
W6 活跃 follow-up：PASS/FAIL
W7 start_required：PASS/FAIL
W8 边界与降级：逐项结果
W9 收尾：PASS/FAIL

失败项原始输出与日志片段：
已知问题：
```

## 10. 判定

- W2 失败 → 阻止一切后续，先修 Desktop 回归。
- W4 失败 → 记录 `CODEX_HOME` 长度与是否静默回退 embedded；这是 Windows
  侧唯一已知可行性风险，需回到计划评审。
- W5/W6 失败 → 记录 `cli_*` 结构化事件与 marker，回传后修投递契约。
- W7 失败（RA2A 自动拉起了 daemon）→ 违反 PD29，阻止发布。
