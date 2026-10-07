# RA2A v0.0.19 目标与验收入口

- 更新日期：2026-10-08。
- 状态：开发中，未发布；当前正式版本仍为 v0.0.18。

## 版本目标

| 目标 | 产品依据 | 当前状态 |
|---|---|---|
| Pi Agent 原生接入与全交叉互通 | [Pi PRD](../pi-support/prd.md)、[工程方案与证据](../pi-support/plan.md) | 已实现并完成部分原生及 CLI/App 互通验证；其余发布门禁待完成 |
| 直接查询当前 session 的 RA2A 投递地址 | [身份查询 PRD：PD34](session-query-prd.md#confirmed-product-decisions) | Owner 已确认纳入；未实现 |
| 修复同网段双网卡 DTLS 回包源地址偏移 | [Issue #1](https://github.com/ceasarXuu/RA2A/issues/1)、[排查与验收](issue-triage.md#issue-1双网卡-dtls) | 纳入修复目标；故障机制有实测及依赖源码证据，未实现 |
| 修复已打开 Codex 会话未被发布及缺失目标错误分类 | [Issue #2](https://github.com/ceasarXuu/RA2A/issues/2)、[排查与验收](issue-triage.md#issue-2会话未发布) | 纳入修复目标；枚举缺失根因待 Ubuntu 现场核对，错误分类路径已定位 |
| 只读查询本机 RA2A PIN | [身份查询 PRD：PD35](session-query-prd.md#confirmed-product-decisions) | Owner 已确认纳入；未实现 |

查询功能属于本版本必须交付的目标。查询不能触发重新配对、修改 PIN、启动宿主或重启服务；当前身份无法确认时应明确返回未知，不能猜测地址。正式支持与发版继续遵守既有全交叉及人工交互准入要求。

## 修复验收入口

两个 issue 均归入 GitHub [v0.0.19 milestone](https://github.com/ceasarXuu/RA2A/milestone/1)。排查证据、未决项和修复验收见 [issue 排查记录](issue-triage.md)。登记修复目标不代表已修复；生产实现需在证据确认后进入批准的修复阶段。
