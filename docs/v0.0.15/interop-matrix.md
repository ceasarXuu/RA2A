# 适配器互通矩阵

- 更新日期：2026-09-30
- 要求（项目硬约束）：**任何已适配的 harness 之间必须双向互通**。未通过的组合不得列为支持。
- **正式路径：session 直投。** 地址取自 `list_targets`，投递即在目标会话里产生一个
  turn，与 codex↔codex 同构。信箱是实现期的临时测试手段，不构成互通证据。

图例：`✅` 真机验证通过 ｜ `⚠️` 部分验证 ｜ `❌` 未通过 ｜ `—` 未验证

## 当前矩阵

| 发送端 ＼ 接收端 | codex-app | codex-cli | opencode |
| --- | --- | --- | --- |
| **codex-app** | ✅ 长期回归，Windows/Ubuntu/macOS | ⚠️ Windows 本机通过，LAN 未验 | ✅ 用户目视确认实时渲染 + 跨机往返（`FINAL-034633`） |
| **codex-cli** | ⚠️ 同上，反向 | ⚠️ Windows 本机通过，LAN 未验 | — |
| **opencode** | ✅ 跨机往返（`FINAL-034633`，rog306 回） | — | ✅ RA2A 投递侧通，排队消息会被执行 |

## 接收方向：零配置已达成

opencode 适配器默认**发布共享 server 报告的全部会话**，与 codex-app 发布全部
316 个 session 的口径一致。任何 session 都能收到投递，无需登记。

`ra2a adopt-oc` 保留为**收窄**手段（只暴露指定会话），不再是前置步骤。

## 未打通的格子：opencode agent 未稳定选用 RA2A 工具

真机追查（`RA2A-FULLCHAIN-024937` 同一会话）：

- **可达**：opencode agent 能发现 `ra2a_send_message`，并构造出正确参数
  （实测 `{"to": ..., "text": ...}`）。
- **不稳定**：三次测试中 agent 两次改用 bash 自行调查，而不是调用 MCP 工具。
  这是模型行为问题，不是接口或协议问题。
- **调用方识别已不再阻塞投递**：归因改为尽力而为（`8e9f1a4`），无法识别时记为
  anonymous 照常投递。回信地址随消息正文的 `from:` 行传递，跨 `/new` 不失效，
  不需要任何进程级或会话级注入。

## 接收方向的目视确认记录

| 时间 | marker | 目标 | 发起方 | 确认 |
|---|---|---|---|---|
| 02:31 | `RA2A-LIVE-023158` | Quick greeting | 探针客户端 | 用户目视确认 |
| 02:49 | `RA2A-FULLCHAIN-024937` | Quick greeting | codex-app session `019f247e-…` | 用户目视确认 |

第二条走的是完整部署链路：`POST /v1/send` → 注册表 → opencode 适配器 → 共享
server → 用户 TUI，且 `delivery: confirmed`（适配器等到了 `session.idle` 终态，
而非仅"已交给传输层"）。

真机追查结论（opencode 1.18.33）：

1. **工具层通了**：opencode agent 能找到并调用 `ra2a_send_message`（隔离 server 上实测，
   工具名 `ra2a_send_message`，参数正确）。
2. **被调用方识别拒绝**：返回 `CALLER_SESSION_UNKNOWN`。
3. **根因是设计使然，不是 bug**：opencode 不在 MCP `_meta` 里携带稳定调用方身份，
   适配器因此拒绝从已加载会话里猜测（猜测会把消息归到错误的会话）。
4. **多会话时必然要求 `from`**：只有 1 个 opencode 会话时适配器可自动认领；≥2 个时
   必须显式传 `from`，而 agent 并不知道自己的地址。这与「用户无感」冲突。

因此这条链路的**产品缺口**是：调用方身份在 opencode 侧不可自动获得。

## 三节点验收记录（2026-09-30）

| 方向 | marker | 结果 |
| --- | --- | --- |
| macmini-m4 → ubuntu407 opencode | `MACMINI-PONG-040921` | 到达 |
| ubuntu407 opencode → rog306 | `FINAL-034633` | 到达 |
| rog306 → ubuntu407 opencode | `FINAL-034633` | 到达（14s 往返） |
| macmini-m4 → ubuntu407 opencode（新构建） | `FINAL-MACMINI-20260930-042216` | 到达 |

