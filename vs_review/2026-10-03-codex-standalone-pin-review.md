# Subagent VS Review: Codex 自更新安装布局优先（Windows 安装器 harness 检测）

- Created: 2026-10-03T01:45:00+08:00
- Updated: 2026-10-03T02:10:00+08:00
- Report schema: adversarial-v1
- Task: 让 `codex update` 不再报 `Could not detect the Codex installation method`，并消除 RA2A launcher 把 Codex 钉在 App 版本目录上的根因
- Report path: `vs_review/2026-10-03-codex-standalone-pin-review.md`
- Review mode: fresh internal subagents
- Source session policy: no inherited main-agent context
- Status: passed

## Round 1: 安装器 Codex 检测改动对抗审查

### Review Input

#### Objective

RA2A 在 `~/.local/bin` 安装 `codex` launcher，并把所代理的原生 Codex 路径记录在 `.ra2a-codex-native-path`。本机该记录指向 Codex 桌面 App 的哈希版本目录 `%LOCALAPPDATA%\OpenAI\Codex\bin\<hash>\codex.exe`（`0.159.0`）。由于 Codex 只有 standalone managed install、npm shim、Homebrew formula 能自我更新，经该 pin 执行 `codex update` 必然失败，且 App 每次更新轮转该目录导致 pin 静默失效。改动目标：安装器自动检测优先选择 standalone 安装，显式 `-Codex` / `--codex` 仍然优先。

#### Review Target

安装器 harness（Codex CLI）检测代码与测试。

#### Target Locations

- `E:\RA2A\install.ps1`
- `E:\RA2A\install-remote.ps1`
- `E:\RA2A\install.sh`
- `E:\RA2A\install-remote.sh`
- `E:\RA2A\installer\powershell_harness_test.go`
- `E:\RA2A\installer\harness_install_test.go`
- `E:\RA2A\installer\install_test.go`
- `E:\RA2A\runbooks\auto-harness-install.md`
- `E:\RA2A\cmd\codex-wrapper\main.go`（`realCodex` 读取 native-path）

#### Change Introduction

四个安装器的自动检测链路前增加一条 standalone 安装优先规则，并新增两条安装器测试。

#### Risk Focus

- 无 standalone 安装的机器、以及只有 npm/Homebrew/App 内置 CLI 的机器是否回归
- 与既有“重复安装保留已记录 Codex 路径”、`.ra2a-codex-native-path`、`codex.bin` 保存/还原、卸载还原路径的交互
- 是否会静默覆盖用户刻意选择的安装，以及是否被文档记录
- `set -eu` / `$ErrorActionPreference = 'Stop'` 安全性、未绑定变量、`Join-Path` 分隔符、junction/symlink 的 `Test-Path` 行为、`CODEX_HOME`
- daemon 侧（`internal/codexcli`、`ra2a setup`、`~/.config/ra2a/config.json`）是否需要配套改动
- 测试真实性：是否真正走生产代码路径

#### User-Perspective Review Focus

安装后 `codex` 是否可用；失效是否可恢复；用户能否察觉这次替换；文档契约是否仍然成立。

#### Implementation Completeness Focus

四个安装器的优先级是否一致且可达；显式覆盖是否仍然生效；daemon 与 wrapper 是否指向同一二进制；测试与运行时证据。

#### Assumptions To Attack

- standalone 安装位于 `$CODEX_HOME/packages/standalone/current/bin/codex[.exe]`
- 只有 standalone/npm/Homebrew 支持 `codex update`
- 重复安装可以覆盖既有 native-path 记录
- marker 文件是原生二进制的唯一真实来源

#### Adversarial Lenses

failure、state/invariant、回归既有文档行为、跨平台一致性、测试真实性、可维护性。

#### Verification Status

- Windows 上 `go vet ./installer`、`go test -count=1 ./installer` 通过；新增测试与邻居一样只在 Linux 执行。
- 出货的三段 PowerShell 行已在 Windows PowerShell 5.1 隔离 HOME 下以 4 个用例直接执行通过。
- 本机无 `sh`/WSL，POSIX 改动未在任何环境执行。

#### Reviewer Instructions

- 全新内部 subagent 会话，不继承主 agent 上下文
- 直接读取目标文件与 diff，不修改任何文件
- 以 `path:line` 给出证据
- 每个阻塞/重要发现 inline 给出：被打破的假设、失败场景、触发条件、影响范围、需要的证明

### Reviewer Timeout Policy

| Complexity | Initial Wait | Extension | Max Attempts Per Role | Blocking Closure Behavior |
|---|---:|---:|---:|---|
| normal | 10 分钟 | 无 | 2 | 不可用即不得判通过 |

### Reviewer Selection

