# 适配器互通矩阵

- 更新日期：2026-09-29
- 要求（项目硬约束）：**任何已适配的 harness 之间必须双向互通**。未通过的组合不得列为支持。
- **正式路径：session 直投。** 地址取自 `list_targets`，投递即在目标会话里产生一个
  turn，与 codex↔codex 同构。信箱是实现期的临时测试手段，不构成互通证据。

图例：`✅` 真机验证通过 ｜ `⚠️` 部分验证 ｜ `❌` 未通过 ｜ `—` 未验证

## 当前矩阵

| 发送端 ＼ 接收端 | codex-app | codex-cli | opencode |
| --- | --- | --- | --- |
| **codex-app** | ✅ 长期回归，Windows/Ubuntu/macOS | ⚠️ Windows 本机通过，LAN 未验 | ✅ **用户目视确认**（`RA2A-FULLCHAIN-024937` 实时出现在 attach 的 TUI 中） |
| **codex-cli** | ⚠️ 同上，反向 | ⚠️ Windows 本机通过，LAN 未验 | — |
| **opencode** | ⚠️ 工具可达，模型未稳定选用（见下） | — | ✅ RA2A 投递侧通 |

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

## 顺带确认的两个运行时事实

| 事实 | 影响 |
| --- | --- |
| 注册 MCP server **不影响已存在的会话**，工具列表是会话创建时的快照 | 改配置后必须新建会话或重启 server |
| opencode **server 进程**也只在启动时加载 MCP 配置 | 改配置后必须重启 server，而用户 attach 的 TUI 依赖该 server |

第二点是共享 server 设计的真实运维代价：配置变更 = server 重启 = TUI 断连。
