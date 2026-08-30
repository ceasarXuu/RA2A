# RA2A

RA2A 是一个运行在局域网内的 MCP，目标是让多台设备上的多个 Codex Agent 能够互相发现，并向指定 Codex session 定向发送消息。

当前处于产品设计阶段。极简模型与待确认决策见：

- [RA2A 局域网 Agent 通信（极简闭环）](prd/2026-08-31-ra2a-minimal-model.md)

## 当前范围

- 局域网节点自动发现
- Codex session 列举与寻址
- Agent 到指定 session 的文本消息投递
- 来源地址保留与双向回复

暂不包含中心服务、离线消息、持久队列、工作流编排和公网通信。
