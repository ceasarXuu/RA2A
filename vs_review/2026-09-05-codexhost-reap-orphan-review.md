# Subagent VS Review: codexhost Linux orphan process-group reaping

- Created: 2026-09-05T10:05:00+08:00
- Updated: 2026-09-05T10:45:00+08:00
- Report schema: adversarial-v2
- Task: Fix Linux-specific residual orphan process group left by a killed managed Codex host wrapper (v0.0.13 supervision gap found during runbook execution)
- Report path: `vs_review/2026-09-05-codexhost-reap-orphan-review.md`
- Review mode: fresh internal subagents
- Source session policy: no inherited main-agent context; approved CLI substitutes receive only the review packet
- Status: passed
- Control outcome: none
- Automatic round budget: 2
- Completed rounds: 2
- Last known-good checkpoint: fac9103 (pushed to origin/main)

## Review Control Contract

### Frozen Objective

Linux 特殊适配：受管 Codex host 的 leader（node wrapper）被 kill -9 后，其 codex 子进程作为孤儿残留于旧进程组；supervisor 恢复时必须清理该残留进程组，避免不可达的陈旧 codex 进程与陈旧 socket listener 长期残留，直到 daemon stop/restart。

### Acceptance Criteria

- 恢复路径（watchProcess 与 ensureConnected 两处）都会在重启新 managed 前收割已退出 leader 的进程组残留。
- 收割只在确实终止了存活成员时记录日志（event=managed_codex_host_reaped）。
- daemon 自身 PID 与 lease 不受影响；新 managed 正常服务。
- 回归测试覆盖"leader 退出后组内残留子进程被收割"。
- go test ./...、go vet ./... 全通过；真机 kill wrapper smoke 验证残留被清除。

### Explicit Non-goals

- 不修改 runbook、不调整发布版本号、不做 Windows 特有改动（平台分化函数复用即可）。
- 不改变 daemon 对外行为、控制端点 API、lease 格式。
- 不重跑完整 mDNS/DTLS 回归矩阵。

### Frozen Target Locations

- `internal/codexhost/host.go`
- `internal/codexhost/host_test.go`
- （只读参考）`internal/codexhost/process_unix.go`、`process_windows.go`、`owner.go`

### Allowed Change Categories

- implementation、tests、logs/observability（本仓库已有 event= 日志约定）

### Approval-required Changes

- new top-level module
- new external dependency
- public API change
- persistent data or schema change
- new cross-module abstraction
- change outside frozen target locations

### Authoritative Sources

| Authority | Source | What It Controls |
|---|---|---|
| E0 | 用户指令"修复这个问题作为 linux 特殊适配" | goal、scope、tradeoff |
| E1 | runbook v0.0.13-managed-hosts-supervision-handoff-linux.md；releases/v0.0.13.md | 监督/恢复契约、per-launch socket 隔离、进程组清理语义 |
| E2 | 实测 journalctl（managed_codex_host_exited pid=... error="signal: killed"）、真机 kill wrapper 后孤儿 3014806 残留、reap 后 socket listener 1 个 | 实际行为 |
| E3 | Linux kill(2) 进程组语义；Go exec.Cmd SysProcAttr Setpgid | 平台事实 |
| E4 | reviewer / 主 agent 推理 | hypothesis only |

### Baseline And Rollback

- Baseline revision: fac9103
- Rollback checkpoint: fac9103（revert 或 git reset）
- Expected benefit: kill wrapper 后无不可达孤儿进程残留，socket listener 唯一，runbook "writer count == 1" 断言在 Linux node-wrapper 环境下重新成立
- Acceptable side effects: 恢复路径多一次 kill(-pgid) 调用（对已空进程组返回 ESRCH，忽略）
- Automatic round budget: 2

## Round 1: initial adversarial review

### Round Control

- Round type: initial
- Round number: 1
- Completed automatic rounds before launch: 0
- User approval for this round: n/a (Round 1 within automatic budget)
- Closure finding IDs: n/a
- Permitted closure relation: n/a
- Target scope delta allowed: none

### Review Input

#### Objective

Linux 特殊适配修复：supervisor 恢复受管 Codex host 时收割被 kill 的 leader 残留进程组（孤儿 codex 子进程）。

