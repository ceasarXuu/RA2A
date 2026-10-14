# PRD：自动接入已支持的 Harness

- Status: Ready for implementation
- Created: 2026-09-30
- Updated: 2026-09-30
- Owner / requester: 项目 Owner
- Source request: 安装时自动识别本机已支持的 harness 并安装适配及 wrapper，用户不应了解内部安装开关。
- Product Authority: Confirmed Product Decisions

## Requester Review Summary

- 核心：以已安装的 harness 为接入依据，而非以 Codex 是否存在为准；同机可同时接入多种，OpenCode-only 节点也必须正常通信。
- 普通 `opencode` TUI 自动接入 RA2A；Codex CLI wrapper 随检测自动安装且可逆。
- 已有全交叉互通决策见 `docs/v0.0.15/prd.md` 的 PD26、PD31；此 PRD 补足安装与启动体验，不改变投递协议。
- Status reason: 用户已明确确认上述物质产品决策，验收标准可直接观察。

## 1. Background And Product Intent

现有安装器将 OpenCode/Codex CLI wrapper 设为用户必须知道的可选参数，且 RA2A 初始化强制要求 Codex 路径。结果是已安装 OpenCode 的用户可能装好了 RA2A 却无法用普通 `opencode` 加入网络，OpenCode-only 主机甚至无法初始化。这违反全交叉支持目标。

## 2. Goals And Success Criteria

安装后，机器上每种已支持、已安装的 harness 均可参与发现和互通；用户不需了解 `--opencode-wrapper`、`--codex-wrapper` 等内部开关。同类 OpenCode↔OpenCode 跨节点与异类之间采用同一支持标准。

## 3. Users And Usage Context

从源码安装或正式 Release 资产安装的 macOS、Linux、Windows 用户；重复升级、混合安装、仅有单一 harness 的设备均属于主流程。

## 4. Scope

### In Scope

- 检测已适配的 Codex App、Codex CLI、OpenCode 的本机可用性；自动安装对应集成及需要的透明 launcher。
- OpenCode 普通交互式 TUI 启动即接入共享 RA2A server；非 TUI 子命令维持原有 CLI 行为。
- 仅安装 OpenCode、未安装 Codex 时，仍可完成 RA2A 配置、启动、发现和收发。
- 重复安装/升级与卸载可逆，不覆盖或遗失用户的原生可执行文件。
- 源码安装与 Release 下载入口在所有支持的 OS 上体验一致。

### Out Of Scope

- 未实现适配器的其他 CLI 自动接入；跨公网通信；自动安装第三方 harness 本体。

## 5. Core User Journey

用户执行一个常规安装命令，安装器发现可用的 harness、配置对应的 RA2A 集成并给出实际接入结果。随后用户直接启动已安装的交互式 harness，打开会话即成为可发现且可投递的 RA2A 端点。升级无需重新指定 wrapper 参数；卸载恢复原生 CLI。

## 6. Interaction And Information Design

安装结果按 harness 展示“检测到/未检测到”和“集成已就绪/失败及原因”；缺少某个 harness 不阻塞其他已检测到的 harness。用户无需学习额外的 OpenCode 启动标志，但显式 `--ra2a` 仍可用于兼容已有使用习惯。

## 7. Product Rules And State Logic

- 单个 RA2A daemon 根据当前机器可用的已适配 harness 启动对应适配器，不得因为 Codex 不存在而排斥 OpenCode。
- OpenCode 原生可执行文件与 wrapper 路径分离，普通 TUI 自动附着；状态、消息和权限处理仍按已有适配契约。
- Codex CLI wrapper 默认透明接入；不会接管非交互命令，也不能遮蔽原生命令。重复升级和卸载保持可恢复。
- 安装器的检测结果只是安装时事实；后续增删第三方 harness 时再次执行常规安装/升级即可同步集成。

## 8. Edge Cases, Errors, And Recovery

- 只存在一种已支持 harness：只安装该种配套，其余明确跳过；可正常运行 RA2A。
- 没有已支持 harness：命令本体可以安装，但 setup 明确提示缺少可接入的 harness，不伪称通信就绪。
- 已有第三方同名可执行文件：保全原始路径和行为；无法安全安装 launcher 时明确报错，不静默覆盖。
- RA2A 暂时不可达：launcher 给出可操作的状态，不出现递归调用或把旧会话错误地当作可执行目标。

