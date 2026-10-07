# Problem P-001

- 状态：目标缺失根因待证据，次级错误分类问题已确认；未修复，纳入 v0.0.19。
- 来源：https://github.com/ceasarXuu/RA2A/issues/2。
- 症状：目标会话一直打开，但 319 条发布列表不含目标，投递报 DELIVERY_UNKNOWN: endpoint not found。
- 已知事实：节点 ready、sessionsStale=false，远端业务错误证明 DTLS/CoAP 可往返。原目标 ID 已匿名化，Ubuntu 实际版本、宿主、用户及有效 CODEX_HOME 未采集。
- 基准：v0.0.18 与 e891277；相关枚举及分类路径相同。
- 修复标准：原目标发布、授权投递收到并回复；无替代会话/竞争 writer；缺失目标与投递未知准确分类。

## Hypothesis H-001

- 状态：pending。
- 主张：原始 thread/list 因宿主/数据根目录、state DB 或过滤条件缺少原目标。
- 预测：原始全分页列表不含目标；只读单变量对照能定位条件。
- 诊断证据计划：确认真实 ID、宿主、用户、有效 CODEX_HOME，对比 thread/list 与只读 thread/read，逐项对照 archived/sourceKinds/useStateDbOnly。列表含目标将反驳该分支；不执行 resume/start。
- 缺失：匿名 issue 不足以定位原目标，待 Ubuntu 现场上下文。

## Hypothesis H-002

- 状态：pending。
- 主张：原始列表含目标，但 CLI 所有权排除、发布状态、endpoint 校验或枚举错误使 registry 缺失。
- 预测：原始列表含目标，registry 不含目标，存在具体排除/校验证据。
- 诊断证据计划：同时间对照原始列表、CLI 注册/发布和 endpoint problems。原始列表不含目标将降级此分支。
- 缺失：真实目标和 Ubuntu registry 证据。

## Hypothesis H-003

- 状态：confirmed；仅解释错误分类，不解释主故障会话缺失。
- 主张：registry 的 not_found 被 deliverOverLAN 包装为 ErrDeliveryUnknown，即使没有任何宿主调用。
- 预测：空 registry 直接 Deliver 返回 not_found，经 LAN 桥接返回 DELIVERY_UNKNOWN。
- 诊断证据计划：本地空 registry 测试，无 adapter、真实宿主或网络；若 LAN 保留 not_found 则反驳。
- 证据：E-002、E-003。

## Evidence E-001

- 对应：H-001/H-002 路径定位；类型：代码事实，不能确认现场根因。
- 结果：appserverprobe 使用 archived=false、useStateDbOnly=true、sourceKinds 和分页；codexapp 排除明确接受 CLI 注册的 ID；managedCommand 未覆盖 Env，继承 daemon 环境；registry.Lookup 忽略 Endpoints problems。
- 含义：仍有多个需要 Ubuntu 单变量证据区分的分支，不推断用户关闭会话或提供错误 ID。

## Evidence E-002

- 对应：H-003 源码预测；类型：代码事实。
- 结果：registry.explainMiss 返回 ResultNotFound；cmd/ra2a/registry.go 的 deliverOverLAN 仅区分 delivered/start_required，剩余结果统一 ErrDeliveryUnknown。v0.0.18 和 e891277 均存在。

## Evidence E-003

- 对应：H-003 受控测试预测；类型：本地诊断实验，2026-10-08。
- 命令：go test ./cmd/ra2a -run TestIssue2DiagnosticMissingEndpointClassification -v。
- 结果：PASS；registry=not_found; LAN=DELIVERY_UNKNOWN: endpoint missing-session not found; no adapters registered and no host delivery possible。
- 含义：支持错误分类机制，不证明 H-001/H-002。临时诊断测试不保留为以错误行为为标准的永久回归测试。后续现场步骤见 docs/v0.0.19/issue-triage.md。