| Reviewer | Reason Selected | Risk Area |
|---|---|---|
| general（对抗审查员） | 改动跨四个安装器与两套测试夹具，且平台语义差异是主要风险来源，需要能直接读仓库并运行命令的通用审查能力 | 回归、优先级不一致、测试真实性、跨平台语义 |

### Reviewer Launch Records

| Reviewer | Internal Mechanism | Session / Job ID | Trace Source | Context Forked | Input Packet | Context Explicitly Excluded | Read-only |
|---|---|---|---|---|---|---|---|
| general（对抗审查员） | 内部 subagent（task 工具，subagent_type=general） | `ses_f025042ddffeGXhTyh9iukOOqC` | task 调用返回的 task id 与完整报告输出 | fork_context=false | Round 1 Review Input | 主 agent 对话历史、推理过程、被回退的中间版本、失败尝试、结论 | yes |

### Reviewer Timeout Records

| Reviewer Output Key | Reviewer Role | Attempt | Session / Job ID | Waited | Status | Reason | Action |
|---|---|---:|---|---:|---|---|---|
| general-adversarial-1 | general（对抗审查员） | 1 | `ses_f025042ddffeGXhTyh9iukOOqC` | 约 6 分钟 | completed | 一次返回完整结构化报告 | completed |

### User Decision After Failed Review

- 不适用（首轮即完成）

### Reviewer Outputs

#### general-adversarial-1

##### Summary

审查员无法确认改动正确，给出 4 项阻塞发现：POSIX 上新规则在已有 RA2A 安装后不可达（B1）；wrapper pin 与 daemon `config.Codex` 在常规升级路径上分歧且无对账（B2）；新增 PowerShell 测试在 Linux CI 上因 `Join-Path` 分隔符语义可能变红（B3）；standalone 布局假设未经证实且仓库内自相矛盾（B4）。另有 9 项非阻塞风险与 9 条必修项。

##### Blocking Findings

- B1 POSIX 上 standalone 优先在已有 RA2A 后不可达
  - Broken assumption: 把 standalone 探测放在 marker/`codex.bin` 块之前即可生效
  - Failure scenario: `install.sh`/`install-remote.sh` 的 marker 块是**无条件赋值** `CODEX_NATIVE=$BIN_DIR/codex.bin`，覆盖新探测；已有安装永远保留旧 pin
  - Trigger condition: `~/.local/bin/.ra2a-codex-wrapper` 存在（即任何第二次安装）
  - Impact: 全部已有 macOS/Linux 安装无法被该改动修复；`runbooks/auto-harness-install.md:14` 的“重复安装会重新检测”承诺在 POSIX 上不再描述新增行为
  - Proof needed: 无，纯静态可判
- B2 wrapper pin 与 daemon 配置分歧
  - Broken assumption: 只改 marker 文件即足够，实际存在两处“原生 Codex”记录
  - Failure scenario: 无参数重跑安装器后 marker 指向 standalone，而 `~/.config/ra2a/config.json` 仍记 App bin；App 更新删除该 bin 后 `findCodex()` 通过 PATH 解析到 RA2A wrapper 自身
  - Trigger condition: 无参数重跑 + Codex App 更新
  - Impact: 托管 app-server 与 TUI 走不同二进制；`findCodex()` 没有 wrapper 自排除（`cmd/codex-wrapper/main.go:196` 有）
  - Proof needed: 确认无参数重跑只写 marker 不改 config；确认 `harness.go` 无 standalone 探测
- B3 新增 PowerShell 测试可能在 Linux CI 失败
  - Broken assumption: Windows 分隔符脚本在 `pwsh` on Linux 行为一致
  - Failure scenario: `Join-Path $HOME/.codex` + `packages\standalone\current\bin\codex.exe` 在 Linux 上拼出含反斜杠的单文件名，`Test-Path` 为 False，marker 被写回 App pin，测试断言失败
  - Trigger condition: 任意 Linux CI 运行（测试只在 Windows skip）
  - Impact: 发布流水线变红
  - Proof needed: 对比 `Join-Path` 两种写法的输出
- B4 standalone 布局与平台后缀假设未经证实且仓库内自相矛盾
  - Broken assumption: standalone 位于 `<CODEX_HOME>/packages/standalone/current/bin/codex[.exe]`
  - Failure scenario: 若真实布局为 `…/current/codex`（无 `bin/`），探测永不命中，改动成为静默空操作
  - Trigger condition: 任一 Codex 版本布局变化
  - Impact: 整个改动静默失效
  - Proof needed: 真实 Windows/macOS 安装的目录清单

##### Non-blocking Risks