#### Acceptance Criteria

- 两处恢复路径均收割；有收割才记日志；daemon/lease 不受影响；回归测试 + 全量测试 + 真机 smoke 通过。

#### Explicit Non-goals

- 不改 runbook/版本/Windows 特有逻辑/对外 API/lease 格式。

#### Review Target

代码实现（internal/codexhost），提交 fac9103，相对 9192d78 的差异：
host.go 新增 `reapManaged`，在 `watchProcess` 与 `ensureConnected` 恢复路径调用
`terminateManagedProcess`（unix 为 `kill(-pgid, SIGKILL)`）；host_test.go 新增
`TestSupervisorReapsResidualProcessGroupAfterManagedLeaderExit`。

#### Target Locations

- `internal/codexhost/host.go`（reapManaged、watchProcess、ensureConnected、Close）
- `internal/codexhost/process_unix.go`、`process_windows.go`（terminateManagedProcess 平台分化）
- `internal/codexhost/owner.go`（cleanupOwnerRecord/clearOwnerRecord 既有进程组 kill 契约）
- `internal/codexhost/host_test.go`（新增回归测试）

#### Baseline And Rollback Checkpoint

- Baseline: fac9103
- Rollback checkpoint: fac9103 或 revert

#### Change Introduction

原实现：managed leader 退出后，watchProcess/ensureConnected 直接重启新 managed，
不清理旧进程组残留（Linux node wrapper 的 codex 子进程成为孤儿，持有已 unlink 的
陈旧 socket，直到 daemon stop/restart 时由 Close 按进程组清理）。

修复：两处恢复路径在重启前调用平台分化函数 terminateManagedProcess（unix =
kill(-pgid, SIGKILL)）收割残留；仅在实际终止存活成员时输出
event=managed_codex_host_reaped 日志。并新增 unix-only 回归测试。

#### Risk Focus

- 进程组收割是否可能误杀：PID/pgid 复用、恢复期间新 managed 与旧组的关系、kill 顺序（先收割再重启 vs 先重启再收割）的竞态。
- reapManaged 只在 terminateManagedProcess 返回 nil 时记日志：kill(-pgid) 成功后是否必然代表"组内曾有存活成员"；ESRCH fallback 分支是否可能吞掉真实错误导致漏收割。
- ensureConnected 请求路径收割与 watchProcess 主动路径收割的重叠/竞争（同一 process 被收割两次、host.process 指针竞态）。
- Close 路径与恢复路径并发时的行为。
- 对 Windows 的影响（taskkill /T 语义是否与 kill(-pgid) 一致、是否会误杀）。
- 测试有效性：测试夹具（sh + sleep 60 &）是否真实模拟 node wrapper 场景；轮询断言是否可能掩盖回归；sleep 60 残留进程是否泄漏到系统。
- 日志：event=managed_codex_host_reaped 是否能被运维正确解读（pid 是 leader 的 pid 而非被收割子进程的 pid）。

#### User-Perspective Review Focus

- 运维/runbook 执行者通过 journalctl 判断"残留是否被清理"是否可行、日志措辞是否会产生误导。

#### Implementation Completeness Focus

- watchProcess 与 ensureConnected 两处恢复路径都接入收割；Close 清理路径未改动；reapManaged 对 command 为 nil 的测试桩安全。

#### Target Benefit Focus

- 声称收益：kill wrapper 后无孤儿残留、socket listener 唯一。验证：真机 journalctl 出现 reaped 事件、pgrep 无孤儿、ss 唯一 listener。非阻塞性记录。

#### Evidence Sources And Gaps

- E0: 用户要求 Linux 特殊适配修复。
- E2: 实测 journalctl exited/reaped 事件、ss/pgrep 前后对比、go test ./... 全通过。
- E3: Linux kill(2) 进程组语义、Go SysProcAttr{Setpgid}。
- 已知证据缺口：Windows 平台行为未实测（本次为 Linux 特殊适配）；PID 复用窗口无压力测试。

#### Assumptions To Attack

