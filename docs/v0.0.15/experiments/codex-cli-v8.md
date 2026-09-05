# Codex CLI V8 写入路径前置条件探测实验

- 状态：待执行（Phase 0）
- 计划日期：2026-09-06
- 关联决策：PD31、PD32
- 背景：依据 Codex Desktop 开发沉淀（v0.0.10-v0.0.14）补充的实验。Desktop 经验证明宿主写入存在隐藏前置条件：`text_elements` 缺失会让 renderer 进入 error boundary，thread 空 model 会让 Desktop 先回 turn ID 再异步失败。V5 只比较了 schema 参数与必填字段，未排查这类只有真实投递才能发现的 renderer/运行时前置。

## 目标

回答以下问题，为 CLI 适配器固定写入前置条件集：

1. `thread/queue/add` 与 `thread/queue/start` 的真实投递是否依赖任何前置字段（thread model、sessionId、clientUserMessageId 之外的上下文）？
2. CLI TUI renderer（remote TUI 持有 connection 的场景）是否对缺失或空字段敏感（等效 Desktop `text_elements` 场景）？
3. `thread/resume` 的 `sessionId` 语义：外部调用者必须提供哪些字段才能精确追加到已知 CLI thread？
4. queue 投递完成后 TUI 是否实时显示、人工是否可继续；队列消息的失败形态（accept 后异步失败）是否存在。

## 隔离门禁

依据 PD32，使用工作区内独立 `CODEX_HOME` 与独立认证，不得复用或改写正式版认证、配置或 session 存储。若独立认证需要用户交互，先由用户明确完成或授权；在此之前只允许无模型的协议与单元测试。不得调用正式版 RA2A 的 `setup`、`restart`、`update`、`stop`、`start` 或 daemon 命令，不得使用正式版控制地址、节点身份或 App Server socket。

实验 App Server 与工具 socket 只监听工作区 `docs/v0.0.15/.cache/` 下的独立地址（沿用 V4-V7 模式）。启动前检查资源归属，结束只清理本次实验创建的资源。

## 方法

1. 在独立 `CODEX_HOME` 下启动工作区 App Server，确定客户端 `codex-tui`、`ra2a-cli` 的实际版本（`initialize.userAgent`），记录版本与平台。
2. 用 `codex --remote <实验-socket>` 创建 remote TUI thread；TUI 现存于整个测试期间。
3. 按字段逐步构造 `thread/queue/add`：从完整契约到逐项移除/置空候选前置（model、sessionId 等），观察每次请求的结果与 TUI 行为。
4. 验证 `thread/queue/start` 的依赖：是否必须先有 thread 的 model、历史或 settings；缺什么时是同步拒绝还是 accept 后异步失败。
5. 外部调用 `thread/resume` 追加回合，核对返回字段与 TUI 实时表现、人工继续输入。
6. 全程用日志记录每步的 request 形状（只记字段名、方法、thread ID、时戳，不记正文）与 TUI 状态变化。

## 通过条件

- 能枚举并确认 queue/resume 投递的前置字段集合，且每一项都能在真实写入前探测/解析；前置不足时明确拒绝，不出现「先 accept 再异步失败」的不可判定结果。
- 确认 renderer 敏感字段并写进单元测试（序列化契约钉死），等效 Desktop `text_elements` 的钉法。
- TUI 实时显示、无 error boundary、投递后人工继续输入正常。
- 结果写入本报告，只有结论进入契约测试与适配器实现。

## 失败后的处理

- 若发现不可满足的前置（如必须存在活跃 writer、必须持有 sessionId），记录受影响投递入口并回到计划评审，调整 Phase 3 queue 路线，不得静默降级到 direct resume。
- PD32 隔离未通过时立即停止，回传现场，不继续创建测试资源。

## 对实现的约束（结论确认前生效）

1. CLI 适配器写入前必须完成前置核验；未确认的前置字段不得假设。
2. `DELIVERY_UNKNOWN` 依旧不重试、不切换投递路径。
3. 任何缺失字段的现实形态必须成为契约测试用例，不允许只在文档里声明。