# Codex App 最小可行性实验

- 日期：2026-08-31
- 产品权威：[RA2A 极简 PRD](../prd/2026-08-31-ra2a-minimal-model.md#12-confirmed-product-decisions)
- 实验目标：只验证会直接决定 RA2A 能否成立的 Codex App 集成假设，不实现 daemon、mDNS 或 PIN 通信。

## 模块

| 模块 | 位置 | 单一职责 | 默认副作用 |
|---|---|---|---|
| App Server probe | `internal/appserverprobe`、`cmd/appserver-probe` | 使用 JSON-RPC 初始化 App Server、列举/恢复 thread、直接调用 MCP 工具、启动回合 | 默认仅调用 `thread/list`，写操作必须显式传入 `--allow-write` |
| MCP context probe | `internal/mcpcontext`、`cmd/mcp-context-probe` | 实现最小 STDIO MCP，报告 `_meta` 键名和 thread/session 身份候选 | 不读取文件、不访问网络、不输出非身份元数据值 |

两个模块只依赖 Go 标准库，可以单独构建：

```sh
go build ./cmd/appserver-probe
go build ./cmd/mcp-context-probe
```

完整验证：

```sh
go test ./...
go vet ./...
go build ./cmd/...
```

## 已验证结果

| 假设 | 方法 | 结果 | 证据等级 |
|---|---|---|---|
| 独立 App Server 能读取 Codex App 使用的会话存储 | 使用 `thread/list` 且设置 `useStateDbOnly=true` | 返回 58 个 thread，当前工作区命中 1 个 | 方向成立；未证明实时 UI 同步 |
| App Server 调用 MCP 时包含来源 thread ID | 在普通可恢复 thread 上调用 `mcpServer/tool/call`，由 context probe 检查 `_meta` | `_meta` 包含 `progressToken`、`threadId`，且 `threadId` 与目标完全一致 | 当前版本实机通过 |
| Codex App 自带版本也具备上述 MCP 元数据行为 | 使用 `/Applications/ChatGPT.app/Contents/Resources/codex` 0.151.0-alpha.7.2 重复实验 | 与全局 Codex 0.146.0 结果一致 | 当前 macOS App 实机通过 |
| `turn/start` 能启动回合 | 创建 `ephemeral: true` 的临时 thread，再发送最短文本输入 | 返回 turn 且状态为 `inProgress` | 协议执行通过；未证明 App UI 显示 |
| 任意未归档 thread 都能由独立 App Server 恢复 | 尝试恢复当前长任务和普通 thread | 普通 thread 可恢复；当前长任务因 paginated history lineage cycle 失败 | 假设不成立，必须把不可恢复状态暴露给调用方 |

## 实验中发现的协议前置条件

1. `thread/list` 必须设置 `useStateDbOnly=true`，否则只读探针可能触发 rollout 扫描与 state DB read-repair。
2. 对已有 thread 调用 MCP 或注入消息前必须先 `thread/resume`。
3. 对刚创建且已加载的 ephemeral thread 不能先 `thread/resume`；应直接 `turn/start`。
4. `mcpServer/tool/call` 的当前实现会向下游 MCP 请求写入 `_meta.threadId`，但官方文档没有把该字段承诺为稳定兼容契约，因此正式 daemon 仍需启动时能力检测。

## 尚未证明

- 外部 daemon 能否附着桌面 App 正在运行的同一个 stdio App Server 实例。当前 App 进程没有公开可附着的 socket 参数。
- 外部 App Server 向持久化既有 thread 执行 `turn/start` 后，桌面 App UI 是否实时出现并跟踪该回合。
- 由桌面 App 中的模型正常发起 MCP 工具调用时，是否与 `mcpServer/tool/call` 直调路径保持完全一致。
- Windows、Linux 上的同等行为。

## Product Decision Delta

本阶段没有新增产品语义。实验只补充了一项工程约束：会话“可发现”不等于“可恢复”，daemon 后续必须把不可恢复状态显式返回并拒绝注入；这属于已确认“发送失败需明确返回”的实现细化，不改变 PRD 决策。

下一步需要临时修改当前用户的 Codex MCP 配置，并创建一个可在桌面 App 中观察的测试 session；这两项都会写入仓库以外的用户状态，执行前需要用户明确授权。