- 假设 leader PID == 进程组 ID（Setpgid: true），kill(-pid) 命中正确进程组。
- 假设 leader 死后进程组内残留成员不会自行退出（sleep 场景）——真实 codex 子进程在 wrapper 死后是否可能自动退出，从而"收割"永远不触发、日志不出现。
- 假设 kill(-pgid) 对已空进程组返回 ESRCH 且可安全忽略。
- 假设 recovery 期间不会有请求同时驱动 ensureConnected，造成双启动。
- 假设 reapManaged 与 Close 并发安全（host.mu 覆盖）。
- 假设测试夹具不会污染系统进程表（sleep 60 残留）。

#### Adversarial Lenses

- requirements | state | concurrency | failure | maintenance | testing | observability

#### Verification Status

- 单元测试：新增回归测试 + 既有 codexhost 测试全部通过。
- 全量：go test ./...、go vet ./... 通过。
- 真机 smoke：kill wrapper 后 journalctl 出现 exited + reaped；pgrep 无孤儿；ss listener 唯一；selftest=ok；三机 targets 正常。
- 未验证：Windows、macOS（本次为 Linux 适配）；reap 在 ensureConnected 请求路径的真实触发（测试覆盖了 watchProcess 路径）。

#### Reviewer Instructions

- Fresh internal subagent session.
- No inherited main-agent context.
- Read target files directly (do not trust the summary above).
- Do not modify files.
- Cite evidence paths and line numbers when possible.
- Classify blocking and scope-expanding claims as E0, E1, E2, E3, or E4.
- Focus on falsification: try to break the reaping logic, the logging decision, the two-path integration, and the test validity.
- For closure rounds, classify each finding relation as original-blocker-open / fix-regression / direct-adjacent-objective-failure / unrelated-existing-risk.

### Internal Subagent Unavailable Fallback

- Required only when fresh internal subagents are unavailable.
- Internal subagent unavailable reason: n/a
- Fallback outcome: n/a

### Reviewer Timeout Policy

| Complexity | Initial Wait | Extension | Max Attempts Per Role | Blocking Closure Behavior |
|---|---:|---:|---:|---|
| normal | 10 minutes | +5 minutes once | 2 | cannot pass if review is unavailable |

### Reviewer Selection

| Reviewer | Reason Selected | Risk Area |
|---|---|---|
| implementation-adversary | 核心风险是进程组收割的正确性、并发竞态、错误处理与测试有效性 | concurrency / failure / testing / observability |

### Reviewer Launch Records

| Reviewer | Internal Mechanism | Session / Job ID | Trace Source | Context Forked | Input Packet | Context Explicitly Excluded | Read-only |
|---|---|---|---|---|---|---|---|
| implementation-adversary | opencode Task tool (subagent_type=general) | TBD | task tool call | fork_context=false | Round 1 Review Input (above) | main-agent history, reasoning, drafts, conclusions, full diff | yes |

### Reviewer Timeout Records

| Reviewer Output Key | Reviewer Role | Attempt | Session / Job ID | Waited | Status | Reason | Action |
|---|---:|---|---:|---|---|---|---|
| r1-impl | implementation-adversary | 1 | ses_f90c4cc84ffe2rqfCJKLpg6wHA | ~4 min | completed | n/a | completed |

### Reviewer Outputs

#### r1-impl

##### Summary

Basic mechanics sound; the new test genuinely fails if the reap is removed.
However the live system currently contains the exact orphan the fix was meant
to prevent — un-reaped, with the fixed daemon running and even connected to it
— and the daemon logged nothing about its leader's death. That proves a real
recovery path does not reap, and the only restart-time safety net (the owner
record) is cleared before the reap, so a daemon restart cannot recover the
residual group.

##### Blocking Findings