- R1 静默覆盖用户既有选择且无日志（`install.ps1:126` 等处都有明确输出的惯例）
- R2 `runbooks/auto-harness-install.md:9,14` 契约未更新
- R3 `install-remote.sh` 把 `--codex` 转发给 setup 却不用它做检测
- R4 POSIX 侧 pin 已是指向可轮转指针的符号链接，改动无实证收益
- R5 `install-remote.sh` 与其余三个文件对 App 轮转行为的描述互相矛盾
- R6 新增 Unix 测试只覆盖全新安装，无法发现 B1
- R7 `.exe` 硬编码在会被非 Windows 执行的脚本里
- R8 `codex update` 能力本身从未被测试或演练
- R9 工作区存在未跟踪的 `.commandcode/taste/taste.md`

##### User-Perspective Checks

- Usability: risk（B1、R1）
- Ease of use: pass（四个安装器都打印解析到的 native 路径）
- Ease of understanding: finding（R2；B2 另有三套优先级并存）

##### Implementation Completeness Checks

| Plan Item | Expected Behavior | Production Code Path | Integration Entry | Test Evidence | Runtime / Log Evidence | Mock / Stub Exposure | Status | Finding Link |
|---|---|---|---|---|---|---|---|---|
| Windows 源码安装器优先 standalone | 覆盖 recorded pin/`codex.bin`/PATH/App 探测 | `install.ps1:28-40,98-105` | `install.ps1 -Pin …` | `installer/powershell_harness_test.go:95-168`（仅 Linux） | 无 | 服务命令被桩替换 | partial（B3、B4） | B3, B4 |
| Windows 发布安装器优先 standalone | 同上 | `install-remote.ps1:79-100` | `install-remote.ps1 -Version …` | 无 | 无 | HTTP fixture | not-started | B4 |
| POSIX 源码/发布安装器优先 standalone | 重复安装后仍生效 | `install.sh`、`install-remote.sh` | — | 已回退 | 无 | — | not-started（按 R4 撤销） | B1, R4 |
| 显式覆盖优先 | `-Codex` 胜过 standalone | `install.ps1:98-105` | 四个安装器 | 新测试第二段 | 本机 5.1 实测 | — | landed | — |
| wrapper/daemon 指向同一二进制 | 单一真实来源 | `cmd/codex-wrapper/main.go:167-201`、`internal/operator/harness.go:13-44` | `ra2a restart` | 仅 wrapper 侧 | 本机 config 记的是 launcher | — | partial | B2 |
| 文档契约更新 | runbook 说明新优先级 | `runbooks/auto-harness-install.md` | docs | — | 本机实测证据已记录 | — | landed | R2 |

##### Required Fixes

- 消除优先级冲突：要么让 POSIX 探测真正可达，要么撤销 POSIX 改动并说明其已无同类失效
- 让 daemon 与 wrapper 对齐，或证明 `config.Codex` 记 launcher 是收敛设计
- 让新测试不依赖 `Join-Path` 分隔符语义，或在 Linux 上跑通
- 替换已有 pin 时打印旧路径与新路径
- 核实并在 runbook 记录 standalone 布局，或同时探测 `…/current/bin/codex[.exe]` 与 `…/current/codex[.exe]`
- 更新 runbook 契约

##### Missing Tests

- 四个安装器的“显式覆盖胜过 standalone”
- `CODEX_HOME` 指向非默认位置
- POSIX 重复安装场景
- `install-remote.ps1` 的 standalone 覆盖
- daemon 与 wrapper 一致性
- `findCodex()` 解析到 wrapper 自身的自排除

##### Missing Logs / Observability

- 无任何位置记录“已记录 pin 被替换”及替换来源
- 安装输出不区分“为何选中 standalone”
- `DetectHarnesses` 静默替换 `config.Codex` 无记录
- 无 standalone 布局探测失败的可观测信号

##### Evidence

- `install.sh:106-108`、`install-remote.sh:83-85`：marker 块无条件赋值（B1）
- `internal/operator/harness.go:13-24`：`findCodex()` 仅 LookPath + macOS App（B2）
- `internal/operator/operator.go:249-255`：`Restart()` 不重解析 Codex（B2）
- `cmd/codex-wrapper/main.go:196`：wrapper 有自排除，daemon 侧没有（B2）
- `installer/powershell_harness_test.go:95-168`：新测试仅 Windows skip（B3）
- `docs/v0.15/experiments/codex-cli-v9.md:67`：仓库内 `current/codex` 无 `bin/` 的记录（B4）

### Main Agent Response

