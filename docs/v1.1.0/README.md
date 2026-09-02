# RA2A v1.1.0 规划

v1.1.0 的目标是加入 **Codex CLI** 支持，并将 RA2A 从“Codex App 互联工具”演进为可持续扩展的异构 Agent 互通层。

本版本的核心不是简单增加一个 CLI 分支，而是建立统一的 Agent 适配架构：任意已支持 Agent 都应能通过 RA2A 与其他任意已支持 Agent 通信，新增 Agent 时不增加两两适配代码。

## 文档

- [产品需求与已确认决策](./prd.md)
- [工程实施计划](./engineering-plan.md)

## v1.1.0 目标矩阵

| 发送端 | 接收端 | v1.1.0 目标 |
| --- | --- | --- |
| Codex App | Codex App | 保持兼容并回归验证 |
| Codex App | Codex CLI | 新增支持 |
| Codex CLI | Codex App | 新增支持 |
| Codex CLI | Codex CLI | 新增支持 |

当前状态：**Phase 0 feasibility validation**。产品准入规则已经确认；[V1-V4 macOS 首轮实验](./experiments/codex-cli-v1-v4.md)已完成，direct resume 因活跃 writer 冲突被排除，单 App Server + remote TUI 路线首轮通过。后续实验必须与本机已安装的 RA2A 正式版隔离；V5-V7、三平台和 PD31 交叉矩阵完成前不进入主体架构重构。