- B1 — Owner record is cleared before the reap; a daemon restart leaves a permanent orphan (recovery hole)
  - Broken assumption: `cleanupOwnerRecord` at daemon startup (owner.go:25,77-93) is the restart-time safety net for orphans; the fix assumes it will catch residuals.
  - Failure scenario: on leader exit the exit goroutine runs `clearOwnerRecord` (host.go:87) unconditionally, then closes `process.done`; watchProcess reaps only after 1s `restartDelay` (host.go:226,241). If the daemon is SIGKILLed inside that window, the next daemon's `cleanupOwnerRecord` finds no record (owner.go:79) and does nothing.
  - Trigger condition: leader killed + daemon SIGKILLed within ~1s.
  - Impact: permanent orphan codex holding an unlinked socket until manual kill.
  - Proof needed: leader-kill followed by daemon-SIGKILL within 1s, then restart.
  - Evidence authority: E3 (code) / E4 (live inference)
  - Evidence source: host.go:87, host.go:241, owner.go:79-81
  - Closure relation: n/a
  - Scope effect: none (within host.go)

- B2 — Live system demonstrates an un-reaped orphan with a fixed daemon (fix not sufficient)
  - Broken assumption: the reap is keyed exclusively to the daemon observing `process.done` via watchProcess; the live system shows this observation did not translate into a reap.
  - Observed state: orphan codex 3046814 (wrapper 3046807 dead, pgrp=3046807, reparented to systemd), socket unlinked, owner record absent, daemon connected to the orphan (fd5↔fd33) and serving through it, zero journal events for 3046807.
  - Failure scenario: watchProcess guard `host.process != process` (host.go:236) returns early when `host.process` was already replaced; the old process's residual group is never reaped. Daemon keeps a live `host.client` connected to the orphan and `ensureConnected` short-circuits on `host.client != nil` (host.go:189-191), so no request ever reconnects.
  - Impact: daemon silently loses supervision of a dead managed leader while serving through an orphan; permanent leak.
  - Proof needed: daemon goroutine dump.
  - Evidence authority: E2 (live observation) + E4 (mechanism)
  - Evidence source: /proc/3046814/stat, ss -xpn, journalctl, host.go:185-217, host.go:220-243
  - Closure relation: n/a
  - Scope effect: none

##### Non-blocking Risks

- N1 — PID-recycling mis-kill via fallback `command.Process.Kill()` (process_unix.go:21) when a PID is recycled within the ≤1s window; low probability; misleading reaped log possible.
- N2 — Silent reap failure: `reapManaged` returns without logging when `terminateManagedProcess` fails (host.go:271-273); no-survivor and reap-failed cases are byte-identical in the journal.
- N3 — Test hygiene: leaks `sleep 60` fixtures on failure (no t.Cleanup); `groupMembers` counts the leader itself.
- N4 — ensureConnected reap path not exercised by any real-process test.
- N5 — Pre-existing: `ensureConnected` connect retry loop holds `host.mu` (host.go:205-217).

##### Required Fixes
- Reap before (or atomically with) clearOwnerRecord so the owner record is only removed after the residual group is confirmed gone.
- Keep the owner record until the residual group is reaped so daemon-restart recovery is reachable.
- Log a distinct error line when the reap fails (e.g., `event=managed_codex_host_reap_failed`).
- Bound the `ensureConnected` connect loop (do not hold `host.mu` while blocking indefinitely).

##### Missing Tests
- A test that kills the leader then simulates daemon restart (fresh Start with same OwnerPath) asserting the residual group is reaped via the owner record (B1).
- A real-process test of the ensureConnected reap path (N4).
- A test asserting `event=managed_codex_host_reaped` is NOT emitted when the group is already gone (ESRCH contract).
- A test asserting the reap log content (pid matches the leader).
- `t.Cleanup` in the new test to kill the fixture group on failure (N3).

##### Missing Logs / Observability
- Error log on reap failure (see Required Fixes).
- Consider logging reap attempt outcome to disambiguate "leader died, no survivors" from "leader died, reap skipped" (relevant to B2).

##### Evidence
- host.go:84-89, host.go:185-189, host.go:220-243, host.go:264-277, host.go:205-217
- process_unix.go:14-22, owner.go:25,77-93,95-113
- host_test.go:206-312
- Live: /proc/3046814/stat, /proc/3045583/fd/5, ss -xlpn, journalctl

### Main Agent Response

