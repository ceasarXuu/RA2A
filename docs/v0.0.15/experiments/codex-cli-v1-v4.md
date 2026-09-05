# Codex CLI V1-V4 可行性实验

- 日期：2026-09-02
- 产品权威：[PRD PD25-PD31](../prd.md#confirmed-product-decisions)
- 执行阶段：Phase 0
- 平台：macOS（Darwin arm64）
- Codex CLI：`0.152.1`
- 工作区：RA2A 当前 `main`

## 结论摘要

| 验证项 | 本机结论 | 对产品与工程的约束 |
| --- | --- | --- |
| V1：CLI MCP 来源身份 | 通过 | CLI 可作为可回复发送端继续验证；必须使用宿主提供的 thread/session 身份，不得伪造 |
| V2：空闲 session direct resume | macOS 通过 | 只适用于空闲 session；三平台结论尚未完成 |
| V3：活跃 TUI direct resume | 不通过 | 排除 direct resume 作为正式活跃 TUI 投递路线，不能满足 PD31 |
| V4：单 App Server + remote TUI | macOS 通过 | 当前首选所有权路线；仍需 V5/V6、三平台和长时间矩阵验证 |

V1-V4 的首轮结果支持继续研究“RA2A 管理单一 App Server，Codex CLI TUI 通过 `--remote` 接入”的路线。当前证据不允许把 Codex CLI 标记为正式支持，也不允许提前进入会锁定该路线的主体架构重构。

## 官方能力基线

实验前按当前官方文档核对了以下入口：

- [Codex MCP](https://learn.chatgpt.com/docs/extend/mcp?surface=cli)：CLI 支持 STDIO MCP；MCP 配置可声明工具审批策略。
- [Codex developer commands](https://learn.chatgpt.com/docs/developer-commands?surface=cli)：`codex exec resume` 可恢复 exec session；`--remote` 可让交互式客户端连接 App Server。
- [Codex App Server](https://learn.chatgpt.com/docs/app-server)：`thread/resume` 后可用 `turn/start` 追加回合；`thread.sessionId` 应从宿主返回值读取。

App Server 仍属于实验接口，本报告记录的是 `0.152.1` 实机行为，不把命令存在等同于稳定兼容承诺。

## V1：CLI MCP 来源身份

### 方法

使用仓库已有 `cmd/mcp-context-probe`，通过一次性 CLI 配置注册为 STDIO MCP。执行两次独立的 `codex exec --json` session，各调用一次 `probe_context`，比较 CLI 的 `thread.started.thread_id` 与 MCP `_meta` 中所有身份候选。

关键配置：

```sh
codex -a never -s read-only -C "$PWD" \
  -c 'mcp_servers.ra2a_probe.command="go"' \
  -c 'mcp_servers.ra2a_probe.args=["run","./cmd/mcp-context-probe"]' \
  -c 'mcp_servers.ra2a_probe.required=true' \
  -c 'mcp_servers.ra2a_probe.tools.probe_context.approval_mode="approve"' \
  exec --json --ignore-user-config \
  'Call the ra2a_probe probe_context tool exactly once.'
```

### 观察

两次独立 session 均出现相同映射关系：

```text
thread.started.thread_id
  == _meta.threadId
  == _meta.x-codex-turn-metadata.thread_id
  == _meta.x-codex-turn-metadata.session_id
```

探针还观察到 `_meta.x-codex-turn-metadata.thread_source = "user"`。两次 session 的实际 ID 不同，且各自四个字段内部完全一致。

当 CLI 使用 `approval_policy=never` 且未显式批准 probe 工具时，调用会在客户端被拒绝，MCP server 不执行。设置 per-tool `approval_mode="approve"` 后稳定成功。因此 RA2A 安装配置必须显式声明只读工具审批策略，不能依赖默认值。

### 结论

V1 在 Codex CLI `0.152.1` / macOS 上通过。CLI MCP 调用能稳定映射到当前 session，满足继续验证 CLI 发送端的必要条件。字段仍需纳入 V5 版本契约探测，不应被视为跨版本无条件稳定。

## V2：空闲 session direct resume

### 方法

选择 V1 创建并已完成回合的空闲 session，连续执行两次：

```sh
codex -a never -s read-only -C "$PWD" \
  exec resume --json --ignore-user-config <session-id> '<prompt>'
```

第一次要求只回复 `V2_RESUME_ONCE_OK`；第二次要求复述上一轮 token，以同时验证历史连续性。

### 观察

- 两次命令都返回原 session ID。
- 每次只出现一个 `turn.started` 和一个 `turn.completed`。
- 第一次准确回复 `V2_RESUME_ONCE_OK`。
- 第二次从历史中准确复述同一 token。
- 未观察到重复 turn 或结果不确定。

### 结论

V2 在 macOS 上通过：空闲 CLI session 可以被精确恢复并追加一次回合，之后仍可继续恢复。工程计划要求的 Linux、Windows 实机核对尚未执行，因此不能把 V2 标记为三平台完成。

## V3：活跃 TUI direct resume

### 方法

使用交互式 `codex resume <session-id>` 打开同一 session，使 TUI 持有活跃 writer；在另一进程执行 `codex exec resume <session-id> <prompt>`。失败后回到原 TUI 输入新消息，检查 writer 和人工交互是否仍可用。

### 观察

外部 resume 立即失败，核心错误为：

```text
thread-store conflict: thread <session-id> already has an active writer
thread/resume failed: ... already has an active writer (code -32600)
```

失败调用没有创建外部回合，也没有抢占 TUI writer。原 TUI 仍能接受并启动后续人工输入。

### 结论

V3 对 direct-resume 路线不通过。这是路线排除结论，而不是可通过重试掩盖的瞬时失败：活跃 TUI 与第二个 resume 进程存在明确的 writer 所有权冲突。根据 PD31，direct resume 不得作为 Codex CLI 正式接收端的活跃会话投递路径。

## V4：单 App Server + remote TUI

### 方法

1. 在工作区内创建临时 Unix socket 并启动唯一 App Server：

   ```sh
   codex app-server --listen "unix://$PWD/.phase0-v4.sock"
   ```

2. TUI 连接该 owner 并创建 session：

   ```sh
   codex --remote "unix://$PWD/.phase0-v4.sock" --no-alt-screen \
     -a never -s read-only -C "$PWD" '<prompt>'
   ```

3. TUI `/status` 确认 remote 地址、CLI 版本和 session ID。
4. 从另一进程向同一 App Server 连续投递两次：

   ```sh
   codex queue --remote "unix://$PWD/.phase0-v4.sock" \
     --thread <session-id> --message '<prompt>'
   ```

5. 两次外部投递完成后，在原 TUI 手工输入第三条消息。

`codex queue` 是 `0.152.1` 提供的同 App Server 投递客户端，本实验用它验证 owner 和 TUI 行为；RA2A 后续仍应通过 App Server 协议实现统一投递结果，不能在产品层依赖 shell 输出。

### 观察

- TUI `/status` 显示连接到实验 Unix socket，App Server 与 TUI 均为 `0.152.1`。
- 第一次外部投递立即返回 queued message ID；TUI 实时出现完整输入并回复 `V4_EXTERNAL_QUEUE_OK`。
- 第二次投递同样实时显示并回复 `V4_EXTERNAL_QUEUE_REPEAT_OK`。
- 两次外部投递后，TUI 人工输入正常显示并回复 `V4_MANUAL_CONTINUES_OK`。
- 未出现第二 writer、永久 thinking、重复 turn 或 TUI 重连。
- 退出 TUI 和 App Server 后，临时 socket 被正常清理。

### 结论

V4 在 macOS 上通过。单一 App Server owner 能同时服务 remote TUI 与外部投递，消息实时显示，连续外部回合后人工输入正常。这是当前唯一同时满足本机 writer 所有权、TUI 刷新和 PD31 人工继续交互要求的候选路线。

本轮 V4 的两次外部投递均在前一回合完成后进行，没有覆盖 turn 执行期间到达的 follow-up。v0.0.10 的 Codex Desktop active-turn 修复证明该时序会影响 turn 归属和 UI 状态，因此工程计划新增 V7 单独验证 CLI active-turn follow-up；V4 的既有结论保持不变。

## 决策映射与后续门槛

| 产品决策 | 本轮证据 | 当前状态 |
| --- | --- | --- |
| PD29：不得静默启动 | 本轮只验证所有权路线，未实现启动生命周期 | 仍需设计 `start_required`，不得因 V4 通过而静默拉起 App Server |
| PD30：地址不透明 | 实验使用 native session ID 仅作宿主验证 | 正式接口不得暴露或要求调用方拼接 native ID/socket 地址 |
| PD31：全交叉准入 | V1 提供发送方身份证据；V4 提供接收与人工继续证据；V3 排除不安全路线 | 尚未达到正式支持：缺少 V5/V6、三平台、双设备与完整交叉矩阵 |

## Phase 0 状态

本轮完成了 V1-V4 的 macOS 首轮实机验证，并冻结以下局部工程结论：

1. 保留 CLI MCP 来源身份探测路线。
2. direct resume 只可视为“空闲 session 的可用原语”，不能承担活跃 TUI 投递。
3. 后续 Codex CLI 所有权实验以“单 App Server + remote TUI”为主线。
4. 后续实验遵守 PD32，与本机已安装的 RA2A 正式版隔离，不停止、重启、重新配置或复用其资源。
5. 在完成 V5-V7、三平台与 PD31 交叉验证前，不进行主体架构重构，不把 CLI 列为正式支持。

原始终端输出未提交；报告只保留复现命令、非敏感观察结果和结论。