## 9. Content And Terminology

“检测到”指当前安装用户能运行对应的已支持 harness；“接入就绪”指对应集成和启动方式确实可用，而不只是构建了 wrapper。

## 10. Acceptance Criteria

1. Given 只有 OpenCode 的机器，when 常规安装和初始化，then daemon 运行且其会话与另一台 OpenCode 的会话互相发现、双向投递。
2. Given OpenCode 已安装，when 用户直接执行 `opencode`，then 当前 TUI 接入共享 server 且会话可发布；`opencode --version` 等非 TUI 命令仍委托原生程序。
3. Given Codex CLI 已安装，when 常规安装、重复安装与卸载，then wrapper 自动存在且原生 CLI 全程可用，卸载后原生命令恢复。
4. Given 同机同时装有 Codex 与 OpenCode，when 常规安装，then 两类集成同时就绪；缺少其中之一不使另一种失败。
5. Given Release 安装，when 本机有可用 harness，then 行为与源码安装相同，无须额外 wrapper 参数。

## 11. Review Checklist And Sign-off Questions

上述自动接入、OpenCode-only、Codex wrapper 自动可逆均由用户直接确认；具体二进制路径、服务管理与备份方式属于工程实现。

## Confirmed Product Decisions

> PROTECTED USER-AUTHORITY SECTION
> Rows in this section MUST NOT be created, modified, deleted, reinterpreted,
> or superseded without explicit user approval for that specific decision change.
> Agent self-approval is forbidden.

| ID | Confirmed Decision | Must Do | Must Not Do | Rationale | Violation Signal | Confirmation | Status |
|---|---|---|---|---|---|---|---|
| PD1 | 安装时自动检测已支持的 harness 并安装配套与 wrapper。 | 常规安装自动接入检测到的 harness。 | 要求用户知道并传入内部 wrapper 开关。 | 降低安装门槛。 | 安装成功但仍需再运行 `--opencode-wrapper`。 | user-confirmed-direct: “检测到机器上有哪个已支持的 harness就应该安装配套和wrapper等，不要做成可选的，用户又不知道怎么安装” | active |
| PD2 | 普通交互式 OpenCode TUI 自动接入 RA2A。 | 用户运行 `opencode` 即可成为接入会话。 | 仅安装 wrapper 但仍要求 `--ra2a`。 | 默认路径真正可用。 | 普通 TUI 会话未发布。 | user-confirmed-direct: 对“普通启动自动接入”选择“普通启动自动接入 (Recommended)” | active |
| PD3 | 已支持 harness 的准入不依赖 Codex；OpenCode-only 必须能与 OpenCode 及其他已支持 harness 通信。 | 仅有 OpenCode 也能初始化并形成双向通信。 | 无 Codex 就拒绝 setup 或不启动 daemon。 | 全交叉互通是产品基础。 | OpenCode-only setup 报缺失 Codex。 | user-confirmed-direct: “本产品是适配的所有harness都应该能互相通信的，除了opencode和codex,当然也要支持opencode 2 opencode , 所以安装怎么能限制呢？” | active |
| PD4 | 发现 Codex CLI 时自动安装可恢复的透明 wrapper。 | 升级、卸载恢复原生可执行文件。 | 把 wrapper 留作只有用户显式开启的选项。 | 无需用户理解 Codex CLI 的例外接入步骤。 | `codex` 安装后仍需指定 `--codex-wrapper`。 | user-confirmed-direct: 对“自动安装并可恢复”选择“自动安装并可恢复 (Recommended)” | active |

## 12. Open Questions And Risks

- Windows npm/shim 等不同安装布局下的原生命令发现、备份及 PATH 优先级，需通过隔离环境验证，不改变上述用户可见规则。
- Release 资产目前只包含 `ra2a` 本体；launcher 打包与校验需要纳入发布流水线。

## 13. Implementation Notes

用户已有原生 harness 配置与会话不应因自动安装而丢失；安装与升级应可重复、可诊断。