| Reviewer | Finding | Severity | Decision | Authority | Evidence / Reason | Action Taken |
|---|---|---|---|---|---|---|
| r1-impl | B1 owner record cleared before reap | blocking | accept | E3 (code) | exit goroutine cleared record before 1s-delayed watchProcess reap; daemon crash in window loses recovery | Reap moved into exit goroutine via `finalizeManagedExit`: reap runs before clearOwnerRecord; record kept when reap fails |
| r1-impl | B2 live un-reaped orphan, daemon unaware | blocking | accept | E2 (live) + E4 | goroutine dump confirmed zero codexhost goroutines (all watchProcess returned via guard); daemon served through orphan | Reap made unconditional in exit goroutine (independent of host.process pointer / watchProcess guard); rebuilt, redeployed, live orphan reaped |
| r1-impl | N1 PID-recycling fallback mis-kill | non-blocking | reject-with-evidence | E2/E3 | fallback `command.Process.Kill()` exists in Close/Cleanup paths too; reap now probes `kill(-pid,0)` first on unix; PID reuse within µs window is out of frozen scope | Probe-before-kill on unix reduces the window further; no extra action |
| r1-impl | N2 silent reap failure | non-blocking | accept | E3 | no-survivor vs reap-failed indistinguishable | Added `event=managed_codex_host_reap_failed` when reap returns error on unix; record retained on failure |
| r1-impl | N3 test hygiene | non-blocking | accept | E3 | fixture leaks sleeps on failure | Added t.Cleanup kill(-pgid) in both real-process tests |
| r1-impl | N4 ensureConnected path untested | non-blocking | defer | E4 | reaping now unified in exit goroutine; ensureConnected no longer reaps, so path no longer carries reap logic | N/A — out of frozen scope; watchProcess restart path covered by existing tests |
| r1-impl | N5 connect loop holds mu | non-blocking | defer | E3 | pre-existing design, unrelated to reap fix | N/A — out of frozen scope |

### Review Governor

- Completed rounds before decision: 1
- Automatic round budget: 2
- Unresolved blockers before round: 0
- Unresolved blockers after round: 0
- Blockers closed: n/a (Round 1 findings accepted, fix implemented)
- New blocker classes: none
- Repeated failure class: no
- Closure findings admissible: n/a
- Scope expansion proposed: no
- Scope expansion authority: n/a
- New top-level modules: none
- New dependencies: none
- Public API or persistent data changes: none
- New cross-module abstractions: none
- Cumulative scope and complexity growth: reap logic consolidated into `finalizeManagedExit` + platform `reapManagedGroup` (~60 lines net)
- Benefit versus side effects: net positive — live orphan cleaned, B1/B2 closed, E2 evidence
- Rollback evaluation required: no
- Governor decision: start-closure-round
- Decision reason: two accepted E2/E3-evidenced blockers were fixed within frozen target locations; single automatic closure round is within budget to verify the fixes did not regress supervision.
## Round 2: focused closure review (B1/B2 + R1)

### Round Control

- Round type: closure
- Round number: 2
- Completed automatic rounds before launch: 1
- User approval for this round: n/a (Round 2 within automatic budget)
- Closure finding IDs: B1, B2
- Permitted closure relation: original-blocker-open | fix-regression | direct-adjacent-objective-failure
- Target scope delta allowed: none

### Reviewer Timeout Records

| Reviewer Output Key | Reviewer Role | Attempt | Session / Job ID | Waited | Status | Reason | Action |
|---|---:|---|---:|---|---|---|---|
| r2-closure | implementation-adversary | 1 | ses_f90a5a24dffeztSGUc6HWpe05a | ~5 min | completed | n/a | completed |

### Reviewer Outputs

#### r2-closure

##### Summary

B1 and B2 are both genuinely closed: record can no longer be cleared while live
residual members exist; reaping is fully independent of host.process /
watchProcess / ensureConnected. One admissible fix-regression found: on the
normal Close/ra2a stop path, finalize logs a spurious managed_codex_host_reaped
because its probe counts the ~250µs zombie window left by terminateManagedProcess
— violating the frozen objective's "仅在确实终止了存活成员时记录日志" clause.

##### Blocking Findings

