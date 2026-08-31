# Codex App 最小可行性实验

- 日期：2026-08-31
- 产品权威：[RA2A 极简 PRD](../prd/2026-08-31-ra2a-minimal-model.md#12-confirmed-product-decisions)
- 实验目标：只验证会直接决定 RA2A 能否成立的 Codex App 集成假设，不实现 daemon、mDNS 或 PIN 通信。

局域网发现与安全连接的独立开源方案实验见 [`network/README.md`](network/README.md)。

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

## 正式 LAN Node 接入进展

现有 App Server 客户端已接入正式 `ra2a` 命令。2026-08-31 本机只读验证中，全局 Codex 0.146.0 与 Codex App 内置 0.151.0-alpha.7.2 均通过 RA2A 的 mDNS → DTLS → CoAP `/v1/sessions` 完整链路返回 60 个未归档 thread。实现已补齐官方要求的 `initialized` 通知，并跟随 `nextCursor` 分页。

该结果仍只证明独立 App Server 能读取共享会话存储，不证明桌面 App UI 会实时同步外部进程发起的回合。

## 实验中发现的协议前置条件

1. `thread/list` 必须设置 `useStateDbOnly=true`，否则只读探针可能触发 rollout 扫描与 state DB read-repair。
2. 对已有 thread 调用 MCP 或注入消息前必须先 `thread/resume`。
3. 对刚创建且已加载的 ephemeral thread 不能先 `thread/resume`；应直接 `turn/start`。
4. `mcpServer/tool/call` 的当前实现会向下游 MCP 请求写入 `_meta.threadId`，但官方文档没有把该字段承诺为稳定兼容契约，因此正式 daemon 仍需启动时能力检测。

## 已否定与剩余验证

- 已否定：外部 daemon 不能附着 Desktop 正在运行的私有 stdio App Server；第二个 App Server 会遇到 writer conflict。
- 已否定：共享会话存储不等于实时 UI 同步，不能把外部写盘当成 Desktop 注入。
- 由桌面 App 中的模型正常发起 MCP 工具调用时，是否与 `mcpServer/tool/call` 直调路径保持完全一致。
- Windows、Linux 上的同等行为。

## 2026-09-01 单宿主闭环结论

后续实机实验否定了“第二个 App Server 可以写入普通 Desktop 本地 session”的假设：即使 `thread/list` 显示 `notLoaded`，`thread/resume` 仍可能返回 `already has an active writer`。OpenClaw、Codex Remote 等开源实现也采用独立宿主或明确规避跨进程 writer，而不是抢占 Desktop 的私有 stdio 宿主。

正式实现已经改为官方单宿主模型：RA2A 连接 canonical Unix control socket；不存在时启动 `codex app-server --listen unix://`。Codex 客户端通过 `--remote unix://` 或 Codex App Remote/SSH 连接同一宿主。

本机 Codex App 捆绑版本 `0.151.0-alpha.7.2` 的完整验证结果：

1. `ra2a serve` 启动受管 App Server，并经真实 mDNS/DTLS/CoAP 返回 61 个 thread。
2. 官方 `codex --remote unix://` 在该宿主创建持久 session `01a05904-edea-7743-a249-3c221e124fcf`。
3. 第二个 RA2A 节点调用 `/v1/messages`，发送端返回 `delivered`。
4. 官方客户端实时显示带 `from: ra2a://managed-sender-2` 的用户回合，并回复 `RA2A_MANAGED_HOST_OK`。

因此 V1 的可写目标收敛为受管宿主 session。普通 Desktop 本地 session 只可作为共享存储中的只读发现结果，不得宣称可注入；正式代码不接入私有 `send_message_to_thread` bridge。

## 2026-09-01 正式 MCP 闭环

正式 `ra2a mcp` 已替代实验 probe 进入产品链路，验证结果如下：

1. MCP `initialize` 成功，`tools/list` 只返回 `list_targets` 与 `send_message`。
2. `list_targets` 经本地 daemon 控制面、mDNS、DTLS 和 CoAP 返回真实节点与 61 个 session；不可连接 peer 不进入结果。
3. `send_message` 使用 `_meta.threadId=caller-mcp-e2e`，目标客户端显示 `from: ra2a://mcp-e2e-local/caller-mcp-e2e` 与随机 message ID。
4. 目标受管 session 完成新回合并回复 `RA2A_MCP_COMPLETE_OK`。
5. 隔离 `CODEX_HOME` 下执行官方 `codex mcp add ra2a -- /tmp/ra2a mcp` 与 `codex mcp get ra2a`，确认安装器使用的 stdio 注册形式被 Codex 正确识别。

错误路径通过自动化测试覆盖：缺失调用 thread、未知节点、忙碌 session、daemon 不可用和结果未知超时。应用层没有消息重试。

## Product Decision Delta

本阶段新增了用户明确确认的产品边界：V1 使用 RA2A 受管的单一 App Server；普通 Desktop 本地 session 不属于正式可写目标。该决策已记录到 PRD 的 Confirmed Product Decisions。