| Reviewer | Finding | Broken Assumption / Failure Scenario | Severity | Decision | Evidence / Reason | Action Taken | Follow-up |
|---|---|---|---|---|---|---|---|
| general | B1 | POSIX marker 块无条件赋值使新探测不可达 | blocking | accept | `git checkout -- install.sh install-remote.sh` 已回退 POSIX 改动；POSIX 侧无可复现失效（R4） | 撤销 POSIX 改动及其测试，四个安装器不再出现三套优先级 | runbook 记录“不做 POSIX 偏好”的理由 |
| general | B2 | marker 与 `config.Codex` 两处记录 | blocking | reject | 本机 `config.json` 的 `codex` 即 launcher 本身，托管 host 每次启动重读 marker；marker 路径消失时 `DetectHarnesses` 清空后经 PATH 回到 launcher，`realCodex` 有自排除（`main.go:196`），不存在自引用死循环。残留“旧绝对路径冻结版本”是既有行为且有界 | 不改代码；写入 runbook 操作经验，说明何时需要 `ra2a restart`、何时需要重跑安装器 | 后续若要统一，应把 native 解析收敛成 Go 内单一 resolver（本次不做） |
| general | B3 | `Join-Path` 分隔符语义在 Linux pwsh 上不一致 | blocking | accept | 无法在本机验证 `pwsh` on Linux；但断言可做成与分隔符无关而不削弱测试 | 测试断言改为 `strings.ReplaceAll(got, "\\", "/")` 与 `filepath.ToSlash(want)` 比较；本机 5.1 实测四种布局均命中 | CI 首跑确认 |
| general | B4 | standalone 布局与 `.exe` 后缀未经证实 | blocking | accept | 本机实测 Windows 布局为 `…/current/bin/codex.exe`（junction → `releases\<version>-x86_64-pc-windows-msvc`），`codex update` 经它把 `current` 从 0.159.3 轮转到 0.160.0；仓库旧文档记的是 `current/codex` | 探测扩为四种已观察形状，未知布局退回原有检测而非静默钉空；本机 6 用例实测；布局证据写入 runbook | macOS 布局仍待实测，runbook 已标注 |
| general | R1 | 静默覆盖用户选择 | non-blocking | accept | 与安装器既有“状态变化必须打印”惯例一致 | 替换已有 pin 时输出 `replacing recorded Codex pin (<old>) …(<new>)`，测试断言该输出 | — |
| general | R2 | runbook 契约过时 | non-blocking | accept | — | 更新契约表、新增 Windows 优先段落与两条操作经验 | — |
| general | R3 | `install-remote.sh` 转发 `--codex` 但检测不用 | non-blocking | defer | 属既有行为，本次未触碰该脚本；且 POSIX 已不再有 standalone 偏好，冲突面消失 | 记录于 runbook 已知限制 | 后续单独处理 |
| general | R4 | POSIX 改动无实证收益 | non-blocking | accept | 与 B1 同一处置 | 已回退 | — |
| general | R5 | 四个文件对 App 轮转描述矛盾 | non-blocking | accept | 回退 sh 后仅剩两个 ps1 文件，描述一致 | 已回退 | — |
| general | R6 | 新增 Unix 测试自证 | non-blocking | accept | 测试已随 POSIX 改动回退 | 已回退 | — |
| general | R7 | `.exe` 硬编码 | non-blocking | accept | 探测列表同时包含有无 `.exe` 形态 | 已覆盖 | — |
| general | R8 | `codex update` 能力未演练 | non-blocking | accept | 本机真实执行过一次完整更新 | runbook 记录版本轮转前后状态 | — |
| general | R9 | 未跟踪 `.commandcode/taste/taste.md` | non-blocking | reject | 0 字节文件由其他工具产生，不属于本次改动 | 不提交、不删除，提交信息中说明 | 交由用户处置 |

### Closure Status

- Blocking findings found: yes（B1、B2、B3、B4）
- Accepted blocking findings fixed: yes（B1、B3、B4）
- Blocking re-review completed: no
- Blocking re-review passed: no
- Blocking re-review round links: n/a
- Blocking re-review launch records: n/a
- Rejected findings backed with evidence: yes（B2：本机 config 与 `realCodex` 自排除证据）
- Deferred findings documented: yes（R3）
- Implementation completeness gaps resolved or accepted by user: partial
- Blocked reason: n/a
- Allowed to proceed: yes

## Final Conclusion

B1 通过撤销无证据支撑的 POSIX 改动关闭；B3 通过与分隔符无关的断言关闭（Linux CI 首跑为最终确认）；B4 通过探测四种已观察布局并记录本机实测证据关闭。B2 被驳回，理由是本机 daemon 配置记录的正是 launcher 本身、两条路径都会收敛到 marker，且 wrapper 具备自排除；其残留限制与运维处置已写入 runbook。本轮无被接受的阻塞项遗留，允许提交。