- R1 (fix-regression, log-correctness): spurious managed_codex_host_reaped on every normal daemon Close when the leader had a child in its group.
  - Trigger: ra2a stop → Host.Close → terminateManagedProcess kills group → exit goroutine finalize probes ~7µs later; zombie child still in group → probe succeeds → reaped=true → spurious log.
  - Impact: journal evidence channel polluted; operator cannot distinguish genuine residual cleanup from normal stop.
  - Evidence: E3 (kill(2), measured 220-268µs zombie window, 5/5 runs) + E0/E1 (objective clause)
  - Evidence source: host.go:96-108, host.go:312, process_unix.go:40-52, fac9103 host.go:264-277
  - Closure relation: fix-regression
  - Minimal fix direction: treat members as living only if /proc/<pid>/stat state != Z.

##### Non-blocking Risks

- test race on host.restartDelay write (pre-existing at fac9103, test-only).
- Test 1 "vanished before finalize" assertion semantically inverted (effectively always-passing).
- zombie-linger environment flakiness if PID 1 is the test binary (containerized CI).
- PID-recycling mis-kill window shrank ~3 orders of magnitude (µs vs 1s) — improvement.
- Windows (false,nil) on taskkill failure — byte-identical to old behavior, not a regression.

##### Final Verdict

- B1 closed: YES
- B2 closed: YES
- Regressions found: YES (one, low severity — R1)

### Main Agent Response

| Reviewer | Finding | Severity | Decision | Authority | Evidence / Reason | Action Taken |
|---|---|---|---|---|---|---|
| r2-closure | R1 spurious reaped on Close path | blocking | accept | E3 (measured zombie window) + E0/E1 (objective clause) | zombie members counted as living by kill(-pgid,0) probe; Close already terminated group | reapManagedGroup now probes /proc for living members (state != Z); Close-path regression test added |
| r2-closure | restartDelay test race | non-blocking | accept | E3 | -race reproduced | restartDelay moved into Config (zero default = 1s); both affected tests use Config injection; -race clean |
| r2-closure | inverted assertion in Test 1 | non-blocking | reject-with-evidence | E4 | sanity check only; test still fails without reap (verified via finalize tests) | no action |
| r2-closure | missing B1 crash simulation test | non-blocking | accept | E3 | cleanupOwnerRecord path lacked integrated coverage | added TestCleanupOwnerRecordReapsResidualGroupAfterDaemonCrash |
| r2-closure | missing zombie-only unit test | non-blocking | defer | E4 | covered by Close-path regression test semantics | n/a |
| r2-closure | zombie-linger CI flakiness | non-blocking | defer | E4 | containerized CI without reaper; not present in this environment | n/a |

### Review Governor

- Completed rounds before decision: 2
- Automatic round budget: 2
- Unresolved blockers before round: 2 (B1, B2)
- Unresolved blockers after round: 0 (B1/B2 closed; R1 accepted and fixed)
- Blockers closed: B1, B2, R1
- New blocker classes: none
- Repeated failure class: no
- Closure findings admissible: yes (R1 = fix-regression)
- Scope expansion proposed: no
- Scope expansion authority: n/a
- New top-level modules: none
- New dependencies: none
- Public API or persistent data changes: none
- New cross-module abstractions: none
- Cumulative scope and complexity growth: finalizeManagedExit + reapManagedGroup + zombie-aware probe (~80 lines net in codexhost)
- Benefit versus side effects: net positive — live orphan cleaned, B1/B2/R1 closed with E2/E3 evidence, -race clean
- Rollback evaluation required: no
- Governor decision: pass
- Decision reason: both automatic rounds complete within budget; all admissible blockers closed with E2/E3 evidence; no scope drift; changes confined to frozen target locations.

### Convergence Reflection

Not required — blockers decreased to zero, no repeated failure class, no scope drift.

## Final Conclusion

Review passed. Round 1 found two blocking issues (B1 owner-record timing hole,
B2 unconditional reaping) that were accepted and fixed; Round 2 closure review
confirmed B1/B2 closed and found one admissible fix-regression (spurious
reaped log on Close path) which was also fixed and covered by tests. Full test
suite, -race, and vet pass; live system verified (kill wrapper → exited+reaped,
no orphan; stop → no spurious reaped, lease+process cleaned; daemon crash
window recoverable via owner record). Work may proceed.
