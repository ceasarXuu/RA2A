# PRD：下一版本 Pi Agent 支持

- 状态：Draft；支持范围待 Owner 确认。
- 版本：下一版本，版本号暂未确认；当前已发布 v0.0.18。
- 请求：Owner「开始下一个版本，做对 pi agent 的支持」。
- 既有产品约束：[统一互通 PRD](../v0.0.15/prd.md) 的 PD26–PD33 继续有效；本文不替代这些决策。
- 工程方案：[plan.md](./plan.md)（Draft，收件路线待确认）。

## 目标与用户

让使用原生 Pi Coding Agent 的用户通过 RA2A 发现其他设备上的 Agent 会话，并双向发送文本。接入仍遵循统一端点与消息模型。

## 建议范围（待确认）

- 原生 Pi TUI 中提供与其他 Agent 一致的发现与发送入口。
- 仅发布当前被运行中 Pi 实例持有的会话；历史记录不代表执行权。
- 关闭、重启并 resume 同一原生会话后，公开地址保持稳定；TUI 内切换会话后跟随当前会话。
- 收件确认与模型回复、执行完成分离；工作中的消息能被宿主接收。是否可声明 `steerActiveTurn`，需由原生证据决定。
- 保留现有 Pi 和 Codex/OpenCode 的配置、认证、模型、代理与人工交互。
- 正式支持准入按既有 PD31 完成与已支持 Agent 的全交叉验证；未完成时明确标为预览。

不包含自动安装模型服务、变更用户权限策略、文件传输、工作流编排或公网中继。

## 用户流程与失败行为

1. 用户正常启动已接入 RA2A 的 Pi 原生交互会话。
2. 其他 Agent 通过 `list_targets` 获取其完整地址和经验证的能力。
3. 向该地址发送消息，目标宿主明确接收后返回收件成功。
4. Pi 后续处理并可独立回信；用户仍可继续输入。
5. 会话关闭或所有权失效后撤回可投递端点；切换会话不能误投到启动时的旧会话。
6. 未取得明确收件确认时返回未知结果，不自动重发。

## 验收条件

- 发送方身份精确绑定 Pi 当前原生会话，接收消息在对应 TUI 可见。
- 空闲多轮、工作中收件、独立业务回信分别核验，不能以 accepted 冒充执行完成。
- resume 地址稳定，切换、多个实例和异常退出不串会话、不保留错误执行权。
- 正式进程、配置、凭据及代理保护快照前后符合预期；测试使用隔离配置和假凭据。
- Linux/macOS/Windows 的支持声明分别基于真实原生验证，跳过项单独记录。
- 人工操作留到自动验证之后，提供操作与观察清单。

## Confirmed Product Decisions

只记录直接确认的新决策；继承的 PD26–PD33 以原文为准。新增、修改或取代有效决策必须取得 Owner 针对该变更的明确确认；Agent 推断、实现、测试和未反对均不是批准。

| ID | Confirmed Decision | Must Do | Must Not Do | Rationale | Violation Signal | Confirmation | Status |
|---|---|---|---|---|---|---|---|
| PI1 | 下一版本开展 Pi Agent 支持 | 核验 Pi 原生接口并推进接入 | 将调研完成当作支持完成 | 扩展 RA2A Agent 集合 | 未开展 Pi 接入工作 | Owner：开始下一个版本，做对 pi agent 的支持 | active |

## Open Questions And Risks

- 全交叉准入沿用既有 PD26 / PD31，包括 Pi ↔ Codex App、Codex CLI、OpenCode 及 Pi ↔ Pi；此前范围提问未获答复，不将其升级为新确认，也不缩减既有准入门槛。
- 版本号和用于跨设备验收的 Pi 会话尚未指定。
- 本机安装包已按 Owner 要求从 `@earendil-works/pi-coding-agent` 0.87.1 升级到 1.0.0；后续调研以 1.0.0 为基线，最低兼容版本待技术验证。
- 扩展 `sendUserMessage()` 返回 void，并在宿主中异步捕获失败；调用返回本身不足以证明收件。需要先实验确认可靠收件边界，不能重复上一版本的确认耦合问题。

## 技术证据入口

以下仅是调研事实，不是产品决策或完成声明。

- 本机安装包的 `package.json`、`docs/extensions.md`、`docs/configuration.md` 与 `dist/core/extensions/types.d.ts` 已只读检查。
- `session_start` 原生事件区分 startup/reload/new/resume/fork；`ctx.sessionManager.getSessionId()` 提供原生身份。仍需真实切换实验确认绑定时序。
- Pi 原生扩展支持注册模型工具和注入用户消息，可优先验证扩展接入，不另建替代 TUI。
- 官方入口：[Pi 仓库](https://github.com/earendil-works/pi)、[扩展文档](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/extensions.md)。

### 首次隔离收件实验

2026-10-04，使用本机 0.87.1 的真实 SDK 和内联扩展；新临时 HOME、Pi agent 目录、无真实凭据、禁止 fetch、空资源加载器、无工具和生产配置发现。

- `pi.sendUserMessage()` 立即返回 undefined。
- `input` 事件已经触发，但后续认证校验失败。
- 宿主报告 `send_user_message` 错误，原生 user message 数为 0。
- 因此，调用返回和 `input` 事件都不能独立作为收件成功的证明；正式接入前必须验证更晚且不依赖模型完成的确认边界。
- Codex、RA2A 及 Pi 顶层 JSON 配置文件的内容哈希与 mtime 前后相同。
- 原始脚本、结果、错误和保护快照在本机忽略目录 `.cache/pi-discovery/`；这只是 SDK 调研，不是 TUI、多平台或正式接入验收。

### Pi 1.0.0 基线升级

Owner 要求先升级 Pi，避免基于旧接口继续适配。已沿用本机既有 npm prefix，执行固定版本、禁用安装脚本的全局升级；`pi --version` 和安装包元数据均为 1.0.0。旧安装包与 Pi 配置已在私有忽略目录 `.cache/pi-upgrade-1.0.0/` 备份。217 个已记录的 Pi、Codex、OpenCode 和 RA2A 配置/资源文件内容哈希及 mtime 前后相同；没有执行登录、交互任务或正式服务重启。

官方 1.0.0 默认全屏 TUI，见 [发行说明](https://github.com/earendil-works/pi/releases/tag/v1.0.0)。上述 0.87.1 收件实验仅保留为历史证据。

### 1.0.0 收件接口重验与方案

在同样隔离 SDK 条件下重新执行无凭据反例，1.0.0 仍出现调用返回 undefined、input 已触发、后续 send_user_message 认证错误、user message 数为 0；保护文件哈希与 mtime 未变化。原始结果在 `.cache/pi-discovery/`。独立只读源码核对进一步确认：RPC 暴露的 preflight queued/started/handled 回调未通过普通扩展 sendUserMessage 暴露，user message_start 要等排队消息消费，不能代表即时入队。

因此工程方案先验证原生扩展内明确收件的路线；“扩展收到即成功、模型输入另行处理”属于新的具体收件语义，待 Owner 确认，不自行等同 Pi 原生入队。忙时 followUp、初期 ra2a pi 入口、后续自动安装与版本号也在方案中明确列为待确认或后置事项。
