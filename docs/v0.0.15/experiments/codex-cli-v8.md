# Codex CLI V8 写入路径前置条件探测实验

- 状态：目标已重定向（V8-R），部分前置由 V10 提供证据，真实 TUI 投递仍待执行
- 计划日期：2026-09-06
- 重定向日期：2026-09-28（依据 [V10](./codex-cli-v10.md) 在 Codex CLI `0.158.0` 上的实测）
- 关联决策：PD31、PD32
- 背景：依据 Codex Desktop 开发沉淀（v0.0.10-v0.0.14）补充的实验。Desktop 经验证明宿主写入存在隐藏前置条件：`text_elements` 缺失会让 renderer 进入 error boundary，thread 空 model 会让 Desktop 先回 turn ID 再异步失败。V5 只比较了 schema 参数与必填字段，未排查这类只有真实投递才能发现的 renderer/运行时前置。

## 重定向说明

原 V8 把 `thread/queue/add` / `thread/queue/start` 当作投递入口。V10 在 `0.158.0` 上证明：

- `thread/queue/*` 全部为 experimental 能力（需 `capabilities.experimentalApi`），且 `thread/queue/start` 要求 thread 已 loaded 且 idle，不适合作为通用投递入口。
- 真正的投递入口是 stable 的 `thread/resume` + `turn/start` / `turn/steer`；`turn/steer` 带 `expectedTurnId` 前置。
- `turn/start` 的响应**不可作为投递确认**：实测先返回 turn ID（`status: inProgress`、`error: null`），失败在 `error` 通知（5 次 `Reconnecting... N/5`）与随后的 `turn/completed`（`status: failed`）里才出现。这与 Desktop 空 model 竞态是同一类失败，V8 的核心担忧在新入口上依然成立且更隐蔽。

因此 V8 的目标改为：在真实 TUI thread 上确认 `turn/start` / `turn/steer` 的完整前置条件集与失败形态，并把确认项固定进契约测试。

## 目标

回答以下问题，为 CLI 适配器固定写入前置条件集：

1. `thread/resume` + `turn/start` 在真实 TUI 拥有的 thread 上，是否依赖任何前置上下文（thread 已 loaded、`canAcceptDirectInput`、thread model、cwd/environment）？
2. 活跃回合下 `turn/steer` 的 `expectedTurnId` 前置在 TUI 侧的实际取值与失配表现是什么？
3. CLI TUI renderer（remote TUI 持有 connection 的场景）是否对缺失或空字段敏感（等效 Desktop `text_elements` 场景）？
4. 真实投递后 TUI 是否实时显示、人工是否可继续；从 `turn/start` 响应到 `turn/completed` 的延迟与失败形态分布如何？

## V10 已确认、可直接固定的前置项

以下前置已由 V10 在 `0.158.0` 实测确认，进入契约测试（不再重复验证）：

| 前置项 | 证据 |
| --- | --- |
| `threadId` 必须是 UUID（可带 `urn:uuid:` 前缀） | `thread/read` / `thread/resume` / `turn/*` 同步 -32600 `invalid thread id` / `invalid session id` |
| 未知 thread 的错误是同步的 | `thread/read` `thread not loaded`；`thread/resume` `no rollout found`；`turn/start`/`turn/steer` `thread not found`；`thread/queue/add` -32603 |
| `turn/start` / `turn/steer` 不受 `experimentalApi` 门禁 | `experimentalApi: false` 时 `turn/start` 仍进入 thread 解析 |
| 投递确认只能来自 `turn/completed` | 实测先返回 turn ID 再异步 `status: failed` |
| `canAcceptDirectInput` 可作为写入前门禁 | `thread/read` 在 experimentalApi 下返回 `true` |
| `textElements: []` 显式携带 | 协议层 `#[serde(default)]` 可省略，但沿用 Desktop IPC 修复后的契约显式发送 |
| 订阅会钉住 thread 内存，需显式退订 | 订阅者断开后广播 `notLoaded` + `thread/closed` 并卸载 |

## 隔离门禁

依据 PD32，使用工作区内独立 `CODEX_HOME` 与独立认证，不得复用或改写正式版认证、配置或 session 存储。若独立认证需要用户交互，先由用户明确完成或授权；在此之前只允许无模型的协议与单元测试。不得调用正式版 RA2A 的 `setup`、`restart`、`update`、`stop`、`start` 或 daemon 命令，不得使用正式版控制地址、节点身份或 App Server socket。

`CODEX_HOME` 决定 daemon socket 路径，因此隔离 `CODEX_HOME` 即等于隔离 daemon、socket 与 session 存储三件事，不再需要 `-c ephemeral=true` 这类已证明无效的覆盖（V6 记录其仍产生 `ephemeral=false` 的持久 thread）。具体隔离与探针脚本口径见 `runbooks/codex-cli-isolated-daemon-experiment.md`。

## 方法

1. 在独立 `CODEX_HOME` 下由用户完成独立登录；先按 `runbooks/codex-account-usage-check.md` 核对 plan 桶用量，再开始真实投递。
2. 用普通 `codex`（零 flag）启动 TUI，确认它自动挂接 daemon（socket inode 与 `ss -xap` 比对，方法见 V10 §8），并记录 `initialize.userAgent` 版本。
3. 记录 TUI 自建 thread 的 ID 与 `thread/loaded/list` 输出，作为后续注入目标。
4. RA2A 侧以 WebSocket over AF_UNIX 接入同一 socket，`thread/resume` 建立订阅，然后：
   - 空闲 thread → `turn/start`，核对 TUI 实时显示、响应延迟、`turn/completed` 终态；
   - 活跃 thread → `turn/steer` 并带 `expectedTurnId`，核对同 turn 精确执行一次；
   - 故意失配 `expectedTurnId`、故意对未 loaded thread 投递，记录同步错误文案。
5. 逐步移除/置空候选前置（`canAcceptDirectInput` 门禁、`textElements`、thread model 上下文），观察每次是同步拒绝还是 `turn/completed: failed`。
6. 记录 TUI 审批弹窗是否也发给 RA2A 连接，确认 RA2A 不代答时的用户可见行为。
7. 全程只记字段名、方法、thread ID、时戳与状态，不记消息正文。

## 通过条件

- 能枚举并确认 `turn/start` / `turn/steer` 投递的前置条件集合，且每一项都能在真实写入前探测/解析。
- 投递确认口径明确：只有 `turn/completed` 为成功终态才算 `delivered`；`turn/start` 响应本身不算。适配器在此窗口内不重试、不切路径。
- renderer 敏感字段写进单元测试（序列化契约钉死），等效 Desktop `text_elements` 的钉法。
- TUI 实时显示、无 error boundary、投递后人工继续输入正常。
- 结果写入本报告，只有结论进入契约测试与适配器实现。

## 失败后的处理

- 若发现不可满足的前置（如必须存在活跃 writer、必须持有 `expectedTurnId`），记录受影响投递入口并回到计划评审，不得静默降级到 `thread/resume` direct 追加。
- PD32 隔离未通过时立即停止，回传现场，不继续创建测试资源。

## 对实现的约束（结论确认前生效）

1. CLI 适配器写入前必须完成前置核验；未确认的前置字段不得假设。
2. `DELIVERY_UNKNOWN` 依旧不重试、不切换投递路径。
3. 任何缺失字段的现实形态必须成为契约测试用例，不允许只在文档里声明。
4. 投递确认以 `turn/completed` 为唯一依据（V10 已复现"先回 turn ID 再异步失败"）。