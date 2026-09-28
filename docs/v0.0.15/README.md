# RA2A v0.0.15 规划

v0.0.15 的目标是加入 **Codex CLI** 支持，并将 RA2A 从“Codex App 互联工具”演进为可持续扩展的异构 Agent 互通层。

本版本的核心不是简单增加一个 CLI 分支，而是建立统一的 Agent 适配架构：任意已支持 Agent 都应能通过 RA2A 与其他任意已支持 Agent 通信，新增 Agent 时不增加两两适配代码。

## 文档

- [产品需求与已确认决策](./prd.md)
- [工程实施计划](./engineering-plan.md)

## v0.0.15 目标矩阵

| 发送端 | 接收端 | v0.0.15 目标 |
| --- | --- | --- |
| Codex App | Codex App | 保持兼容并回归验证 |
| Codex App | Codex CLI | 新增支持 |
| Codex CLI | Codex App | 新增支持 |
| Codex CLI | Codex CLI | 新增支持 |

当前状态：**Phase 0 可行性验证（路线已重定向）**。产品准入规则已经确认。[V1-V4 macOS 首轮实验](./experiments/codex-cli-v1-v4.md)、[V7 macOS active-turn 实验](./experiments/codex-cli-v7.md)和 [V5 双版本 App Server 契约实验](./experiments/codex-cli-v5.md)已完成 macOS 验证。direct resume 因活跃 writer 冲突被排除。

**2026-09-28 更新（[V10](./experiments/codex-cli-v10.md)，Codex CLI `0.158.0`，Ubuntu native，隔离 `CODEX_HOME`）**：

- **零动作路线成立**。官方 daemon 自挂接已是默认开启的稳定特性，实测普通 `codex` 零参数即拉起/接入共享 daemon（socket inode 双向比对确认）。[V9](./experiments/codex-cli-v9.md) 在 `0.153.4` 上的失败结论仅对该版本有效；`codex wrapper` **降级为排除场景兜底**，不再是主路径。
- **投递入口改为 stable 路径**：`thread/resume` + `turn/start`（空闲）/ `turn/steer`（活跃，带 `expectedTurnId`）。`thread/queue/*` 全部 experimental，退出主路径。[V8](./experiments/codex-cli-v8.md) 已重定向为 V8-R。
- **投递确认口径唯一**：`turn/start` 先返回 turn ID 且 `error: null`，失败在 5 次重连后由 `turn/completed`（`status: failed`）暴露，与 Desktop 空 model 竞态同类。必须等终态。
- **所有权仍需自建登记**：`Thread.source` 恒为 `vscode`，`Thread.originator` 为 daemon 进程级全局值（first-writer-wins），协议层无可用判别字段。
- **PD32 隔离成本下降**：独立 `CODEX_HOME` 即等于隔离 daemon、socket、session 存储。

剩余阻塞：配置本地 mock 模型端点后，**PD31 准入验证不再被登录阻塞**——TUI 完整回合、向 TUI 自有 thread 注入并实时渲染、活跃回合 `turn/steer` follow-up 均已在无账号条件下真机通过。剩余项为三平台复现（Ubuntu 之外需 macOS 与 Windows）与真实后端行为（模型回合质量、plan 桶用量门禁、rate-limit 弹条）。标准流程与免登录方法见 `runbooks/codex-cli-isolated-daemon-experiment.md`。

另：`v0.0.15` 已于 2026-09-13 发布，但发布范围小于本计划范围（CLI 适配器未交付），承载版本待 Owner 决定，详见 [engineering-plan.md §0](./engineering-plan.md)。
