# Codex App/CLI 同节点端点隔离实验

- 执行日期：2026-09-03
- 执行阶段：Phase 0
- 平台：macOS 26.5.2（arm64）
- Codex CLI：`0.152.1`
- 关联决策：PD30、PD31、PD32
- 状态：未通过

## 结论

V6 在 macOS 上未通过。原生 thread ID 可以唯一标识并精确投递到已知 CLI thread，但 App Server 的 `thread.source` 不能可靠区分 Codex App 与通过 `codex --remote` 接入的 Codex CLI：本次两类测试 thread 都返回 `source: vscode`。

因此不能用 `sourceKinds` 或 `thread.source` 推导公开的 `agentType`，也不能据此把同一节点上的 thread 分配给 Codex App/CLI 适配器。Phase 0 保持开放，Phase 1 主体架构重构继续禁止。

## 隔离门禁

实验前确认本机已安装的 RA2A 正式版 daemon 和 Codex App Server 正在运行。实验未调用 RA2A 的 `setup`、`restart`、`update` 或 daemon 命令，也未使用正式版配置、控制地址、节点身份或 App Server socket。

实验 App Server 只监听工作区内的独立地址：

```text
unix:///Volumes/inwolf-4T/projects/RA2A/.cache/v1.1.0-v6-20260903/app-server.sock
```

测试只创建带 `V6` 标记的 thread。实验结束后 TUI 和 App Server 正常退出，隔离 socket 自动清理；正式版 RA2A daemon 和 Codex App Server 的 PID 保持不变。

## 方法

1. 在工作区 `.cache/` 下启动独立 App Server。
2. 用 probe 在该 server 上创建 ephemeral App Server thread，并保留 `thread/start` 的原始响应。
3. 使用 `codex --remote` 连接同一 socket，创建一个 CLI TUI 测试 thread。
4. 分别记录两类 thread 的原生 ID、`source` 和状态。
5. 用 `thread/list` 的全来源查询及 `sourceKinds=cli` 查询精确筛选 CLI 测试 ID。
6. 使用 CLI 测试 thread 的精确原生 ID 投递一条消息，观察 TUI 与人工交互。

## 观察

- probe 创建的 ephemeral thread ID 为 `01a062f6-5ec8-7f93-b1df-7909d3a13cc1`，`thread/start` 原始响应返回 `source: vscode`。
- remote TUI thread ID 为 `01a062f3-c445-7492-9e9b-831940c3f7d8`。全来源 `thread/list` 返回 `source: vscode`、`status: idle`。
- 用 `sourceKinds=cli` 查询同一 CLI thread 时结果为空，证明不能把 remote TUI 等同于 App Server 的 `cli` 来源类别。
- 对 CLI 原生 ID 执行 `codex queue` 后，命令返回唯一 queued message ID，目标 TUI 准确显示并回复 `V6_CLI_TARGET_OK`；未观察到误投或重复。
- ephemeral thread 不进入 `useStateDbOnly=true` 的列表，不能用该查询证明其重启稳定性或完成 App 侧端到端投递。

## 根因与规划影响

`source` 描述 App Server 内部创建来源，不是 RA2A 产品定义中的宿主所有权。remote TUI 通过 App Server 创建 thread 时也可能使用 `vscode`，所以“`vscode` 对应 Codex App、`cli` 对应 Codex CLI”的映射假设不成立。

后续必须先验证显式所有权方案：由 CLI 接入边界在 attach/create/resume 时登记其 thread，而不是扫描共享 thread 列表后猜测类型。候选接入边界可以是受管 socket/proxy 或可观测的 launcher；选择由复验结果决定，不在本报告中提前冻结。

复验至少必须证明：

1. 同机真实 App 与 remote CLI 同时存在时，两类端点类型正确且地址唯一。
2. App 与 CLI 各自的原生 thread 只归属一个适配器，未知归属不得展示为 ready。
3. 对两个不透明地址分别投递时只命中唯一目标，投递后两端均可继续人工交互。
4. CLI 断开、重连和 resume 后，登记关系不会错误迁移到其他 thread。

## 对实现的约束

1. `thread.source` 只能保留为宿主诊断字段，不能成为 `agentType` 或适配器选择依据。
2. 内部端点身份必须包含经验证的适配器所有权和原生 thread ID；PD30 要求的公开地址仍由 daemon 完整生成并保持不透明。
3. 未建立所有权的 thread 应映射为不可用或不展示，不得猜测为 Codex App/CLI。
4. 已知原生 ID 的精确投递能力不等于 V6 通过；发现、类型判定和双目标投递必须同时成立。
5. V6 复验通过前不得提取或实现正式的多适配器主体架构。
