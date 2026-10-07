# PRD：Codex session 实际持有者与动态迁移

- 状态：Ready for implementation（产品规则已确认；跨平台取证及投递时序仍需工程验证）。
- 创建 / 更新：2026-10-08。
- 发布目标：v0.0.19。
- Product Authority：本文件 Confirmed Product Decisions。
- 背景：Issue #2 原会话从 CLI 转到 App 后，永久 cliSessions 登记导致两条发布路径同时排除；手工 release-cli 只能单次恢复。

## 目标与规则

同一个原生 session ID 的 RA2A 地址保持不变。发布与投递归属由官方 writer 互斥锁的当前真实持有者决定，CLI/App 没有固定优先级。

| 实际状态 | RA2A 行为 |
|---|---|
| 官方 CLI 接入宿主持锁 | 登记 / 发布为 codex-cli，使用该持锁宿主投递 |
| 官方 App 宿主持锁 | 登记 / 发布为 codex-app，使用该持锁宿主投递 |
| 确认无人持锁 | 保持睡眠、不可达；投递不唤醒、不 resume 获锁 |
| 持锁身份 / 读取结果无法确认 | 明确表示无法确定；不得猜测归属或冒充睡眠 |
| 持有者正在迁移 | 旧归属撤回；新持有者证实后切换；迁移间隙遵守睡眠 / 未确定规则 |

历史记录、静态登记、锁文件存在、daemon loaded、source/originator 均不能单独证明实际持有。RA2A 不获取官方 writer 锁，也不解除或抢占它；独立 writer 不在支持路径时不可向另一个宿主投递。

## 验收

- 同一原会话 CLI → App → CLI 后地址 ID 不变，不需 adopt/release 或重启 RA2A 才迁移。
- 两者都未持有时，即使历史/旧配置/残锁文件还存在，也保持睡眠不可达，不产生新的 writer 或模型任务。
- 发现之后、投递之前持有者变化，必须复核；原路径没有写入则可以更新归属，未知是否写入时不能向新持有者重发。
- 正常退出、异常退出、同 PID 再利用、旧 socket 重连、文件 inode 被替换均不沿用过期身份。
- 不改原生 CLI/App/登录/模型/代理设置；仅在验证通过后部署 RA2A。
- 动态刷新有明确实测延迟和不误投证据，不能把“下一次启动才更新”算作及时切换。

## Confirmed Product Decisions

> PROTECTED USER-AUTHORITY SECTION
> 以下确认项不得由 Agent 自行改写、取代或删除。

| ID | Confirmed Decision | Must Do | Must Not Do | Rationale | Violation Signal | Confirmation | Status |
|---|---|---|---|---|---|---|---|
| PD36 | Codex CLI/App 归属跟随 session 实际互斥锁持有者；无人持有则睡眠不可达 | 核实实际持锁宿主并映射原地址 | 固定 CLI/App 优先级、永久登记当所有权、投递隐式唤醒 | 按原生实际所有权兼容，避免竞争 | 旧登记覆盖新持有者，或无持有者时创建 writer | user-confirmed-direct：Owner「谁持有则登记给谁，都未持有就保持睡眠状态」（2026-10-08） | active |
| PD37 | session 持有者切换时 RA2A 也要及时切换归属 | 动态刷新，并处理发现与发送之间的迁移 | 只在 daemon 启动时计算归属或使用过期持有者写入 | 支持正常关闭、resume 和宿主切换 | 迁移后仍要求手工 release/adopt 或发往旧宿主 | user-confirmed-direct：Owner「要注意动态时机，持有者切换的时候也要能及时切换过去」（2026-10-08） | active |

## 工程边界

沿用既有不透明地址、收件与业务完成分离、未知投递不重放、正式配置及宿主保护约束。具体轮询周期、系统接口、结构化错误字段属于工程设计，必须验证后报告，不把暂定数值冒充用户承诺。
