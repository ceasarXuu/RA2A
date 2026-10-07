# RA2A v0.0.19 目标与验收入口

- 更新日期：2026-10-08。
- 状态：开发中，未发布；当前正式版本仍为 v0.0.18。

## 版本目标

| 目标 | 产品依据 | 当前状态 |
|---|---|---|
| Pi Agent 原生接入与全交叉互通 | [Pi PRD](../pi-support/prd.md)、[工程方案与证据](../pi-support/plan.md) | 已实现并完成部分原生及 CLI/App 互通验证；其余发布门禁待完成 |
| 直接查询当前 session 的 RA2A 投递地址 | [身份查询 PRD：PD34](session-query-prd.md#confirmed-product-decisions) | Owner 已确认纳入；未实现 |
| 只读查询本机 RA2A PIN | [身份查询 PRD：PD35](session-query-prd.md#confirmed-product-decisions) | Owner 已确认纳入；未实现 |

查询功能属于本版本必须交付的目标。查询不能触发重新配对、修改 PIN、启动宿主或重启服务；当前身份无法确认时应明确返回未知，不能猜测地址。正式支持与发版继续遵守既有全交叉及人工交互准入要求。
