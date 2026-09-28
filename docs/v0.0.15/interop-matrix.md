# 适配器互通矩阵

- 更新日期：2026-09-29
- 要求（项目硬约束）：**任何已适配的 harness 之间必须双向互通**。未通过的组合不得列为支持。

图例：`✅` 真机验证通过 ｜ `⚠️` 部分验证 ｜ `❌` 未通过 ｜ `—` 未验证

## 当前矩阵

| 发送端 ＼ 接收端 | codex-app | codex-cli | opencode |
| --- | --- | --- | --- |
| **codex-app** | ✅ 长期回归，Windows/Ubuntu/macOS | ⚠️ Windows 本机通过，LAN 未验 | ⚠️ RA2A 投递侧验证（`delivery: confirmed`），非 Codex App agent 实际发起 |
| **codex-cli** | ⚠️ 同上，反向 | ⚠️ Windows 本机通过，LAN 未验 | — |
| **opencode** | ❌ **未通过**（见下） | — | ❌ 未验证 |

## 唯一未打通的格子：opencode → 任何 agent

真机追查结论（opencode 1.18.33）：

1. **工具层通了**：opencode agent 能找到并调用 `ra2a_send_message`（隔离 server 上实测，
   工具名 `ra2a_send_message`，参数正确）。
2. **被调用方识别拒绝**：返回 `CALLER_SESSION_UNKNOWN`。
3. **根因是设计使然，不是 bug**：opencode 不在 MCP `_meta` 里携带稳定调用方身份，
   适配器因此拒绝从已加载会话里猜测（猜测会把消息归到错误的会话）。
4. **多会话时必然要求 `from`**：登记 1 个会话时适配器可自动认领；登记 ≥2 个时
   必须显式传 `from`，而 agent 并不知道自己的地址。

因此这条链路的**产品缺口**是：调用方身份在 opencode 侧不可自动获得。

## 顺带确认的两个运行时事实

| 事实 | 影响 |
| --- | --- |
| 注册 MCP server **不影响已存在的会话**，工具列表是会话创建时的快照 | 改配置后必须新建会话或重启 server |
| opencode **server 进程**也只在启动时加载 MCP 配置 | 改配置后必须重启 server，而用户 attach 的 TUI 依赖该 server |

第二点是共享 server 设计的真实运维代价：配置变更 = server 重启 = TUI 断连。