### 发送方返回值：LAN 成功现在报 `confirmed`

原先跨机成功只报 `handed_to_transport`，语义弱于本地目标的 `confirmed`，
三个节点之间四个方向的成功投递都因此被弱化表述（macmini-m4 联调时指出）。

该判断来自「跨节点结论不可信」的年代：接收端要等整个 turn，超时后结论回不来
（见 §11.5），所以只能保守表述。投递耗时修复后这个前提已不存在——LAN 发送是
同步 CoAP 往返，`deliverOverLAN` 只在远端适配器返回 `Delivered` 时才返回成功，
因此**跨机成功返回本身就携带了远端的确认**。

已收紧（`DeliveryConfirmed` 不再按目标节点判定）：三条成功路径全部以「目标已
收下」为返回条件——本地 registry 内联投递、本地信箱落盘、跨机同步往返。确认是
「发送成功」的属性，不是地址的属性。

验证：

| 场景 | 修复前 | 修复后 | 实测 |
| --- | --- | --- | --- |
| 本地目标成功 | `confirmed` | `confirmed` | ✅ |
| 跨机成功 → rog306 | `handed_to_transport` | `confirmed` | ✅ 1.187s |
| 跨机成功 → macmini-m4 | `handed_to_transport` | `confirmed` | ✅ 0.881s |
| 目标不存在 | 报错 | 报错（未误报 confirmed） | ✅ `TARGET_UNSUPPORTED` |
| 对端 Desktop 未运行 | `DELIVERY_UNKNOWN` | `DELIVERY_UNKNOWN`（未误报 confirmed） | ✅ 两次实测 |

本条链路的三个独立缺陷一并留档：投递耗用阻塞端点（§11.5）、wrapper 参数转发、
Codex 会话模型被改写（见 §11.6）。

## 节点升级记录：macmini-m4

- 2026-09-30 升级到 `e2ff3dd`，成为完整参与者：21 个会话全部发布
  `agent=codex-app`（升级前**完全无 `agent` 字段**，只能被当作旧节点）。
- 三节点同口径：macmini-m4 21 / rog306 287 / ubuntu407 333（codex-app 316 +
  opencode 17），全部 `ready`。

升级过程中遇到的四个问题，均已解决，记录备查：

| 问题 | 现象 | 处理 |
| --- | --- | --- |
| 本地分支严重落后 | `main` ahead 5 / behind 52，`git pull --no-rebase` 产生 7 个冲突（`cmd/ra2a/main.go`、`internal/agentbridge/registry.go`、`internal/codexapp/adapter.go`、`internal/control/control.go`、`internal/lannode/node.go`、`internal/mcpserver/server.go` 等） | `git merge --abort` 放弃合并，改用 `origin/main` 快照构建二进制，不覆盖本地提交 |
| Codex MCP 注册失败 | `register Codex MCP: fork/exec /Applications/ChatGPT.app/Contents/Resources/codex: no such file or directory` | 更新本机 config 里的 codex 路径为已安装的原生 codex 后重启成功 |
| macOS 没有 `/proc` | 无法用 `readlink /proc/<pid>/exe` 核实二进制是否被替换 | 改用运行进程的 inode/size 与磁盘二进制比对 |
| `go build` 不退出 | 二进制已生成但进程无输出、不退出，持续 7 分钟后被 SIGTERM（退出码 143） | 未定位根因；二进制本身经 version/sha256/inode/agent 字段四项验证可用 |

## 顺带确认的两个运行时事实

| 事实 | 影响 |
| --- | --- |
| 注册 MCP server **不影响已存在的会话**，工具列表是会话创建时的快照 | 改配置后必须新建会话或重启 server |
| opencode **server 进程**也只在启动时加载 MCP 配置 | 改配置后必须重启 server，而用户 attach 的 TUI 依赖该 server |

第二点是共享 server 设计的真实运维代价：配置变更 = server 重启 = TUI 断连。
